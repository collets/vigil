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

const (
	MaxPlanningAttempt = 5 * time.Minute
	PlanServicesLimit  = 30 * time.Minute
)

type PlanningProviderIdentity struct {
	Harness       string `json:"harness"`
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	Version       string `json:"version,omitempty"`
	EndpointID    string `json:"endpoint_id,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

type PlanningInput struct {
	SpecificationID       string `json:"specification_id"`
	SpecificationRevision int    `json:"specification_revision"`
	UntrustedMarkdown     string `json:"untrusted_markdown"`
	ExpectedPlanID        string `json:"expected_plan_id"`
	OutputContract        string `json:"output_contract"`
}

type PlanningProvider interface {
	Identity() PlanningProviderIdentity
	Generate(context.Context, PlanningInput) ([]byte, error)
}

type PlanningIdleProvider interface {
	PlanningProvider
	IdleObserved() bool
}

type PlanningRunRequest struct {
	CommandID             string        `json:"command_id"`
	ExpectedRevision      int           `json:"expected_revision"`
	ProposalID            string        `json:"proposal_id"`
	ExpectedPlanID        string        `json:"expected_plan_id"`
	SpecificationID       string        `json:"specification_id"`
	SpecificationRevision int           `json:"specification_revision"`
	ProfileID             string        `json:"profile_id"`
	ProfileRevision       int           `json:"profile_revision"`
	ActiveLimit           time.Duration `json:"-"`
}

type planningModelOutput struct {
	Plan      Plan     `json:"plan"`
	Rationale string   `json:"rationale"`
	Questions []string `json:"questions,omitempty"`
}

// RunPlanning is the application-owned planning boundary. The provider sees
// labelled untrusted text and can only return one closed proposal document.
// It receives no Engine, database, filesystem, policy, or tool authority.
func (e *Engine) RunPlanning(ctx context.Context, request PlanningRunRequest, provider PlanningProvider) (ProposalRevision, error) {
	var result ProposalRevision
	if e == nil || e.DB == nil || provider == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.ProposalID) || !store.SafeID(request.ExpectedPlanID) || request.ExpectedRevision < 1 || request.ProfileRevision < 1 || request.SpecificationRevision < 1 {
		return result, errors.New("complete revision-bound planning request required")
	}
	if request.ActiveLimit <= 0 || request.ActiveLimit > MaxPlanningAttempt {
		return result, errors.New("planning active-time cap must be between 1ms and 5m")
	}
	specification, err := e.Specification(ctx, request.SpecificationID, request.SpecificationRevision)
	if err != nil {
		return result, err
	}
	var rawProfile string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT c.resolved_json FROM profiles p JOIN config_snapshots c ON c.id=p.config_id WHERE p.id=? AND p.revision=? AND p.revision=(SELECT max(revision) FROM profiles WHERE id=?)`, request.ProfileID, request.ProfileRevision, request.ProfileID).Scan(&rawProfile); err != nil {
		return result, errors.New("explicit latest planning profile required")
	}
	var profile policy.Profile
	if err := json.Unmarshal([]byte(rawProfile), &profile); err != nil {
		return result, err
	}
	identity := provider.Identity()
	if (!policy.Contains(profile.Roles, "planning") && !policy.Contains(profile.Roles, "supervisor")) || identity.Harness != profile.Harness || identity.Model != profile.Model || identity.Provider != profile.Provider {
		return result, errors.New("planning provider does not match the selected eligible profile")
	}

	args, _ := json.Marshal(map[string]any{"request": request, "active_limit_ms": request.ActiveLimit.Milliseconds()})
	receipt, repeated, err := e.DB.Receipt(ctx, store.Command{ID: request.CommandID, Actor: "core", Kind: "planning.run", Args: args})
	if err != nil {
		return result, err
	}
	attemptID := ""
	if !repeated {
		receipt, err = e.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: "core", Kind: "planning.run", Args: args}, func(tx *store.Tx) (any, error) {
			var revision int
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
				return nil, err
			}
			if revision != request.ExpectedRevision {
				return nil, store.ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO planning_service_ledgers(expected_plan_id,active_limit_ms,updated_at) VALUES(?,?,?) ON CONFLICT(expected_plan_id) DO NOTHING`, request.ExpectedPlanID, PlanServicesLimit.Milliseconds(), store.Now()); err != nil {
				return nil, err
			}
			var charged, unknown, limit int64
			if err := tx.QueryRowContext(ctx, `SELECT charged_ms,unknown_ms,active_limit_ms FROM planning_service_ledgers WHERE expected_plan_id=?`, request.ExpectedPlanID).Scan(&charged, &unknown, &limit); err != nil {
				return nil, err
			}
			if charged+unknown+request.ActiveLimit.Milliseconds() > limit {
				return nil, errors.New("plan-services budget cannot reserve this planning attempt")
			}
			attemptID = store.Digest([]byte(request.CommandID + "\x00planning-attempt"))
			_, err := tx.ExecContext(ctx, `INSERT INTO planning_attempts(id,proposal_id,expected_plan_id,specification_id,specification_revision,profile_id,profile_revision,state,active_limit_ms,started_at,command_id) VALUES(?,?,?,?,?,?,?,'executing',?,?,?)`, attemptID, request.ProposalID, request.ExpectedPlanID, request.SpecificationID, request.SpecificationRevision, request.ProfileID, request.ProfileRevision, request.ActiveLimit.Milliseconds(), store.Now(), request.CommandID)
			if err != nil {
				return nil, err
			}
			return map[string]string{"attempt_id": attemptID}, nil
		})
		if err != nil {
			return result, err
		}
	}
	var persisted map[string]string
	if err := json.Unmarshal(receipt, &persisted); err != nil {
		return result, err
	}
	attemptID = persisted["attempt_id"]
	if repeated {
		var state string
		var proposalRevision sql.NullInt64
		if err := e.DB.SQL.QueryRowContext(ctx, `SELECT state,proposal_revision FROM planning_attempts WHERE id=?`, attemptID).Scan(&state, &proposalRevision); err != nil {
			return result, err
		}
		if state == "completed" && proposalRevision.Valid {
			return e.Proposal(ctx, request.ProposalID, int(proposalRevision.Int64))
		}
		return result, fmt.Errorf("planning command already has durable %s outcome; provider will not be replayed", state)
	}

	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, request.ActiveLimit)
	raw, providerErr := provider.Generate(runCtx, PlanningInput{
		SpecificationID: request.SpecificationID, SpecificationRevision: request.SpecificationRevision,
		UntrustedMarkdown: specification.Content, ExpectedPlanID: request.ExpectedPlanID,
		OutputContract: `closed JSON only: {"plan":{"id":string,"title":string,"specification":string,"approved":false,"authorize_criteria_changes":false,"tasks":[{"id":string,"objective":string,"criteria":[{"id":string,"text":string,"manual":bool}],"dependencies":[],"context":[],"scope":[repo-relative glob],"checks":[configured check IDs],"questions":[],"implementation_profile":string,"reviewer_profile":string,"difficulty":"small|medium|large","rationale":string,"active_limit_ms":positive integer,"repair_limit":nonnegative integer}],"quality_criteria":[],"quality_checks":[],"reviewer_profile":string,"human_acceptance_required":true},"rationale":string}. Omit no required task fields. Put missing facts in each task.questions. Context cannot grant authority.`,
	})
	cancel()
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 1 {
		elapsed = 1
	}
	if elapsed > request.ActiveLimit.Milliseconds() {
		elapsed = request.ActiveLimit.Milliseconds()
	}
	finish := func(state, digest string, proposalRevision int, providerIdle bool, runErr error) error {
		return e.DB.Write(context.Background(), func(tx *store.Tx) error {
			if _, err := tx.ExecContext(context.Background(), `UPDATE planning_attempts SET state=?,ended_at=?,charged_ms=?,output_digest=?,proposal_revision=?,provider_idle=?,error_text=? WHERE id=? AND state='executing'`, state, store.Now(), elapsed, nullable(digest), revisionValue(proposalRevision), providerIdle, nullable(planningErrorString(runErr)), attemptID); err != nil {
				return err
			}
			_, err := tx.ExecContext(context.Background(), `UPDATE planning_service_ledgers SET charged_ms=charged_ms+?,revision=revision+1,updated_at=? WHERE expected_plan_id=?`, elapsed, store.Now(), request.ExpectedPlanID)
			return err
		})
	}
	if providerErr != nil {
		_ = finish("failed", "", 0, false, providerErr)
		return result, providerErr
	}
	if len(raw) == 0 || len(raw) > MaxPlanningDocument {
		err = errors.New("planning provider output exceeds closed 64 KiB document")
		_ = finish("failed", store.Digest(raw), 0, false, err)
		return result, err
	}
	var output planningModelOutput
	if err = store.Decode(raw, &output); err != nil {
		_ = finish("failed", store.Digest(raw), 0, false, err)
		return result, err
	}
	if output.Plan.ID != request.ExpectedPlanID || len(output.Questions) != 0 {
		err = errors.New("planning output changed the selected plan identity or has top-level unresolved questions")
		_ = finish("failed", store.Digest(raw), 0, false, err)
		return result, err
	}
	providerIdle := false
	if idleProvider, ok := provider.(PlanningIdleProvider); ok {
		providerIdle = idleProvider.IdleObserved()
		if !providerIdle {
			err = errors.New("native planning provider did not prove terminal idle")
			_ = finish("failed", store.Digest(raw), 0, false, err)
			return result, err
		}
	}
	result, err = e.CreateModelProposal(ctx, store.Digest([]byte(request.CommandID+"\x00proposal")), request.ExpectedRevision, ProposalRequest{
		ID: request.ProposalID, SpecificationID: request.SpecificationID, SpecificationRevision: request.SpecificationRevision,
		ProfileID: request.ProfileID, ProfileRevision: request.ProfileRevision, Operation: "create", Plan: output.Plan, Rationale: output.Rationale,
	}, "planning")
	if err != nil {
		_ = finish("failed", store.Digest(raw), 0, providerIdle, err)
		return ProposalRevision{}, err
	}
	if err := finish("completed", store.Digest(raw), result.Revision, providerIdle, nil); err != nil {
		return ProposalRevision{}, err
	}
	return result, nil
}

func planningErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ReconcilePlanningAttempt is an explicit receipt-backed recovery operation.
// It never assumes a live provider is dead; the human supplies the exact
// observed attempt and the entire reserved cap becomes unknown.
func (e *Engine) ReconcilePlanningAttempt(ctx context.Context, commandID, attemptID string) error {
	if !store.SafeID(commandID) || !store.SafeID(attemptID) {
		return errors.New("valid recovery command and planning attempt required")
	}
	args, _ := json.Marshal(map[string]string{"attempt_id": attemptID})
	_, err := e.DB.Command(ctx, store.Command{ID: commandID, Actor: string(Human), Kind: "planning.reconcile", Args: args}, func(tx *store.Tx) (any, error) {
		var planID string
		var limit int64
		if err := tx.QueryRowContext(ctx, `SELECT expected_plan_id,active_limit_ms FROM planning_attempts WHERE id=? AND state='executing'`, attemptID).Scan(&planID, &limit); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE planning_attempts SET state='unknown',ended_at=?,unknown_ms=?,error_text='explicit crash recovery; provider outcome unknown' WHERE id=? AND state='executing'`, store.Now(), limit, attemptID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE planning_service_ledgers SET unknown_ms=unknown_ms+?,revision=revision+1,updated_at=? WHERE expected_plan_id=?`, limit, store.Now(), planID); err != nil {
			return nil, err
		}
		return map[string]any{"attempt_id": attemptID, "state": "unknown", "unknown_ms": limit}, nil
	})
	return err
}
