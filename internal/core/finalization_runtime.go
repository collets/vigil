package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vigil/internal/policy"
	"vigil/internal/store"
)

type FinalizationInput struct {
	ManifestRevision int            `json:"manifest_revision"`
	ManifestDigest   string         `json:"manifest_digest"`
	Manifest         FactualArchive `json:"untrusted_factual_manifest"`
	OutputContract   string         `json:"output_contract"`
}

// The provider receives facts, never Engine, filesystem, DB or publication
// authority. A native implementation must prove terminal idle independently.
type FinalizationProvider interface {
	Identity() PlanningProviderIdentity
	GenerateFinalization(context.Context, FinalizationInput) ([]byte, error)
	IdleObserved() bool
}

type FinalizationRunRequest struct {
	CommandID        string        `json:"command_id"`
	PlanID           string        `json:"plan_id"`
	ManifestRevision int           `json:"manifest_revision"`
	ManifestDigest   string        `json:"manifest_digest"`
	ProfileID        string        `json:"profile_id"`
	ProfileRevision  int           `json:"profile_revision"`
	ActiveLimit      time.Duration `json:"-"`
}

type finalizationModelOutput struct {
	Text     string   `json:"text"`
	CitedIDs []string `json:"cited_ids"`
}

func (e *Engine) RunFinalization(ctx context.Context, request FinalizationRunRequest, provider FinalizationProvider) (ArchiveRecord, error) {
	var empty ArchiveRecord
	if e == nil || e.DB == nil || provider == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.PlanID) ||
		request.ManifestRevision < 1 || !validDigest(request.ManifestDigest) || request.ProfileID == "" || request.ProfileRevision < 1 ||
		request.ActiveLimit <= 0 || request.ActiveLimit > MaxPlanningAttempt {
		return empty, errors.New("bounded exact finalization run request required")
	}
	args, _ := json.Marshal(map[string]any{"request": request, "active_limit_ms": request.ActiveLimit.Milliseconds()})
	command := store.Command{ID: request.CommandID, Actor: "core", Kind: "finalization.run", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err != nil {
			return empty, err
		}
		var prior struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(receipt, &prior); err != nil {
			return empty, err
		}
		var state string
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM runs WHERE id=? AND role='finalization'", prior.RunID).Scan(&state); err != nil {
			return empty, err
		}
		if state == "completed" {
			record, _, err := e.Archive(ctx, request.PlanID, request.ManifestRevision)
			return record, err
		}
		return empty, fmt.Errorf("finalization command has durable %s attempt; provider will not be replayed", state)
	}
	record, manifest, err := e.Archive(ctx, request.PlanID, request.ManifestRevision)
	if err != nil || record.State != "narrative_pending" || record.ManifestDigest != request.ManifestDigest {
		return empty, errors.New("current pending factual archive and exact digest required")
	}
	var latest int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", request.PlanID).Scan(&latest); err != nil || latest != request.ManifestRevision {
		return empty, errors.New("a newer factual archive superseded the selected revision")
	}
	var profileRaw, configRaw, configID string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT c.resolved_json FROM profiles p JOIN config_snapshots c ON c.id=p.config_id
		WHERE p.id=? AND p.revision=? AND p.revision=(SELECT max(revision) FROM profiles WHERE id=?)`, request.ProfileID, request.ProfileRevision, request.ProfileID).Scan(&profileRaw); err != nil {
		return empty, errors.New("explicit latest finalization profile required")
	}
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT s.id,s.resolved_json FROM project_configurations c JOIN config_snapshots s ON s.id=c.config_id ORDER BY c.revision DESC LIMIT 1`).Scan(&configID, &configRaw); err != nil {
		return empty, err
	}
	var profile policy.Profile
	var config policy.Config
	if err := json.Unmarshal([]byte(profileRaw), &profile); err != nil {
		return empty, err
	}
	if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
		return empty, err
	}
	identity := provider.Identity()
	if err := profile.Validate(); err != nil || profile.ID != request.ProfileID ||
		identity.Harness != profile.Harness || identity.Model != profile.Model || identity.Provider != profile.Provider ||
		len(policy.Eligibility(config, profile, "finalization")) != 0 {
		return empty, errors.New("finalization provider does not match selected eligible profile")
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil || len(manifestBytes) > 256<<10 {
		return empty, errors.New("factual archive exceeds bounded finalization input")
	}
	runID := store.Digest([]byte("finalization-run\x00" + request.CommandID))
	ledgerID := store.Digest([]byte("plan-services\x00" + request.PlanID))
	_, err = e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var state string
		var planRevision, serviceLimit int
		if err := tx.QueryRowContext(ctx, "SELECT state,revision,service_limit_ms FROM plans WHERE id=?", request.PlanID).Scan(&state, &planRevision, &serviceLimit); err != nil || state != "finalization_pending" || planRevision != manifest.PlanRevision {
			return nil, errors.New("plan changed before finalization effect")
		}
		var archiveState, archiveID string
		if err := tx.QueryRowContext(ctx, "SELECT state,factual_manifest FROM archives WHERE plan_id=? AND revision=?", request.PlanID, request.ManifestRevision).Scan(&archiveState, &archiveID); err != nil || archiveState != "narrative_pending" || archiveID != record.ManifestID {
			return nil, errors.New("archive changed before finalization effect")
		}
		var acceptanceID string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM quality_acceptances_v2 WHERE plan_id=? AND target_kind='plan' AND invalidated_at IS NULL", request.PlanID).Scan(&acceptanceID); err != nil || acceptanceID != manifest.AcceptanceID {
			return nil, errors.New("plan acceptance changed before finalization effect")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_ledgers(id,scope,plan_id,active_limit_ms,updated_at)
			VALUES(?,'plan_services',?,?,?) ON CONFLICT(id) DO NOTHING`, ledgerID, request.PlanID, serviceLimit, store.Now()); err != nil {
			return nil, err
		}
		var charged, unknown, limit int64
		if err := tx.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE id=? AND scope='plan_services' AND plan_id=?", ledgerID, request.PlanID).Scan(&charged, &unknown, &limit); err != nil {
			return nil, err
		}
		if charged+unknown+request.ActiveLimit.Milliseconds() > limit {
			return nil, errors.New("plan-services budget cannot reserve finalization attempt")
		}
		var currentConfigID string
		if err := tx.QueryRowContext(ctx, "SELECT config_id FROM project_configurations ORDER BY revision DESC LIMIT 1").Scan(&currentConfigID); err != nil || currentConfigID != configID {
			return nil, errors.New("configuration changed before finalization effect")
		}
		var currentProfileRevision int
		if err := tx.QueryRowContext(ctx, "SELECT max(revision) FROM profiles WHERE id=?", request.ProfileID).Scan(&currentProfileRevision); err != nil || currentProfileRevision != request.ProfileRevision {
			return nil, errors.New("profile changed before finalization effect")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at,started_at)
			VALUES(?,?,?,?,?,?,?,?,'finalization','initial','active','unconfirmed',?,?,?,?)`, runID, request.PlanID, manifest.PlanRevision, record.TaskID, 1, configID, request.ProfileID, request.ProfileRevision,
			request.ActiveLimit.Milliseconds(), request.ActiveLimit.Milliseconds(), store.Now(), store.Now())
		return map[string]string{"run_id": runID}, err
	})
	if err != nil {
		return empty, err
	}
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, request.ActiveLimit)
	raw, providerErr := provider.GenerateFinalization(runCtx, FinalizationInput{ManifestRevision: request.ManifestRevision,
		ManifestDigest: request.ManifestDigest, Manifest: manifest,
		OutputContract: `closed JSON only: {"text":string,"cited_ids":[manifest ID strings]}; cite the plan acceptance and every task acceptance; never invent facts, publish, modify files or request tools`})
	cancel()
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 1 {
		elapsed = 1
	}
	if elapsed > request.ActiveLimit.Milliseconds() {
		elapsed = request.ActiveLimit.Milliseconds()
	}
	idle := provider.IdleObserved()
	finish := func(outcome string, charge int64, unknown int64) error {
		writer := "observed_stopped"
		if outcome == "unknown" {
			writer = "unconfirmed"
		}
		return e.DB.Write(context.Background(), func(tx *store.Tx) error {
			updated, err := tx.ExecContext(context.Background(), "UPDATE runs SET state=?,writer_state=?,ended_at=? WHERE id=? AND state='active'", outcome, writer, store.Now(), runID)
			if err != nil {
				return err
			}
			if count, err := updated.RowsAffected(); err != nil || count != 1 {
				return errors.New("finalization run changed before outcome")
			}
			_, err = tx.ExecContext(context.Background(), "UPDATE budget_ledgers SET charged_ms=charged_ms+?,unknown_ms=unknown_ms+?,revision=revision+1,updated_at=? WHERE id=?", charge, unknown, store.Now(), ledgerID)
			return err
		})
	}
	if !idle {
		_ = finish("unknown", 0, request.ActiveLimit.Milliseconds())
		return empty, errors.New("finalization provider did not prove terminal idle; run remains unknown")
	}
	if providerErr != nil {
		_ = finish("failed", elapsed, 0)
		return empty, providerErr
	}
	if len(raw) == 0 || len(raw) > store.MaxDocument {
		_ = finish("failed", elapsed, 0)
		return empty, errors.New("finalization output exceeds closed 64 KiB result")
	}
	var output finalizationModelOutput
	if err := store.Decode(raw, &output); err != nil {
		_ = finish("failed", elapsed, 0)
		return empty, err
	}
	actor := "core"
	var acceptanceActor string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT actor FROM quality_acceptances_v2 WHERE id=?", manifest.AcceptanceID).Scan(&acceptanceActor); err != nil {
		_ = finish("failed", elapsed, 0)
		return empty, err
	}
	if acceptanceActor == "fixture_core" {
		actor = "fixture"
	}
	completed, err := e.recordNarrative(ctx, NarrativeResult{CommandID: store.Digest([]byte("finalization.narrative\x00" + request.CommandID)), PlanID: request.PlanID,
		ManifestRevision: request.ManifestRevision, ManifestDigest: request.ManifestDigest, Text: output.Text, CitedIDs: output.CitedIDs, Actor: actor})
	if err != nil {
		_ = finish("failed", elapsed, 0)
		return empty, err
	}
	if err := finish("completed", elapsed, 0); err != nil {
		return empty, fmt.Errorf("narrative persisted but finalization run outcome is unresolved: %w", err)
	}
	return completed, nil
}

// Failed, idle-observed attempts may be retried with a new command ID. An
// unknown attempt is never replayed or cleared without qualified containment.
func (e *Engine) FinalizationRun(ctx context.Context, runID string) (string, error) {
	if !store.SafeID(runID) {
		return "", errors.New("run ID required")
	}
	var state string
	err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM runs WHERE id=? AND role='finalization'", runID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("finalization run not found")
	}
	return state, err
}
