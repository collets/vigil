package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vigil/internal/policy"
	"vigil/internal/store"
)

func setup(t *testing.T) (*Manager, *Engine, Project) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := OpenManager(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	p, err := m.Init(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	e, err := m.Open(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.DB.Close() })
	return m, e, p
}
func envelope(t *testing.T, e *Engine, kind string, payload any) Envelope {
	t.Helper()
	var revision int
	if err := e.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Envelope{store.ID(), revision, kind, b}
}
func apply(t *testing.T, e *Engine, kind string, payload any) json.RawMessage {
	t.Helper()
	result, err := e.Apply(context.Background(), Human, envelope(t, e, kind, payload))
	if err != nil {
		t.Fatal(kind, err)
	}
	return result
}
func config() policy.Config {
	return policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"project-check"}, TaskLimitMS: 2700000, AttemptLimitMS: 600000, RepairLimit: 2, SupervisorProfile: "local", ApprovalMode: "supervised"}
}
func profile() policy.Profile {
	return policy.Profile{ID: "local", Harness: "hermes", Version: "0.21.3", Model: "fixture-local", Provider: "custom", CredentialRef: "env:VIGIL_LLAMA_API_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "windows-llama", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
}
func plan() Plan {
	return Plan{ID: "plan", Title: "Fixture", Specification: "A small human-authored specification", Approved: true, Tasks: []policy.Task{
		{ID: "first", Objective: "First change", Criteria: []policy.Criterion{{ID: "c1", Text: "Check result"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"task-check"}, Difficulty: "small", Rationale: "one file", ActiveLimitMS: 600000, RepairLimit: 2},
		{ID: "second", Objective: "Follow-up", Criteria: []policy.Criterion{{ID: "c2", Text: "Verify manually", Manual: true}}, Scope: []string{"src/**"}, Dependencies: []string{"first"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "one file", ActiveLimitMS: 600000, RepairLimit: 2},
	}}
}
func resultString(t *testing.T, b json.RawMessage, key string) string {
	t.Helper()
	var v struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	s, ok := v.Result[key].(string)
	if !ok {
		t.Fatalf("missing result %s: %s", key, b)
	}
	return s
}

func TestProjectIdentityAndDefinitionCommands(t *testing.T) {
	m, e, p := setup(t)
	ctx := context.Background()
	alias := filepath.Join(filepath.Dir(p.Root), "alias")
	if err := os.Symlink(p.Root, alias); err != nil {
		t.Fatal(err)
	}
	again, err := m.Init(ctx, alias)
	if err != nil || again.ID != p.ID {
		t.Fatal("project identity replay", again, err)
	}
	nested := filepath.Join(p.Root, "nested")
	if err = os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Init(ctx, nested); err == nil {
		t.Fatal("nested registration bypass")
	}
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	command := envelope(t, e, "plan.put", plan())
	first, err := e.Apply(ctx, Human, command)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.Apply(ctx, Human, command)
	if err != nil || string(replay) != string(first) {
		t.Fatal("command replay", err)
	}
	stale := command
	stale.CommandID = store.ID()
	if _, err = e.Apply(ctx, Human, stale); err == nil {
		t.Fatal("stale revision accepted")
	}
	r, err := e.Readiness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.ExecutionEligible || len(r.RuntimeIssues) == 0 || len(r.Tasks) != 2 {
		t.Fatal("unsafe readiness", r)
	}
	if len(r.DefinitionIssues) != 0 || len(r.Tasks[0].RequiredChecks) != 2 || len(r.Tasks[1].Issues) != 1 {
		t.Fatal("readiness details", r)
	}
	changed := plan()
	changed.Tasks[0].Criteria[0].Text = "weaker criteria"
	if _, err = e.Apply(ctx, Human, envelope(t, e, "plan.put", changed)); err == nil {
		t.Fatal("silent criteria change")
	}
	changed.AuthorizeCriteriaChanges = true
	apply(t, e, "plan.put", changed)
	if _, err = e.Apply(ctx, Worker, envelope(t, e, "project.configure", config())); err == nil {
		t.Fatal("worker gained authority")
	}
	if _, err = e.Apply(ctx, Human, envelope(t, e, "task.accept", map[string]string{})); err == nil {
		t.Fatal("invented acceptance command")
	}
	var taskRevision int
	e.DB.SQL.QueryRow("SELECT revision FROM tasks WHERE id='first'").Scan(&taskRevision)
	reorder := envelope(t, e, "plan.reorder", map[string]any{"plan_id": "plan", "tasks": []string{"second", "first"}})
	if _, err = e.Apply(ctx, Supervisor, reorder); err != nil {
		t.Fatal(err)
	}
	var after int
	e.DB.SQL.QueryRow("SELECT revision FROM tasks WHERE id='first'").Scan(&after)
	if after != taskRevision {
		t.Fatal("reorder invalidated definition")
	}
}
func TestCycleAndProfileRestrictions(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	p := profile()
	p.LocalInference = false
	p.AuxiliaryLocal = false
	apply(t, e, "profile.put", p)
	bad := plan()
	bad.Tasks[0].Dependencies = []string{"second"}
	if _, err := e.Apply(context.Background(), Human, envelope(t, e, "plan.put", bad)); err == nil {
		t.Fatal("cycle accepted")
	}
	var count int
	e.DB.SQL.QueryRow("SELECT count(*) FROM plans").Scan(&count)
	if count != 0 {
		t.Fatal("partial plan committed")
	}
	apply(t, e, "plan.put", plan())
	r, err := e.Readiness(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tasks[0].Issues) < 2 {
		t.Fatal("cloud profile accepted by local-only", r)
	}
}
func TestGrantStartRevocationAndConsumption(t *testing.T) {
	_, e, _ := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	operation := OperationRequest{Category: "commit", ResourceDigest: strings.Repeat("a", 64), ArgumentsDigest: strings.Repeat("b", 64), PlanID: "plan", TaskID: "first", TaskRevision: 1}
	requested := apply(t, e, "operation.request", operation)
	op := resultString(t, requested, "operation_id")
	request := resultString(t, requested, "request_id")
	granted := apply(t, e, "permission.grant", GrantRequest{RequestID: request, Scope: "once", Decision: "allow"})
	grant := resultString(t, granted, "grant_id")
	if _, err := e.Apply(ctx, Human, envelope(t, e, "permission.grant", GrantRequest{RequestID: request, Scope: "once", Decision: "allow"})); err == nil {
		t.Fatal("duplicate request resolution")
	}
	start := envelope(t, e, "operation.start", map[string]string{"operation_id": op, "grant_id": grant})
	if _, err := e.Apply(ctx, Human, start); err == nil {
		t.Fatal("CLI forged effect start")
	}
	if _, err := e.Apply(ctx, Core, start); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, Core, envelope(t, e, "operation.start", map[string]string{"operation_id": op, "grant_id": grant})); err == nil {
		t.Fatal("effect replay")
	}
	second := apply(t, e, "operation.request", operation)
	op2 := resultString(t, second, "operation_id")
	if _, err := e.Apply(ctx, Core, envelope(t, e, "operation.start", map[string]string{"operation_id": op2, "grant_id": grant})); err == nil {
		t.Fatal("once grant reuse")
	}
	grant2 := resultString(t, apply(t, e, "permission.grant", GrantRequest{RequestID: resultString(t, second, "request_id"), Scope: "task", Decision: "allow"}), "grant_id")
	apply(t, e, "permission.revoke", map[string]string{"grant_id": grant2})
	if _, err := e.Apply(ctx, Core, envelope(t, e, "operation.start", map[string]string{"operation_id": op2, "grant_id": grant2})); err == nil {
		t.Fatal("revoked grant authorized")
	}
	c := config()
	c.Deny = []string{"commit"}
	apply(t, e, "project.configure", c)
	if _, err := e.Apply(ctx, Human, envelope(t, e, "operation.request", operation)); err == nil {
		t.Fatal("project deny bypass")
	}
}
func TestStaleApprovalOnTaskRevision(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	r := apply(t, e, "operation.request", OperationRequest{Category: "commit", ResourceDigest: strings.Repeat("a", 64), ArgumentsDigest: strings.Repeat("b", 64), PlanID: "plan", TaskID: "first", TaskRevision: 1})
	p := plan()
	p.Tasks[0].Objective = "Changed work"
	apply(t, e, "plan.put", p)
	if _, err := e.Apply(context.Background(), Human, envelope(t, e, "permission.grant", GrantRequest{RequestID: resultString(t, r, "request_id"), Scope: "once", Decision: "allow"})); err == nil {
		t.Fatal("stale approval accepted")
	}
}
