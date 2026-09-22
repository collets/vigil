// Package core owns application commands, revisions and readiness. No model is
// launched from these planning handlers; native observations are not authority.
package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"vigil/internal/boundary"
	"vigil/internal/coordinator"
	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

type Manager struct {
	Dir         string
	Coordinator *coordinator.Coordinator
}
type Project struct {
	ID       string `json:"id"`
	Root     string `json:"root"`
	Revision int    `json:"revision"`
	State    string `json:"state"`
}
type Engine struct {
	DB        *store.DB
	ProjectID string
}
type Envelope struct {
	CommandID        string          `json:"command_id"`
	ExpectedRevision int             `json:"expected_revision"`
	Kind             string          `json:"kind"`
	Payload          json.RawMessage `json:"payload"`
}
type Authority string

const (
	Human      Authority = "human"
	Supervisor Authority = "supervisor"
	Worker     Authority = "worker"
	Core       Authority = "core"
)

func OpenManager(ctx context.Context, dir string) (*Manager, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = store.PrivateDir(dir); err != nil {
		return nil, err
	}
	c, err := coordinator.Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &Manager{dir, c}, nil
}
func (m *Manager) Close() error { return m.Coordinator.DB.Close() }
func (m *Manager) Init(ctx context.Context, root string) (Project, error) {
	id, err := workspace.Inspect(ctx, root)
	if err != nil {
		return Project{}, err
	}
	state, err := workspace.Inspect(ctx, m.Dir)
	if err != nil {
		return Project{}, err
	}
	if workspace.Overlap(id, state) {
		return Project{}, errors.New("private application state must be outside the managed project tree")
	}
	var project Project
	err = m.Coordinator.DB.Write(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id,root,identity_json FROM project_registry")
		if err != nil {
			return err
		}
		found := ""
		for rows.Next() {
			var pid, path, raw string
			if err := rows.Scan(&pid, &path, &raw); err != nil {
				rows.Close()
				return err
			}
			var old workspace.Identity
			if err := json.Unmarshal([]byte(raw), &old); err != nil {
				rows.Close()
				return err
			}
			if old.Key == id.Key {
				found = pid
				id = old
			} else if workspace.Overlap(old, id) {
				rows.Close()
				return fmt.Errorf("overlapping or parent project %s already registered", pid)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if found != "" {
			project = Project{ID: found, Root: id.Root}
			return nil
		}
		project = Project{ID: store.ID(), Root: id.Root, Revision: 1, State: "paused"}
		raw, _ := json.Marshal(id)
		_, err = tx.ExecContext(ctx, "INSERT INTO project_registry VALUES(?,?,?,?)", project.ID, id.Root, string(raw), store.Now())
		return err
	})
	if err != nil {
		return Project{}, err
	}
	// Registry publication precedes DB creation: a crash retries this identity,
	// never allocates a second project. No external effect is inside the registry tx.
	db, err := store.Open(ctx, filepath.Join(m.Dir, "projects", project.ID, "project.sqlite"), "project")
	if err != nil {
		return Project{}, err
	}
	defer db.Close()
	raw, _ := json.Marshal(id)
	_, err = db.Command(ctx, store.Command{ID: "initialize:" + project.ID, Actor: string(Human), Kind: "project.initialize", Args: raw}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, "INSERT INTO project(id,root,identity_json,revision,policy_epoch,state,created_at) VALUES(?,?,?,1,1,'paused',?) ON CONFLICT(id) DO NOTHING", project.ID, id.Root, string(raw), store.Now())
		return project.ID, err
	})
	if err != nil {
		return Project{}, err
	}
	err = db.SQL.QueryRowContext(ctx, "SELECT id,root,revision,state FROM project").Scan(&project.ID, &project.Root, &project.Revision, &project.State)
	return project, err
}
func (m *Manager) Open(ctx context.Context, id string) (*Engine, error) {
	if !store.SafeID(id) {
		return nil, errors.New("invalid project ID")
	}
	var count int
	if err := m.Coordinator.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM project_registry WHERE id=?", id).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, errors.New("unknown project")
	}
	db, err := store.Open(ctx, filepath.Join(m.Dir, "projects", id, "project.sqlite"), "project")
	if err != nil {
		return nil, err
	}
	return &Engine{db, id}, nil
}
func (m *Manager) List(ctx context.Context) ([]Project, error) {
	rows, err := m.Coordinator.DB.SQL.QueryContext(ctx, "SELECT id,root FROM project_registry ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Root); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func snapshot(ctx context.Context, tx *store.Tx, value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	b, err = store.Canonical(b)
	if err != nil {
		return "", err
	}
	digest := store.Digest(b)
	id := digest
	_, err = tx.ExecContext(ctx, "INSERT INTO config_snapshots VALUES(?,?,1,?,'{"+`"source":"explicit_command"`+"}',?) ON CONFLICT(digest) DO NOTHING", id, digest, string(b), store.Now())
	return id, err
}
func (e *Engine) Apply(ctx context.Context, actor Authority, cmd Envelope) (json.RawMessage, error) {
	if actor != Human && actor != Supervisor && actor != Core && actor != Worker {
		return nil, errors.New("unrecognized caller authority")
	}
	if actor != Human && !(actor == Supervisor && cmd.Kind == "plan.reorder") && !(actor == Core && cmd.Kind == "operation.start") {
		return nil, errors.New("caller role cannot perform this command")
	}
	if cmd.ExpectedRevision < 1 {
		return nil, errors.New("expected_revision required")
	}
	if cmd.Kind == "repository.enroll" {
		return e.applyRepositoryEnrollment(ctx, actor, cmd)
	}
	args, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	return e.DB.Command(ctx, store.Command{ID: cmd.CommandID, Actor: string(actor), Kind: cmd.Kind, Args: args}, func(tx *store.Tx) (any, error) {
		var revision, epoch int
		if err := tx.QueryRowContext(ctx, "SELECT revision,policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&revision, &epoch); err != nil {
			return nil, err
		}
		if revision != cmd.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", cmd.ExpectedRevision, revision)
		}
		var result any
		var err error
		switch cmd.Kind {
		case "project.configure":
			var config policy.Config
			if err = store.Decode(cmd.Payload, &config); err != nil {
				return nil, err
			}
			if err = config.Validate(); err != nil {
				return nil, err
			}
			id, err := snapshot(ctx, tx, config)
			if err != nil {
				return nil, err
			}
			epoch++
			if _, err = tx.ExecContext(ctx, "INSERT INTO project_configurations VALUES(?,?,?,?)", e.ProjectID, revision+1, epoch, id); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE project SET model_policy=?,policy_epoch=?,state='paused'", config.ModelPolicy, epoch); err != nil {
				return nil, err
			}
			// Any policy replacement retires pending requests; approvals never float across contexts.
			_, err = tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE state='pending'", store.Now())
			result = map[string]string{"config_id": id}
		case "profile.put":
			var p policy.Profile
			if err = store.Decode(cmd.Payload, &p); err != nil {
				return nil, err
			}
			if err = p.Validate(); err != nil {
				return nil, err
			}
			id, err := snapshot(ctx, tx, p)
			if err != nil {
				return nil, err
			}
			var pr int
			if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(revision),0)+1 FROM profiles WHERE id=?", p.ID).Scan(&pr); err != nil {
				return nil, err
			}
			caps, _ := json.Marshal(p.Capabilities)
			_, err = tx.ExecContext(ctx, "INSERT INTO profiles VALUES(?,?,?,?)", p.ID, pr, id, string(caps))
			result = map[string]any{"profile_id": p.ID, "profile_revision": pr}
		case "plan.put":
			result, err = e.putPlan(ctx, tx, cmd)
			if err == nil {
				// A definition replacement invalidates old operation authority,
				// including plan-wide restrictions not bound to a task revision.
				_, err = tx.ExecContext(ctx, "UPDATE project SET policy_epoch=policy_epoch+1")
				if err == nil {
					_, err = tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE state='pending'", store.Now())
				}
			}
		case "plan.reorder":
			result, err = e.reorder(ctx, tx, actor, cmd)
		case "task.criteria.revise":
			result, err = e.reviseCriteria(ctx, tx, cmd)
		case "operation.request", "permission.grant", "permission.revoke", "operation.start":
			result, err = e.permission(ctx, tx, actor, cmd, epoch)
		default:
			return nil, errors.New("unsupported command; execution, acceptance and delivery are not enabled")
		}
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		return map[string]any{"project_id": e.ProjectID, "revision": revision + 1, "result": result}, nil
	})
}

func (e *Engine) reviseCriteria(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var request struct {
		TaskID   string             `json:"task_id"`
		Criteria []policy.Criterion `json:"criteria"`
		Reason   string             `json:"reason"`
	}
	if err := store.Decode(cmd.Payload, &request); err != nil {
		return nil, err
	}
	if !store.SafeID(request.TaskID) || request.Reason == "" {
		return nil, errors.New("task and explicit human criteria-revision reason required")
	}
	if err := policy.ValidateCriteria(request.Criteria); err != nil {
		return nil, err
	}
	var planID, state, taskRaw, oldCriteria string
	var taskRevision, planRevision int
	if err := tx.QueryRowContext(ctx, `SELECT t.plan_id,t.state,t.revision,tr.definition_json,tr.criteria_digest,p.revision FROM tasks t JOIN task_revisions tr ON tr.task_id=t.id AND tr.revision=t.revision JOIN plans p ON p.id=t.plan_id WHERE t.id=?`, request.TaskID).Scan(&planID, &state, &taskRevision, &taskRaw, &oldCriteria, &planRevision); err != nil {
		return nil, err
	}
	if state != "awaiting_human" && state != "needs_repair" && state != "blocked" && state != "accepted" {
		return nil, errors.New("criteria may be revised only at a human/rework boundary")
	}
	var task policy.Task
	if err := json.Unmarshal([]byte(taskRaw), &task); err != nil {
		return nil, err
	}
	task.Criteria = request.Criteria
	definition, _ := json.Marshal(task)
	criteriaRaw, _ := json.Marshal(request.Criteria)
	definitionDigest, criteriaDigest := store.Digest(definition), store.Digest(criteriaRaw)
	if criteriaDigest == oldCriteria {
		return nil, errors.New("criteria revision does not change criteria")
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO task_revisions VALUES(?,?,?,?,?,?,?)", request.TaskID, taskRevision+1, string(definition), criteriaDigest, definitionDigest, "human", cmd.CommandID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET revision=revision+1,state='draft',block_reason=? WHERE id=?", "criteria revised by human: "+request.Reason, request.TaskID); err != nil {
		return nil, err
	}
	var planRaw, spec string
	var accepted any
	if err := tx.QueryRowContext(ctx, "SELECT definition_json,spec_digest,accepted_at FROM plan_revisions WHERE plan_id=? AND revision=?", planID, planRevision).Scan(&planRaw, &spec, &accepted); err != nil {
		return nil, err
	}
	var plan Plan
	if err := json.Unmarshal([]byte(planRaw), &plan); err != nil {
		return nil, err
	}
	found := false
	for index := range plan.Tasks {
		if plan.Tasks[index].ID == request.TaskID {
			plan.Tasks[index] = task
			found = true
		}
	}
	if !found {
		return nil, errors.New("task missing from plan definition")
	}
	nextPlanRaw, _ := json.Marshal(plan)
	if _, err := tx.ExecContext(ctx, "INSERT INTO plan_revisions VALUES(?,?,?,?,?,?)", planID, planRevision+1, spec, string(nextPlanRaw), "human", accepted); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plans SET revision=revision+1,state=CASE WHEN state='verifying' THEN 'active' ELSE state END WHERE id=?", planID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE quality_acceptances_v2 SET invalidated_at=?,invalidation_reason='criteria_retired' WHERE task_id=? AND invalidated_at IS NULL", store.Now(), request.TaskID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE task_id=? AND state='pending'", store.Now(), request.TaskID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE project SET policy_epoch=policy_epoch+1"); err != nil {
		return nil, err
	}
	return map[string]any{"task_id": request.TaskID, "task_revision": taskRevision + 1, "plan_revision": planRevision + 1, "criteria_digest": criteriaDigest}, nil
}

type Plan struct {
	Restrictions             policy.Restrictions `json:"restrictions,omitempty"`
	ID                       string              `json:"id"`
	Title                    string              `json:"title"`
	Specification            string              `json:"specification"`
	Approved                 bool                `json:"approved"`
	AuthorizeCriteriaChanges bool                `json:"authorize_criteria_changes"`
	Tasks                    []policy.Task       `json:"tasks"`
	Criteria                 []policy.Criterion  `json:"quality_criteria,omitempty"`
	Checks                   []string            `json:"quality_checks,omitempty"`
	Reviewer                 string              `json:"reviewer_profile,omitempty"`
	HumanAcceptanceRequired  bool                `json:"human_acceptance_required,omitempty"`
}

func (e *Engine) putPlan(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var p Plan
	if err := store.Decode(cmd.Payload, &p); err != nil {
		return nil, err
	}
	if !store.SafeID(p.ID) || p.Title == "" || p.Specification == "" {
		return nil, errors.New("plan ID/title/specification required")
	}
	if err := policy.ValidateTasks(p.Tasks); err != nil {
		return nil, err
	}
	if err := policy.ValidateQualityDefinition(p.Criteria, p.Checks, p.Reviewer); err != nil {
		return nil, err
	}
	if err := p.Restrictions.Validate(); err != nil {
		return nil, err
	}
	var revision int
	var state string
	err := tx.QueryRowContext(ctx, "SELECT revision,state FROM plans WHERE id=?", p.ID).Scan(&revision, &state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil && state != "draft" && state != "ready" {
		return nil, errors.New("plan must be inactive for definition changes")
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM tasks WHERE plan_id=?", p.ID)
	if err != nil {
		return nil, err
	}
	current := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		current[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, task := range p.Tasks {
		delete(current, task.ID)
	}
	if len(current) != 0 {
		return nil, errors.New("plan.put cannot remove existing tasks; use a reviewed change proposal")
	}
	revision++
	b, _ := json.Marshal(p)
	newState := "draft"
	var accepted any
	if p.Approved {
		accepted = store.Now()
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO plans(id,revision,queue_rank,state,service_limit_ms,created_at) VALUES(?,?,0,?,1800000,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,state=excluded.state", p.ID, revision, newState, store.Now()); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO plan_revisions VALUES(?,?,?,?,?,?)", p.ID, revision, store.Digest([]byte(p.Specification)), string(b), "human", accepted); err != nil {
		return nil, err
	}
	for rank, task := range p.Tasks {
		definition, _ := json.Marshal(task)
		criteria, _ := json.Marshal(task.Criteria)
		digest := store.Digest(definition)
		cd := store.Digest(criteria)
		var tr int
		var oldPlan, oldState, oldDigest, oldCriteria string
		err = tx.QueryRowContext(ctx, "SELECT t.plan_id,t.state,t.revision,r.definition_digest,r.criteria_digest FROM tasks t JOIN task_revisions r ON r.task_id=t.id AND r.revision=t.revision WHERE t.id=?", task.ID).Scan(&oldPlan, &oldState, &tr, &oldDigest, &oldCriteria)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if oldPlan != "" && oldPlan != p.ID {
			return nil, errors.New("task belongs to another plan")
		}
		if oldState != "" && oldState != "draft" && oldState != "ready" && oldState != "blocked" {
			return nil, errors.New("cannot revise active or accepted work")
		}
		changed := digest != oldDigest
		if changed {
			tr++
			var authorization any
			if oldCriteria != "" && oldCriteria != cd {
				if !p.AuthorizeCriteriaChanges {
					return nil, errors.New("criteria change requires explicit human authorization")
				}
				authorization = cmd.CommandID
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO tasks(id,plan_id,revision,kind,state,rank,active_limit_ms,repair_limit,infra_limit) VALUES(?,?,?,'implementation','draft',?,?,?,1) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,state='draft',rank=excluded.rank,active_limit_ms=excluded.active_limit_ms,repair_limit=excluded.repair_limit", task.ID, p.ID, tr, rank, task.ActiveLimitMS, task.RepairLimit); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO task_revisions VALUES(?,?,?,?,?,?,?)", task.ID, tr, string(definition), cd, digest, "human", authorization); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE task_id=? AND state='pending'", store.Now(), task.ID); err != nil {
				return nil, err
			}
		} else {
			if _, err = tx.ExecContext(ctx, "UPDATE tasks SET rank=? WHERE id=?", rank, task.ID); err != nil {
				return nil, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM task_dependencies WHERE plan_id=?", p.ID); err != nil {
		return nil, err
	}
	for _, task := range p.Tasks {
		for _, dep := range task.Dependencies {
			if _, err = tx.ExecContext(ctx, "INSERT INTO task_dependencies VALUES(?,?,?)", p.ID, task.ID, dep); err != nil {
				return nil, err
			}
		}
	}
	return map[string]any{"plan_id": p.ID, "plan_revision": revision, "state": "draft"}, nil
}
func (e *Engine) reorder(ctx context.Context, tx *store.Tx, actor Authority, cmd Envelope) (any, error) {
	var p struct {
		PlanID string   `json:"plan_id"`
		Tasks  []string `json:"tasks"`
	}
	if err := store.Decode(cmd.Payload, &p); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,state FROM tasks WHERE plan_id=?", p.PlanID)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			rows.Close()
			return nil, err
		}
		if state != "draft" && state != "ready" && state != "blocked" {
			rows.Close()
			return nil, errors.New("cannot reorder active/accepted work")
		}
		ids[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) != len(p.Tasks) {
		return nil, errors.New("reorder must preserve the task set")
	}
	for rank, id := range p.Tasks {
		if !ids[id] {
			return nil, errors.New("duplicate or foreign task")
		}
		delete(ids, id)
		if _, err = tx.ExecContext(ctx, "UPDATE tasks SET rank=? WHERE id=?", rank, id); err != nil {
			return nil, err
		}
	}
	var revision int
	var definition, spec string
	var accepted any
	if err = tx.QueryRowContext(ctx, "SELECT p.revision,r.definition_json,r.spec_digest,r.accepted_at FROM plans p JOIN plan_revisions r ON r.plan_id=p.id AND r.revision=p.revision WHERE p.id=?", p.PlanID).Scan(&revision, &definition, &spec, &accepted); err != nil {
		return nil, err
	}
	var plan Plan
	if err = json.Unmarshal([]byte(definition), &plan); err != nil {
		return nil, err
	}
	byID := map[string]policy.Task{}
	for _, task := range plan.Tasks {
		byID[task.ID] = task
	}
	plan.Tasks = nil
	for _, id := range p.Tasks {
		plan.Tasks = append(plan.Tasks, byID[id])
	}
	b, _ := json.Marshal(plan)
	if _, err = tx.ExecContext(ctx, "INSERT INTO plan_revisions VALUES(?,?,?,?,?,?)", p.PlanID, revision+1, spec, string(b), string(actor), accepted); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE plans SET revision=revision+1 WHERE id=?", p.PlanID)
	return map[string]int{"plan_revision": revision + 1}, err
}

type Readiness struct {
	Project           Project         `json:"project"`
	DefinitionIssues  []string        `json:"definition_issues"`
	RuntimeIssues     []string        `json:"runtime_issues"`
	Tasks             []TaskReadiness `json:"tasks"`
	ExecutionEligible bool            `json:"execution_eligible"`
}
type TaskReadiness struct {
	Restrictions   policy.Restrictions `json:"restrictions"`
	PolicyOrigins  map[string][]string `json:"policy_origins"`
	ID             string              `json:"id"`
	State          string              `json:"state"`
	Revision       int                 `json:"revision"`
	Issues         []string            `json:"issues"`
	RequiredChecks []string            `json:"required_checks"`
}

func (e *Engine) Readiness(ctx context.Context) (Readiness, error) {
	tx, err := e.DB.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Readiness{}, err
	}
	defer tx.Rollback()
	return e.readiness(ctx, tx)
}
func (e *Engine) readiness(ctx context.Context, tx *sql.Tx) (Readiness, error) {
	r := Readiness{DefinitionIssues: []string{}, RuntimeIssues: []string{"exact trusted execution qualification is required at effect start", "no selected live combination has the complete production launch/recovery evidence set"}, Tasks: []TaskReadiness{}}
	err := tx.QueryRowContext(ctx, "SELECT id,root,revision,state FROM project").Scan(&r.Project.ID, &r.Project.Root, &r.Project.Revision, &r.Project.State)
	if err != nil {
		return r, err
	}
	var config policy.Config
	var raw string
	err = tx.QueryRowContext(ctx, "SELECT c.resolved_json FROM project_configurations p JOIN config_snapshots c ON c.id=p.config_id ORDER BY p.revision DESC LIMIT 1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		r.DefinitionIssues = append(r.DefinitionIssues, "explicit project policy/configuration required")
	} else if err != nil {
		return r, err
	} else if err = json.Unmarshal([]byte(raw), &config); err != nil {
		return r, err
	}
	profiles := map[string]policy.Profile{}
	checks := map[string]bool{}
	for _, check := range config.CheckDefinitions {
		checks[check.ID] = true
	}
	for _, id := range config.RequiredChecks {
		if !checks[id] {
			r.DefinitionIssues = append(r.DefinitionIssues, "required check definition missing: "+id)
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT c.resolved_json FROM profiles p JOIN config_snapshots c ON c.id=p.config_id WHERE p.revision=(SELECT max(revision) FROM profiles WHERE id=p.id)")
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return r, err
		}
		var p policy.Profile
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			rows.Close()
			return r, err
		}
		profiles[p.ID] = p
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	if p, ok := profiles[config.SupervisorProfile]; !ok {
		r.DefinitionIssues = append(r.DefinitionIssues, "supervisor profile missing")
	} else {
		r.DefinitionIssues = append(r.DefinitionIssues, policy.Eligibility(config, p, "supervisor")...)
	}
	rows, err = tx.QueryContext(ctx, "SELECT t.id,t.state,t.revision,r.definition_json,pr.definition_json FROM tasks t JOIN task_revisions r ON r.task_id=t.id AND r.revision=t.revision JOIN plans p ON p.id=t.plan_id JOIN plan_revisions pr ON pr.plan_id=p.id AND pr.revision=p.revision ORDER BY t.plan_id,t.rank,t.id")
	if err != nil {
		return r, err
	}
	definitions := []policy.Task{}
	planRestrictions := []policy.Restrictions{}
	states := map[string]string{}
	for rows.Next() {
		var task TaskReadiness
		var raw string
		var planRaw string
		if err := rows.Scan(&task.ID, &task.State, &task.Revision, &raw, &planRaw); err != nil {
			rows.Close()
			return r, err
		}
		var d policy.Task
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			rows.Close()
			return r, err
		}
		definitions = append(definitions, d)
		var plan Plan
		if err := json.Unmarshal([]byte(planRaw), &plan); err != nil {
			rows.Close()
			return r, err
		}
		planRestrictions = append(planRestrictions, plan.Restrictions)
		states[task.ID] = task.State
		r.Tasks = append(r.Tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	for n, d := range definitions {
		t := &r.Tasks[n]
		t.Issues = []string{}
		resolved, err := policy.Resolve([]policy.Layer{{Name: "project", Restrictions: config.Restrictions, Checks: config.RequiredChecks, Deny: config.Deny}, {Name: "plan", Restrictions: planRestrictions[n]}, {Name: "task", Restrictions: d.Restrictions, Checks: d.Checks}})
		if err != nil {
			return r, err
		}
		taskConfig := config
		taskConfig.Restrictions = resolved.Restrictions
		t.Restrictions = resolved.Restrictions
		t.PolicyOrigins = resolved.Origins
		for _, dep := range d.Dependencies {
			if states[dep] != "accepted" {
				t.Issues = append(t.Issues, "dependency not accepted: "+dep)
			}
		}
		for role, id := range map[string]string{"implementation": d.Implementation, "review": d.Reviewer, "supervisor": config.SupervisorProfile} {
			p, ok := profiles[id]
			if !ok {
				t.Issues = append(t.Issues, "missing "+role+" profile: "+id)
			} else {
				t.Issues = append(t.Issues, policy.Eligibility(taskConfig, p, role)...)
			}
		}
		for _, question := range d.Questions {
			t.Issues = append(t.Issues, "unresolved clarification: "+question)
		}
		for _, prerequisite := range d.ManualPrerequisites {
			if !prerequisite.Satisfied {
				t.Issues = append(t.Issues, "manual prerequisite unsatisfied: "+prerequisite.ID)
			}
		}
		if config.TaskLimitMS > 0 && d.ActiveLimitMS > config.TaskLimitMS {
			t.Issues = append(t.Issues, "task allowance exceeds project ceiling")
		}
		if d.RepairLimit > config.RepairLimit {
			t.Issues = append(t.Issues, "repair allowance exceeds project ceiling")
		}
		t.RequiredChecks = resolved.Checks
		for _, id := range resolved.Checks {
			if !checks[id] {
				t.Issues = append(t.Issues, "check definition missing: "+id)
			}
		}
		sort.Strings(t.Issues)
	}
	var unapproved int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM plans p JOIN plan_revisions r ON r.plan_id=p.id AND r.revision=p.revision WHERE r.accepted_at IS NULL").Scan(&unapproved); err != nil {
		return r, err
	}
	if unapproved > 0 {
		r.DefinitionIssues = append(r.DefinitionIssues, "plan specification approval required")
	}
	if len(r.Tasks) == 0 {
		r.DefinitionIssues = append(r.DefinitionIssues, "no plan tasks defined")
	}
	sort.Strings(r.DefinitionIssues)
	return r, nil
}

// ExecutionEligibility binds a launcher's exact runtime inputs to the latest
// persisted profile revision before consulting trusted qualification evidence.
// It performs no launch and is the API Stage 5.2 must call again immediately
// before an external effect starts.
func (e *Engine) ExecutionEligibility(ctx context.Context, request boundary.EligibilityRequest) (boundary.Eligibility, error) {
	result := boundary.Eligibility{Status: "unsupported", Reasons: []string{}}
	var digest, raw string
	err := e.DB.SQL.QueryRowContext(ctx, `
		SELECT c.digest,c.resolved_json
		FROM profiles p JOIN config_snapshots c ON c.id=p.config_id
		WHERE p.id=? AND p.revision=?
		  AND p.revision=(SELECT max(revision) FROM profiles WHERE id=p.id)`,
		request.Inputs.ProfileID, request.Inputs.ProfileRevision).Scan(&digest, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		result.Reasons = []string{"execution inputs do not reference the latest persisted profile revision"}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var profile policy.Profile
	if err := json.Unmarshal([]byte(raw), &profile); err != nil {
		return result, err
	}
	if digest != request.Inputs.ProfileDigest {
		result.Reasons = append(result.Reasons, "execution profile digest does not match persisted configuration")
	}
	if profile.Harness != request.Inputs.Harness || profile.Version != request.Inputs.HarnessVersion || profile.Model != request.Inputs.Model || profile.Provider != request.Inputs.Provider {
		result.Reasons = append(result.Reasons, "execution harness, version, model or provider does not match persisted profile")
	}
	if profile.EndpointID != "" && profile.EndpointID != request.Inputs.EndpointAuthority {
		result.Reasons = append(result.Reasons, "execution endpoint authority does not match persisted profile")
	}
	if !policy.Contains(profile.Roles, request.Role) {
		result.Reasons = append(result.Reasons, "persisted profile does not permit role: "+request.Role)
	}
	if len(result.Reasons) != 0 {
		sort.Strings(result.Reasons)
		return result, nil
	}
	return boundary.QueryEligibility(ctx, e.DB, request)
}

func DefaultStateDir() (string, error) {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		if !filepath.IsAbs(base) {
			return "", errors.New("XDG_STATE_HOME must be absolute")
		}
		return filepath.Join(base, "vigil"), nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "vigil"), err
}
