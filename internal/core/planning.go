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
	"strings"

	"vigil/internal/artifacts"
	"vigil/internal/policy"
	"vigil/internal/store"
)

const MaxPlanningDocument = 64 << 10

type SpecificationRevision struct {
	ID         string `json:"id"`
	Revision   int    `json:"revision"`
	ArtifactID string `json:"artifact_id"`
	Digest     string `json:"digest"`
	SourceName string `json:"source_name"`
	Bytes      int    `json:"bytes"`
	Content    string `json:"content,omitempty"`
}
type ProposalRequest struct {
	ID                    string   `json:"id"`
	SpecificationID       string   `json:"specification_id"`
	SpecificationRevision int      `json:"specification_revision"`
	ProfileID             string   `json:"profile_id"`
	ProfileRevision       int      `json:"profile_revision"`
	Operation             string   `json:"operation"`
	ExpectedPlanID        string   `json:"expected_plan_id,omitempty"`
	ExpectedPlanRevision  int      `json:"expected_plan_revision,omitempty"`
	AffectedTasks         []string `json:"affected_tasks"`
	Plan                  Plan     `json:"plan"`
	Rationale             string   `json:"rationale"`
}
type ProposalRevision struct {
	ID                    string   `json:"id"`
	Revision              int      `json:"revision"`
	SpecificationID       string   `json:"specification_id"`
	SpecificationRevision int      `json:"specification_revision"`
	ProfileID             string   `json:"profile_id"`
	ProfileRevision       int      `json:"profile_revision"`
	Operation             string   `json:"operation"`
	AffectedTasks         []string `json:"affected_tasks"`
	State                 string   `json:"state"`
	Digest                string   `json:"digest"`
	Plan                  Plan     `json:"plan"`
	Rationale             string   `json:"rationale"`
	Actor                 string   `json:"actor"`
}

func (e *Engine) ImportMarkdown(ctx context.Context, commandID string, expectedRevision int, specID, path string) (SpecificationRevision, error) {
	var out SpecificationRevision
	if !store.SafeID(commandID) || !store.SafeID(specID) || expectedRevision < 1 {
		return out, errors.New("valid command, specification ID and revision required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return out, err
	}
	var root string
	if err = e.DB.SQL.QueryRowContext(ctx, "SELECT root FROM project WHERE id=?", e.ProjectID).Scan(&root); err != nil {
		return out, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return out, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return out, errors.New("Markdown source must be a regular non-symlink file")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return out, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Ext(resolved) != ".md" {
		return out, errors.New("Markdown source must be an owned .md file inside the project root")
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return out, err
	}
	if len(b) == 0 || len(b) > MaxPlanningDocument {
		return out, errors.New("Markdown source must be 1–65536 bytes")
	}
	args, _ := json.Marshal(map[string]any{"specification_id": specID, "path": rel, "digest": store.Digest(b), "expected_revision": expectedRevision})
	command := store.Command{ID: commandID, Actor: string(Human), Kind: "specification.import", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &out)
			if err == nil {
				content, readErr := e.Specification(ctx, specID, out.Revision)
				if readErr != nil {
					return out, readErr
				}
				out.Content = content.Content
			}
		}
		return out, err
	}
	repo, err := artifacts.New(e.DB)
	if err != nil {
		return out, err
	}
	artifact, err := repo.Put(ctx, commandID+"-source", "specification.markdown", "durable", bytes.NewReader(b))
	if err != nil {
		return out, err
	}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", expectedRevision, revision)
		}
		var sr int
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0)+1 FROM specification_revisions WHERE id=?", specID).Scan(&sr); err != nil {
			return nil, err
		}
		out = SpecificationRevision{ID: specID, Revision: sr, ArtifactID: artifact.ID, Digest: artifact.Digest, SourceName: filepath.ToSlash(rel), Bytes: len(b)}
		if _, err := tx.ExecContext(ctx, `INSERT INTO specification_revisions(id,revision,artifact_id,content_digest,source_name,byte_count,actor,created_at) VALUES(?,?,?,?,?,?,?,?)`, specID, sr, artifact.ID, artifact.Digest, out.SourceName, len(b), "human", store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(out)
		if _, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,command_id,kind,occurred_at,payload_json) VALUES(1,?,'specification_imported',?,?)", commandID, store.Now(), string(payload)); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(receipt, &out)
	out.Content = string(b)
	return out, err
}

func (e *Engine) Specification(ctx context.Context, id string, revision int) (SpecificationRevision, error) {
	var out SpecificationRevision
	if !store.SafeID(id) || revision < 1 {
		return out, errors.New("valid specification revision required")
	}
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT id,revision,artifact_id,content_digest,source_name,byte_count FROM specification_revisions WHERE id=? AND revision=?`, id, revision).Scan(&out.ID, &out.Revision, &out.ArtifactID, &out.Digest, &out.SourceName, &out.Bytes); err != nil {
		return out, err
	}
	repo, err := artifacts.New(e.DB)
	if err != nil {
		return out, err
	}
	b, err := repo.Read(ctx, out.ArtifactID)
	if err != nil {
		return out, err
	}
	if len(b) != out.Bytes || store.Digest(b) != out.Digest {
		return out, errors.New("specification artifact is corrupt")
	}
	out.Content = string(b)
	return out, nil
}

func (e *Engine) CreateFixtureProposal(ctx context.Context, commandID string, expectedRevision int, request ProposalRequest) (ProposalRevision, error) {
	return e.createProposal(ctx, commandID, expectedRevision, request, "fixture")
}

func (e *Engine) CreateModelProposal(ctx context.Context, commandID string, expectedRevision int, request ProposalRequest, role string) (ProposalRevision, error) {
	if role != "planning" && role != "supervisor" {
		return ProposalRevision{}, errors.New("role cannot create planning proposals")
	}
	return e.createProposal(ctx, commandID, expectedRevision, request, "model:"+role)
}

func (e *Engine) createProposal(ctx context.Context, commandID string, expectedRevision int, request ProposalRequest, actor string) (ProposalRevision, error) {
	var out ProposalRevision
	if !store.SafeID(commandID) || !store.SafeID(request.ID) || expectedRevision < 1 {
		return out, errors.New("valid command, proposal and revision required")
	}
	if request.Operation != "create" && request.Operation != "edit" && request.Operation != "split" && request.Operation != "merge" && request.Operation != "reorder" {
		return out, errors.New("unsupported proposal operation")
	}
	if request.Rationale == "" || len(request.Rationale) > 4096 || len(request.AffectedTasks) > 50 {
		return out, errors.New("bounded rationale and at most 50 affected tasks required")
	}
	if request.Plan.Approved || request.Plan.AuthorizeCriteriaChanges {
		return out, errors.New("proposal cannot approve a plan or authorize criteria changes")
	}
	if err := policy.ValidateTasks(request.Plan.Tasks); err != nil {
		return out, err
	}
	if err := policy.ValidateQualityDefinition(request.Plan.Criteria, request.Plan.Checks, request.Plan.Reviewer); err != nil {
		return out, err
	}
	if _, err := e.Specification(ctx, request.SpecificationID, request.SpecificationRevision); err != nil {
		return out, err
	}
	rawDefinition, err := json.Marshal(request.Plan)
	if err != nil {
		return out, err
	}
	definition, err := store.Canonical(rawDefinition)
	if err != nil || len(definition) > MaxPlanningDocument {
		return out, errors.New("proposal definition exceeds closed 64 KiB document")
	}
	args, _ := json.Marshal(map[string]any{"request": request, "expected_revision": expectedRevision})
	command := store.Command{ID: commandID, Actor: actor, Kind: "planning.proposal.create", Args: args}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, store.ErrConflict
		}
		var raw string
		if request.ProfileRevision < 1 {
			return nil, errors.New("explicit eligible planning profile revision required")
		}
		if err := tx.QueryRowContext(ctx, `SELECT c.resolved_json FROM profiles p JOIN config_snapshots c ON c.id=p.config_id WHERE p.id=? AND p.revision=? AND p.revision=(SELECT max(revision) FROM profiles WHERE id=?)`, request.ProfileID, request.ProfileRevision, request.ProfileID).Scan(&raw); err != nil {
			return nil, errors.New("explicit eligible planning profile required")
		}
		var profile policy.Profile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			return nil, err
		}
		if !policy.Contains(profile.Roles, "planning") && !policy.Contains(profile.Roles, "supervisor") {
			return nil, errors.New("profile is not eligible for planning")
		}
		var pr int
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0)+1 FROM planning_proposals WHERE id=?", request.ID).Scan(&pr); err != nil {
			return nil, err
		}
		affected, _ := json.Marshal(request.AffectedTasks)
		requestID := store.ID()
		out = ProposalRevision{ID: request.ID, Revision: pr, SpecificationID: request.SpecificationID, SpecificationRevision: request.SpecificationRevision, ProfileID: request.ProfileID, ProfileRevision: request.ProfileRevision, Operation: request.Operation, AffectedTasks: request.AffectedTasks, State: "proposed", Digest: store.Digest(definition), Plan: request.Plan, Rationale: request.Rationale, Actor: actor}
		approvalContext, _ := json.Marshal(map[string]any{"proposal_id": request.ID, "proposal_revision": pr, "specification_id": request.SpecificationID, "specification_revision": request.SpecificationRevision, "definition_digest": out.Digest, "operation": request.Operation, "affected_tasks": request.AffectedTasks, "proposal_actor": actor, "requires_human_application": true})
		if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,context_json,blocking,created_at) VALUES(?,'approval','pending',?,1,?)`, requestID, string(approvalContext), store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO planning_proposals(id,revision,specification_id,specification_revision,expected_plan_id,expected_plan_revision,profile_id,profile_revision,definition_json,definition_digest,operation,affected_tasks_json,rationale,state,request_id,actor,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'proposed',?,?,?)`, request.ID, pr, request.SpecificationID, request.SpecificationRevision, nullable(request.ExpectedPlanID), revisionValue(request.ExpectedPlanRevision), request.ProfileID, request.ProfileRevision, string(definition), out.Digest, request.Operation, string(affected), request.Rationale, requestID, actor, store.Now()); err != nil {
			return nil, err
		}
		for _, task := range request.Plan.Tasks {
			for _, question := range task.Questions {
				contextJSON, _ := json.Marshal(map[string]any{"proposal_id": request.ID, "proposal_revision": pr, "task_id": task.ID, "question": question, "untrusted_context": true})
				if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,context_json,blocking,created_at) VALUES(?,'input','pending',?,1,?)`, store.ID(), string(contextJSON), store.Now()); err != nil {
					return nil, err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(receipt, &out)
	return out, err
}

func (e *Engine) Proposal(ctx context.Context, id string, revision int) (ProposalRevision, error) {
	var out ProposalRevision
	var definition, affected string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT id,revision,specification_id,specification_revision,profile_id,profile_revision,operation,affected_tasks_json,state,definition_digest,definition_json,rationale,actor FROM planning_proposals WHERE id=? AND revision=?`, id, revision).Scan(&out.ID, &out.Revision, &out.SpecificationID, &out.SpecificationRevision, &out.ProfileID, &out.ProfileRevision, &out.Operation, &affected, &out.State, &out.Digest, &definition, &out.Rationale, &out.Actor); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(affected), &out.AffectedTasks); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(definition), &out.Plan); err != nil {
		return out, err
	}
	return out, nil
}

func (e *Engine) applyPlanningProposal(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var p struct {
		ProposalID               string `json:"proposal_id"`
		ProposalRevision         int    `json:"proposal_revision"`
		AuthorizeCriteriaChanges bool   `json:"authorize_criteria_changes"`
	}
	if err := store.Decode(cmd.Payload, &p); err != nil {
		return nil, err
	}
	var definition, state, expectedPlan string
	var expectedRevision sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT definition_json,state,coalesce(expected_plan_id,''),expected_plan_revision FROM planning_proposals WHERE id=? AND revision=?`, p.ProposalID, p.ProposalRevision).Scan(&definition, &state, &expectedPlan, &expectedRevision); err != nil {
		return nil, err
	}
	if state != "proposed" {
		return nil, errors.New("proposal is no longer pending")
	}
	var pendingInputs int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM requests WHERE kind='input' AND state='pending' AND json_extract(context_json,'$.proposal_id')=? AND json_extract(context_json,'$.proposal_revision')=?`, p.ProposalID, p.ProposalRevision).Scan(&pendingInputs); err != nil {
		return nil, err
	}
	if pendingInputs != 0 {
		return nil, errors.New("proposal has unresolved clarification requests")
	}
	var plan Plan
	if err := json.Unmarshal([]byte(definition), &plan); err != nil {
		return nil, err
	}
	if expectedPlan != "" {
		var current int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM plans WHERE id=?", expectedPlan).Scan(&current); err != nil {
			return nil, err
		}
		if !expectedRevision.Valid || int64(current) != expectedRevision.Int64 {
			return nil, errors.New("stale proposal plan revision")
		}
	}
	var executingAttempts int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM planning_attempts WHERE expected_plan_id=? AND state='executing'`, plan.ID).Scan(&executingAttempts); err != nil {
		return nil, err
	}
	if executingAttempts != 0 {
		return nil, errors.New("planning attempt outcome is unresolved; reconcile it before proposal approval")
	}
	plan.Approved = true
	plan.AuthorizeCriteriaChanges = p.AuthorizeCriteriaChanges
	payload, _ := json.Marshal(plan)
	result, err := e.putPlan(ctx, tx, Envelope{CommandID: cmd.CommandID, ExpectedRevision: cmd.ExpectedRevision, Kind: "plan.put", Payload: payload})
	if err != nil {
		return nil, err
	}
	var planningCharged, planningUnknown int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(a.charged_ms),0),coalesce(sum(a.unknown_ms),0) FROM planning_attempts a LEFT JOIN planning_budget_transfers x ON x.attempt_id=a.id WHERE a.expected_plan_id=? AND a.state IN('completed','failed','unknown') AND x.attempt_id IS NULL`, plan.ID).Scan(&planningCharged, &planningUnknown); err != nil {
		return nil, err
	}
	if planningCharged+planningUnknown > 0 {
		ledgerID := store.Digest([]byte("plan-services\x00" + plan.ID))
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_ledgers(id,scope,plan_id,active_limit_ms,charged_ms,unknown_ms,updated_at) VALUES(?,'plan_services',?,1800000,?,?,?) ON CONFLICT(id) DO UPDATE SET charged_ms=charged_ms+excluded.charged_ms,unknown_ms=unknown_ms+excluded.unknown_ms,revision=revision+1,updated_at=excluded.updated_at`, ledgerID, plan.ID, planningCharged, planningUnknown, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO planning_budget_transfers(attempt_id,plan_id,charged_ms,unknown_ms,transferred_at) SELECT a.id,?,a.charged_ms,a.unknown_ms,? FROM planning_attempts a LEFT JOIN planning_budget_transfers x ON x.attempt_id=a.id WHERE a.expected_plan_id=? AND a.state IN('completed','failed','unknown') AND x.attempt_id IS NULL`, plan.ID, store.Now(), plan.ID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE planning_proposals SET state='applied',approved_at=?,applied_at=? WHERE id=? AND revision=? AND state='proposed'", store.Now(), store.Now(), p.ProposalID, p.ProposalRevision); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE project SET policy_epoch=policy_epoch+1"); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE state='pending' AND json_extract(context_json,'$.proposal_id')=?", store.Now(), p.ProposalID); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Engine) decidePlanningProposal(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var decision struct {
		ProposalID       string `json:"proposal_id"`
		ProposalRevision int    `json:"proposal_revision"`
		Action           string `json:"action"`
		Rationale        string `json:"rationale"`
	}
	if err := store.Decode(cmd.Payload, &decision); err != nil {
		return nil, err
	}
	if !store.SafeID(decision.ProposalID) || decision.ProposalRevision < 1 || (decision.Action != "reject" && decision.Action != "request_revision") {
		return nil, errors.New("exact proposal revision and reject or request_revision action required")
	}
	decision.Rationale = strings.TrimSpace(decision.Rationale)
	if decision.Rationale == "" || len(decision.Rationale) > 4096 {
		return nil, errors.New("bounded human rationale required")
	}
	var state, requestID string
	if err := tx.QueryRowContext(ctx, `SELECT state,coalesce(request_id,'') FROM planning_proposals WHERE id=? AND revision=?`, decision.ProposalID, decision.ProposalRevision).Scan(&state, &requestID); err != nil {
		return nil, err
	}
	if state != "proposed" {
		return nil, errors.New("proposal is no longer pending")
	}
	nextState, requestState := "rejected", "denied"
	if decision.Action == "request_revision" {
		nextState, requestState = "stale", "resolved"
	}
	resultJSON, _ := json.Marshal(map[string]any{
		"action":            decision.Action,
		"proposal_id":       decision.ProposalID,
		"proposal_revision": decision.ProposalRevision,
		"rationale":         decision.Rationale,
		"actor":             "human",
	})
	if _, err := tx.ExecContext(ctx, `UPDATE planning_proposals SET state=? WHERE id=? AND revision=? AND state='proposed'`, nextState, decision.ProposalID, decision.ProposalRevision); err != nil {
		return nil, err
	}
	if requestID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE requests SET state=?,result_json=?,resolved_at=? WHERE id=? AND state='pending'`, requestState, string(resultJSON), store.Now(), requestID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE requests SET state='cancelled',resolved_at=? WHERE state='pending' AND id!=? AND json_extract(context_json,'$.proposal_id')=? AND json_extract(context_json,'$.proposal_revision')=?`, store.Now(), requestID, decision.ProposalID, decision.ProposalRevision); err != nil {
		return nil, err
	}
	return map[string]any{
		"proposal_id":       decision.ProposalID,
		"proposal_revision": decision.ProposalRevision,
		"action":            decision.Action,
		"state":             nextState,
	}, nil
}
