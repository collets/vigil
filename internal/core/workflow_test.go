package core

import (
	"context"
	"testing"

	"vigil/internal/policy"
)

func TestExplicitPlanQueueAndStableTaskSelection(t *testing.T) {
	_, e, _ := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	first := Plan{ID: "b-plan", Title: "B", Specification: "fixture B", Approved: true, Tasks: []policy.Task{{ID: "blocked", Objective: "blocked fixture", Criteria: []policy.Criterion{{ID: "b", Text: "done"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 1000}, {ID: "independent", Objective: "independent fixture", Criteria: []policy.Criterion{{ID: "i", Text: "done"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 1000}}}
	second := Plan{ID: "a-plan", Title: "A", Specification: "fixture A", Approved: true, Tasks: []policy.Task{{ID: "a-task", Objective: "stable first", Criteria: []policy.Criterion{{ID: "a", Text: "done"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 1000}}}
	apply(t, e, "plan.put", first)
	apply(t, e, "plan.put", second)
	var revision int
	if err := e.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := e.QueuePlan(ctx, "queue-b", revision, "b-plan", 10); err != nil {
		t.Fatal(err)
	}
	revision++
	receipt, err := e.QueuePlan(ctx, "queue-a", revision, "a-plan", 10)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.QueuePlan(ctx, "queue-a", revision, "a-plan", 10)
	if err != nil || !replay.Repeated || replay.PlanID != receipt.PlanID {
		t.Fatal("queue replay", replay, err)
	}
	revision++
	if _, err = e.Continue(ctx, "continue-workflow", revision); err != nil {
		t.Fatal(err)
	}
	revision++
	decision, err := e.Advance(ctx, "advance-a", revision)
	if err != nil || decision.PlanID != "a-plan" || decision.TaskID != "a-task" || decision.State != "selected" {
		t.Fatal(decision, err)
	}
	if _, err = e.Pause(ctx, "pause-workflow", revision); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Advance(ctx, "advance-paused", revision+1); err == nil {
		t.Fatal("paused project scheduled work")
	}
}

func TestSchedulerPreservesUnsafeBlockerAndSkipsSafeIndependentBlock(t *testing.T) {
	_, e, _ := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	p := plan()
	p.Tasks[1].Dependencies = nil
	apply(t, e, "plan.put", p)
	var revision int
	e.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err := e.QueuePlan(ctx, "queue-plan", revision, "plan", 0); err != nil {
		t.Fatal(err)
	}
	revision++
	if _, err := e.Continue(ctx, "continue-plan", revision); err != nil {
		t.Fatal(err)
	}
	revision++
	if _, err := e.DB.SQL.Exec("UPDATE tasks SET state='blocked',block_reason='fixture clarification' WHERE id='first'"); err != nil {
		t.Fatal(err)
	}
	decision, err := e.Advance(ctx, "advance-independent", revision)
	if err != nil || decision.TaskID != "second" {
		t.Fatal(decision, err)
	}
	var reason string
	if err := e.DB.SQL.QueryRow("SELECT block_reason FROM tasks WHERE id='first'").Scan(&reason); err != nil || reason != "fixture clarification" {
		t.Fatal("blocker was not preserved", reason, err)
	}
	var automatic int
	if err := e.DB.SQL.QueryRow("SELECT automatic_plan_advance FROM workflow_controls").Scan(&automatic); err != nil || automatic != 0 {
		t.Fatal("automatic advancement enabled", automatic, err)
	}
	if _, err := e.DB.SQL.Exec("UPDATE workflow_controls SET automatic_plan_advance=1"); err == nil {
		t.Fatal("automatic advancement became writable")
	}
}
