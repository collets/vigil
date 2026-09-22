package quality

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"vigil/internal/artifacts"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type ManualRequest struct {
	CommandID   string `json:"command_id"`
	Target      Target `json:"target"`
	CriterionID string `json:"criterion_id"`
	State       string `json:"state"`
	Evaluator   string `json:"evaluator"`
	Notes       string `json:"notes"`
	ArtifactID  string `json:"artifact_id,omitempty"`
	Actor       string `json:"actor"`
}

type HumanDecisionRequest struct {
	CommandID string `json:"command_id"`
	Target    Target `json:"target"`
	Action    string `json:"action"`
	Rationale string `json:"rationale"`
	Actor     string `json:"actor"`
}

func validFixtureActor(scope Scope, actor string) bool {
	return actor == "human" || actor == "fixture_human" && fixtureScope(scope)
}

func RecordManual(ctx context.Context, engine *core.Engine, request ManualRequest) (string, error) {
	if engine == nil || engine.DB == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.CriterionID) || !validManualState(request.State) || strings.TrimSpace(request.Evaluator) == "" || strings.TrimSpace(request.Notes) == "" {
		return "", errors.New("complete typed manual result required")
	}
	scope, err := Observe(ctx, engine, request.Target)
	if err != nil {
		return "", err
	}
	if !validFixtureActor(scope, request.Actor) {
		return "", errors.New("manual result requires human authority")
	}
	manual := false
	for _, criterion := range scope.Criteria {
		if criterion.ID == request.CriterionID && criterion.Manual {
			manual = true
		}
	}
	if !manual {
		return "", errors.New("criterion is not a configured manual gate")
	}
	if err := Persist(ctx, engine, scope); err != nil {
		return "", err
	}
	if err := DetectAndRecordStaleness(ctx, engine, scope); err != nil {
		return "", err
	}
	artifactDigest := ""
	if request.ArtifactID != "" {
		repository, err := artifacts.New(engine.DB)
		if err != nil {
			return "", err
		}
		content, err := repository.Read(ctx, request.ArtifactID)
		if err != nil {
			return "", err
		}
		artifactDigest = store.Digest(content)
	}
	args, _ := json.Marshal(request)
	receipt, err := engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: request.Actor, Kind: "quality.manual.record", Args: args}, func(tx *store.Tx) (any, error) {
		id := store.ID()
		_, err := tx.ExecContext(ctx, `INSERT INTO manual_results_v2(id,scope_id,criterion_id,state,evaluator,notes,artifact_id,artifact_digest,actor,command_id,evaluated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, scope.ID, request.CriterionID, request.State, request.Evaluator, request.Notes, nullable(request.ArtifactID), nullable(artifactDigest), request.Actor, request.CommandID, store.Now())
		if err != nil {
			return nil, err
		}
		if scope.Target.Kind == "task" && (request.State == "fail" || request.State == "cannot_verify") {
			state, reason := "needs_repair", "manual gate failed"
			if request.State == "cannot_verify" {
				state, reason = "blocked", "manual gate cannot be verified"
			}
			_, err = tx.ExecContext(ctx, "UPDATE tasks SET state=?,block_reason=? WHERE id=? AND state IN('reviewing','awaiting_human')", state, reason, scope.Target.TaskID)
		}
		return map[string]string{"id": id}, err
	})
	if err != nil {
		return "", err
	}
	var result map[string]string
	err = json.Unmarshal(receipt, &result)
	return result["id"], err
}

func validManualState(value string) bool {
	return value == "pending" || value == "pass" || value == "fail" || value == "cannot_verify"
}

func RecordHumanDecision(ctx context.Context, engine *core.Engine, request HumanDecisionRequest) (string, error) {
	if engine == nil || engine.DB == nil || !store.SafeID(request.CommandID) || !contains([]string{"accept", "request_changes", "clarify", "stop"}, request.Action) || strings.TrimSpace(request.Rationale) == "" {
		return "", errors.New("complete typed human decision required")
	}
	scope, err := Observe(ctx, engine, request.Target)
	if err != nil {
		return "", err
	}
	if !validFixtureActor(scope, request.Actor) {
		return "", errors.New("decision requires human authority")
	}
	if err := Persist(ctx, engine, scope); err != nil {
		return "", err
	}
	if err := DetectAndRecordStaleness(ctx, engine, scope); err != nil {
		return "", err
	}
	args, _ := json.Marshal(request)
	receipt, err := engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: request.Actor, Kind: "quality.human.decision", Args: args}, func(tx *store.Tx) (any, error) {
		id := store.ID()
		_, err := tx.ExecContext(ctx, `INSERT INTO human_decisions_v2(id,scope_id,action,actor,rationale,command_id,decided_at) VALUES(?,?,?,?,?,?,?)`, id, scope.ID, request.Action, request.Actor, request.Rationale, request.CommandID, store.Now())
		if err != nil {
			return nil, err
		}
		if scope.Target.Kind == "task" {
			switch request.Action {
			case "request_changes":
				_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='needs_repair',block_reason='human requested changes' WHERE id=?", scope.Target.TaskID)
			case "clarify":
				_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='blocked',block_reason='human clarification requires a criteria revision' WHERE id=?", scope.Target.TaskID)
			case "stop":
				_, err = tx.ExecContext(ctx, "UPDATE tasks SET state='stopped',block_reason='stopped by human quality decision' WHERE id=?", scope.Target.TaskID)
			}
		} else {
			switch request.Action {
			case "request_changes", "clarify":
				_, err = tx.ExecContext(ctx, "UPDATE plans SET state='blocked' WHERE id=?", scope.Target.PlanID)
			case "stop":
				_, err = tx.ExecContext(ctx, "UPDATE plans SET state='stopped' WHERE id=?", scope.Target.PlanID)
			}
		}
		return map[string]string{"id": id}, err
	})
	if err != nil {
		return "", err
	}
	var result map[string]string
	err = json.Unmarshal(receipt, &result)
	return result["id"], err
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type ReviewGate struct {
	ResultID     string `json:"result_id"`
	ResultDigest string `json:"result_digest"`
	Satisfied    bool   `json:"satisfied"`
	Reason       string `json:"reason,omitempty"`
}
type ManualGate struct {
	CriterionID string `json:"criterion_id"`
	ResultID    string `json:"result_id,omitempty"`
	State       string `json:"state,omitempty"`
	Satisfied   bool   `json:"satisfied"`
}
type HumanGate struct {
	Required   bool   `json:"required"`
	DecisionID string `json:"decision_id,omitempty"`
	Satisfied  bool   `json:"satisfied"`
}

func EvaluateReview(ctx context.Context, engine *core.Engine, scope Scope) (ReviewGate, error) {
	var gate ReviewGate
	compatible, err := CompatibleScopeIDs(ctx, engine, scope)
	if err != nil {
		return gate, err
	}
	repository, err := artifacts.New(engine.DB)
	if err != nil {
		return gate, err
	}
	var latest int64 = -1
	var artifactID, artifactDigest, status string
	var readOnly, blocking int
	for _, scopeID := range compatible {
		var candidateID, candidateDigest, candidateArtifact, candidateArtifactDigest, candidateStatus string
		var candidateReadOnly, candidateBlocking int
		var ended int64
		err := engine.DB.SQL.QueryRowContext(ctx, `SELECT r.id,r.result_digest,r.result_artifact_id,r.result_artifact_digest,r.status,r.read_only_verified,r.ended_at,(SELECT count(*) FROM quality_findings_v2 f WHERE f.review_id=r.id AND f.blocking=1 AND f.resolution='open') FROM review_results_v2 r WHERE r.scope_id=? ORDER BY r.ended_at DESC,r.id DESC LIMIT 1`, scopeID).Scan(&candidateID, &candidateDigest, &candidateArtifact, &candidateArtifactDigest, &candidateStatus, &candidateReadOnly, &ended, &candidateBlocking)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return gate, err
		}
		if ended > latest || ended == latest && candidateID > gate.ResultID {
			latest = ended
			gate.ResultID, gate.ResultDigest = candidateID, candidateDigest
			artifactID, artifactDigest, status, readOnly, blocking = candidateArtifact, candidateArtifactDigest, candidateStatus, candidateReadOnly, candidateBlocking
		}
	}
	if latest < 0 {
		gate.Reason = "missing fresh review"
		return gate, nil
	}
	if status != "pass" || readOnly != 1 || blocking != 0 {
		gate.Reason = "review is not a read-only nonblocking pass"
		return gate, nil
	}
	if err := repository.Verify(ctx, artifactID, artifactDigest, "review-result"); err != nil {
		gate.Reason = "review artifact missing or corrupt"
		_ = RecordStaleness(ctx, engine, "review", gate.ResultID, scope, scope, []string{"artifact_missing_or_corrupt"})
		return gate, nil
	}
	gate.Satisfied = true
	return gate, nil
}

func EvaluateManual(ctx context.Context, engine *core.Engine, scope Scope) ([]ManualGate, error) {
	compatible, err := CompatibleScopeIDs(ctx, engine, scope)
	if err != nil {
		return nil, err
	}
	var result []ManualGate
	for _, criterion := range scope.Criteria {
		if !criterion.Manual {
			continue
		}
		gate := ManualGate{CriterionID: criterion.ID}
		var latest int64 = -1
		var artifactID, artifactDigest string
		for _, scopeID := range compatible {
			var id, state, candidateArtifact, candidateDigest string
			var at int64
			err := engine.DB.SQL.QueryRowContext(ctx, `SELECT id,state,evaluated_at,coalesce(artifact_id,''),coalesce(artifact_digest,'') FROM manual_results_v2 WHERE scope_id=? AND criterion_id=? ORDER BY evaluated_at DESC,id DESC LIMIT 1`, scopeID, criterion.ID).Scan(&id, &state, &at, &candidateArtifact, &candidateDigest)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if at > latest || at == latest && id > gate.ResultID {
				latest = at
				gate.ResultID, gate.State = id, state
				artifactID, artifactDigest = candidateArtifact, candidateDigest
			}
		}
		gate.Satisfied = gate.State == "pass"
		if gate.Satisfied && artifactID != "" {
			repository, err := artifacts.New(engine.DB)
			if err != nil {
				return nil, err
			}
			content, err := repository.Read(ctx, artifactID)
			if err != nil || store.Digest(content) != artifactDigest {
				gate.Satisfied = false
				_ = RecordStaleness(ctx, engine, "manual", gate.ResultID, scope, scope, []string{"artifact_missing_or_corrupt"})
			}
		}
		result = append(result, gate)
	}
	return result, nil
}

func EvaluateHuman(ctx context.Context, engine *core.Engine, scope Scope) (HumanGate, error) {
	gate := HumanGate{Required: scope.HumanAcceptanceRequired, Satisfied: !scope.HumanAcceptanceRequired}
	if !gate.Required {
		return gate, nil
	}
	compatible, err := CompatibleScopeIDs(ctx, engine, scope)
	if err != nil {
		return gate, err
	}
	var latest int64 = -1
	for _, scopeID := range compatible {
		var id, action string
		var at int64
		err := engine.DB.SQL.QueryRowContext(ctx, `SELECT id,action,decided_at FROM human_decisions_v2 WHERE scope_id=? ORDER BY decided_at DESC,id DESC LIMIT 1`, scopeID).Scan(&id, &action, &at)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return gate, err
		}
		if at > latest || at == latest && id > gate.DecisionID {
			latest = at
			gate.DecisionID = id
			gate.Satisfied = action == "accept"
		}
	}
	return gate, nil
}

type AcceptanceRequest struct {
	CommandID string `json:"command_id"`
	Target    Target `json:"target"`
	Actor     string `json:"actor"`
}
type Acceptance struct {
	ID             string `json:"id"`
	ScopeID        string `json:"scope_id"`
	TargetKind     string `json:"target_kind"`
	ManifestID     string `json:"manifest_id"`
	ManifestDigest string `json:"manifest_digest"`
	AcceptedAt     int64  `json:"accepted_at"`
}
type acceptanceManifest struct {
	ScopeID             string       `json:"scope_id"`
	RepositorySetDigest string       `json:"repository_set_digest"`
	Checks              CheckGates   `json:"checks"`
	Review              ReviewGate   `json:"review"`
	Manual              []ManualGate `json:"manual"`
	Human               HumanGate    `json:"human"`
	TaskAcceptances     []string     `json:"task_acceptances,omitempty"`
}

type Acceptor struct {
	Engine *core.Engine
	Owner  *coordinator.Owner
	Hook   func(string) error
}

func (a *Acceptor) gates(ctx context.Context, scope Scope) (acceptanceManifest, []string, error) {
	manifest := acceptanceManifest{ScopeID: scope.ID, RepositorySetDigest: scope.RepositorySetDigest}
	var reasons []string
	var err error
	manifest.Checks, err = EvaluateChecks(ctx, a.Engine, scope)
	if err != nil {
		return manifest, nil, err
	}
	if !manifest.Checks.Satisfied {
		reasons = append(reasons, "required checks are not current and passing")
	}
	manifest.Review, err = EvaluateReview(ctx, a.Engine, scope)
	if err != nil {
		return manifest, nil, err
	}
	if !manifest.Review.Satisfied {
		reasons = append(reasons, manifest.Review.Reason)
	}
	manifest.Manual, err = EvaluateManual(ctx, a.Engine, scope)
	if err != nil {
		return manifest, nil, err
	}
	for _, gate := range manifest.Manual {
		if !gate.Satisfied {
			reasons = append(reasons, "manual criterion "+gate.CriterionID+" is not pass")
		}
	}
	manifest.Human, err = EvaluateHuman(ctx, a.Engine, scope)
	if err != nil {
		return manifest, nil, err
	}
	if !manifest.Human.Satisfied {
		reasons = append(reasons, "explicit human acceptance is missing")
	}
	var unresolved int
	if err := a.Engine.DB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM quality_effects_v2 e JOIN quality_scopes_v2 s ON s.id=e.scope_id WHERE s.target_kind=? AND s.plan_id=? AND coalesce(s.task_id,'')=? AND e.state IN('prepared','executing','uncertain')`, scope.Target.Kind, scope.Target.PlanID, scope.Target.TaskID).Scan(&unresolved); err != nil {
		return manifest, nil, err
	}
	if unresolved != 0 {
		reasons = append(reasons, "quality effects remain unresolved")
	}
	var charged, unknown, limit int64
	if scope.Target.Kind == "task" {
		err = a.Engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND plan_id=? AND task_id=?", scope.Target.PlanID, scope.Target.TaskID).Scan(&charged, &unknown, &limit)
	} else {
		err = a.Engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id=? AND task_id IS NULL", scope.Target.PlanID).Scan(&charged, &unknown, &limit)
	}
	if err != nil {
		return manifest, nil, err
	}
	if charged+unknown >= limit {
		reasons = append(reasons, "cumulative quality budget is exhausted")
	}
	if scope.Target.Kind == "plan" {
		type taskAcceptance struct{ taskID, state, acceptanceID, acceptanceScopeID, manifestID, manifestDigest, repositoryDigest string }
		var acceptedTasks []taskAcceptance
		rows, err := a.Engine.DB.SQL.QueryContext(ctx, `SELECT t.id,t.state,coalesce(a.id,''),coalesce(a.scope_id,''),coalesce(a.evidence_manifest_id,''),coalesce(a.evidence_manifest_digest,''),coalesce(s.repository_set_digest,'') FROM tasks t LEFT JOIN quality_acceptances_v2 a ON a.task_id=t.id AND a.invalidated_at IS NULL LEFT JOIN quality_scopes_v2 s ON s.id=a.scope_id WHERE t.plan_id=? ORDER BY t.rank,t.id`, scope.Target.PlanID)
		if err != nil {
			return manifest, nil, err
		}
		for rows.Next() {
			var item taskAcceptance
			if err := rows.Scan(&item.taskID, &item.state, &item.acceptanceID, &item.acceptanceScopeID, &item.manifestID, &item.manifestDigest, &item.repositoryDigest); err != nil {
				rows.Close()
				return manifest, nil, err
			}
			acceptedTasks = append(acceptedTasks, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return manifest, nil, err
		}
		rows.Close()
		repository, artifactErr := artifacts.New(a.Engine.DB)
		if artifactErr != nil {
			return manifest, nil, artifactErr
		}
		for _, item := range acceptedTasks {
			if item.state != "accepted" || item.acceptanceID == "" || item.repositoryDigest != scope.RepositorySetDigest {
				reasons = append(reasons, "task "+item.taskID+" is not authoritatively accepted")
				continue
			}
			currentTask, observeErr := Observe(ctx, a.Engine, Target{Kind: "task", PlanID: scope.Target.PlanID, TaskID: item.taskID})
			if observeErr != nil {
				return manifest, nil, observeErr
			}
			compatible, compatibleErr := CompatibleScopeIDs(ctx, a.Engine, currentTask)
			if compatibleErr != nil {
				return manifest, nil, compatibleErr
			}
			currentAcceptance := false
			for _, compatibleID := range compatible {
				if compatibleID == item.acceptanceScopeID {
					currentAcceptance = true
					break
				}
			}
			if !currentAcceptance {
				reasons = append(reasons, "task "+item.taskID+" acceptance is stale")
				continue
			}
			_, childReasons, childErr := a.gates(ctx, currentTask)
			if childErr != nil {
				return manifest, nil, childErr
			}
			if len(childReasons) != 0 {
				reasons = append(reasons, "task "+item.taskID+" gates are no longer satisfied: "+strings.Join(childReasons, ", "))
				continue
			}
			if artifactErr = repository.Verify(ctx, item.manifestID, item.manifestDigest, "acceptance-manifest"); artifactErr != nil {
				reasons = append(reasons, "task "+item.taskID+" acceptance artifact is missing or corrupt")
			} else {
				manifest.TaskAcceptances = append(manifest.TaskAcceptances, item.acceptanceID)
			}
		}
	}
	sort.Strings(reasons)
	return manifest, reasons, nil
}

func (a *Acceptor) Accept(ctx context.Context, request AcceptanceRequest) (Acceptance, error) {
	var accepted Acceptance
	if a.Engine == nil || a.Engine.DB == nil || a.Owner == nil || !store.SafeID(request.CommandID) || (request.Actor != "core" && request.Actor != "fixture_core") {
		return accepted, errors.New("core acceptance authority and live owner required")
	}
	scope, err := Observe(ctx, a.Engine, request.Target)
	if err != nil {
		return accepted, err
	}
	if request.Actor == "fixture_core" && !fixtureScope(scope) {
		return accepted, errors.New("fixture acceptance requires disposable repositories")
	}
	if err := Persist(ctx, a.Engine, scope); err != nil {
		return accepted, err
	}
	if err := DetectAndRecordStaleness(ctx, a.Engine, scope); err != nil {
		return accepted, err
	}
	roots := make([]workspace.Identity, 0, len(scope.Repositories))
	for _, item := range scope.Repositories {
		roots = append(roots, item.Identity)
	}
	claims, err := a.Owner.Claims(ctx, a.Engine.ProjectID, roots)
	ownedForAcceptance := false
	if err != nil {
		claims, err = a.Owner.Claim(ctx, a.Engine.ProjectID, store.Digest([]byte("accept\x00"+request.CommandID)), roots)
		ownedForAcceptance = err == nil
	}
	if err != nil {
		return accepted, err
	}
	defer func() {
		if ownedForAcceptance {
			for _, claim := range claims {
				_ = a.Owner.Release(context.Background(), claim, "never_started")
			}
		}
	}()
	args, _ := json.Marshal(request)
	var methodErr error
	err = a.Owner.HoldClaims(ctx, a.Engine.ProjectID, roots, claims, func() error {
		if a.Hook != nil {
			if err := a.Hook("before_final_observation"); err != nil {
				return err
			}
		}
		current, err := Observe(ctx, a.Engine, request.Target)
		if err != nil {
			return err
		}
		outcome := "accepted"
		var finalReasons []string
		if stale := StaleReasons(scope, current); len(stale) != 0 || scope.ID != current.ID {
			outcome = "raced"
			finalReasons = append(finalReasons, "scope changed: "+strings.Join(stale, ","))
		}
		authorityRevision, err := AuthorityRevision(ctx, a.Engine)
		if err != nil {
			return err
		}
		manifest, gateReasons, err := a.gates(ctx, current)
		if err != nil {
			return err
		}
		if outcome == "accepted" {
			finalReasons = gateReasons
			if err != nil {
				return err
			}
			if len(finalReasons) != 0 {
				outcome = "rejected"
			}
		}
		afterGates, err := AuthorityRevision(ctx, a.Engine)
		if err != nil {
			return err
		}
		if afterGates != authorityRevision {
			outcome = "raced"
			finalReasons = append(finalReasons, "quality authority changed while evaluating gates")
		}
		manifestRaw, _ := json.Marshal(manifest)
		manifestRaw, _ = store.Canonical(manifestRaw)
		repository, err := artifacts.New(a.Engine.DB)
		if err != nil {
			return err
		}
		artifact, err := repository.PutCore(ctx, store.Digest([]byte("acceptance-manifest\x00"+request.CommandID)), "acceptance-manifest", "durable", bytes.NewReader(manifestRaw))
		if err != nil {
			return err
		}
		receipt, err := a.Engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: "core", Kind: "quality.accept", Args: args}, func(tx *store.Tx) (any, error) {
			var committedRevision int64
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM quality_authority_v2 WHERE singleton=1").Scan(&committedRevision); err != nil {
				return nil, err
			}
			if committedRevision != afterGates {
				outcome = "raced"
				finalReasons = append(finalReasons, "quality authority changed before acceptance transaction")
			}
			attemptID := store.ID()
			reasonRaw, _ := json.Marshal(finalReasons)
			if _, err := tx.ExecContext(ctx, `INSERT INTO quality_acceptance_attempts_v2(id,scope_id,target_kind,outcome,reasons_json,command_id,attempted_at) VALUES(?,?,?,?,?,?,?)`, attemptID, scope.ID, scope.Target.Kind, outcome, string(reasonRaw), request.CommandID, store.Now()); err != nil {
				return nil, err
			}
			if outcome != "accepted" {
				return map[string]any{"outcome": outcome, "reasons": finalReasons}, nil
			}
			var state, projectState string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", a.Engine.ProjectID).Scan(&projectState); err != nil {
				return nil, err
			}
			if projectState != "ready" {
				return nil, errors.New("project state changed before acceptance")
			}
			if scope.Target.Kind == "task" {
				var planState string
				if err := tx.QueryRowContext(ctx, "SELECT state FROM plans WHERE id=? AND revision=?", scope.Target.PlanID, scope.PlanRevision).Scan(&planState); err != nil {
					return nil, err
				}
				if planState != "active" {
					return nil, errors.New("plan state changed before task acceptance")
				}
				if err := tx.QueryRowContext(ctx, "SELECT state FROM tasks WHERE id=? AND revision=?", scope.Target.TaskID, scope.TaskRevision).Scan(&state); err != nil {
					return nil, err
				}
				if state != "reviewing" && state != "awaiting_human" {
					return nil, errors.New("task state changed before acceptance")
				}
			} else {
				if err := tx.QueryRowContext(ctx, "SELECT state FROM plans WHERE id=? AND revision=?", scope.Target.PlanID, scope.PlanRevision).Scan(&state); err != nil {
					return nil, err
				}
				if state != "verifying" {
					return nil, errors.New("plan state changed before acceptance")
				}
			}
			accepted = Acceptance{ID: store.ID(), ScopeID: current.ID, TargetKind: current.Target.Kind, ManifestID: artifact.ID, ManifestDigest: artifact.Digest, AcceptedAt: store.Now()}
			_, err := tx.ExecContext(ctx, `INSERT INTO quality_acceptances_v2(id,scope_id,target_kind,plan_id,task_id,evidence_manifest_id,evidence_manifest_digest,actor,accepted_at) VALUES(?,?,?,?,?,?,?,?,?)`, accepted.ID, current.ID, current.Target.Kind, current.Target.PlanID, nullable(current.Target.TaskID), artifact.ID, artifact.Digest, request.Actor, accepted.AcceptedAt)
			if err != nil {
				return nil, err
			}
			if scope.Target.Kind == "task" {
				if _, err = tx.ExecContext(ctx, "UPDATE tasks SET state='accepted',block_reason=NULL WHERE id=?", scope.Target.TaskID); err != nil {
					return nil, err
				}
				var remaining int
				if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE plan_id=? AND state!='accepted'", scope.Target.PlanID).Scan(&remaining); err != nil {
					return nil, err
				}
				if remaining == 0 {
					_, err = tx.ExecContext(ctx, "UPDATE plans SET state='verifying' WHERE id=?", scope.Target.PlanID)
				}
			} else {
				_, err = tx.ExecContext(ctx, "UPDATE plans SET state='finalizing' WHERE id=?", scope.Target.PlanID)
			}
			if err != nil {
				return nil, err
			}
			return map[string]any{"outcome": "accepted", "acceptance": accepted}, nil
		})
		if err != nil {
			return err
		}
		var response struct {
			Outcome    string     `json:"outcome"`
			Reasons    []string   `json:"reasons"`
			Acceptance Acceptance `json:"acceptance"`
		}
		if err = json.Unmarshal(receipt, &response); err != nil {
			return err
		}
		if response.Outcome != "accepted" {
			methodErr = fmt.Errorf("acceptance %s: %s", response.Outcome, strings.Join(response.Reasons, "; "))
		} else {
			accepted = response.Acceptance
		}
		return nil
	})
	if err != nil {
		return accepted, err
	}
	return accepted, methodErr
}
