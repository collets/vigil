package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"vigil/internal/artifacts"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

// FactualArchive is a portable snapshot of persisted facts. It deliberately
// contains no generated prose or ambient configuration/credential values.
type FactualArchive struct {
	SchemaVersion  int               `json:"schema_version"`
	PlanID         string            `json:"plan_id"`
	PlanRevision   int               `json:"plan_revision"`
	PlanDefinition json.RawMessage   `json:"plan_definition"`
	SpecDigest     string            `json:"spec_digest"`
	AcceptanceID   string            `json:"acceptance_id"`
	Tasks          []ArchiveTask     `json:"tasks"`
	Evidence       []ArchiveEvidence `json:"evidence"`
	Artifacts      []ArchiveArtifact `json:"artifacts"`
	Runs           []ArchiveRun      `json:"runs"`
	Deliveries     []ArchiveDelivery `json:"deliveries"`
	Recovery       []ArchiveRecovery `json:"recovery"`
}

type ArchiveTask struct {
	ID           string          `json:"id"`
	Revision     int             `json:"revision"`
	Definition   json.RawMessage `json:"definition"`
	Digest       string          `json:"digest"`
	AcceptanceID string          `json:"acceptance_id"`
}

type ArchiveEvidence struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	ScopeID string `json:"scope_id"`
	Result  string `json:"result"`
}

type ArchiveArtifact struct {
	Purpose string `json:"purpose"`
	ID      string `json:"id"`
	Digest  string `json:"digest"`
	Kind    string `json:"kind"`
	Bytes   int64  `json:"bytes"`
}

type ArchiveRun struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id"`
	Role           string `json:"role"`
	State          string `json:"state"`
	InputTokens    *int64 `json:"input_tokens,omitempty"`
	OutputTokens   *int64 `json:"output_tokens,omitempty"`
	CostMicrounits *int64 `json:"cost_microunits,omitempty"`
	Provenance     string `json:"provenance,omitempty"`
}

type ArchiveDelivery struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	Repository string `json:"repository_id"`
	HeadOID    string `json:"head_oid"`
	URL        string `json:"url,omitempty"`
}

type ArchiveRecovery struct {
	SetID string `json:"set_id"`
	State string `json:"state"`
}

type ArchiveRecord struct {
	PlanID         string `json:"plan_id"`
	Revision       int    `json:"revision"`
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	NarrativeID    string `json:"narrative_id,omitempty"`
	State          string `json:"state"`
	TaskID         string `json:"finalization_task_id"`
}

type NarrativeResult struct {
	CommandID        string   `json:"command_id"`
	PlanID           string   `json:"plan_id"`
	ManifestRevision int      `json:"manifest_revision"`
	ManifestDigest   string   `json:"manifest_digest"`
	Text             string   `json:"text"`
	CitedIDs         []string `json:"cited_ids"`
	Actor            string   `json:"actor"`
}

func finalizationTaskID(planID string) string {
	return store.Digest([]byte("finalization\x00" + planID))
}

// FactualArchive prepares a durable manifest only after an independently
// persisted plan acceptance. It does not dispatch a model or publish a ref.
func (e *Engine) BuildFactualArchive(ctx context.Context, commandID, planID string) (ArchiveRecord, error) {
	var record ArchiveRecord
	if !store.SafeID(commandID) || !store.SafeID(planID) {
		return record, errors.New("command and plan IDs required")
	}
	args, _ := json.Marshal(map[string]string{"plan_id": planID})
	command := store.Command{ID: commandID, Actor: string(Core), Kind: "archive.factual", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &record)
		}
		return record, err
	}
	manifest, revision, qualityRevision, err := e.collectFactualArchive(ctx, planID)
	if err != nil {
		return record, err
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		return record, err
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return record, err
	}
	artifact, err := repository.PutCore(ctx, store.Digest([]byte("archive.factual\x00"+commandID)), "factual-archive", "durable", bytes.NewReader(content))
	if err != nil {
		return record, err
	}
	record = ArchiveRecord{PlanID: planID, Revision: revision, ManifestID: artifact.ID, ManifestDigest: artifact.Digest, State: "narrative_pending", TaskID: finalizationTaskID(planID)}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var state string
		var currentPlanRevision, latest int
		var currentQualityRevision int64
		var currentAcceptance string
		if err := tx.QueryRowContext(ctx, "SELECT state,revision FROM plans WHERE id=?", planID).Scan(&state, &currentPlanRevision); err != nil {
			return nil, err
		}
		if state != "finalizing" && state != "finalization_pending" {
			return nil, errors.New("plan is not awaiting finalization")
		}
		if currentPlanRevision != manifest.PlanRevision {
			return nil, errors.New("plan changed before archive publication")
		}
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM quality_authority_v2 WHERE singleton=1").Scan(&currentQualityRevision); err != nil {
			return nil, err
		}
		if currentQualityRevision != qualityRevision {
			return nil, errors.New("quality authority changed before archive publication")
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM quality_acceptances_v2 WHERE plan_id=? AND target_kind='plan' AND invalidated_at IS NULL", planID).Scan(&currentAcceptance); err != nil || currentAcceptance != manifest.AcceptanceID {
			return nil, errors.New("current plan acceptance changed before archive publication")
		}
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", planID).Scan(&latest); err != nil {
			return nil, err
		}
		if latest+1 != revision {
			return nil, errors.New("archive revision changed before publication")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO archives(plan_id,revision,factual_manifest,state,created_at) VALUES(?, ?, ?, 'narrative_pending', ?)", planID, revision, artifact.ID, store.Now()); err != nil {
			return nil, err
		}
		if latest == 0 {
			definition, _ := json.Marshal(map[string]any{"id": record.TaskID, "objective": "Summarize the verified factual archive", "manifest_id": artifact.ID})
			digest := store.Digest(definition)
			var rank int
			if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(rank),0)+1 FROM tasks WHERE plan_id=?", planID).Scan(&rank); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO tasks(id,plan_id,revision,kind,state,rank,active_limit_ms,repair_limit,infra_limit) VALUES(?,?,1,'finalization','ready',?,300000,0,0)", record.TaskID, planID, rank); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO task_revisions(task_id,revision,definition_json,criteria_digest,definition_digest,author) VALUES(?,1,?,?,?,'core')", record.TaskID, string(definition), store.Digest([]byte("archive completeness")), digest); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='finalization_pending' WHERE id=?", planID); err != nil {
			return nil, err
		}
		return record, nil
	})
	if err != nil {
		return ArchiveRecord{}, err
	}
	err = json.Unmarshal(receipt, &record)
	return record, err
}

func (e *Engine) collectFactualArchive(ctx context.Context, planID string) (FactualArchive, int, int64, error) {
	manifest := FactualArchive{SchemaVersion: 1, PlanID: planID, Tasks: []ArchiveTask{}, Evidence: []ArchiveEvidence{}, Artifacts: []ArchiveArtifact{}, Runs: []ArchiveRun{}, Deliveries: []ArchiveDelivery{}, Recovery: []ArchiveRecovery{}}
	var state, definition, acceptanceArtifact, acceptanceDigest, scopeID string
	var qualityRevision int64
	var latest int
	err := e.DB.SQL.QueryRowContext(ctx, `SELECT p.state,p.revision,pr.definition_json,pr.spec_digest,a.id,a.scope_id,a.evidence_manifest_id,a.evidence_manifest_digest
		FROM plans p JOIN plan_revisions pr ON pr.plan_id=p.id AND pr.revision=p.revision
		JOIN quality_acceptances_v2 a ON a.plan_id=p.id AND a.target_kind='plan' AND a.invalidated_at IS NULL
		WHERE p.id=?`, planID).Scan(&state, &manifest.PlanRevision, &definition, &manifest.SpecDigest, &manifest.AcceptanceID, &scopeID, &acceptanceArtifact, &acceptanceDigest)
	if err != nil {
		return manifest, 0, 0, fmt.Errorf("current plan acceptance required: %w", err)
	}
	if state != "finalizing" && state != "finalization_pending" {
		return manifest, 0, 0, errors.New("plan must have accepted quality gates before finalization")
	}
	manifest.PlanDefinition = json.RawMessage(definition)
	var scopePlanRevision int
	var scopeRepositories string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT plan_revision,repository_manifest_json FROM quality_scopes_v2 WHERE id=?", scopeID).Scan(&scopePlanRevision, &scopeRepositories); err != nil || scopePlanRevision != manifest.PlanRevision {
		return manifest, 0, 0, errors.New("accepted plan scope is stale or unavailable")
	}
	var acceptedRepositories []struct {
		ID       string             `json:"id"`
		Revision int                `json:"revision"`
		Identity workspace.Identity `json:"identity"`
		Observed workspace.Baseline `json:"observed"`
	}
	if err := json.Unmarshal([]byte(scopeRepositories), &acceptedRepositories); err != nil || len(acceptedRepositories) == 0 {
		return manifest, 0, 0, errors.New("accepted plan has no repository fingerprint manifest")
	}
	var enrolledCount int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM plan_repositories WHERE plan_id=?", planID).Scan(&enrolledCount); err != nil || enrolledCount != len(acceptedRepositories) {
		return manifest, 0, 0, errors.New("accepted repository set changed")
	}
	seenRepositories := map[string]bool{}
	for _, accepted := range acceptedRepositories {
		if seenRepositories[accepted.ID] {
			return manifest, 0, 0, errors.New("duplicate accepted repository")
		}
		seenRepositories[accepted.ID] = true
		current, err := e.Repository(ctx, accepted.ID)
		if err != nil || current.PlanID != planID || current.Revision != accepted.Revision || !reflect.DeepEqual(current.Identity, accepted.Identity) {
			return manifest, 0, 0, errors.New("accepted repository identity changed")
		}
		if err := current.Identity.Validate(); err != nil {
			return manifest, 0, 0, err
		}
		observed, err := workspace.Fingerprint(ctx, current.Root, accepted.Observed.Exclusions)
		if err != nil || !reflect.DeepEqual(observed, accepted.Observed) {
			return manifest, 0, 0, fmt.Errorf("accepted repository %s changed before archive", accepted.ID)
		}
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM quality_authority_v2 WHERE singleton=1").Scan(&qualityRevision); err != nil {
		return manifest, 0, 0, err
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", planID).Scan(&latest); err != nil {
		return manifest, 0, 0, err
	}
	scopes := map[string]bool{scopeID: true}
	manifest.Artifacts = append(manifest.Artifacts, ArchiveArtifact{Purpose: "plan_acceptance", ID: acceptanceArtifact, Digest: acceptanceDigest, Kind: "acceptance-manifest"})
	rows, err := e.DB.SQL.QueryContext(ctx, `SELECT t.id,t.revision,t.state,tr.definition_json,tr.definition_digest,coalesce(a.id,''),coalesce(a.scope_id,''),coalesce(a.evidence_manifest_id,''),coalesce(a.evidence_manifest_digest,'')
		FROM tasks t JOIN task_revisions tr ON tr.task_id=t.id AND tr.revision=t.revision
		LEFT JOIN quality_acceptances_v2 a ON a.task_id=t.id AND a.invalidated_at IS NULL
		WHERE t.plan_id=? AND t.kind!='finalization' ORDER BY t.rank,t.id`, planID)
	if err != nil {
		return manifest, 0, 0, err
	}
	for rows.Next() {
		var task ArchiveTask
		var taskState, raw, taskScope, artifactID, digest string
		if err := rows.Scan(&task.ID, &task.Revision, &taskState, &raw, &task.Digest, &task.AcceptanceID, &taskScope, &artifactID, &digest); err != nil {
			rows.Close()
			return manifest, 0, 0, err
		}
		if taskState != "accepted" || task.AcceptanceID == "" || taskScope == "" || artifactID == "" {
			rows.Close()
			return manifest, 0, 0, fmt.Errorf("task %s lacks current acceptance", task.ID)
		}
		task.Definition = json.RawMessage(raw)
		manifest.Tasks = append(manifest.Tasks, task)
		scopes[taskScope] = true
		manifest.Artifacts = append(manifest.Artifacts, ArchiveArtifact{Purpose: "task_acceptance:" + task.ID, ID: artifactID, Digest: digest, Kind: "acceptance-manifest"})
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(manifest.Tasks) == 0 {
		return manifest, 0, 0, errors.New("accepted plan has no complete task set")
	}
	if err := e.collectArchiveEvidence(ctx, planID, scopes, &manifest); err != nil {
		return manifest, 0, 0, err
	}
	if err := e.collectArchiveOperations(ctx, planID, &manifest); err != nil {
		return manifest, 0, 0, err
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return manifest, 0, 0, err
	}
	for i := range manifest.Artifacts {
		ref := &manifest.Artifacts[i]
		if err := repository.Verify(ctx, ref.ID, ref.Digest, ref.Kind); err != nil {
			return manifest, 0, 0, fmt.Errorf("archive reference %s: %w", ref.Purpose, err)
		}
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT byte_count FROM artifacts WHERE id=?", ref.ID).Scan(&ref.Bytes); err != nil {
			return manifest, 0, 0, err
		}
	}
	sort.Slice(manifest.Artifacts, func(i, j int) bool { return manifest.Artifacts[i].Purpose < manifest.Artifacts[j].Purpose })
	return manifest, latest + 1, qualityRevision, nil
}

func (e *Engine) collectArchiveEvidence(ctx context.Context, planID string, scopes map[string]bool, manifest *FactualArchive) error {
	type evidenceQuery struct{ kind, query, artifactKind string }
	queries := []evidenceQuery{
		{"check", `SELECT c.id,c.scope_id,c.status,c.output_artifact_id,c.output_artifact_digest FROM check_results_v2 c JOIN quality_scopes_v2 s ON s.id=c.scope_id WHERE s.plan_id=? ORDER BY c.id`, "check-output"},
		{"review", `SELECT r.id,r.scope_id,r.status,r.result_artifact_id,r.result_artifact_digest FROM review_results_v2 r JOIN quality_scopes_v2 s ON s.id=r.scope_id WHERE s.plan_id=? ORDER BY r.id`, "review-result"},
	}
	for _, item := range queries {
		rows, err := e.DB.SQL.QueryContext(ctx, item.query, planID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, scopeID, result, artifactID, digest string
			if err := rows.Scan(&id, &scopeID, &result, &artifactID, &digest); err != nil {
				rows.Close()
				return err
			}
			if scopes[scopeID] {
				manifest.Evidence = append(manifest.Evidence, ArchiveEvidence{Kind: item.kind, ID: id, ScopeID: scopeID, Result: result})
				manifest.Artifacts = append(manifest.Artifacts, ArchiveArtifact{Purpose: item.kind + ":" + id, ID: artifactID, Digest: digest, Kind: item.artifactKind})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	rows, err := e.DB.SQL.QueryContext(ctx, `SELECT m.id,m.scope_id,m.state,coalesce(m.artifact_id,''),coalesce(m.artifact_digest,''),coalesce(a.kind,'') FROM manual_results_v2 m JOIN quality_scopes_v2 s ON s.id=m.scope_id LEFT JOIN artifacts a ON a.id=m.artifact_id WHERE s.plan_id=? ORDER BY m.id`, planID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, scopeID, result, artifactID, digest, kind string
		if err := rows.Scan(&id, &scopeID, &result, &artifactID, &digest, &kind); err != nil {
			rows.Close()
			return err
		}
		if scopes[scopeID] {
			manifest.Evidence = append(manifest.Evidence, ArchiveEvidence{Kind: "manual", ID: id, ScopeID: scopeID, Result: result})
			if artifactID != "" {
				manifest.Artifacts = append(manifest.Artifacts, ArchiveArtifact{Purpose: "manual:" + id, ID: artifactID, Digest: digest, Kind: kind})
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = e.DB.SQL.QueryContext(ctx, `SELECT h.id,h.scope_id,h.action FROM human_decisions_v2 h JOIN quality_scopes_v2 s ON s.id=h.scope_id WHERE s.plan_id=? ORDER BY h.id`, planID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, scopeID, action string
		if err := rows.Scan(&id, &scopeID, &action); err != nil {
			rows.Close()
			return err
		}
		if scopes[scopeID] {
			manifest.Evidence = append(manifest.Evidence, ArchiveEvidence{Kind: "human_decision", ID: id, ScopeID: scopeID, Result: action})
		}
	}
	err = rows.Err()
	rows.Close()
	return err
}

func (e *Engine) collectArchiveOperations(ctx context.Context, planID string, manifest *FactualArchive) error {
	rows, err := e.DB.SQL.QueryContext(ctx, `SELECT r.id,r.task_id,r.role,r.state,u.input_tokens,u.output_tokens,u.cost_microunits,coalesce(u.provenance,'')
		FROM runs r LEFT JOIN usage_observations u ON u.id=(SELECT id FROM usage_observations WHERE run_id=r.id ORDER BY observed_at DESC,id DESC LIMIT 1)
		WHERE r.plan_id=? ORDER BY r.created_at,r.id`, planID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var run ArchiveRun
		var input, output, cost sql.NullInt64
		if err := rows.Scan(&run.ID, &run.TaskID, &run.Role, &run.State, &input, &output, &cost, &run.Provenance); err != nil {
			rows.Close()
			return err
		}
		if input.Valid {
			run.InputTokens = &input.Int64
		}
		if output.Valid {
			run.OutputTokens = &output.Int64
		}
		if cost.Valid {
			run.CostMicrounits = &cost.Int64
		}
		manifest.Runs = append(manifest.Runs, run)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = e.DB.SQL.QueryContext(ctx, "SELECT id,kind,state,repository_id,head_oid,coalesce(url,'') FROM deliveries WHERE plan_id=? ORDER BY id", planID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var delivery ArchiveDelivery
		if err := rows.Scan(&delivery.ID, &delivery.Kind, &delivery.State, &delivery.Repository, &delivery.HeadOID, &delivery.URL); err != nil {
			rows.Close()
			return err
		}
		manifest.Deliveries = append(manifest.Deliveries, delivery)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = e.DB.SQL.QueryContext(ctx, "SELECT c.id,c.state FROM checkpoint_sets c JOIN runs r ON r.id=c.run_id WHERE r.plan_id=? ORDER BY c.id", planID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var recovery ArchiveRecovery
		if err := rows.Scan(&recovery.SetID, &recovery.State); err != nil {
			rows.Close()
			return err
		}
		manifest.Recovery = append(manifest.Recovery, recovery)
	}
	err = rows.Err()
	rows.Close()
	return err
}

func (e *Engine) Archive(ctx context.Context, planID string, revision int) (ArchiveRecord, FactualArchive, error) {
	var record ArchiveRecord
	var manifest FactualArchive
	if !store.SafeID(planID) || revision < 1 {
		return record, manifest, errors.New("exact archive revision required")
	}
	var narrative sql.NullString
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT factual_manifest,narrative,state FROM archives WHERE plan_id=? AND revision=?", planID, revision).Scan(&record.ManifestID, &narrative, &record.State); err != nil {
		return record, manifest, err
	}
	record.PlanID, record.Revision, record.TaskID = planID, revision, finalizationTaskID(planID)
	if narrative.Valid {
		record.NarrativeID = narrative.String
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return record, manifest, err
	}
	b, err := repository.Read(ctx, record.ManifestID)
	if err != nil {
		return record, manifest, err
	}
	record.ManifestDigest = store.Digest(b)
	if err := repository.Verify(ctx, record.ManifestID, record.ManifestDigest, "factual-archive"); err != nil {
		return record, manifest, fmt.Errorf("factual archive artifact: %w", err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		return record, manifest, err
	}
	if manifest.PlanID != planID || manifest.SchemaVersion != 1 || manifest.PlanRevision < 1 {
		return record, manifest, errors.New("factual archive identity is inconsistent")
	}
	for _, ref := range manifest.Artifacts {
		if err := repository.Verify(ctx, ref.ID, ref.Digest, ref.Kind); err != nil {
			return record, manifest, fmt.Errorf("archive reference %s: %w", ref.Purpose, err)
		}
	}
	if record.State == "verified" {
		if record.NarrativeID == "" {
			return record, manifest, errors.New("verified archive lacks a narrative")
		}
		narrative, err := repository.Read(ctx, record.NarrativeID)
		if err != nil {
			return record, manifest, fmt.Errorf("narrative artifact: %w", err)
		}
		if err := repository.Verify(ctx, record.NarrativeID, store.Digest(narrative), "finalization-narrative"); err != nil {
			return record, manifest, fmt.Errorf("narrative artifact: %w", err)
		}
	}
	return record, manifest, nil
}

// RecordFixtureNarrative exercises the finalization result contract without
// enabling model dispatch. Fixture provenance remains visible in the command
// receipt and can only complete a plan carrying fixture acceptance evidence.
func (e *Engine) RecordFixtureNarrative(ctx context.Context, request NarrativeResult) (ArchiveRecord, error) {
	var record ArchiveRecord
	if !store.SafeID(request.CommandID) || !store.SafeID(request.PlanID) || request.ManifestRevision < 1 || request.Actor != "fixture" || len(request.Text) == 0 || len(request.Text) > store.MaxDocument || strings.TrimSpace(request.Text) == "" || len(request.CitedIDs) == 0 || len(request.CitedIDs) > 1000 {
		return record, errors.New("bounded fixture narrative and exact archive identity required")
	}
	args, err := json.Marshal(request)
	if err != nil || len(args) > store.MaxDocument {
		return record, errors.New("narrative result exceeds command limit")
	}
	command := store.Command{ID: request.CommandID, Actor: "fixture", Kind: "archive.narrative", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &record)
		}
		return record, err
	}
	if err := e.requireFixtureArchive(ctx, request.PlanID); err != nil {
		return record, err
	}
	record, manifest, err := e.Archive(ctx, request.PlanID, request.ManifestRevision)
	if err != nil {
		return record, err
	}
	if record.State != "narrative_pending" || record.ManifestDigest != request.ManifestDigest {
		return ArchiveRecord{}, errors.New("narrative refers to a stale or completed factual manifest")
	}
	allowed := map[string]bool{manifest.AcceptanceID: true}
	required := map[string]bool{manifest.AcceptanceID: true}
	for _, task := range manifest.Tasks {
		allowed[task.ID], allowed[task.AcceptanceID] = true, true
		required[task.AcceptanceID] = true
	}
	for _, evidence := range manifest.Evidence {
		allowed[evidence.ID] = true
	}
	for _, artifact := range manifest.Artifacts {
		allowed[artifact.ID] = true
	}
	for _, run := range manifest.Runs {
		allowed[run.ID] = true
	}
	for _, delivery := range manifest.Deliveries {
		allowed[delivery.ID] = true
	}
	for _, recovery := range manifest.Recovery {
		allowed[recovery.SetID] = true
	}
	seen := map[string]bool{}
	for _, id := range request.CitedIDs {
		if !allowed[id] || seen[id] {
			return ArchiveRecord{}, errors.New("narrative has duplicate or foreign references")
		}
		seen[id] = true
	}
	for id := range required {
		if !seen[id] {
			return ArchiveRecord{}, fmt.Errorf("narrative omits required acceptance %s", id)
		}
	}
	result, err := json.Marshal(map[string]any{"schema_version": 1, "manifest_revision": request.ManifestRevision, "manifest_digest": request.ManifestDigest, "text": request.Text, "cited_ids": request.CitedIDs, "actor": request.Actor})
	if err != nil {
		return ArchiveRecord{}, err
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return ArchiveRecord{}, err
	}
	artifact, err := repository.PutCore(ctx, store.Digest([]byte("archive.narrative\x00"+request.CommandID)), "finalization-narrative", "durable", bytes.NewReader(result))
	if err != nil {
		return ArchiveRecord{}, err
	}
	record.NarrativeID, record.State = artifact.ID, "verified"
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var state, manifestID, archiveState, acceptanceActor string
		var latest int
		if err := tx.QueryRowContext(ctx, "SELECT state FROM plans WHERE id=?", request.PlanID).Scan(&state); err != nil || state != "finalization_pending" {
			return nil, errors.New("plan is no longer awaiting finalization")
		}
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", request.PlanID).Scan(&latest); err != nil || latest != request.ManifestRevision {
			return nil, errors.New("a newer factual manifest superseded the narrative")
		}
		if err := tx.QueryRowContext(ctx, "SELECT factual_manifest,state FROM archives WHERE plan_id=? AND revision=?", request.PlanID, request.ManifestRevision).Scan(&manifestID, &archiveState); err != nil || manifestID != record.ManifestID || archiveState != "narrative_pending" {
			return nil, errors.New("archive state changed before narrative publication")
		}
		if err := tx.QueryRowContext(ctx, "SELECT actor FROM quality_acceptances_v2 WHERE id=? AND invalidated_at IS NULL", manifest.AcceptanceID).Scan(&acceptanceActor); err != nil || acceptanceActor != "fixture_core" {
			return nil, errors.New("fixture acceptance is no longer current")
		}
		for _, task := range manifest.Tasks {
			var id string
			if err := tx.QueryRowContext(ctx, "SELECT id FROM quality_acceptances_v2 WHERE id=? AND invalidated_at IS NULL", task.AcceptanceID).Scan(&id); err != nil {
				return nil, errors.New("task acceptance changed before finalization")
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE archives SET narrative=?,state='verified' WHERE plan_id=? AND revision=?", artifact.ID, request.PlanID, request.ManifestRevision); err != nil {
			return nil, err
		}
		updated, err := tx.ExecContext(ctx, "UPDATE tasks SET state='accepted' WHERE id=? AND kind='finalization' AND state='ready'", record.TaskID)
		if err != nil {
			return nil, err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return nil, errors.New("finalization task is not ready")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='completed',completed_at=? WHERE id=?", store.Now(), request.PlanID); err != nil {
			return nil, err
		}
		return record, nil
	})
	if err != nil {
		return ArchiveRecord{}, err
	}
	err = json.Unmarshal(receipt, &record)
	return record, err
}

func (e *Engine) requireFixtureArchive(ctx context.Context, planID string) error {
	rows, err := e.DB.SQL.QueryContext(ctx, "SELECT r.root FROM plan_repositories p JOIN repositories r ON r.id=p.repository_id WHERE p.plan_id=?", planID)
	if err != nil {
		return err
	}
	var count int
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			rows.Close()
			return err
		}
		info, err := os.Lstat(filepath.Join(root, ".vigil-disposable-fixture"))
		if err != nil || !info.Mode().IsRegular() {
			rows.Close()
			return errors.New("fixture narrative requires marked disposable repositories")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("fixture narrative requires enrolled repositories")
	}
	return nil
}
