package supervisor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"vigil/internal/artifacts"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type HistoryObservation struct {
	State               string `json:"state"` // readable, missing, corrupt, unsupported
	RecoveryClass       string `json:"recovery_class"`
	NativeHomeRef       string `json:"native_home_ref"`
	NativeSessionID     string `json:"native_session_id"`
	ProfileDigest       string `json:"profile_digest"`
	WorkspaceIdentity   string `json:"workspace_identity"`
	TransportGeneration string `json:"transport_generation"`
	AutomaticWork       bool   `json:"automatic_work"`
	Qualification       string `json:"qualification"` // supported, unsupported, unverified, synthetic
}

type HistoryInspector interface {
	InspectHistory(context.Context, PreparedRun) (HistoryObservation, error)
}

type CheckpointVerifier interface {
	VerifyCheckpoint(context.Context, string) error
}

type CurrentCheckpointVerifier interface {
	CheckpointVerifier
	VerifyCheckpointCurrent(context.Context, string) error
}

type RecoveryInspector interface {
	HistoryInspector
	CheckpointVerifier
}

type CheckpointVerifierFunc func(context.Context, string) error

func (f CheckpointVerifierFunc) VerifyCheckpoint(ctx context.Context, checkpointID string) error {
	return f(ctx, checkpointID)
}

type CheckpointVerifierPair struct {
	Verify        func(context.Context, string) error
	VerifyCurrent func(context.Context, string) error
}

func (v CheckpointVerifierPair) VerifyCheckpoint(ctx context.Context, checkpointID string) error {
	if v.Verify == nil {
		return errors.New("checkpoint verifier is unavailable")
	}
	return v.Verify(ctx, checkpointID)
}

func (v CheckpointVerifierPair) VerifyCheckpointCurrent(ctx context.Context, checkpointID string) error {
	if v.VerifyCurrent == nil {
		return errors.New("current checkpoint verifier is unavailable")
	}
	return v.VerifyCurrent(ctx, checkpointID)
}

type RecoveryEligibility struct {
	ExactResume        bool                    `json:"exact_resume"`
	FreshContext       bool                    `json:"fresh_context"`
	CheckpointID       string                  `json:"checkpoint_id,omitempty"`
	ResumeRepositories []core.RepositoryRecord `json:"resume_repositories,omitempty"`
	Reasons            []string                `json:"reasons"`
}

type RecoveryChoiceRequest struct {
	CommandID        string `json:"command_id"`
	ExpectedRevision int    `json:"expected_revision"`
	RunID            string `json:"run_id"`
	Mode             string `json:"mode"`
}

type RecoveryChoiceReceipt struct {
	ChoiceID          string              `json:"choice_id"`
	RunID             string              `json:"run_id"`
	Mode              string              `json:"mode"`
	State             string              `json:"state"`
	Eligibility       RecoveryEligibility `json:"eligibility"`
	ContextArtifactID string              `json:"context_artifact_id,omitempty"`
	Repeated          bool                `json:"repeated,omitempty"`
}

type ExactResumeRequest struct {
	CommandID        string `json:"command_id"`
	ExpectedRevision int    `json:"expected_revision"`
	ChoiceID         string `json:"choice_id"`
}

func (r *Runner) RecoveryEligibility(ctx context.Context, prepared PreparedRun, inspector RecoveryInspector) (RecoveryEligibility, HistoryObservation, error) {
	var result RecoveryEligibility
	if r == nil || r.Engine == nil || r.Engine.DB == nil || inspector == nil {
		return result, HistoryObservation{}, errors.New("persisted run and trusted history inspector required")
	}
	observation, err := inspector.InspectHistory(ctx, prepared)
	if err != nil {
		return result, observation, err
	}
	if observation.State != "readable" && observation.State != "missing" && observation.State != "corrupt" && observation.State != "unsupported" {
		return result, observation, errors.New("invalid native history observation")
	}
	var runState, writerState, nativeHome, nativeSession, profileDigest, workspaceIdentity, transport string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", prepared.RunID).Scan(&runState, &writerState); err != nil {
		return result, observation, err
	}
	err = r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT s.native_home_ref,coalesce(s.durable_id,''),s.profile_digest,s.workspace_identity,s.generation FROM sessions s WHERE s.run_id=? ORDER BY s.rowid DESC LIMIT 1`, prepared.RunID).Scan(&nativeHome, &nativeSession, &profileDigest, &workspaceIdentity, &transport)
	if errors.Is(err, sql.ErrNoRows) {
		result.Reasons = append(result.Reasons, "no application-owned native session is recorded")
	} else if err != nil {
		return result, observation, err
	}
	if writerState != "contained_stopped" {
		result.Reasons = append(result.Reasons, "writer containment is not independently proven")
	}
	if runState == "completed" {
		result.Reasons = append(result.Reasons, "completed native outcome is not resumable task authority")
	}
	exactIdentity := err == nil && observation.NativeHomeRef == nativeHome && observation.NativeSessionID == nativeSession && observation.ProfileDigest == profileDigest && observation.WorkspaceIdentity == workspaceIdentity && observation.TransportGeneration == transport
	qualified := prepared.RuntimeKind == "synthetic" && observation.Qualification == "synthetic"
	if prepared.RuntimeKind != "synthetic" && observation.Qualification == "supported" && prepared.Eligibility != nil && stringSetContains(prepared.Eligibility.RequiredRecoveryClasses, "exact_resume:"+observation.RecoveryClass) {
		qualified = true
	}
	if observation.State == "readable" && exactIdentity && qualified && !observation.AutomaticWork && writerState == "contained_stopped" && runState != "completed" {
		result.ExactResume = true
	} else {
		if observation.State != "readable" {
			result.Reasons = append(result.Reasons, "native history is "+observation.State)
		}
		if !exactIdentity {
			result.Reasons = append(result.Reasons, "native home/session/profile/workspace/generation identity does not match")
		}
		if !qualified {
			result.Reasons = append(result.Reasons, "history recovery class is not qualified")
		}
		if observation.AutomaticWork {
			result.Reasons = append(result.Reasons, "resumed native history has automatic or queued work")
		}
	}
	rows, err := r.Engine.DB.SQL.QueryContext(ctx, `SELECT id FROM checkpoint_sets WHERE run_id=? AND state IN('verified','saved','clearing','restoring','conflicted','restored') AND manifest_id IS NOT NULL ORDER BY created_at DESC,id DESC`, prepared.RunID)
	if err != nil {
		return result, observation, err
	}
	var checkpointIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, observation, err
		}
		checkpointIDs = append(checkpointIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, observation, err
	}
	rows.Close()
	if writerState == "contained_stopped" {
		for _, id := range checkpointIDs {
			if verifyErr := inspector.VerifyCheckpoint(ctx, id); verifyErr == nil {
				result.FreshContext, result.CheckpointID = true, id
				break
			}
		}
	}
	if result.ExactResume {
		currentVerifier, ok := inspector.(CurrentCheckpointVerifier)
		if !result.FreshContext || !ok {
			result.ExactResume = false
			result.Reasons = append(result.Reasons, "exact resume requires a verified current checkpoint workspace")
		} else if verifyErr := currentVerifier.VerifyCheckpointCurrent(ctx, result.CheckpointID); verifyErr != nil {
			result.ExactResume = false
			result.Reasons = append(result.Reasons, "exact resume checkpoint no longer matches the current workspace")
		} else {
			result.ResumeRepositories = append([]core.RepositoryRecord(nil), prepared.Repositories...)
			for n := range result.ResumeRepositories {
				current, fingerprintErr := workspace.Fingerprint(ctx, result.ResumeRepositories[n].Root, withoutGit(result.ResumeRepositories[n].Baseline.Exclusions))
				if fingerprintErr != nil {
					return result, observation, fingerprintErr
				}
				result.ResumeRepositories[n].Baseline = current
			}
		}
	}
	if !result.FreshContext {
		result.Reasons = append(result.Reasons, "fresh reconstruction requires contained writers and a verified checkpoint set")
	}
	sort.Strings(result.Reasons)
	result.Reasons = uniqueStrings(result.Reasons)
	return result, observation, nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func (r *Runner) ChooseRecovery(ctx context.Context, request RecoveryChoiceRequest, inspector RecoveryInspector) (RecoveryChoiceReceipt, error) {
	var receipt RecoveryChoiceReceipt
	if !store.SafeID(request.CommandID) || !store.SafeID(request.RunID) || request.ExpectedRevision < 1 || (request.Mode != "exact_resume" && request.Mode != "fresh_context" && request.Mode != "remain_blocked") {
		return receipt, errors.New("valid recovery command, run, revision and explicit mode required")
	}
	prepared, err := LoadPrepared(ctx, r.Engine, request.RunID)
	if err != nil {
		return receipt, err
	}
	args, _ := json.Marshal(request)
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "execution.recovery.choose", Args: args}
	commandReceipt, repeated, err := r.Engine.DB.Receipt(ctx, command)
	if err != nil {
		return receipt, err
	}
	if !repeated {
		commandReceipt, err = r.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
			var revision int
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", r.Engine.ProjectID).Scan(&revision); err != nil {
				return nil, err
			}
			if revision != request.ExpectedRevision {
				return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
			}
			choiceID := store.ID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_choices(id,run_id,source_generation_id,mode,state,eligibility_json,created_at) VALUES(?,?,?,?,'prepared','{}',?)`, choiceID, request.RunID, prepared.GenerationID, request.Mode, store.Now()); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE run_id=? AND state='pending'", store.Now(), request.RunID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='recovering',revision=revision+1 WHERE id=?", r.Engine.ProjectID); err != nil {
				return nil, err
			}
			return RecoveryChoiceReceipt{ChoiceID: choiceID, RunID: request.RunID, Mode: request.Mode, State: "prepared"}, nil
		})
		if err != nil {
			return receipt, err
		}
	}
	if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
		return receipt, err
	}
	receipt.Repeated = repeated
	var persistedState, eligibilityJSON string
	var artifact sql.NullString
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state,eligibility_json,context_artifact_id FROM recovery_choices WHERE id=?", receipt.ChoiceID).Scan(&persistedState, &eligibilityJSON, &artifact); err != nil {
		return receipt, err
	}
	if persistedState == "eligible" || persistedState == "ineligible" || persistedState == "consumed" {
		receipt.State = persistedState
		var persisted struct {
			Eligibility RecoveryEligibility `json:"eligibility"`
		}
		if err := json.Unmarshal([]byte(eligibilityJSON), &persisted); err != nil {
			return receipt, err
		}
		receipt.Eligibility = persisted.Eligibility
		if artifact.Valid {
			receipt.ContextArtifactID = artifact.String
		}
		return receipt, nil
	}
	var eligibility RecoveryEligibility
	var observation HistoryObservation
	var inspectErr error
	if request.Mode != "remain_blocked" {
		if inspector == nil {
			return receipt, errors.New("qualified recovery inspector required for resume choices")
		}
		eligibility, observation, inspectErr = r.RecoveryEligibility(ctx, prepared, inspector)
	}
	receipt.Eligibility = eligibility
	eligible := request.Mode == "exact_resume" && eligibility.ExactResume || request.Mode == "fresh_context" && eligibility.FreshContext
	eligibilityBytes, _ := json.Marshal(map[string]any{"eligibility": eligibility, "history": observation, "inspection_error": errorString(inspectErr)})
	state := "ineligible"
	if request.Mode == "remain_blocked" {
		state = "consumed"
	} else if eligible && inspectErr == nil {
		state = "eligible"
	}
	contextArtifactID := ""
	if state == "eligible" && request.Mode == "fresh_context" {
		freshContext, err := r.freshContext(ctx, prepared, eligibility, observation)
		if err != nil {
			return receipt, err
		}
		artifactRepository, err := artifacts.New(r.Engine.DB)
		if err != nil {
			return receipt, err
		}
		artifact, err := artifactRepository.PutCore(ctx, "recovery-context:"+receipt.ChoiceID, "fresh-recovery-context", "unfinished", bytes.NewReader(freshContext))
		if err != nil {
			return receipt, err
		}
		contextArtifactID = artifact.ID
	}
	err = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_choices SET state=?,eligibility_json=?,context_artifact_id=? WHERE id=? AND state='prepared'", state, string(eligibilityBytes), nullableString(contextArtifactID), receipt.ChoiceID); err != nil {
			return err
		}
		if state == "eligible" && (request.Mode == "fresh_context" || request.Mode == "exact_resume") {
			_, err := tx.ExecContext(ctx, "INSERT INTO recovery_choice_checkpoints(choice_id,checkpoint_id,created_at) VALUES(?,?,?)", receipt.ChoiceID, eligibility.CheckpointID, store.Now())
			return err
		}
		if request.Mode == "remain_blocked" {
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='paused' WHERE id=?", r.Engine.ProjectID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return receipt, err
	}
	receipt.State, receipt.ContextArtifactID = state, contextArtifactID
	if state == "ineligible" {
		return receipt, errors.New("selected recovery mode is not eligible; run remains blocked")
	}
	return receipt, nil
}

func (r *Runner) freshContext(ctx context.Context, prepared PreparedRun, eligibility RecoveryEligibility, history HistoryObservation) ([]byte, error) {
	fingerprints := map[string]workspace.Baseline{}
	for _, repository := range prepared.Repositories {
		fingerprint, err := workspace.Fingerprint(ctx, repository.Root, withoutGit(repository.Baseline.Exclusions))
		if err != nil {
			return nil, err
		}
		fingerprints[repository.ID] = fingerprint
	}
	var runState, writerState, submissionState string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT r.state,r.writer_state,g.submission_state FROM runs r JOIN run_generations g ON g.run_id=r.id WHERE r.id=? ORDER BY g.ordinal DESC LIMIT 1`, prepared.RunID).Scan(&runState, &writerState, &submissionState); err != nil {
		return nil, err
	}
	var charged, unknown, limit int64
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&charged, &unknown, &limit); err != nil {
		return nil, err
	}
	var checks, findings int
	_ = r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM quality_evidence WHERE task_id=? AND kind='check'", prepared.TaskID).Scan(&checks)
	_ = r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM findings f JOIN quality_evidence q ON q.id=f.evidence_id WHERE q.task_id=?`, prepared.TaskID).Scan(&findings)
	value := map[string]any{
		"schema_version": 1, "source_run_id": prepared.RunID, "source_generation_id": prepared.GenerationID,
		"task": prepared.Task, "repository_fingerprints": fingerprints, "run_state": runState, "writer_state": writerState,
		"submission_state": submissionState, "history": history, "eligibility": eligibility,
		"quality_evidence":  map[string]int{"checks": checks, "findings": findings},
		"cumulative_budget": map[string]int64{"charged_ms": charged, "unknown_ms": unknown, "limit_ms": limit, "remaining_ms": maxInt64(0, limit-charged-unknown)},
		"warning":           "This is a new recorded attempt. It is not native resume and must not replay an uncertain submission.",
	}
	return json.Marshal(value)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (r *Runner) PrepareExactResume(ctx context.Context, request ExactResumeRequest) (PreparedRun, error) {
	var result PreparedRun
	if r == nil || r.Engine == nil || r.Engine.DB == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.ChoiceID) || request.ExpectedRevision < 1 {
		return result, errors.New("valid exact-resume command, choice and revision required")
	}
	args, _ := json.Marshal(request)
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "execution.exact_resume.prepare", Args: args}
	if receipt, found, err := r.Engine.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &result)
		}
		return result, err
	}
	var sourceRunID, sourceGenerationID, mode, state string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT run_id,source_generation_id,mode,state FROM recovery_choices WHERE id=?", request.ChoiceID).Scan(&sourceRunID, &sourceGenerationID, &mode, &state); err != nil || mode != "exact_resume" || state != "eligible" {
		return result, errors.New("exact-resume choice is absent, stale, or ineligible")
	}
	source, err := LoadPrepared(ctx, r.Engine, sourceRunID)
	if err != nil {
		return result, err
	}
	var createdAt, wallLimit, charged, unknown, ledgerLimit int64
	var writerState, runState, nativeSession string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT r.created_at,r.wall_limit_ms,r.writer_state,r.state,coalesce(g.native_session_id,''),l.charged_ms,l.unknown_ms,l.active_limit_ms FROM runs r JOIN run_generations g ON g.id=? JOIN budget_ledgers l ON l.scope='task' AND l.task_id=r.task_id WHERE r.id=?`, sourceGenerationID, sourceRunID).Scan(&createdAt, &wallLimit, &writerState, &runState, &nativeSession, &charged, &unknown, &ledgerLimit); err != nil {
		return result, err
	}
	if writerState != "contained_stopped" || runState == "completed" || nativeSession == "" {
		return result, errors.New("exact resume requires a non-completed run, contained writers and an exact native session")
	}
	var checkpointID, eligibilityJSON string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT b.checkpoint_id,c.eligibility_json FROM recovery_choices c JOIN recovery_choice_checkpoints b ON b.choice_id=c.id WHERE c.id=?`, request.ChoiceID).Scan(&checkpointID, &eligibilityJSON); err != nil {
		return result, errors.New("exact resume lacks bound checkpoint workspace authority")
	}
	var persisted struct {
		Eligibility RecoveryEligibility `json:"eligibility"`
	}
	if err := json.Unmarshal([]byte(eligibilityJSON), &persisted); err != nil || persisted.Eligibility.CheckpointID != checkpointID || len(persisted.Eligibility.ResumeRepositories) == 0 {
		return result, errors.New("exact resume workspace authority is invalid")
	}
	for _, repository := range persisted.Eligibility.ResumeRepositories {
		current, err := workspace.Fingerprint(ctx, repository.Root, withoutGit(repository.Baseline.Exclusions))
		if err != nil || !snapshotEqualValue(current, repository.Baseline) {
			return result, fmt.Errorf("exact-resume checkpoint workspace changed for repository %s", repository.ID)
		}
	}
	resumeAt := store.Now()
	remainingWall := createdAt + wallLimit - resumeAt
	if remainingWall <= 0 {
		return result, errors.New("original attempt wall limit is exhausted")
	}
	if charged+unknown >= ledgerLimit {
		_ = persistBudgetExhaustion(ctx, r.Engine, source, "exact_resume_prepare")
		return result, ErrBudgetExhausted
	}
	receipt, err := r.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", r.Engine.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != request.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
		}
		if err := tx.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", sourceRunID).Scan(&runState, &writerState); err != nil {
			return nil, err
		}
		if writerState != "contained_stopped" || runState == "completed" {
			return nil, errors.New("exact-resume writer safety changed before preparation")
		}
		var currentChoice string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM recovery_choices WHERE id=?", request.ChoiceID).Scan(&currentChoice); err != nil || currentChoice != "eligible" {
			return nil, errors.New("exact-resume choice changed before preparation")
		}
		var ordinal int
		var runtimeKind, qualificationJSON, routesJSON, submissionState string
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(ordinal),0)+1 FROM run_generations WHERE run_id=?`, sourceRunID).Scan(&ordinal); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT runtime_kind,qualification_request_json,expected_routes_json,submission_state FROM run_generations WHERE id=?`, sourceGenerationID).Scan(&runtimeKind, &qualificationJSON, &routesJSON, &submissionState); err != nil {
			return nil, err
		}
		generationID, transport := store.ID(), store.ID()
		runtimeResource := fmt.Sprintf("vigil:%s:%d", sourceRunID, ordinal)
		container := ""
		if runtimeKind == "docker" {
			container = fmt.Sprintf("vigil-%s-g%d", sourceRunID, ordinal)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,container_name,native_session_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) SELECT ?,?, ?,?,?,?, ?,?,'prepared',?,?,checkout_plan_digest,?,? FROM run_generations WHERE id=?`, generationID, sourceRunID, ordinal, runtimeKind, runtimeResource, nullableString(container), nativeSession, transport, submissionState, qualificationJSON, routesJSON, store.Now(), sourceGenerationID); err != nil {
			return nil, err
		}
		repositoriesJSON, _ := json.Marshal(persisted.Eligibility.ResumeRepositories)
		if _, err := tx.ExecContext(ctx, `INSERT INTO generation_recovery_snapshots(generation_id,checkpoint_id,repository_snapshot_json,digest,created_at) VALUES(?,?,?,?,?)`, generationID, checkpointID, string(repositoriesJSON), store.Digest(repositoriesJSON), store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE runs SET state='prepared',writer_state='unconfirmed',wall_limit_ms=?,created_at=?,ended_at=NULL WHERE id=?", remainingWall, resumeAt, sourceRunID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_choices SET state='consumed',consumed_at=? WHERE id=?", store.Now(), request.ChoiceID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_attempt_links(choice_id,source_run_id,prepared_run_id,prepared_generation_id,mode,expected_project_revision,created_at) VALUES(?,?,?,?,'exact_resume',?,?)`, request.ChoiceID, sourceRunID, sourceRunID, generationID, revision, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='running',block_reason=NULL WHERE id=?", source.TaskID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE id=?", source.PlanID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET state='ready',revision=revision+1 WHERE id=?", r.Engine.ProjectID); err != nil {
			return nil, err
		}
		result = source
		result.Repositories = append([]core.RepositoryRecord(nil), persisted.Eligibility.ResumeRepositories...)
		result.GenerationID, result.TransportGeneration, result.RuntimeResourceID, result.ContainerName = generationID, transport, runtimeResource, container
		result.ExpectedRevision, result.WallLimitMS, result.ExactResume, result.ResumeNativeSession = revision+1, remainingWall, true, nativeSession
		return result, nil
	})
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(receipt, &result)
	return result, err
}
