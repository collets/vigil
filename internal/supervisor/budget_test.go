package supervisor

import (
	"context"
	"testing"

	"vigil/internal/store"
)

func TestCrashGapConsumesRemainingBudget(t *testing.T) {
	fixture := setupFixture(t)
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	now := store.Now()
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE active_segments SET wall_started_at=?,wall_checkpoint_at=?,charged_ms=500 WHERE run_id=? AND ended_at IS NULL", now-3000, now-2000, fixture.prepared.RunID); err != nil {
		t.Fatal(err)
	}
	if err := runner.ReconcileOpenSegment(context.Background(), fixture.prepared, 0); err != nil {
		t.Fatal(err)
	}
	var charged, unknown int64
	if err := fixture.engine.DB.SQL.QueryRow("SELECT charged_ms,unknown_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", fixture.prepared.TaskID).Scan(&charged, &unknown); err != nil {
		t.Fatal(err)
	}
	if charged < 1000 || unknown < 1900 {
		t.Fatal("crash gap increased remaining budget", charged, unknown)
	}
	if err := runner.ReconcileOpenSegment(context.Background(), fixture.prepared, 0); err != nil {
		t.Fatal("budget reconciliation not idempotent", err)
	}
}

func TestQueueAndProvenHumanWaitAreNotCharged(t *testing.T) {
	fixture := setupFixture(t)
	var before int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM active_segments").Scan(&before); err != nil || before != 0 {
		t.Fatal("queue wait opened active segment", before, err)
	}
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	if err := runner.BeginHumanWait(context.Background(), fixture.prepared, false); err == nil {
		t.Fatal("unproven human wait excluded")
	}
	if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
		t.Fatal(err)
	}
	if err := runner.EndHumanWait(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	if err := runner.closeSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	var humanCharged int64
	if err := fixture.engine.DB.SQL.QueryRow("SELECT charged_ms FROM active_segments WHERE category='human_wait'").Scan(&humanCharged); err != nil || humanCharged != 0 {
		t.Fatal("human wait charged", humanCharged, err)
	}
}
