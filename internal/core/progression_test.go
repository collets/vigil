package core

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"vigil/internal/policy"
	"vigil/internal/store"
)

// TestProgressionSingleSnapshot exposes the P15 state from one transaction:
// active plan, selected task, queue position, eligibility and the exact
// blocking issues, with ActiveRun and Queue as first-class fields.
func TestProgressionSingleSnapshot(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	ctx := context.Background()
	detail, err := e.Progression(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ProjectID == "" || detail.ProjectRev < 1 {
		t.Fatal("progression missing project identity", detail)
	}
	if len(detail.Queue) != 1 || detail.Queue[0].ID != "plan" {
		t.Fatal("progression queue missing the seeded plan", detail.Queue)
	}
	if detail.ActivePlan == nil || detail.ActivePlan.ID != "plan" {
		t.Fatal("progression missing active plan", detail.ActivePlan)
	}
	if detail.QueuePosition != 0 {
		t.Fatal("queue position wrong", detail.QueuePosition)
	}
	if detail.SelectedTask == nil || detail.SelectedTask.ID != "first" {
		t.Fatal("progression missing current task", detail.SelectedTask)
	}
	if len(detail.BlockingIssues) == 0 {
		t.Fatal("progression manufactured eligibility")
	}
	if detail.Eligible {
		t.Fatal("progression eligible despite runtime gates")
	}
	if detail.ActiveRun != "" || detail.Run != nil {
		t.Fatal("progression invented a run")
	}
	if detail.Usage.Observed {
		t.Fatal("progression invented usage")
	}
	snapshot, err := e.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Progression.ActivePlan == nil || snapshot.Progression.ActivePlan.ID != "plan" {
		t.Fatal("dashboard progression not populated")
	}
	if snapshot.Run != nil || snapshot.Usage.Observed {
		t.Fatal("dashboard invented run or usage")
	}
}

// TestProgressionConcurrentWriterCannotTearSnapshot hammers Progression
// while a writer flips the ranked queue order and asserts every snapshot
// satisfies the single-snapshot invariant: the active plan appears in the
// queue at the recorded position. The writer mutates the same rows the
// reader reports (queue order), so a torn read across two revisions would
// be observable here.
func TestProgressionConcurrentWriterCannotTearSnapshot(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	second := plan()
	second.ID = "plan-b"
	second.Title = "Second"
	second.Tasks = []policy.Task{
		{ID: "third", Objective: "Third change", Criteria: []policy.Criterion{{ID: "c3", Text: "Check result"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"task-check"}, Difficulty: "small", Rationale: "one file", ActiveLimitMS: 600000, RepairLimit: 2},
	}
	apply(t, e, "plan.put", second)
	ctx := context.Background()
	var revision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueuePlan(ctx, store.ID(), revision, "plan", 0); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueuePlan(ctx, store.ID(), revision, "plan-b", 1); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = e.DB.Write(ctx, func(tx *store.Tx) error {
				_, err := tx.ExecContext(ctx, "UPDATE plans SET queue_rank=1-queue_rank WHERE id IN('plan','plan-b')")
				return err
			})
		}
	}()
	defer func() { close(stop); writer.Wait() }()
	for n := 0; n < 100; n++ {
		detail, err := e.Progression(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if detail.ActivePlan == nil {
			t.Fatal("active plan vanished mid-write")
		}
		if len(detail.Queue) != 2 {
			t.Fatal("queue lost a plan mid-write", len(detail.Queue))
		}
		if detail.QueuePosition < 0 || detail.QueuePosition >= len(detail.Queue) {
			t.Fatal("queue position out of range", detail.QueuePosition, len(detail.Queue))
		}
		if detail.Queue[detail.QueuePosition].ID != detail.ActivePlan.ID {
			t.Fatal("queue position does not name the active plan")
		}
		if detail.ProjectID == "" {
			t.Fatal("project identity tore")
		}
	}
}

// seedActiveRun inserts a live-shaped run with a generation, session,
// budget charge and one usage observation, using real config/profile
// identities so foreign keys hold.
func seedActiveRun(t *testing.T, e *Engine, runID string, withUsage bool) {
	t.Helper()
	ctx := context.Background()
	var configID string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT id FROM config_snapshots LIMIT 1").Scan(&configID); err != nil {
		t.Fatal(err)
	}
	var profileRev int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM profiles WHERE id='local'").Scan(&profileRev); err != nil {
		t.Fatal(err)
	}
	var planRev, taskRev int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM plans WHERE id='plan'").Scan(&planRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id='first'").Scan(&taskRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES(?,?,?, ?,?,?,?,?,'implementation','initial','active','unconfirmed',600000,1800000,?)`, runID, "plan", planRev, "first", taskRev, configID, "local", profileRev, store.Now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,native_session_id,native_turn_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "gen-"+runID, runID, 1, "synthetic", "resource-"+runID, "session-"+runID, "turn-"+runID, "transport-"+runID, "active", "writing", "{}", "digest", "[]", store.Now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,run_id,generation,harness,native_home_ref,workspace_identity,profile_digest,capabilities_json,last_sequence) VALUES(?,?,?,?,?,?,?,?,0)`, "sessionrow-"+runID, runID, "genrow-"+runID, "hermes", "home", "work", "digest", "{}"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_ledgers(id,scope,plan_id,task_id,active_limit_ms,charged_ms,unknown_ms,revision,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "ledger-"+runID, "task", "plan", "first", 2700000, 40000, 0, 1, store.Now()); err != nil {
			return err
		}
		// The attempt-scoped accounting the read model reports comes from
		// active_segments, the table the supervisor actually writes.
		if _, err := tx.ExecContext(ctx, `INSERT INTO active_segments(id,run_id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,wall_started_at,wall_checkpoint_at,ended_at,charged_ms,unknown_ms) VALUES(?,?,?,?,?,?,?,?,NULL,?,?)`, "seg-"+runID, runID, "ledger-"+runID, "active", 0, 0, 1728000000000, 1728000050000, 12345, 0); err != nil {
			return err
		}
		if withUsage {
			if _, err := tx.ExecContext(ctx, `INSERT INTO usage_observations(id,run_id,observed_at,scope,provenance,input_tokens,output_tokens) VALUES(?,?,?,?,?,?,?)`, "usage-"+runID, runID, store.Now(), "turn", "observed", 10, 20); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestActiveRunDetailExposesBudgetsSessionAndAllowedNext proves checkpoint
// C's read model: identity, budgets, allowed commands and bounded sanitized
// activity without raw harness output.
func TestActiveRunDetailExposesBudgetsSessionAndAllowedNext(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	seedActiveRun(t, e, "run-live", true)
	ctx := context.Background()
	detail, err := e.Progression(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ActiveRun != "run-live" || detail.Run == nil {
		t.Fatal("active run missing", detail.ActiveRun, detail.Run)
	}
	run := detail.Run
	if run.TaskID != "first" || run.PlanID != "plan" || run.RuntimeKind != "synthetic" {
		t.Fatal("run identity wrong", run)
	}
	if run.GenerationID != "gen-run-live" {
		t.Fatal("run generation missing", run.GenerationID)
	}
	// The durable sessions row wins over the provider handle.
	if run.SessionID != "sessionrow-run-live" {
		t.Fatal("run session should be the durable identity", run.SessionID)
	}
	if run.NativeRequestKey != "turn-run-live" {
		t.Fatal("run native turn handle missing", run.NativeRequestKey)
	}
	// Attempt-scoped figures come from active_segments and pair with the
	// attempt limit; task-cumulative figures come from budget_ledgers and
	// are reported separately so neither is compared against the other.
	if !run.BudgetObserved {
		t.Fatal("budget should be observed for a run with a recorded segment")
	}
	if run.ActiveLimitMS != 600000 || run.ActiveChargedMS != 12345 {
		t.Fatal("attempt-scoped budget wrong", run)
	}
	if run.WallConsumedMS != 50000 {
		t.Fatalf("wall consumed is %d, want 50000 from the recorded segment", run.WallConsumedMS)
	}
	if run.TaskLimitMS != 2700000 || run.TaskChargedMS != 40000 {
		t.Fatal("task-cumulative budget wrong", run)
	}
	found := false
	for _, next := range run.AllowedNext {
		if next == "stop" {
			found = true
		}
	}
	if !found {
		t.Fatal("allowed next omits stop", run.AllowedNext)
	}
	if len(run.Activity) == 0 || run.ActivitySummary == "" {
		t.Fatal("activity missing")
	}
	for _, item := range run.Activity {
		if item.Label == "" || len([]rune(item.Label)) > 81 {
			t.Fatal("activity label unbounded", item)
		}
	}
	if !detail.Usage.Observed || detail.Usage.Provenance != "observed" {
		t.Fatal("usage not observed", detail.Usage)
	}
	if detail.Usage.InputTokens != 10 || detail.Usage.OutputTokens != 20 {
		t.Fatal("usage totals wrong", detail.Usage)
	}
}

// seedRunWithState inserts one run in the given run/generation/
// submission states for the allowed-next matrix.
func seedRunWithState(t *testing.T, e *Engine, runID, runState, genState, submission string) {
	t.Helper()
	ctx := context.Background()
	var configID string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT id FROM config_snapshots LIMIT 1").Scan(&configID); err != nil {
		t.Fatal(err)
	}
	var profileRev, planRev, taskRev int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM profiles WHERE id='local'").Scan(&profileRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM plans WHERE id='plan'").Scan(&planRev); err != nil {
		t.Fatal(err)
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id='first'").Scan(&taskRev); err != nil {
		t.Fatal(err)
	}
	writer := "unconfirmed"
	if runState == "completed" {
		writer = "contained_stopped"
	}
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs(id,plan_id,plan_revision,task_id,task_revision,config_id,profile_id,profile_revision,role,attempt_kind,state,writer_state,active_limit_ms,wall_limit_ms,created_at) VALUES(?,?,?,?,?,?,?,?,'implementation','initial',?,?,600000,1800000,?)`, runID, "plan", planRev, "first", taskRev, configID, "local", profileRev, runState, writer, store.Now()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO run_generations(id,run_id,ordinal,runtime_kind,runtime_resource_id,transport_generation,state,submission_state,qualification_request_json,checkout_plan_digest,expected_routes_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "gen-"+runID, runID, 1, "synthetic", "resource-"+runID, "transport-"+runID, genState, submission, "{}", "digest", "[]", store.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func allowedNextFor(t *testing.T, e *Engine, runID string) []string {
	t.Helper()
	ctx := context.Background()
	tx, err := e.DB.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	events, err := readEvents(ctx, tx, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	run, err := readActiveRunDetail(ctx, tx, runID, events)
	if err != nil {
		t.Fatal(err)
	}
	return run.AllowedNext
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAllowedNextMatchesSupervisorRunView pins the allowed-next mapping
// per state against supervisor.RunView's own table.
func TestAllowedNextMatchesSupervisorRunView(t *testing.T) {
	cases := []struct {
		run, runState, genState, submission string
		want                                []string
	}{
		{"run-prepared", "prepared", "starting", "not_attempted", []string{"inspect", "start", "reconcile", "stop"}},
		{"run-active", "active", "active", "writing", []string{"inspect", "start", "reconcile", "stop"}},
		{"run-uncertain", "active", "active", "uncertain", []string{"inspect", "reconcile", "stop"}},
		{"run-done", "completed", "terminal", "delivered", []string{"inspect"}},
		{"run-unknown", "unknown", "unknown", "writing", []string{"inspect", "reconcile"}},
	}
	for _, tc := range cases {
		_, ce, _ := setup(t)
		apply(t, ce, "project.configure", config())
		apply(t, ce, "profile.put", profile())
		apply(t, ce, "plan.put", plan())
		seedRunWithState(t, ce, tc.run, tc.runState, tc.genState, tc.submission)
		if got := allowedNextFor(t, ce, tc.run); !sameStrings(got, tc.want) {
			t.Fatalf("run %s: got %v, want %v", tc.run, got, tc.want)
		}
	}
}

// TestUsageAggregatesMixedProvenance proves totals sum every row and the
// provenance reads mixed instead of naming only the most frequent source.
func TestUsageAggregatesMixedProvenance(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	seedActiveRun(t, e, "run-mixed", false)
	ctx := context.Background()
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_observations(id,run_id,observed_at,scope,provenance,input_tokens,output_tokens) VALUES(?,?,?,?,?,?,?)`, "u1", "run-mixed", store.Now(), "turn", "observed", 10, 20); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO usage_observations(id,run_id,observed_at,scope,provenance,input_tokens,output_tokens) VALUES(?,?,?,?,?,?,?)`, "u2", "run-mixed", store.Now(), "turn", "estimated", 5, 7)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	detail, err := e.Progression(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Usage.Observed || detail.Usage.Provenance != "mixed" {
		t.Fatal("mixed provenance not reported", detail.Usage)
	}
	if detail.Usage.InputTokens != 15 || detail.Usage.OutputTokens != 27 {
		t.Fatal("mixed totals undercount", detail.Usage)
	}
}

// TestBudgetUnobservedNeverReadsAsZero is the 6.3 review round 9 P0
// regression: a run with no active_segment must report its budget as
// unobserved, never as a measured zero. The earlier implementation read
// the v1 `time_segments` table, which no production code ever writes, so
// every run rendered a structural zero that read as a measurement.
func TestBudgetUnobservedNeverReadsAsZero(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	seedActiveRun(t, e, "run-nosegment", false)
	// Drop the segment the helper inserted: a prepared run has no open
	// budget segment until the supervisor writes one.
	if _, err := e.DB.SQL.Exec(`DELETE FROM active_segments WHERE run_id='run-nosegment'`); err != nil {
		t.Fatal(err)
	}
	detail, err := e.Progression(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Run == nil {
		t.Fatal("run detail missing")
	}
	if detail.Run.BudgetObserved {
		t.Fatal("budget reported observed with no segment row")
	}
	if detail.Run.ActiveChargedMS != 0 || detail.Run.WallConsumedMS != 0 {
		t.Fatal("unobserved budget carries non-zero figures", detail.Run)
	}
	// The time_segments table has no production writer; prove it so this
	// cannot regress to being read as a source again.
	var writers int
	if err := e.DB.SQL.QueryRow(`SELECT count(*) FROM time_segments`).Scan(&writers); err != nil {
		t.Fatal(err)
	}
	if writers != 0 {
		t.Fatal("time_segments unexpectedly populated")
	}
}

// TestEventsFilteredCursorAndKind proves checkpoint D's read model: the
// --after cursor pages forward, kind filters exactly, and truncation
// reports the continuation cursor.
func TestEventsFilteredCursorAndKind(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	ctx := context.Background()
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		for _, kind := range []string{"alpha", "beta", "alpha", "beta", "alpha"} {
			if _, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,occurred_at,kind,payload_json) VALUES(1,?,?,'{}')", store.Now(), kind); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page, truncated, cursor, err := e.EventsFiltered(ctx, 0, "", 2)
	if err != nil || len(page) != 2 || !truncated {
		t.Fatal("first page should truncate", page, truncated, err)
	}
	all, _, _, err := e.EventsFiltered(ctx, 0, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	rest, truncated, _, err := e.EventsFiltered(ctx, cursor, "", 100)
	if err != nil || truncated || len(rest) != len(all)-2 {
		t.Fatal("second page wrong", len(rest), len(all), truncated, err)
	}
	if rest[0].Sequence <= page[len(page)-1].Sequence {
		t.Fatal("cursor did not advance")
	}
	alphas, truncated, _, err := e.EventsFiltered(ctx, 0, "alpha", 100)
	if err != nil || truncated || len(alphas) != 3 {
		t.Fatal("kind filter wrong", len(alphas), err)
	}
	for _, event := range alphas {
		if event.Kind != "alpha" {
			t.Fatal("filter leaked", event.Kind)
		}
	}
	if _, _, _, err := e.EventsFiltered(ctx, -1, "", 10); err == nil {
		t.Fatal("negative cursor accepted")
	}
	if _, _, _, err := e.EventsFiltered(ctx, 0, "", -5); err == nil {
		t.Fatal("negative limit accepted")
	}
	// Zero limit means the default window.
	if _, truncated, _, err := e.EventsFiltered(ctx, 0, "", 0); err != nil || truncated {
		t.Fatal("zero limit should return the default untruncated window")
	}
	// Exact fit is not truncation.
	fit, truncated, _, err := e.EventsFiltered(ctx, 0, "", len(all))
	if err != nil || truncated || len(fit) != len(all) {
		t.Fatal("exact fit misreported as truncated", len(fit), len(all), truncated)
	}
	// Empty window echoes the cursor.
	var maxSeq int64
	for _, event := range all {
		if event.Sequence > maxSeq {
			maxSeq = event.Sequence
		}
	}
	empty, truncated, cursor, err := e.EventsFiltered(ctx, maxSeq, "", 100)
	if err != nil || truncated || len(empty) != 0 || cursor != maxSeq {
		t.Fatal("empty window should echo the cursor untruncated")
	}
}
