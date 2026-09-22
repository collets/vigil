package quality

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type AssessmentDocument struct {
	SchemaVersion int    `json:"schema_version"`
	Action        string `json:"action"`
	Rationale     string `json:"rationale"`
}
type Assessor interface {
	Assess(context.Context, Scope) ([]byte, error)
}
type AssessmentRequest struct {
	CommandID  string `json:"command_id"`
	Target     Target `json:"target"`
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id,omitempty"`
	Actor      string `json:"actor"`
}
type Assessment struct {
	ID             string `json:"id"`
	EffectID       string `json:"effect_id"`
	ScopeID        string `json:"scope_id"`
	SourceKind     string `json:"source_kind"`
	SourceID       string `json:"source_id,omitempty"`
	Action         string `json:"action"`
	Rationale      string `json:"rationale"`
	ArtifactID     string `json:"artifact_id"`
	ArtifactDigest string `json:"artifact_digest"`
	AssessedAt     int64  `json:"assessed_at"`
}

func RunAssessment(ctx context.Context, engine *core.Engine, owner *coordinator.Owner, assessor Assessor, request AssessmentRequest) (Assessment, error) {
	var result Assessment
	if engine == nil || engine.DB == nil || owner == nil || assessor == nil || !store.SafeID(request.CommandID) || request.Target.Kind != "task" || request.Actor != "fixture" || !contains([]string{"repair_exhaustion", "budget_exhaustion"}, request.SourceKind) {
		return result, errors.New("bounded fixture supervisor assessment required")
	}
	scope, err := Observe(ctx, engine, request.Target)
	if err != nil {
		return result, err
	}
	if !fixtureScope(scope) {
		return result, errors.New("fixture assessment requires disposable repositories")
	}
	if request.SourceKind == "budget_exhaustion" && !store.SafeID(request.SourceID) {
		return result, errors.New("budget exhaustion evidence ID required")
	}
	if request.SourceKind == "repair_exhaustion" && request.SourceID != "" {
		return result, errors.New("repair exhaustion has no caller-supplied source ID")
	}
	sourceID := request.SourceID
	if request.SourceKind == "repair_exhaustion" {
		sourceID = fmt.Sprintf("repair-limit-r%d", scope.TaskRevision)
	}
	if _, err = RemainingBudgetMS(ctx, engine, scope); err != nil {
		return result, errors.New("no task budget remains for a supervisor assessment; task stays blocked")
	}
	var eligible int
	if request.SourceKind == "repair_exhaustion" {
		err = engine.DB.SQL.QueryRowContext(ctx, `SELECT CASE WHEN t.state IN('needs_repair','blocked') AND (SELECT count(*) FROM runs r WHERE r.task_id=t.id AND r.attempt_kind='repair')>=t.repair_limit THEN 1 ELSE 0 END FROM tasks t WHERE t.id=?`, scope.Target.TaskID).Scan(&eligible)
	} else {
		err = engine.DB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM budget_exhaustions WHERE id=? AND task_id=? AND state='pending_supervisor'`, request.SourceID, scope.Target.TaskID).Scan(&eligible)
	}
	if err != nil {
		return result, err
	}
	if eligible != 1 {
		return result, errors.New("no matching persisted exhaustion is eligible")
	}
	if err = Persist(ctx, engine, scope); err != nil {
		return result, err
	}
	effectID := store.Digest([]byte("quality.supervisor\x00" + request.CommandID))
	var existingID string
	if queryErr := engine.DB.SQL.QueryRowContext(ctx, "SELECT id FROM supervisor_assessments_v2 WHERE effect_id=?", effectID).Scan(&existingID); queryErr == nil {
		return loadAssessment(ctx, engine, existingID)
	} else if !errors.Is(queryErr, sql.ErrNoRows) {
		return result, queryErr
	}
	roots := make([]workspace.Identity, 0, len(scope.Repositories))
	for _, repository := range scope.Repositories {
		roots = append(roots, repository.Identity)
	}
	claims, err := owner.Claims(ctx, engine.ProjectID, roots)
	ownedForAssessment := false
	if err != nil {
		claims, err = owner.Claim(ctx, engine.ProjectID, store.Digest([]byte("quality.supervisor.claim\x00"+request.CommandID)), roots)
		ownedForAssessment = err == nil
	}
	if err != nil {
		return result, err
	}
	releaseAllowed := true
	defer func() {
		if ownedForAssessment && releaseAllowed {
			for _, claim := range claims {
				_ = owner.Release(context.Background(), claim, "contained_stopped")
			}
		}
	}()
	definitionID := request.SourceKind + ":" + sourceID
	intent, _ := json.Marshal(request)
	if err = engine.DB.Write(ctx, func(tx *store.Tx) error {
		if err := EnsureTargetDispatchable(ctx, tx, scope.Target); err != nil {
			return err
		}
		var projectState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", engine.ProjectID).Scan(&projectState); err != nil {
			return err
		}
		if projectState != "ready" {
			return errors.New("project state prevents supervisor dispatch")
		}
		started := store.Now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO quality_effects_v2(id,scope_id,kind,definition_id,definition_digest,actor,state,intent_json,prepared_at,started_at) VALUES(?,?,'supervisor_assessment',?,?,?,'executing',?,?,?)`, effectID, scope.ID, definitionID, store.Digest(intent), request.Actor, string(intent), started, started); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO quality_assessment_sources_v2(scope_id,plan_id,task_id,source_kind,source_id,effect_id,command_id,state,reserved_at) VALUES(?,?,?,?,?,?,?,'reserved',?)`, scope.ID, scope.Target.PlanID, scope.Target.TaskID, request.SourceKind, sourceID, effectID, request.CommandID, started); err != nil {
			return err
		}
		return StartBudgetSegment(ctx, tx, scope, effectID, started)
	}); err != nil {
		return result, err
	}
	releaseAllowed = false
	remaining, err := RemainingBudgetMS(ctx, engine, scope)
	if err != nil {
		return result, err
	}
	limit := scope.Config.AttemptLimitMS
	if remaining < limit {
		limit = remaining
	}
	assessmentCtx, cancel := context.WithTimeout(ctx, time.Duration(limit)*time.Millisecond)
	defer cancel()
	_, err = EffectStartedAt(ctx, engine, effectID)
	if err != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, err.Error())
		return result, err
	}
	var raw []byte
	var callErr error
	err = owner.HoldClaims(ctx, engine.ProjectID, roots, claims, func() error {
		raw, callErr = assessor.Assess(assessmentCtx, scope)
		return nil
	})
	ended := store.Now()
	if err != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, err.Error())
		return result, err
	}
	if callErr != nil || assessmentCtx.Err() != nil {
		reason := "supervisor assessment timed out or was interrupted"
		if callErr != nil {
			reason = callErr.Error()
		}
		_ = MarkEffectUncertain(context.Background(), engine, effectID, reason)
		return result, errors.New("supervisor assessment failed and cannot be replayed: " + reason)
	}
	var document AssessmentDocument
	if err = store.Decode(raw, &document); err != nil || document.SchemaVersion != 1 || !contains([]string{"clarify", "revise_or_split", "eligible_reassignment", "remain_blocked"}, document.Action) || document.Rationale == "" {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, "malformed supervisor assessment")
		return result, errors.New("malformed supervisor assessment cannot change task state")
	}
	if exhausted, exhaustionErr := WouldExhaust(ctx, engine, effectID, ended); exhaustionErr != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, exhaustionErr.Error())
		return result, exhaustionErr
	} else if exhausted {
		document.Action = "remain_blocked"
		document.Rationale = "cumulative quality budget exhausted; " + document.Rationale
		raw, _ = json.Marshal(document)
		raw, _ = store.Canonical(raw)
	}
	repository, err := artifacts.New(engine.DB)
	if err != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, err.Error())
		return result, err
	}
	artifact, err := repository.PutCore(ctx, store.Digest([]byte(effectID+"\x00result")), "supervisor-assessment", "durable", bytes.NewReader(raw))
	if err != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, err.Error())
		return result, err
	}
	result = Assessment{ID: store.ID(), EffectID: effectID, ScopeID: scope.ID, SourceKind: request.SourceKind, SourceID: sourceID, Action: document.Action, Rationale: document.Rationale, ArtifactID: artifact.ID, ArtifactDigest: artifact.Digest, AssessedAt: ended}
	observation, _ := json.Marshal(result)
	err = engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO supervisor_assessments_v2(id,effect_id,scope_id,source_kind,source_id,action,rationale,result_artifact_id,result_artifact_digest,actor,assessed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, result.ID, effectID, scope.ID, request.SourceKind, sourceID, result.Action, result.Rationale, artifact.ID, artifact.Digest, request.Actor, ended); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='observed',observation_json=?,observed_at=? WHERE id=? AND state='executing'", string(observation), ended, effectID); err != nil {
			return err
		}
		if _, err := FinishBudgetSegment(ctx, tx, effectID, ended); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quality_assessment_sources_v2 SET state='observed',observed_at=? WHERE effect_id=? AND state='reserved'", ended, effectID); err != nil {
			return err
		}
		if request.SourceKind == "budget_exhaustion" {
			if _, err := tx.ExecContext(ctx, "UPDATE budget_exhaustions SET state='assessed' WHERE id=? AND state='pending_supervisor'", request.SourceID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE tasks SET state='blocked',block_reason=? WHERE id=? AND revision=? AND state IN('needs_repair','blocked')", "bounded supervisor assessment: "+result.Action, scope.Target.TaskID, scope.TaskRevision)
		return err
	})
	if err != nil {
		_ = MarkEffectUncertain(context.Background(), engine, effectID, err.Error())
	}
	if err == nil {
		releaseAllowed = true
	}
	return result, err
}

func loadAssessment(ctx context.Context, engine *core.Engine, id string) (Assessment, error) {
	var result Assessment
	err := engine.DB.SQL.QueryRowContext(ctx, `SELECT id,effect_id,scope_id,source_kind,source_id,action,rationale,result_artifact_id,result_artifact_digest,assessed_at FROM supervisor_assessments_v2 WHERE id=?`, id).Scan(&result.ID, &result.EffectID, &result.ScopeID, &result.SourceKind, &result.SourceID, &result.Action, &result.Rationale, &result.ArtifactID, &result.ArtifactDigest, &result.AssessedAt)
	return result, err
}
