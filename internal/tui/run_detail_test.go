package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
)

func runModel() model {
	m := scopedModel()
	m.tab, m.width, m.height = 0, 120, 40
	return m
}

// TestRunDetailOpensFromOverview proves the run detail is its own screen
// one keystroke from the main dashboard's run line.
func TestRunDetailOpensFromOverview(t *testing.T) {
	m := runModel()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	if cmd != nil || !m.showRun {
		t.Fatal("R did not open the run screen")
	}
	view := m.View().Content
	for _, want := range []string{"Run detail.", "Run: run-1 · active · synthetic", "Allowed next: inspect, start, reconcile, stop", "Recent activity"} {
		if !strings.Contains(view, want) {
			t.Fatalf("run screen missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "Focus: Run") {
		t.Fatal("run screen names no focus")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	if cmd != nil || m.showRun {
		t.Fatal("esc did not close the run screen")
	}
}

// TestRunDetailScopedToOverview proves R fires only on the Overview focus.
func TestRunDetailScopedToOverview(t *testing.T) {
	for _, tab := range []int{1, 2, 3, 4} {
		m := runModel()
		m.tab = tab
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
		m = updated.(model)
		if m.showRun {
			t.Fatalf("R opened run detail from tab %d", tab)
		}
	}
}

// TestRunDetailRendersMissingRunExplicitly proves a missing run is a state,
// not a blank line.
func TestRunDetailRendersMissingRunExplicitly(t *testing.T) {
	m := runModel()
	m.snapshot.Run = nil
	m.snapshot.ActiveRun = ""
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	if !m.showRun {
		t.Fatal("R did not open with no run recorded")
	}
	if view := m.View().Content; !strings.Contains(view, "no run is active") {
		t.Fatal("missing run not explicit:\n" + view)
	}
}

// TestOverviewKeepsRunToOneLine proves the main screen shows the run state
// and a one-line activity summary while the session handle, generation,
// native key and allowed-next detail live one keystroke away.
func TestOverviewKeepsRunToOneLine(t *testing.T) {
	m := runModel()
	m.snapshot.Run.GenerationID = "gen-1"
	m.snapshot.Run.SessionID = "session-1"
	m.snapshot.Run.ActivitySummary = "plan_queued, dispatch_selected"
	view := m.View().Content
	for _, want := range []string{"Run: run-1", "plan_queued, dispatch_selected", "Session: session-1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overview missing %q:\n%s", want, view)
		}
	}
	// The detail beyond the P15 elements stays one keystroke away.
	for _, want := range []string{"Allowed next:", "Generation:", "Native request key:"} {
		if strings.Contains(view, want) {
			t.Fatalf("overview carries run detail %q", want)
		}
	}
}

// TestRunDetailEmptyBranches proves the defensive empty states render
// explicitly on the detail screen.
func TestRunDetailEmptyBranches(t *testing.T) {
	m := runModel()
	m.snapshot.Run.Activity = nil
	m.snapshot.Run.ActivitySummary = "no recorded activity"
	m.snapshot.Run.AllowedNext = nil
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	view := m.View().Content
	for _, want := range []string{"no recorded activity", "Allowed next: none"} {
		if !strings.Contains(view, want) {
			t.Fatalf("run screen missing empty state %q:\n%s", want, view)
		}
	}
}

// TestRunDetailShowsFullIdentity proves the detail screen names the
// generation, session, budgets and usage honesty marker.
func TestRunDetailShowsFullIdentity(t *testing.T) {
	m := runModel()
	m.snapshot.Run.GenerationID = "gen-1"
	m.snapshot.Run.GenerationState = "active"
	m.snapshot.Run.TaskID = "task-1"
	m.snapshot.Run.PlanID = "plan-1"
	m.snapshot.Run.SessionID = "session-1"
	m.snapshot.Run.NativeRequestKey = "turn-1"
	m.snapshot.Run.WallLimitMS = 1800000
	m.snapshot.Run.WallConsumedMS = 5000
	m.snapshot.Run.ActiveLimitMS = 600000
	m.snapshot.Run.ActiveChargedMS = 1200
	m.snapshot.Run.BudgetObserved = true
	m.snapshot.Run.TaskBudgetObserved = true
	m.snapshot.Run.TaskChargedMS = 40000
	m.snapshot.Run.TaskLimitMS = 2700000
	m.snapshot.Run.Activity = []core.ActivityItem{{Sequence: 9, At: 1728000000000, Label: "plan_queued"}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	m = updated.(model)
	view := m.View().Content
	for _, want := range []string{"gen-1", "session-1", "turn-1", "task-1", "plan-1", "This run: active 1200ms of 600000ms", "wall 5000ms of 1800000ms", "This task (all attempts): 40000ms of 2700000ms", "Allowed next: inspect, start, reconcile, stop", "plan_queued", "unavailable (no observation)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("run screen missing %q:\n%s", want, view)
		}
	}
}

// TestPaletteReachesRunDetail proves the palette discovers R without
// bypassing its Overview scope.
func TestPaletteReachesRunDetail(t *testing.T) {
	m := runModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m = updated.(model)
	for _, key := range []string{"r", "u", "n"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if !m.showRun {
		t.Fatalf("palette did not open run detail: %q", m.feedback)
	}
	m2 := runModel()
	m2.tab = 3
	updated, _ = m2.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m2 = updated.(model)
	for _, key := range []string{"r", "u", "n"} {
		updated, _ = m2.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m2 = updated.(model)
	}
	updated, cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 = updated.(model)
	if cmd != nil || m2.showRun {
		t.Fatal("palette bypassed run detail scope")
	}
	if !strings.Contains(m2.feedback, "Overview") {
		t.Fatalf("palette did not name the owning screen: %q", m2.feedback)
	}
}
