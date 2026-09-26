package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vigil/internal/core"
	"vigil/internal/mcp"
	"vigil/internal/policy"
	"vigil/internal/store"
	modeltools "vigil/internal/tools"
)

type fixture struct {
	engine   *core.Engine
	handler  *modeltools.Handler
	spec     core.SpecificationRevision
	revision int
}

func setupTools(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := core.OpenManager(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := manager.Init(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.DB.Close(); manager.Close() })
	apply := func(kind string, payload any) {
		var revision int
		engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
		raw, _ := json.Marshal(payload)
		if _, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: kind, Payload: raw}); err != nil {
			t.Fatal(kind, err)
		}
	}
	config := policy.Config{ModelPolicy: "local_only", TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE", Roles: []string{"implementation", "review", "supervisor", "planning"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	apply("project.configure", config)
	apply("profile.put", profile)
	path := filepath.Join(root, "spec.md")
	if err := os.WriteFile(path, []byte("# bounded fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var revision int
	engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	spec, err := engine.ImportMarkdown(ctx, "spec-import", revision, "spec", path)
	if err != nil {
		t.Fatal(err)
	}
	revision++
	plan := core.Plan{ID: "plan", Title: "fixture", Specification: "fixture", Approved: true, Tasks: []policy.Task{{ID: "task", Objective: "fixture", Criteria: []policy.Criterion{{ID: "c", Text: "done"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}}}
	raw, _ := json.Marshal(plan)
	if _, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: "plan-put", ExpectedRevision: revision, Kind: "plan.put", Payload: raw}); err != nil {
		t.Fatal(err)
	}
	revision++
	var configID string
	engine.DB.SQL.QueryRow("SELECT config_id FROM project_configurations ORDER BY revision DESC LIMIT 1").Scan(&configID)
	if _, err := engine.DB.SQL.Exec(`INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES('run','plan',1,'task',1,?,'local',1,'implementation','initial','active','unconfirmed',1,1,1)`, configID); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.DB.SQL.Exec(`INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,native_session_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES('generation-id','run',1,'native','native-resource','native-implementation','generation-implementation','active','delivered','{}','fixture-digest','[]',1)`); err != nil {
		t.Fatal(err)
	}
	return fixture{engine: engine, handler: &modeltools.Handler{Engine: engine}, spec: spec, revision: revision}
}

func TestBoundedRoleScopedHandlersAndSharedTransport(t *testing.T) {
	f := setupTools(t)
	ctx := context.Background()
	planning, err := f.handler.OpenSession(ctx, "open-planning", modeltools.Authority{Role: "planning", NativeSessionID: "native-planning", Generation: "generation-planning"})
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := f.handler.OpenSession(ctx, "open-narrow", modeltools.Authority{Role: "planning", NativeSessionID: "native-narrow", Generation: "generation-narrow", Capabilities: []string{"project.read"}})
	if err != nil || len(narrow.Capabilities) != 1 || narrow.Capabilities[0] != "project.read" {
		t.Fatal("valid server-side capability reduction failed", narrow, err)
	}
	if _, err := f.handler.OpenSession(ctx, "open-expanded", modeltools.Authority{Role: "planning", NativeSessionID: "native-expanded", Generation: "generation-expanded", Capabilities: []string{"project.read", "plan.reorder"}}); err == nil {
		t.Fatal("session capability expansion was accepted")
	}
	implementation, err := f.handler.OpenSession(ctx, "open-implementation", modeltools.Authority{Role: "implementation", RunID: "run", NativeSessionID: "native-implementation", Generation: "generation-implementation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.handler.Call(ctx, planning.ID, "project.read", []byte(`{"limit":100,"unknown":true}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err = f.handler.Call(ctx, planning.ID, "project.read", []byte(`{"limit":1,"limit":2}`)); err == nil {
		t.Fatal("duplicate field accepted")
	}
	if _, err = f.handler.Call(ctx, planning.ID, "project.read", bytes.Repeat([]byte("x"), modeltools.MaxPayload+1)); err == nil {
		t.Fatal("oversized payload accepted")
	}
	page, err := f.handler.Call(ctx, planning.ID, "project.read", []byte(`{"limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte(`"task"`)) {
		t.Fatal("bounded project page missing task", string(page))
	}
	if _, err = f.handler.Call(ctx, planning.ID, "task.read", []byte(`{"id":"foreign","revision":1}`)); err == nil {
		t.Fatal("foreign task read")
	}
	excerpt, err := f.handler.Call(ctx, planning.ID, "artifact.read", []byte(`{"id":"`+f.spec.ArtifactID+`","offset":0,"length":9}`))
	if err != nil || !bytes.Contains(excerpt, []byte("bounded")) {
		t.Fatal(string(excerpt), err)
	}
	if _, err = f.handler.Call(ctx, planning.ID, "artifact.read", []byte(`{"id":"/etc/passwd","offset":0,"length":8}`)); err == nil {
		t.Fatal("arbitrary path accepted")
	}
	if _, err = f.handler.Call(ctx, planning.ID, "artifact.read", []byte(`{"id":"`+f.spec.ArtifactID+`","offset":0,"length":32769}`)); err == nil {
		t.Fatal("oversized excerpt accepted")
	}
	blocked := `{"command_id":"blocked-observation","task_id":"task","expected_task_revision":1,"reason":"missing fixture fact","missing_facts":["fact"],"evidence_ids":[]}`
	result, err := f.handler.Call(ctx, implementation.ID, "task.report_blocked", []byte(blocked))
	if err != nil || !bytes.Contains(result, []byte(`"task_state":"draft"`)) {
		t.Fatal(string(result), err)
	}
	var state string
	f.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id='task'").Scan(&state)
	if state != "draft" {
		t.Fatal("model observation set task state", state)
	}
	if _, err = f.handler.Call(ctx, implementation.ID, "plan.propose_change", []byte(`{}`)); err == nil {
		t.Fatal("implementation gained planning authority")
	}
	if _, err = f.engine.DB.SQL.Exec("UPDATE run_generations SET state='terminal',terminal_at=2 WHERE id='generation-id'"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.handler.Call(ctx, implementation.ID, "project.read", []byte(`{}`)); err == nil {
		t.Fatal("terminal generation retained tool authority")
	}
	proposal := core.ProposalRequest{ID: "model-proposal", SpecificationID: "spec", SpecificationRevision: 1, ProfileID: "local", ProfileRevision: 1, Operation: "edit", ExpectedPlanID: "plan", ExpectedPlanRevision: 1, AffectedTasks: []string{"task"}, Rationale: "fixture", Plan: core.Plan{ID: "plan", Title: "fixture", Specification: "fixture", Approved: true, Tasks: []policy.Task{{ID: "task", Objective: "fixture", Criteria: []policy.Criterion{{ID: "c", Text: "done"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 1}}}}
	proposalRaw, _ := json.Marshal(map[string]any{"command_id": "model-proposal-command", "expected_project_revision": f.revision, "proposal": proposal})
	if _, err = f.handler.Call(ctx, planning.ID, "plan.propose_change", proposalRaw); err == nil {
		t.Fatal("model self-approval accepted")
	}
	proposal.ID = "model-proposal-valid"
	proposal.Plan.Approved = false
	proposal.Plan.Title = "proposed title only"
	proposal.Plan.Tasks[0].Criteria[0].Text = "weaker model-proposed criterion"
	proposalRaw, _ = json.Marshal(map[string]any{"command_id": "model-proposal-valid-command", "expected_project_revision": f.revision, "proposal": proposal})
	if _, err = f.handler.Call(ctx, planning.ID, "plan.propose_change", proposalRaw); err != nil {
		t.Fatal(err)
	}
	var proposalState, taskState string
	if err = f.engine.DB.SQL.QueryRow("SELECT state FROM planning_proposals WHERE id='model-proposal-valid'").Scan(&proposalState); err != nil || proposalState != "proposed" {
		t.Fatal("proposal missing", proposalState, err)
	}
	if err = f.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id='task'").Scan(&taskState); err != nil || taskState != "draft" {
		t.Fatal("proposal mutated task", taskState, err)
	}
	var approvalRequests int
	f.engine.DB.SQL.QueryRow("SELECT count(*) FROM requests WHERE kind='approval' AND state='pending'").Scan(&approvalRequests)
	if approvalRequests != 1 {
		t.Fatal("proposal did not create one approval inbox item", approvalRequests)
	}
	applyPayload, _ := json.Marshal(map[string]any{"proposal_id": "model-proposal-valid", "proposal_revision": 1, "authorize_criteria_changes": false})
	if _, err = f.engine.Apply(ctx, core.Human, core.Envelope{CommandID: "apply-without-criteria-authority", ExpectedRevision: f.revision, Kind: "planning.proposal.apply", Payload: applyPayload}); err == nil {
		t.Fatal("model criteria relaxation applied without distinct human authority")
	}
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "task.read", "arguments": map[string]any{"id": "task", "revision": 1}}}
	wire, _ := json.Marshal(request)
	var output bytes.Buffer
	if err := (&mcp.Server{Handler: f.handler, SessionID: planning.ID}).Serve(ctx, bytes.NewReader(append(wire, '\n')), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `\"state\":\"draft\"`) {
		t.Fatal("MCP did not reuse handler", output.String())
	}
}
