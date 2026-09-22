// Package quality owns immutable evidence scopes, freshness evaluation and
// acceptance authority. It performs no model or subprocess effects itself.
package quality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type Target struct {
	Kind   string `json:"kind"`
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id,omitempty"`
}

type RepositoryFingerprint struct {
	ID       string             `json:"id"`
	Revision int                `json:"revision"`
	Identity workspace.Identity `json:"identity"`
	Observed workspace.Baseline `json:"observed"`
}

type Scope struct {
	ID                      string                   `json:"id"`
	Target                  Target                   `json:"target"`
	PlanRevision            int                      `json:"plan_revision"`
	TaskRevision            int                      `json:"task_revision,omitempty"`
	RepositorySetDigest     string                   `json:"repository_set_digest"`
	Repositories            []RepositoryFingerprint  `json:"repositories"`
	CriteriaDigest          string                   `json:"criteria_digest"`
	DefinitionDigest        string                   `json:"definition_digest"`
	ConfigDigest            string                   `json:"config_digest"`
	CheckSetDigest          string                   `json:"check_set_digest"`
	ReviewerProfileID       string                   `json:"reviewer_profile_id"`
	ReviewerProfileRevision int                      `json:"reviewer_profile_revision"`
	ReviewerProfileDigest   string                   `json:"reviewer_profile_digest"`
	InstructionDigest       string                   `json:"instruction_digest"`
	RequiredChecks          []policy.CheckDefinition `json:"required_checks"`
	Criteria                []policy.Criterion       `json:"criteria"`
	HumanAcceptanceRequired bool                     `json:"human_acceptance_required"`
	Config                  policy.Config            `json:"-"`
	DefinitionJSON          json.RawMessage          `json:"-"`
}

type binding struct {
	Target                  Target `json:"target"`
	PlanRevision            int    `json:"plan_revision"`
	TaskRevision            int    `json:"task_revision,omitempty"`
	RepositorySetDigest     string `json:"repository_set_digest"`
	CriteriaDigest          string `json:"criteria_digest"`
	DefinitionDigest        string `json:"definition_digest"`
	ConfigDigest            string `json:"config_digest"`
	CheckSetDigest          string `json:"check_set_digest"`
	ReviewerProfileID       string `json:"reviewer_profile_id"`
	ReviewerProfileRevision int    `json:"reviewer_profile_revision"`
	ReviewerProfileDigest   string `json:"reviewer_profile_digest"`
	InstructionDigest       string `json:"instruction_digest"`
}

func canonicalDigest(value any) (string, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	raw, err = store.Canonical(raw)
	if err != nil {
		return "", nil, err
	}
	return store.Digest(raw), raw, nil
}

func exclusions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != ".git" {
			result = append(result, value)
		}
	}
	return result
}

// Observe creates an exact current scope. It reads repositories before any
// transaction; a caller making an authoritative transition must recheck or
// hold the live repository claims across its final observation/transaction.
func Observe(ctx context.Context, engine *core.Engine, target Target) (Scope, error) {
	var result Scope
	if engine == nil || engine.DB == nil || !store.SafeID(target.PlanID) || (target.Kind != "task" && target.Kind != "plan") || target.Kind == "task" && !store.SafeID(target.TaskID) || target.Kind == "plan" && target.TaskID != "" {
		return result, errors.New("valid task or plan quality target required")
	}
	result.Target = target
	var planRaw string
	if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT p.revision,pr.definition_json FROM plans p JOIN plan_revisions pr ON pr.plan_id=p.id AND pr.revision=p.revision WHERE p.id=?`, target.PlanID).Scan(&result.PlanRevision, &planRaw); err != nil {
		return result, err
	}
	var plan core.Plan
	if err := json.Unmarshal([]byte(planRaw), &plan); err != nil {
		return result, err
	}
	var configRaw string
	if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT s.digest,s.resolved_json FROM project_configurations pc JOIN config_snapshots s ON s.id=pc.config_id ORDER BY pc.revision DESC LIMIT 1`).Scan(&result.ConfigDigest, &configRaw); err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(configRaw), &result.Config); err != nil {
		return result, err
	}
	checkIDs := append([]string(nil), result.Config.RequiredChecks...)
	if target.Kind == "task" {
		var taskRaw, criteriaDigest, definitionDigest string
		if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT t.revision,tr.definition_json,tr.criteria_digest,tr.definition_digest FROM tasks t JOIN task_revisions tr ON tr.task_id=t.id AND tr.revision=t.revision WHERE t.id=? AND t.plan_id=?`, target.TaskID, target.PlanID).Scan(&result.TaskRevision, &taskRaw, &criteriaDigest, &definitionDigest); err != nil {
			return result, err
		}
		var task policy.Task
		if err := json.Unmarshal([]byte(taskRaw), &task); err != nil {
			return result, err
		}
		result.DefinitionJSON = json.RawMessage(taskRaw)
		result.Criteria, result.CriteriaDigest = task.Criteria, criteriaDigest
		// Task evidence is affected by enclosing restrictions, but not by a
		// harmless task reorder or narrative-only plan revision.
		result.DefinitionDigest, _, _ = canonicalDigest(map[string]any{"task_definition_digest": definitionDigest, "plan_restrictions": plan.Restrictions})
		result.ReviewerProfileID = task.Reviewer
		result.HumanAcceptanceRequired = result.Config.HumanAcceptance
		checkIDs = append(checkIDs, task.Checks...)
	} else {
		result.DefinitionJSON = json.RawMessage(planRaw)
		result.Criteria = append([]policy.Criterion(nil), plan.Criteria...)
		result.CriteriaDigest, _, _ = canonicalDigest(result.Criteria)
		result.DefinitionDigest, _, _ = canonicalDigest(plan)
		result.ReviewerProfileID = plan.Reviewer
		result.HumanAcceptanceRequired = plan.HumanAcceptanceRequired
		checkIDs = append(checkIDs, plan.Checks...)
	}
	if result.ReviewerProfileID == "" {
		return result, errors.New("quality target has no reviewer profile")
	}
	checkSet := map[string]bool{}
	for _, id := range checkIDs {
		checkSet[id] = true
	}
	definitions := map[string]policy.CheckDefinition{}
	for _, definition := range result.Config.CheckDefinitions {
		definitions[definition.ID] = definition
	}
	for id := range checkSet {
		definition, ok := definitions[id]
		if !ok {
			return result, fmt.Errorf("required check definition missing: %s", id)
		}
		result.RequiredChecks = append(result.RequiredChecks, definition)
	}
	sort.Slice(result.RequiredChecks, func(i, j int) bool { return result.RequiredChecks[i].ID < result.RequiredChecks[j].ID })
	result.CheckSetDigest, _, _ = canonicalDigest(result.RequiredChecks)
	var profileRaw string
	if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT p.revision,s.digest,s.resolved_json FROM profiles p JOIN config_snapshots s ON s.id=p.config_id WHERE p.id=? ORDER BY p.revision DESC LIMIT 1`, result.ReviewerProfileID).Scan(&result.ReviewerProfileRevision, &result.ReviewerProfileDigest, &profileRaw); err != nil {
		return result, err
	}
	var profile policy.Profile
	if err := json.Unmarshal([]byte(profileRaw), &profile); err != nil {
		return result, err
	}
	if issues := policy.Eligibility(result.Config, profile, "review"); len(issues) != 0 {
		return result, fmt.Errorf("reviewer profile ineligible: %v", issues)
	}
	result.InstructionDigest, _, _ = canonicalDigest(profile.InstructionDigests)
	rows, err := engine.DB.SQL.QueryContext(ctx, "SELECT repository_id FROM plan_repositories WHERE plan_id=? ORDER BY repository_id", target.PlanID)
	if err != nil {
		return result, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if len(ids) == 0 {
		return result, errors.New("quality scope requires enrolled repositories")
	}
	for _, id := range ids {
		repository, err := engine.Repository(ctx, id)
		if err != nil {
			return result, err
		}
		observed, err := workspace.Fingerprint(ctx, repository.Root, exclusions(repository.Baseline.Exclusions))
		if err != nil {
			return result, fmt.Errorf("fingerprint repository %s: %w", id, err)
		}
		if repository.Identity.Validate() != nil {
			return result, errors.New("repository physical identity changed")
		}
		result.Repositories = append(result.Repositories, RepositoryFingerprint{ID: id, Revision: repository.Revision, Identity: repository.Identity, Observed: observed})
	}
	result.RepositorySetDigest, _, _ = canonicalDigest(result.Repositories)
	b := binding{result.Target, result.PlanRevision, result.TaskRevision, result.RepositorySetDigest, result.CriteriaDigest, result.DefinitionDigest, result.ConfigDigest, result.CheckSetDigest, result.ReviewerProfileID, result.ReviewerProfileRevision, result.ReviewerProfileDigest, result.InstructionDigest}
	result.ID, _, err = canonicalDigest(b)
	return result, err
}

func Persist(ctx context.Context, engine *core.Engine, scope Scope) error {
	manifest, err := json.Marshal(scope.Repositories)
	if err != nil {
		return err
	}
	return engine.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO quality_scopes_v2(id,target_kind,plan_id,plan_revision,task_id,task_revision,repository_set_digest,repository_manifest_json,criteria_digest,definition_digest,config_digest,check_set_digest,reviewer_profile_id,reviewer_profile_revision,reviewer_profile_digest,instruction_digest,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, scope.ID, scope.Target.Kind, scope.Target.PlanID, scope.PlanRevision, nullable(scope.Target.TaskID), nullableInt(scope.TaskRevision), scope.RepositorySetDigest, string(manifest), scope.CriteriaDigest, scope.DefinitionDigest, scope.ConfigDigest, scope.CheckSetDigest, scope.ReviewerProfileID, scope.ReviewerProfileRevision, scope.ReviewerProfileDigest, scope.InstructionDigest, store.Now())
		return err
	})
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

// StaleReasons compares all authoritative scope dimensions. The order is
// stable so callers can persist deterministic diagnostics.
func StaleReasons(prior, current Scope) []string {
	var reasons []string
	if prior.RepositorySetDigest != current.RepositorySetDigest {
		reasons = append(reasons, "repository_content")
	}
	if prior.Target.Kind == "plan" && prior.PlanRevision != current.PlanRevision {
		reasons = append(reasons, "plan_revision")
	}
	if prior.TaskRevision != current.TaskRevision {
		reasons = append(reasons, "task_revision")
	}
	if prior.CriteriaDigest != current.CriteriaDigest {
		reasons = append(reasons, "criteria")
	}
	if prior.DefinitionDigest != current.DefinitionDigest {
		reasons = append(reasons, "definition")
	}
	if prior.ConfigDigest != current.ConfigDigest {
		reasons = append(reasons, "configuration")
	}
	if prior.CheckSetDigest != current.CheckSetDigest {
		reasons = append(reasons, "check_set")
	}
	if prior.ReviewerProfileID != current.ReviewerProfileID || prior.ReviewerProfileRevision != current.ReviewerProfileRevision || prior.ReviewerProfileDigest != current.ReviewerProfileDigest {
		reasons = append(reasons, "reviewer_profile")
	}
	if prior.InstructionDigest != current.InstructionDigest {
		reasons = append(reasons, "reviewer_instructions")
	}
	return reasons
}

// CompatibleScopeIDs returns scopes whose quality authority is semantically
// identical to current. For task evidence this intentionally ignores a bare
// plan revision change; all restrictions and task-relevant definitions remain
// covered by DefinitionDigest. Plan-wide evidence requires the exact revision.
func CompatibleScopeIDs(ctx context.Context, engine *core.Engine, current Scope) ([]string, error) {
	query := `SELECT id FROM quality_scopes_v2 WHERE target_kind=? AND plan_id=? AND coalesce(task_id,'')=? AND repository_set_digest=? AND criteria_digest=? AND definition_digest=? AND config_digest=? AND check_set_digest=? AND reviewer_profile_id=? AND reviewer_profile_revision=? AND reviewer_profile_digest=? AND instruction_digest=?`
	args := []any{current.Target.Kind, current.Target.PlanID, current.Target.TaskID, current.RepositorySetDigest, current.CriteriaDigest, current.DefinitionDigest, current.ConfigDigest, current.CheckSetDigest, current.ReviewerProfileID, current.ReviewerProfileRevision, current.ReviewerProfileDigest, current.InstructionDigest}
	if current.Target.Kind == "task" {
		query += " AND task_revision=?"
		args = append(args, current.TaskRevision)
	} else {
		query += " AND plan_revision=? AND task_revision IS NULL"
		args = append(args, current.PlanRevision)
	}
	query += " ORDER BY created_at DESC,id"
	rows, err := engine.DB.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func Load(ctx context.Context, engine *core.Engine, id string) (Scope, error) {
	var result Scope
	var targetKind, taskID, manifest string
	var taskRevision sql.NullInt64
	err := engine.DB.SQL.QueryRowContext(ctx, `SELECT target_kind,plan_id,plan_revision,coalesce(task_id,''),task_revision,repository_set_digest,repository_manifest_json,criteria_digest,definition_digest,config_digest,check_set_digest,reviewer_profile_id,reviewer_profile_revision,reviewer_profile_digest,instruction_digest FROM quality_scopes_v2 WHERE id=?`, id).Scan(&targetKind, &result.Target.PlanID, &result.PlanRevision, &taskID, &taskRevision, &result.RepositorySetDigest, &manifest, &result.CriteriaDigest, &result.DefinitionDigest, &result.ConfigDigest, &result.CheckSetDigest, &result.ReviewerProfileID, &result.ReviewerProfileRevision, &result.ReviewerProfileDigest, &result.InstructionDigest)
	if err != nil {
		return result, err
	}
	result.ID, result.Target.Kind, result.Target.TaskID = id, targetKind, taskID
	if taskRevision.Valid {
		result.TaskRevision = int(taskRevision.Int64)
	}
	err = json.Unmarshal([]byte(manifest), &result.Repositories)
	return result, err
}

func RecordStaleness(ctx context.Context, engine *core.Engine, evidenceKind, evidenceID string, prior, current Scope, reasons []string) error {
	if len(reasons) == 0 {
		return nil
	}
	details, _ := json.Marshal(map[string]any{"prior_scope": prior.ID, "current_scope": current.ID})
	return engine.DB.Write(ctx, func(tx *store.Tx) error {
		for _, reason := range reasons {
			id := store.Digest([]byte(evidenceKind + "\x00" + evidenceID + "\x00" + prior.ID + "\x00" + current.ID + "\x00" + reason))
			if _, err := tx.ExecContext(ctx, `INSERT INTO evidence_staleness_v2(id,evidence_kind,evidence_id,prior_scope_id,current_scope_id,reason,details_json,detected_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, id, evidenceKind, evidenceID, prior.ID, current.ID, reason, string(details), store.Now()); err != nil {
				return err
			}
		}
		return nil
	})
}

// DetectAndRecordStaleness is the single invalidation entry point. It never
// edits evidence; it appends why each prior record no longer matches the exact
// current scope.
func DetectAndRecordStaleness(ctx context.Context, engine *core.Engine, current Scope) error {
	rows, err := engine.DB.SQL.QueryContext(ctx, `SELECT id FROM quality_scopes_v2 WHERE target_kind=? AND plan_id=? AND coalesce(task_id,'')=? AND id!=? ORDER BY created_at,id`, current.Target.Kind, current.Target.PlanID, current.Target.TaskID, current.ID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		prior, err := Load(ctx, engine, id)
		if err != nil {
			return err
		}
		reasons := StaleReasons(prior, current)
		if len(reasons) == 0 {
			continue
		}
		queries := []struct{ kind, query string }{
			{"check", "SELECT id FROM check_results_v2 WHERE scope_id=?"},
			{"review", "SELECT id FROM review_results_v2 WHERE scope_id=?"},
			{"manual", "SELECT id FROM manual_results_v2 WHERE scope_id=?"},
			{"human_decision", "SELECT id FROM human_decisions_v2 WHERE scope_id=?"},
			{"task_acceptance", "SELECT id FROM quality_acceptances_v2 WHERE scope_id=? AND target_kind='task'"},
			{"plan_acceptance", "SELECT id FROM quality_acceptances_v2 WHERE scope_id=? AND target_kind='plan'"},
		}
		for _, item := range queries {
			evidenceRows, err := engine.DB.SQL.QueryContext(ctx, item.query, prior.ID)
			if err != nil {
				return err
			}
			var evidenceIDs []string
			for evidenceRows.Next() {
				var evidenceID string
				if err := evidenceRows.Scan(&evidenceID); err != nil {
					evidenceRows.Close()
					return err
				}
				evidenceIDs = append(evidenceIDs, evidenceID)
			}
			if err := evidenceRows.Err(); err != nil {
				evidenceRows.Close()
				return err
			}
			evidenceRows.Close()
			for _, evidenceID := range evidenceIDs {
				if err := RecordStaleness(ctx, engine, item.kind, evidenceID, prior, current, reasons); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
