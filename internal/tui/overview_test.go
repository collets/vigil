package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
)

func testConfig() policy.Config {
	return policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"project-check"}, CheckDefinitions: []policy.CheckDefinition{{ID: "project-check", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}, {ID: "task-check", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 2700000, AttemptLimitMS: 600000, RepairLimit: 2, SupervisorProfile: "local", ApprovalMode: "supervised"}
}

func testProfile() policy.Profile {
	return policy.Profile{ID: "local", Harness: "hermes", Version: "0.21.3", Model: "fixture-local", Provider: "custom", CredentialRef: "env:VIGIL_TEST_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "windows-llama", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
}

func testTask(id string, deps ...string) policy.Task {
	return policy.Task{ID: id, Objective: "Objective " + id, Criteria: []policy.Criterion{{ID: "c-" + id, Text: "Check " + id}}, Scope: []string{"src/**"}, Dependencies: deps, Implementation: "local", Reviewer: "local", Checks: []string{"task-check"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 600000, RepairLimit: 2}
}

func tuiApply(t *testing.T, e *core.Engine, kind string, payload any) {
	t.Helper()
	ctx := context.Background()
	var revision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: kind, Payload: raw}); err != nil {
		t.Fatal(kind, err)
	}
}

// seedMainDashboard builds the P15 fixture: a paused project with plan-a
// active, plan-b queued behind it, task t1 blocked, a live-shaped run on
// t1, one blocking and one non-blocking request.
func seedMainDashboard(t *testing.T, e *core.Engine) {
	t.Helper()
	ctx := context.Background()
	tuiApply(t, e, "project.configure", testConfig())
	tuiApply(t, e, "profile.put", testProfile())
	tuiApply(t, e, "plan.put", core.Plan{ID: "plan-a", Title: "A", Specification: "Spec A", Approved: true, Tasks: []policy.Task{testTask("t1"), testTask("t2", "t1")}})
	tuiApply(t, e, "plan.put", core.Plan{ID: "plan-b", Title: "B", Specification: "Spec B", Approved: true, Tasks: []policy.Task{testTask("t3")}})
	var revision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueuePlan(ctx, store.ID(), revision, "plan-a", 0); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueuePlan(ctx, store.ID(), revision, "plan-b", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.SQL.ExecContext(ctx, "UPDATE tasks SET state='blocked',block_reason='waiting on approval x' WHERE id='t1'"); err != nil {
		t.Fatal(err)
	}
	var configID string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT id FROM config_snapshots LIMIT 1").Scan(&configID); err != nil {
		t.Fatal(err)
	}
	var profileRev, planRev, taskRev int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM profiles WHERE id='local'").Scan(&profileRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM plans WHERE id='plan-a'").Scan(&planRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id='t1'").Scan(&taskRev); err != nil {
		t.Fatal(err)
	}
	now := store.Now()
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES('run-p15','plan-a',?,'t1',?,?,'local',?,'implementation','initial','active','unconfirmed',600000,1800000,?)`, planRev, taskRev, configID, profileRev, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES('gen-p15','run-p15',1,'synthetic','res-p15','tp-p15','active','writing','{}','d','[]',?)`, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,context_json,blocking,created_at,deadline) VALUES('req-block','approval','pending','plan-a','t1',1,'{}',1,?,?)`, now-300000, now+1800000); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,context_json,blocking,created_at,deadline) VALUES('req-open','input','pending','plan-a','t1',1,'{}',0,?,?)`, now-30000, now+1800000)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func openTestEngine(t *testing.T) (*core.Manager, *core.Engine) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := core.OpenManager(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	project, err := manager.Init(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.DB.Close() })
	return manager, engine
}

// TestMainDashboardShowsP15WithoutNavigation is the P15 assertion: from a
// cold start with no navigation key, one screen shows the active plan, the
// current task and its blocker, the queue position, the run state, the
// compact quality roll-up and both requests with age and blocking state.
func TestMainDashboardShowsP15WithoutNavigation(t *testing.T) {
	_, engine := openTestEngine(t)
	seedMainDashboard(t, engine)
	snapshot, err := engine.Dashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), snapshot: &snapshot, width: 120, height: 40, tab: 0}
	view := m.View().Content
	for _, want := range []string{"Active plan: plan-a", "Current task: t1", "Blocker: waiting on approval", "position 1/2", "run-p15", "Quality per task:", "chk 0/0", "req-block", "req-open", "non-blocking", "unavailable (no observation)", "Session: none"} {
		if !strings.Contains(view, want) {
			t.Fatalf("main screen missing %q:\n%s", want, view)
		}
	}
	// R45/P15 require the live run AND session state on this screen; the
	// remaining run detail (generation, allowed-next, native key) lives
	// one keystroke away and must not flood this screen.
	for _, want := range []string{"Generation:", "Allowed next:", "Native request key:"} {
		if strings.Contains(view, want) {
			t.Fatalf("main screen leaks run detail %q:\n%s", want, view)
		}
	}
	// Age renders on both request lines.
	if strings.Count(view, "age ") < 2 {
		t.Fatal("request ages missing:\n" + view)
	}
	// The blocking request line must say blocking without the non- prefix.
	foundBlocking := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "req-block") && strings.Contains(line, "blocking") && !strings.Contains(line, "non-blocking") {
			foundBlocking = true
		}
	}
	if !foundBlocking {
		t.Fatal("blocking request not marked blocking:\n" + view)
	}
}

// TestQueueKeyTargetsDisplayedPlan is the 6.1-F2 regression test: with two
// queueable plans, u on the first queues the first, and the queue line
// changes on screen.
func TestQueueKeyTargetsDisplayedPlan(t *testing.T) {
	_, engine := openTestEngine(t)
	ctx := context.Background()
	tuiApply(t, engine, "project.configure", testConfig())
	tuiApply(t, engine, "profile.put", testProfile())
	tuiApply(t, engine, "plan.put", core.Plan{ID: "plan-a", Title: "A", Specification: "Spec A", Approved: true, Tasks: []policy.Task{testTask("t1")}})
	tuiApply(t, engine, "plan.put", core.Plan{ID: "plan-b", Title: "B", Specification: "Spec B", Approved: true, Tasks: []policy.Task{testTask("t3")}})
	snapshot, err := engine.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := model{ctx: ctx, snapshot: &snapshot, width: 120, height: 40, tab: 0, mutate: projectMutator(engine, nil, nil)}
	before := m.View().Content
	if !strings.Contains(before, "plan-a · draft") {
		t.Fatal("queue line missing before:\n" + before)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("u did not start a queue mutation")
	}
	result := cmd().(mutationResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.action != "queue:plan-a:0" {
		t.Fatalf("u queued %q, want the displayed first plan", result.action)
	}
	updated, _ = m.Update(result)
	m = updated.(model)
	after, err := engine.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ = m.Update(loaded{snapshot: after})
	m = updated.(model)
	view := m.View().Content
	if !strings.Contains(view, "plan-a · queued") {
		t.Fatal("queue line did not change on screen:\n" + view)
	}
	// The cursor plan is the target: move to the second and queue it.
	updated, _ = m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m = updated.(model)
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if cmd == nil {
		t.Fatal("u did not start a second queue mutation")
	}
	if got := cmd().(mutationResult).action; got != "queue:plan-b:0" {
		t.Fatalf("u queued %q, want the displayed second plan", got)
	}
}

// TestUnavailableMarkerRendersForUnobservedCost proves the honesty rule: a
// quantity the application does not observe renders as explicitly
// unavailable, never as zero.
func TestUnavailableMarkerRendersForUnobservedCost(t *testing.T) {
	m := scopedModel()
	m.tab, m.width, m.height = 0, 120, 40
	view := m.View().Content
	if !strings.Contains(view, "unavailable") {
		t.Fatal("no unavailable marker:\n" + view)
	}
	if strings.Contains(view, "in 0 · out 0") {
		t.Fatal("unobserved usage rendered as zero:\n" + view)
	}
	observed := scopedModel()
	observed.snapshot.Usage = core.UsageSummary{Observed: true, Provenance: "observed", InputTokens: 10, OutputTokens: 20}
	observed.tab, observed.width, observed.height = 0, 120, 40
	if view := observed.View().Content; !strings.Contains(view, "in 10 · out 20") {
		t.Fatal("observed usage not rendered:\n" + view)
	}
	withCost := scopedModel()
	withCost.snapshot.Usage = core.UsageSummary{Observed: true, Provenance: "mixed", InputTokens: 15, OutputTokens: 27, HasCost: true, CostMicrounits: 4500, Currency: "USD"}
	withCost.tab, withCost.width, withCost.height = 0, 120, 40
	if view := withCost.View().Content; !strings.Contains(view, "cost 4500µUSD") {
		t.Fatal("cost not rendered:\n" + view)
	}
}

// TestQueueCursorCannotNameAnUnrenderedPlan is the round 9 P1-1
// regression: with more plans than the Overview renders, the queue cursor
// must never select past the rendered window, so u can never queue a plan
// the screen never showed.
func TestQueueCursorCannotNameAnUnrenderedPlan(t *testing.T) {
	queue := make([]core.PlanQueueEntry, 0, 14)
	for i := 1; i <= 14; i++ {
		queue = append(queue, core.PlanQueueEntry{ID: fmt.Sprintf("plan-%02d", i), Revision: 1, Rank: i - 1, State: "draft"})
	}
	snapshot := core.DashboardSnapshot{
		Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 1, State: "paused"}},
		Queue:     queue,
	}
	snapshot.Progression = core.ProgressionDetail{ProjectID: "fixture", ProjectRev: 1, Queue: queue, QueuePosition: 0}
	snapshot.Progression.ActivePlan = &core.ProgressionPlan{ID: "plan-01", Revision: 1, State: "draft", Rank: 0}
	actions := make(chan string, 64)
	m := model{ctx: context.Background(), snapshot: &snapshot, width: 120, height: 40, tab: 0, mutate: func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		actions <- action
		return nil
	}}
	// Walk the cursor far past the rendered window.
	for i := 0; i < 13; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
		m = updated.(model)
	}
	if m.queueCursor >= maxRenderedQueue {
		t.Fatalf("queue cursor %d is past the rendered window %d", m.queueCursor, maxRenderedQueue)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("u did not start a queue action")
	}
	action := cmd().(mutationResult).action
	_ = updated
	parts := strings.Split(action, ":")
	if len(parts) != 3 {
		t.Fatalf("malformed queue action %q", action)
	}
	index, err := strconv.Atoi(parts[1][len("plan-"):])
	if err != nil || index > maxRenderedQueue {
		t.Fatalf("u queued %q, which is outside the rendered window", action)
	}
	// The rendered rows must carry the focus marker, or nothing is focused.
	if view := m.View().Content; !strings.Contains(view, "> plan-") {
		t.Fatal("no queue row carries the focus marker:\n" + view)
	}
	// The overflow notice must disclose that the remainder is unselectable.
	if view := m.View().Content; !strings.Contains(view, "not selectable here") {
		t.Fatal("queue overflow not disclosed:\n" + view)
	}
}

// TestQueueCursorBoundedOnLoad proves a reload cannot leave the cursor
// pointing past the rendered window.
func TestQueueCursorBoundedOnLoad(t *testing.T) {
	queue := make([]core.PlanQueueEntry, 0, 30)
	for i := 1; i <= 30; i++ {
		queue = append(queue, core.PlanQueueEntry{ID: fmt.Sprintf("plan-%02d", i), Revision: 1, Rank: i - 1, State: "draft"})
	}
	m := model{ctx: context.Background(), snapshot: &core.DashboardSnapshot{Queue: queue}, width: 120, height: 40, tab: 0}
	// Walk the cursor into the overflow first, or the clamp assertion
	// below would pass against any bound because the cursor starts at 0.
	for i := 0; i < 25; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
		m = updated.(model)
	}
	// Now deliver a snapshot whose queue shrank past the window.
	shrunk := make([]core.PlanQueueEntry, 0, 3)
	for i := 1; i <= 3; i++ {
		shrunk = append(shrunk, core.PlanQueueEntry{ID: fmt.Sprintf("plan-%02d", i), Revision: 1, Rank: i - 1, State: "draft"})
	}
	updated, _ := m.Update(loaded{snapshot: core.DashboardSnapshot{Queue: shrunk}})
	m = updated.(model)
	if m.queueCursor >= len(shrunk) {
		t.Fatalf("queue cursor %d past the shrunken queue %d", m.queueCursor, len(shrunk))
	}
	// And a snapshot at the window bound itself.
	updated, _ = m.Update(loaded{snapshot: core.DashboardSnapshot{Queue: queue}})
	m = updated.(model)
	if m.queueCursor >= maxRenderedQueue {
		t.Fatalf("queue cursor %d past window %d after load", m.queueCursor, maxRenderedQueue)
	}
	// The same for the request cursor: it had its own load clamp with a
	// different shape, one past the rendered window. It was unreachable
	// while moveCursor also bounded it, and this pins it for any other
	// writer that sets the cursor without doing so.
	inbox := make([]core.InboxEntry, 0, 25)
	for i := 1; i <= 25; i++ {
		inbox = append(inbox, core.InboxEntry{ID: fmt.Sprintf("req-%02d", i), Kind: "approval"})
	}
	m = model{ctx: context.Background(), snapshot: &core.DashboardSnapshot{Inbox: inbox}, width: 120, height: 40, tab: 0}
	for i := 0; i < 24; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(model)
	}
	shrunkInbox := inbox[:3]
	updated, _ = m.Update(loaded{snapshot: core.DashboardSnapshot{Inbox: shrunkInbox}})
	m = updated.(model)
	if m.ovInbox >= len(shrunkInbox) {
		t.Fatalf("request cursor %d past the shrunken inbox %d", m.ovInbox, len(shrunkInbox))
	}
	updated, _ = m.Update(loaded{snapshot: core.DashboardSnapshot{Inbox: inbox}})
	m = updated.(model)
	if m.ovInbox >= maxRenderedRequests {
		t.Fatalf("request cursor %d past window %d after load", m.ovInbox, maxRenderedRequests)
	}
}

// TestOverviewEnterCannotOpenAnUnrenderedRequest is the P2-1 regression:
// with more requests than the Overview renders, Enter must open a row the
// screen actually showed.
func TestOverviewEnterCannotOpenAnUnrenderedRequest(t *testing.T) {
	inbox := make([]core.InboxEntry, 0, 15)
	for i := 1; i <= 15; i++ {
		inbox = append(inbox, core.InboxEntry{ID: fmt.Sprintf("req-%02d", i), Kind: "approval"})
	}
	snapshot := core.DashboardSnapshot{
		Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 1, State: "paused"}},
		Inbox:     inbox,
	}
	m := model{ctx: context.Background(), snapshot: &snapshot, width: 120, height: 40, tab: 0}
	for i := 0; i < 14; i++ {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(model)
	}
	if m.ovInbox >= maxRenderedRequests {
		t.Fatalf("request cursor %d is past the rendered window", m.ovInbox)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || m.tab != 2 {
		t.Fatal("Enter did not open the inbox")
	}
	if m.inbox >= maxRenderedRequests {
		t.Fatalf("Enter opened inbox row %d, outside the rendered window", m.inbox)
	}
}

// TestUnobservedBudgetRendersUnavailable is the round 9 P0 regression at
// the view layer: a run with no recorded budget segment must render an
// explicit unavailable marker, never a zero.
func TestUnobservedBudgetRendersUnavailable(t *testing.T) {
	m := scopedModel()
	m.tab, m.width, m.height = 0, 120, 40
	m.snapshot.Run = &core.ActiveRunDetail{RunID: "run-1", State: "active", RuntimeKind: "synthetic", BudgetObserved: false, AllowedNext: []string{"inspect"}, ActivitySummary: "no recorded activity"}
	// Assert through the REAL render path. An earlier draft asserted on
	// overviewLines directly, on the premise that the budget block sat
	// below the fold — it does not, at 120x40 it renders at rows 18-20 —
	// so that draft stopped exercising View() for the very P16 property
	// this test exists to pin. View() covers overviewLines transitively.
	view := m.View().Content
	if !strings.Contains(view, "Budgets: unavailable (no recorded segment)") {
		t.Fatalf("unobserved budget not marked unavailable:\n%s", view)
	}
	for _, forbidden := range []string{"active 0ms", "wall 0ms"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("unobserved budget rendered as a zero (%q):\n%s", forbidden, view)
		}
	}
	m.snapshot.Run.BudgetObserved = true
	m.snapshot.Run.ActiveChargedMS = 1200
	m.snapshot.Run.ActiveLimitMS = 600000
	m.snapshot.Run.WallConsumedMS = 5000
	m.snapshot.Run.WallLimitMS = 1800000
	m.snapshot.Run.TaskBudgetObserved = true
	m.snapshot.Run.TaskChargedMS = 40000
	m.snapshot.Run.TaskLimitMS = 2700000
	view = m.View().Content
	// One line per scope, so neither figure can be truncated away.
	if !strings.Contains(view, "Budget (this run): 1200ms of 600000ms active · 0ms unknown") {
		t.Fatalf("attempt-scoped budget not rendered:\n%s", view)
	}
	if !strings.Contains(view, "Budget (task, all attempts): 40000ms of 2700000ms") {
		t.Fatalf("task-cumulative budget not rendered:\n%s", view)
	}
	// The task pair must be absent-safe too: an unobserved ledger must not
	// render as "0ms of 0ms".
	m.snapshot.Run.TaskBudgetObserved = false
	if view := m.View().Content; !strings.Contains(view, "Budget (task, all attempts): unavailable (no recorded ledger)") {
		t.Fatalf("unobserved task budget not marked unavailable:\n%s", view)
	}
}

// TestRunDetailUnobservedBudgetRendersUnavailable covers the run screen
// specifically, which P16 names: an unobserved budget must render an
// explicit marker there too, never a zero.
func TestRunDetailUnobservedBudgetRendersUnavailable(t *testing.T) {
	m := runModel()
	m.snapshot.Run.BudgetObserved = false
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	view := m.View().Content
	if !strings.Contains(view, "Budgets: unavailable (no recorded segment)") {
		t.Fatalf("run screen does not mark an unobserved budget:\n%s", view)
	}
	for _, forbidden := range []string{"active 0ms", "wall 0ms", "all attempts): 0ms of 0ms"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("run screen rendered an unobserved budget as a zero (%q):\n%s", forbidden, view)
		}
	}
	// Observed, but the task ledger absent: the task pair must degrade
	// rather than print a zero pair.
	m.snapshot.Run.BudgetObserved = true
	m.snapshot.Run.ActiveChargedMS = 1200
	m.snapshot.Run.ActiveLimitMS = 600000
	m.snapshot.Run.TaskBudgetObserved = false
	updated, _ = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	view = m.View().Content
	if !strings.Contains(view, "This task (all attempts): unavailable (no recorded ledger)") {
		t.Fatalf("run screen does not mark an unobserved task ledger:\n%s", view)
	}
}

// TestManualRollupUsesTheLatestOutcome is the P2-4 regression: a criterion
// that passed then failed must render as outstanding, not satisfied.
func TestManualRollupUsesTheLatestOutcome(t *testing.T) {
	// ManualOutcomes is newest-first, so the FIRST entry is the latest
	// state. A criterion that failed most recently is outstanding even if
	// an earlier attempt passed it.
	task := core.TaskDetail{ID: "t1", ManualOutcomes: []string{"c1:fail", "c1:pass"}, ManualCriteria: []string{"c1"}}
	if line := qualityRollup(task); !strings.Contains(line, "man 1") {
		t.Fatalf("a latest fail must make the criterion outstanding: %q", line)
	}
	task = core.TaskDetail{ID: "t1", ManualOutcomes: []string{"c1:pass", "c1:fail"}, ManualCriteria: []string{"c1"}}
	if line := qualityRollup(task); !strings.Contains(line, "man 0") {
		t.Fatalf("a latest pass should satisfy the criterion: %q", line)
	}
	task = core.TaskDetail{ID: "t1", ManualOutcomes: []string{"c1:pass"}, ManualCriteria: []string{"c1", "c2"}}
	if line := qualityRollup(task); !strings.Contains(line, "man 1") {
		t.Fatalf("one outstanding criterion expected: %q", line)
	}
}

// TestCompactQualityRollupIsFixedWidth proves the roll-up is a bounded
// single line per task carrying checks, findings, manual state and health.
func TestCompactQualityRollupIsFixedWidth(t *testing.T) {
	task := core.TaskDetail{ID: "long-task-identifier", Revision: 2, CheckOutputs: []string{"c1:pass:a1", "c2:fail:a2"}, BlockingFindings: 1, Suggestions: 2, ManualOutcomes: []string{"m1:pass"}, ManualCriteria: []string{"m1", "m2"}, BaselineUnhealthy: true}
	line := qualityRollup(task)
	if strings.Contains(line, "\n") {
		t.Fatal("roll-up spans lines", line)
	}
	for _, want := range []string{"chk 1/2", "blk 1", "sug 2", "man 1", "unhealthy"} {
		if !strings.Contains(line, want) {
			t.Fatalf("roll-up missing %q: %q", want, line)
		}
	}
	if len([]rune(line)) > 80 {
		t.Fatalf("roll-up not bounded: %q", line)
	}
}

// TestEnterOpensOverviewRequestInInbox proves the main screen is a genuine
// entry point: Enter carries the selected request into the Inbox screen.
func TestEnterOpensOverviewRequestInInbox(t *testing.T) {
	snapshot := core.DashboardSnapshot{
		Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 1, State: "paused"}},
		Inbox:     []core.InboxEntry{{ID: "first", Kind: "approval"}, {ID: "second", Kind: "approval"}},
		Queue:     []core.PlanQueueEntry{{ID: "plan-1", Revision: 1, Rank: 0, State: "draft"}},
	}
	snapshot.Progression = core.ProgressionDetail{ProjectID: "fixture", ProjectRev: 1, ProjectState: "paused", Queue: snapshot.Queue, QueuePosition: 0}
	snapshot.Progression.ActivePlan = &core.ProgressionPlan{ID: "plan-1", Revision: 1, State: "draft", Rank: 0}
	m := model{ctx: context.Background(), snapshot: &snapshot, width: 120, height: 40, tab: 0}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	if m.ovInbox != 1 {
		t.Fatal("overview request cursor did not move")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || m.tab != 2 || m.inbox != 1 {
		t.Fatal("Enter did not open the selected request in Inbox")
	}
	if !strings.Contains(m.View().Content, "second") {
		t.Fatal("Inbox does not focus the opened request")
	}
}
