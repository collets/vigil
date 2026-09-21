// Package supervisor owns persisted execution attempts and bounded external
// effects. It never accepts tasks; a validated completion stops at checking.
package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"vigil/internal/boundary"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type PrepareRequest struct {
	CommandID               string                       `json:"command_id"`
	ExpectedProjectRevision int                          `json:"expected_project_revision"`
	TaskID                  string                       `json:"task_id"`
	RuntimeKind             string                       `json:"runtime_kind"`
	WallLimitMS             int64                        `json:"wall_limit_ms"`
	Eligibility             *boundary.EligibilityRequest `json:"eligibility,omitempty"`
	ExpectedRoutes          []string                     `json:"expected_routes"`
}

type PreparedRun struct {
	RunID               string                       `json:"run_id"`
	GenerationID        string                       `json:"generation_id"`
	TransportGeneration string                       `json:"transport_generation"`
	RuntimeResourceID   string                       `json:"runtime_resource_id"`
	ContainerName       string                       `json:"container_name,omitempty"`
	RuntimeKind         string                       `json:"runtime_kind"`
	PlanID              string                       `json:"plan_id"`
	TaskID              string                       `json:"task_id"`
	ProfileID           string                       `json:"profile_id"`
	EndpointID          string                       `json:"endpoint_id"`
	ExpectedRevision    int                          `json:"expected_project_revision"`
	ActiveLimitMS       int64                        `json:"active_limit_ms"`
	WallLimitMS         int64                        `json:"wall_limit_ms"`
	Task                policy.Task                  `json:"task"`
	Repositories        []core.RepositoryRecord      `json:"repositories"`
	Eligibility         *boundary.EligibilityRequest `json:"eligibility,omitempty"`
	ExpectedRoutes      []string                     `json:"expected_routes"`
}

type preparationObservation struct {
	request      PrepareRequest
	planID       string
	planRevision int
	taskRevision int
	configID     string
	profileID    string
	profileRev   int
	configRaw    string
	profileRaw   string
	taskRaw      string
	task         policy.Task
	config       policy.Config
	profile      policy.Profile
	repositories []core.RepositoryRecord
}

func observePreparation(ctx context.Context, engine *core.Engine, request PrepareRequest) (preparationObservation, error) {
	var observed preparationObservation
	observed.request = request
	if engine == nil || engine.DB == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.TaskID) || request.ExpectedProjectRevision < 1 {
		return observed, errors.New("explicit command, task and expected project revision required")
	}
	if request.RuntimeKind != "synthetic" && request.RuntimeKind != "docker" && request.RuntimeKind != "native" {
		return observed, errors.New("runtime_kind must be synthetic, docker, or native")
	}
	if request.WallLimitMS <= 0 || request.WallLimitMS > int64((2*time.Hour)/time.Millisecond) {
		return observed, errors.New("wall limit must be positive and no more than two hours")
	}
	if request.RuntimeKind == "synthetic" {
		if request.Eligibility != nil || len(request.ExpectedRoutes) != 0 {
			return observed, errors.New("synthetic fixture runs cannot carry production qualification")
		}
	} else {
		if request.Eligibility == nil || len(request.ExpectedRoutes) == 0 {
			return observed, errors.New("production runtime requires exact qualification and inference routes")
		}
	}
	var projectRevision int
	err := engine.DB.SQL.QueryRowContext(ctx, `SELECT t.plan_id,p.revision,t.revision,tr.definition_json,pr.accepted_at,project.revision
		FROM tasks t JOIN task_revisions tr ON tr.task_id=t.id AND tr.revision=t.revision
		JOIN plans p ON p.id=t.plan_id JOIN plan_revisions pr ON pr.plan_id=p.id AND pr.revision=p.revision
		JOIN project ON project.id=? WHERE t.id=?`, engine.ProjectID, request.TaskID).Scan(&observed.planID, &observed.planRevision, &observed.taskRevision, &observed.taskRaw, new(any), &projectRevision)
	if err != nil {
		return observed, err
	}
	if projectRevision != request.ExpectedProjectRevision {
		return observed, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedProjectRevision, projectRevision)
	}
	var accepted sql.NullInt64
	if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT accepted_at FROM plan_revisions WHERE plan_id=? AND revision=?", observed.planID, observed.planRevision).Scan(&accepted); err != nil || !accepted.Valid {
		return observed, errors.New("accepted plan revision required before execution")
	}
	if err := json.Unmarshal([]byte(observed.taskRaw), &observed.task); err != nil {
		return observed, err
	}
	if len(observed.task.Questions) != 0 {
		return observed, errors.New("task has unresolved questions")
	}
	for _, prerequisite := range observed.task.ManualPrerequisites {
		if !prerequisite.Satisfied {
			return observed, errors.New("task has an unsatisfied manual setup prerequisite")
		}
	}
	rows, err := engine.DB.SQL.QueryContext(ctx, `SELECT d.dependency_id,t.state FROM task_dependencies d JOIN tasks t ON t.id=d.dependency_id WHERE d.task_id=?`, request.TaskID)
	if err != nil {
		return observed, err
	}
	for rows.Next() {
		var dependency, state string
		if err := rows.Scan(&dependency, &state); err != nil {
			rows.Close()
			return observed, err
		}
		if state != "accepted" {
			rows.Close()
			return observed, fmt.Errorf("dependency %s is not accepted", dependency)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return observed, err
	}
	rows.Close()
	if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT pc.config_id,s.resolved_json FROM project_configurations pc JOIN config_snapshots s ON s.id=pc.config_id ORDER BY pc.revision DESC LIMIT 1`).Scan(&observed.configID, &observed.configRaw); err != nil {
		return observed, err
	}
	if err := json.Unmarshal([]byte(observed.configRaw), &observed.config); err != nil {
		return observed, err
	}
	observed.profileID = observed.task.Implementation
	if err := engine.DB.SQL.QueryRowContext(ctx, `SELECT p.revision,s.resolved_json FROM profiles p JOIN config_snapshots s ON s.id=p.config_id WHERE p.id=? ORDER BY p.revision DESC LIMIT 1`, observed.profileID).Scan(&observed.profileRev, &observed.profileRaw); err != nil {
		return observed, err
	}
	if err := json.Unmarshal([]byte(observed.profileRaw), &observed.profile); err != nil {
		return observed, err
	}
	if issues := policy.Eligibility(observed.config, observed.profile, "implementation"); len(issues) != 0 {
		return observed, fmt.Errorf("implementation profile ineligible: %v", issues)
	}
	rows, err = engine.DB.SQL.QueryContext(ctx, "SELECT repository_id FROM plan_repositories WHERE plan_id=? ORDER BY repository_id", observed.planID)
	if err != nil {
		return observed, err
	}
	var repositoryIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return observed, err
		}
		repositoryIDs = append(repositoryIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return observed, err
	}
	rows.Close()
	for _, id := range repositoryIDs {
		repository, err := engine.Repository(ctx, id)
		if err != nil {
			return observed, err
		}
		var state string
		if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM repository_branch_operations WHERE repository_id=? AND repository_revision=?", id, repository.Revision).Scan(&state); err != nil || (state != "observed" && state != "reconciled") {
			return observed, fmt.Errorf("repository %s branch is not durably prepared", id)
		}
		current, err := workspace.Fingerprint(ctx, repository.Root, withoutGit(repository.Baseline.Exclusions))
		if err != nil {
			return observed, err
		}
		if current.Dirty || current.HeadOID != repository.BaseOID || current.HeadRef != "refs/heads/"+repository.PlanBranch || current.IndexDigest != repository.Baseline.IndexDigest || current.ContentDigest != repository.Baseline.ContentDigest {
			return observed, fmt.Errorf("repository %s changed after branch preparation", id)
		}
		observed.repositories = append(observed.repositories, repository)
	}
	if len(observed.repositories) == 0 {
		return observed, errors.New("at least one explicitly enrolled plan repository is required")
	}
	return observed, nil
}

func withoutGit(exclusions []string) []string {
	var result []string
	for _, exclusion := range exclusions {
		if exclusion != ".git" {
			result = append(result, exclusion)
		}
	}
	return result
}

func Prepare(ctx context.Context, engine *core.Engine, request PrepareRequest) (PreparedRun, error) {
	var result PreparedRun
	observed, err := observePreparation(ctx, engine, request)
	if err != nil {
		return result, err
	}
	args, _ := json.Marshal(request)
	receipt, err := engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "execution.prepare", Args: args}, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", engine.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != request.ExpectedProjectRevision {
			return nil, errors.New("project revision changed while preparing execution")
		}
		var planRevision, taskRevision, profileRevision int
		var configID, taskState string
		if err := tx.QueryRowContext(ctx, "SELECT p.revision,t.revision,t.state FROM tasks t JOIN plans p ON p.id=t.plan_id WHERE t.id=? AND t.plan_id=?", request.TaskID, observed.planID).Scan(&planRevision, &taskRevision, &taskState); err != nil {
			return nil, err
		}
		if planRevision != observed.planRevision || taskRevision != observed.taskRevision || (taskState != "draft" && taskState != "ready") {
			return nil, errors.New("task or plan revision/state changed while preparing")
		}
		if err := tx.QueryRowContext(ctx, "SELECT config_id FROM project_configurations ORDER BY revision DESC LIMIT 1").Scan(&configID); err != nil || configID != observed.configID {
			return nil, errors.New("configuration changed while preparing")
		}
		if err := tx.QueryRowContext(ctx, "SELECT max(revision) FROM profiles WHERE id=?", observed.profileID).Scan(&profileRevision); err != nil || profileRevision != observed.profileRev {
			return nil, errors.New("profile changed while preparing")
		}
		activeLimit := observed.task.ActiveLimitMS
		if observed.config.AttemptLimitMS < activeLimit {
			activeLimit = observed.config.AttemptLimitMS
		}
		if activeLimit <= 0 || request.WallLimitMS < activeLimit {
			return nil, errors.New("invalid effective run budget")
		}
		runID := store.ID()
		generationID := store.ID()
		transportGeneration := store.ID()
		runtimeResourceID := "vigil:" + runID + ":1"
		containerName := ""
		if request.RuntimeKind == "docker" {
			containerName = "vigil-" + runID + "-g1"
		}
		result = PreparedRun{RunID: runID, GenerationID: generationID, TransportGeneration: transportGeneration, RuntimeResourceID: runtimeResourceID, ContainerName: containerName, RuntimeKind: request.RuntimeKind, PlanID: observed.planID, TaskID: request.TaskID, ProfileID: observed.profileID, EndpointID: observed.profile.EndpointID, ExpectedRevision: revision + 1, ActiveLimitMS: activeLimit, WallLimitMS: request.WallLimitMS, Task: observed.task, Repositories: observed.repositories, Eligibility: request.Eligibility, ExpectedRoutes: request.ExpectedRoutes}
		budgetSnapshot, _ := json.Marshal(map[string]any{"active_limit_ms": activeLimit, "wall_limit_ms": request.WallLimitMS, "task_limit_ms": observed.config.TaskLimitMS, "plan_services_limit_ms": 1800000})
		repositoriesJSON, _ := json.Marshal(observed.repositories)
		snapshotValue := map[string]json.RawMessage{"task": json.RawMessage(observed.taskRaw), "config": json.RawMessage(observed.configRaw), "profile": json.RawMessage(observed.profileRaw), "budget": budgetSnapshot, "repositories": repositoriesJSON}
		snapshotJSON, _ := json.Marshal(snapshotValue)
		snapshotDigest := store.Digest(snapshotJSON)
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES(?,?,?,?,?,?,?,?,'implementation','initial','prepared','unconfirmed',?,?,?)`, runID, observed.planID, observed.planRevision, request.TaskID, observed.taskRevision, observed.configID, observed.profileID, observed.profileRev, activeLimit, request.WallLimitMS, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_snapshots VALUES(?,?,?,?,?,?,?,?,?,?,?)`, runID, observed.taskRaw, observed.configRaw, observed.profileRaw, string(budgetSnapshot), string(repositoriesJSON), revision, observed.planRevision, observed.taskRevision, snapshotDigest, store.Now()); err != nil {
			return nil, err
		}
		qualificationJSON := "{}"
		if request.Eligibility != nil {
			b, _ := json.Marshal(request.Eligibility)
			qualificationJSON = string(b)
		}
		routesJSON, _ := json.Marshal(request.ExpectedRoutes)
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,container_name,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES(?,?,?,?,?,?,?,'prepared','not_attempted',?,?,?,?)`, generationID, runID, 1, request.RuntimeKind, runtimeResourceID, nullableString(containerName), transportGeneration, qualificationJSON, store.Digest(repositoriesJSON), string(routesJSON), store.Now()); err != nil {
			return nil, err
		}
		ledgerID := store.Digest([]byte("task\x00" + observed.planID + "\x00" + request.TaskID))
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_ledgers(id,scope,plan_id,task_id,active_limit_ms,updated_at) VALUES(?,'task',?,?,?,?) ON CONFLICT(scope,plan_id,task_id) DO NOTHING`, ledgerID, observed.planID, request.TaskID, observed.config.TaskLimitMS, store.Now()); err != nil {
			return nil, err
		}
		planLedgerID := store.Digest([]byte("plan-services\x00" + observed.planID))
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_ledgers(id,scope,plan_id,active_limit_ms,updated_at) VALUES(?,'plan_services',?,1800000,?) ON CONFLICT(id) DO NOTHING`, planLedgerID, observed.planID, store.Now()); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='running' WHERE id=?", request.TaskID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE id=?", observed.planID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET state='ready',revision=revision+1 WHERE id=?", engine.ProjectID); err != nil {
			return nil, err
		}
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

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
