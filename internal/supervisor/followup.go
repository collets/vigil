package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type FollowupRequest struct {
	CommandID        string             `json:"command_id"`
	ExpectedRevision int                `json:"expected_revision"`
	SourceRunID      string             `json:"source_run_id"`
	Kind             string             `json:"kind"` // repair, infrastructure, fresh_context
	ChoiceID         string             `json:"choice_id,omitempty"`
	WallLimitMS      int64              `json:"wall_limit_ms"`
	Verifier         CheckpointVerifier `json:"-"`
}

func PrepareFollowup(ctx context.Context, engine *core.Engine, request FollowupRequest) (PreparedRun, error) {
	var result PreparedRun
	if engine == nil || engine.DB == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.SourceRunID) || request.ExpectedRevision < 1 || request.WallLimitMS <= 0 || request.WallLimitMS > int64((2*time.Hour)/time.Millisecond) {
		return result, errors.New("valid follow-up command, source run, revision and wall limit required")
	}
	if request.Kind != "repair" && request.Kind != "infrastructure" && request.Kind != "fresh_context" {
		return result, errors.New("follow-up kind must be repair, infrastructure, or fresh_context")
	}
	if (request.Kind == "fresh_context" && !store.SafeID(request.ChoiceID)) || (request.Kind != "fresh_context" && request.ChoiceID != "") {
		return result, errors.New("fresh context requires exactly one eligible recovery choice")
	}
	args, _ := json.Marshal(request)
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "execution.followup.prepare", Args: args}
	if receipt, found, err := engine.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &result)
		}
		return result, err
	}
	source, err := LoadPrepared(ctx, engine, request.SourceRunID)
	if err != nil {
		return result, err
	}
	var taskJSON, configJSON, profileJSON, budgetJSON, qualificationJSON, routesJSON string
	var planRevision, taskRevision int
	err = engine.DB.SQL.QueryRowContext(ctx, `SELECT s.task_snapshot_json,s.config_snapshot_json,s.profile_snapshot_json,s.budget_snapshot_json,r.plan_revision,r.task_revision,g.qualification_request_json,g.expected_routes_json FROM run_snapshots s JOIN runs r ON r.id=s.run_id JOIN run_generations g ON g.run_id=r.id WHERE r.id=? ORDER BY g.ordinal DESC LIMIT 1`, request.SourceRunID).Scan(&taskJSON, &configJSON, &profileJSON, &budgetJSON, &planRevision, &taskRevision, &qualificationJSON, &routesJSON)
	if err != nil {
		return result, err
	}
	var taskState string
	var repairLimit, infraLimit int
	if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT state,repair_limit,infra_limit FROM tasks WHERE id=?", source.TaskID).Scan(&taskState, &repairLimit, &infraLimit); err != nil {
		return result, err
	}
	var sourceState, sourceWriter string
	if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", request.SourceRunID).Scan(&sourceState, &sourceWriter); err != nil {
		return result, err
	}
	if sourceWriter != "contained_stopped" || (sourceState != "completed" && sourceState != "failed" && sourceState != "interrupted") {
		return result, errors.New("follow-up requires a terminal source with independently proven writer containment")
	}
	var priorAttempts int
	dbKind := request.Kind
	if request.Kind == "fresh_context" {
		dbKind = "continuation"
	}
	if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE task_id=? AND attempt_kind=?", source.TaskID, dbKind).Scan(&priorAttempts); err != nil {
		return result, err
	}
	switch request.Kind {
	case "repair":
		if taskState != "needs_repair" || priorAttempts >= repairLimit {
			return result, errors.New("repair allowance or task state is ineligible")
		}
	case "infrastructure":
		if priorAttempts >= infraLimit {
			return result, errors.New("infrastructure retry allowance exhausted")
		}
		var submission string
		var events int
		if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT submission_state FROM run_generations WHERE run_id=? ORDER BY ordinal DESC LIMIT 1", request.SourceRunID).Scan(&submission); err != nil {
			return result, err
		}
		if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM normalized_run_events WHERE run_id=?", request.SourceRunID).Scan(&events); err != nil {
			return result, err
		}
		if (submission != "not_attempted" && submission != "proven_not_delivered") || events != 0 {
			return result, errors.New("infrastructure retry requires proof of no delivered prompt or tool effects")
		}
	case "fresh_context":
		var mode, state string
		var choiceRun, checkpointID string
		if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT c.mode,c.state,c.run_id,b.checkpoint_id FROM recovery_choices c JOIN recovery_choice_checkpoints b ON b.choice_id=c.id WHERE c.id=?`, request.ChoiceID).Scan(&mode, &state, &choiceRun, &checkpointID); err != nil || mode != "fresh_context" || state != "eligible" || choiceRun != request.SourceRunID {
			return result, errors.New("fresh-context recovery choice is absent, stale, or ineligible")
		}
		if request.Verifier == nil {
			return result, errors.New("fresh-context preparation requires a checkpoint verifier")
		}
		if err := request.Verifier.VerifyCheckpoint(ctx, checkpointID); err != nil {
			return result, fmt.Errorf("fresh-context checkpoint no longer verifies: %w", err)
		}
	}
	var ledgerCharged, ledgerUnknown, ledgerLimit int64
	var ledgerID string
	if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT id,charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", source.TaskID).Scan(&ledgerID, &ledgerCharged, &ledgerUnknown, &ledgerLimit); err != nil {
		return result, err
	}
	if ledgerCharged+ledgerUnknown >= ledgerLimit {
		_ = persistBudgetExhaustion(ctx, engine, source, "followup_prepare")
		return result, ErrBudgetExhausted
	}
	repositories := append([]core.RepositoryRecord(nil), source.Repositories...)
	for n := range repositories {
		current, err := workspace.Fingerprint(ctx, repositories[n].Root, withoutGit(repositories[n].Baseline.Exclusions))
		if err != nil {
			return result, err
		}
		if current.HeadOID != repositories[n].BaseOID || current.HeadRef != "refs/heads/"+repositories[n].PlanBranch {
			return result, errors.New("follow-up reconstruction refuses branch or HEAD drift")
		}
		repositories[n].Baseline = current
	}
	receipt, err := engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", engine.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != request.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
		}
		if err := tx.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", request.SourceRunID).Scan(&sourceState, &sourceWriter); err != nil {
			return nil, err
		}
		if sourceWriter != "contained_stopped" || (sourceState != "completed" && sourceState != "failed" && sourceState != "interrupted") {
			return nil, errors.New("source writer safety changed before follow-up preparation")
		}
		if request.Kind == "fresh_context" {
			var state string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM recovery_choices WHERE id=?", request.ChoiceID).Scan(&state); err != nil || state != "eligible" {
				return nil, errors.New("fresh recovery choice changed before attempt preparation")
			}
		}
		runID, generationID, transport := store.ID(), store.ID(), store.ID()
		runtimeResource := "vigil:" + runID + ":1"
		container := ""
		if source.RuntimeKind == "docker" {
			container = "vigil-" + runID + "-g1"
		}
		result = PreparedRun{RunID: runID, GenerationID: generationID, TransportGeneration: transport, RuntimeResourceID: runtimeResource, ContainerName: container, RuntimeKind: source.RuntimeKind, AttemptKind: dbKind, PlanID: source.PlanID, TaskID: source.TaskID, ProfileID: source.ProfileID, ProfileRevision: source.ProfileRevision, ProfileDigest: source.ProfileDigest, EndpointID: source.EndpointID, ExpectedRevision: revision + 1, ActiveLimitMS: source.ActiveLimitMS, WallLimitMS: request.WallLimitMS, Task: source.Task, Repositories: repositories, Eligibility: source.Eligibility, ExpectedRoutes: source.ExpectedRoutes}
		repositoriesJSON, _ := json.Marshal(repositories)
		snapshotValue := map[string]json.RawMessage{"task": json.RawMessage(taskJSON), "config": json.RawMessage(configJSON), "profile": json.RawMessage(profileJSON), "budget": json.RawMessage(budgetJSON), "repositories": repositoriesJSON}
		snapshotJSON, _ := json.Marshal(snapshotValue)
		var configID string
		if err := tx.QueryRowContext(ctx, "SELECT config_id FROM runs WHERE id=?", request.SourceRunID).Scan(&configID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES(?,?,?,?,?,?,?,?,'implementation',?,'prepared','unconfirmed',?,?,?)`, runID, source.PlanID, planRevision, source.TaskID, taskRevision, configID, source.ProfileID, source.ProfileRevision, dbKind, source.ActiveLimitMS, request.WallLimitMS, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_snapshots VALUES(?,?,?,?,?,?,?,?,?,?,?)`, runID, taskJSON, configJSON, profileJSON, budgetJSON, string(repositoriesJSON), revision, planRevision, taskRevision, store.Digest(snapshotJSON), store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,container_name,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES(?,?,?,?,?,?,?,'prepared','not_attempted',?,?,?,?)`, generationID, runID, 1, source.RuntimeKind, runtimeResource, nullableString(container), transport, qualificationJSON, store.Digest(repositoriesJSON), routesJSON, store.Now()); err != nil {
			return nil, err
		}
		if request.Kind == "fresh_context" {
			if _, err := tx.ExecContext(ctx, "UPDATE recovery_choices SET state='consumed',consumed_at=? WHERE id=? AND state='eligible'", store.Now(), request.ChoiceID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_attempt_links(choice_id,source_run_id,prepared_run_id,prepared_generation_id,mode,expected_project_revision,created_at) VALUES(?,?,?,?,'fresh_context',?,?)`, request.ChoiceID, request.SourceRunID, runID, generationID, revision, store.Now()); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='running',block_reason=NULL WHERE id=?", source.TaskID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE id=?", source.PlanID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET state='ready',revision=revision+1 WHERE id=?", engine.ProjectID); err != nil {
			return nil, err
		}
		_ = ledgerID
		return result, nil
	})
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(receipt, &result); err != nil {
		return result, err
	}
	sort.Slice(result.Repositories, func(i, j int) bool { return result.Repositories[i].ID < result.Repositories[j].ID })
	return result, nil
}
