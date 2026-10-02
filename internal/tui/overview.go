package tui

import (
	"fmt"
	"strings"
	"time"

	"vigil/internal/core"
)

// overviewBindings are the mutating keys the Overview screen owns. No other
// screen registers them, so pressing them elsewhere does nothing: that is the
// 6.1-F17 fix. stop is present but armed only through the confirmation
// dialog, which shows the exact displayed revision and run ID. u queues the
// queue-cursor plan at its displayed rank (queue:ID:rank); the mutator
// rejects a cursor that no longer matches the snapshot.
func overviewBindings() []Binding {
	return []Binding{
		{Key: "p", Action: "pause", Command: "project pause"},
		{Key: "c", Action: "continue", Command: "project continue"},
		{Key: "a", Action: "advance", Command: "project advance"},
		{Key: "u", Action: "queue", Command: "project queue"},
		{Key: "s", Action: "stop (confirm)", Command: "project execution-stop"},
	}
}

func overviewFocus(m *model) string {
	if m.snapshot == nil {
		return "loading"
	}
	return m.snapshot.Readiness.Project.ID
}

// formatAge renders a request's age from its created-at millis.
func formatAge(createdAt int64) string {
	delta := time.Since(time.UnixMilli(createdAt))
	if delta < 0 {
		delta = 0
	}
	switch {
	case delta < time.Minute:
		return fmt.Sprintf("%ds", int(delta.Seconds()))
	case delta < time.Hour:
		return fmt.Sprintf("%dm", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh", int(delta.Hours()))
	default:
		return fmt.Sprintf("%dd", int(delta.Hours()/24))
	}
}

// qualityRollup renders one task's compact fixed-width quality summary:
// checks passed/total, blocking findings, suggestions, manual outstanding
// and baseline health. One bounded line per task, never a paragraph.
// Unknown check statuses count as non-passed; any manual state other than
// pass counts as outstanding. The persisted formats are
// check_id:status:artifact and criterion_id:state; both IDs may themselves
// contain colons, so both parse from the right.
func qualityRollup(task core.TaskDetail) string {
	passed, total := 0, len(task.CheckOutputs)
	for _, output := range task.CheckOutputs {
		if idx := strings.LastIndex(output, ":"); idx >= 0 {
			rest := output[:idx]
			if sep := strings.LastIndex(rest, ":"); sep >= 0 && rest[sep+1:] == "pass" {
				passed++
			}
		}
	}
	// ManualOutcomes is newest-first (ORDER BY evaluated_at DESC), so the
	// FIRST outcome seen for a criterion is its latest state. A criterion
	// that passed in attempt 1 and failed in attempt 2 must read as
	// outstanding, which a set-of-passing-criteria would get backwards.
	latestManual := map[string]string{}
	for _, outcome := range task.ManualOutcomes {
		if idx := strings.LastIndex(outcome, ":"); idx > 0 {
			if _, seen := latestManual[outcome[:idx]]; !seen {
				latestManual[outcome[:idx]] = outcome[idx+1:]
			}
		}
	}
	passedManual := map[string]bool{}
	for criterion, state := range latestManual {
		if state == "pass" {
			passedManual[criterion] = true
		}
	}
	open := 0
	for _, criterion := range task.ManualCriteria {
		if !passedManual[criterion] {
			open++
		}
	}
	health := "ok"
	if task.BaselineUnhealthy {
		health = "unhealthy"
	}
	line := fmt.Sprintf("%-12.12s chk %d/%d blk %d sug %d man %d %s", clean(task.ID), passed, total, task.BlockingFindings, task.Suggestions, open, health)
	if runes := []rune(line); len(runes) > 80 {
		return string(runes[:79]) + "…"
	}
	return line
}

// runBudgetLines renders the run's budget accounting, one line per scope.
// Separate lines rather than one assembled string because the combined line
// is ~118 columns and truncates away the task figure at the 110 columns this
// screen is measured at — the honesty failure in a different guise. An
// unobserved budget renders as explicitly unavailable, never as a zero (P16).
func runBudgetLines(run *core.ActiveRunDetail) []string {
	if !run.BudgetObserved {
		return []string{"Budgets: unavailable (no recorded segment)"}
	}
	lines := []string{fmt.Sprintf("Budget (this run): %dms of %dms active · %dms unknown", run.ActiveChargedMS, run.ActiveLimitMS, run.UnknownMS),
		fmt.Sprintf("Wall (this run, at last checkpoint): %dms of %dms", run.WallConsumedMS, run.WallLimitMS)}
	if run.TaskBudgetObserved {
		lines = append(lines, fmt.Sprintf("Budget (task, all attempts): %dms of %dms", run.TaskChargedMS, run.TaskLimitMS))
	} else {
		lines = append(lines, "Budget (task, all attempts): unavailable (no recorded ledger)")
	}
	return lines
}

func overviewLines(m *model, s *core.DashboardSnapshot) []string {
	lines := []string{"> Project: " + clean(s.Readiness.Project.ID), "Root: " + clean(s.Readiness.Project.Root), fmt.Sprintf("Revision %d · %s", s.Readiness.Project.Revision, clean(s.Readiness.Project.State)), ""}
	for _, issue := range s.Readiness.RuntimeIssues {
		lines = append(lines, "Runtime: "+clean(issue))
	}
	for _, issue := range s.Readiness.DefinitionIssues {
		lines = append(lines, "Definition: "+clean(issue))
	}
	lines = append(lines, "")
	// Active plan and ranked queue with the operator's position. The
	// listed rows are snapshot.Queue — the same field the u action and
	// the cursors address — while the head/position metadata comes from
	// the progression read model; Dashboard populates both from one
	// read. Equal ranks order by plan ID (see workflow queue ordering).
	progression := s.Progression
	if progression.ActivePlan != nil {
		lines = append(lines, fmt.Sprintf("Active plan: %s · %s · rank %d (position %d/%d)", clean(progression.ActivePlan.ID), clean(progression.ActivePlan.State), progression.ActivePlan.Rank, progression.QueuePosition+1, len(s.Queue)))
	} else {
		lines = append(lines, "Active plan: none")
	}
	lines = append(lines, "Ranked queue (ties order by plan ID):")
	for index, entry := range s.Queue {
		if index >= maxRenderedQueue {
			lines = append(lines, fmt.Sprintf("  …and %d more plans (not selectable here)", len(s.Queue)-maxRenderedQueue))
			break
		}
		marker := "  "
		if index == m.queueCursor {
			marker = "> "
		}
		// The action hint is only advertised on a plan the product will
		// actually queue. QueuePlan rejects active, blocked, paused and
		// terminal plans, so promising u there would be promising an
		// action the command refuses.
		selected := ""
		if index == m.queueCursor {
			selected = " · not queueable in state " + clean(entry.State)
			if entry.State == "draft" || entry.State == "ready" || entry.State == "queued" {
				selected = fmt.Sprintf(" · u queues at rank %d", entry.Rank)
			}
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s · rank %d%s", marker, clean(entry.ID), clean(entry.State), entry.Rank, selected))
	}
	if len(s.Queue) == 0 {
		lines = append(lines, "  No plans queued.")
	}
	lines = append(lines, "")
	// Current task and its blocker.
	if progression.SelectedTask != nil {
		lines = append(lines, fmt.Sprintf("Current task: %s r%d · %s", clean(progression.SelectedTask.ID), progression.SelectedTask.Revision, clean(progression.SelectedTask.State)))
		if len(progression.SelectedTask.Blockers) == 0 {
			lines = append(lines, "Blocker: none")
		} else {
			lines = append(lines, "Blocker: "+clean(progression.SelectedTask.Blockers[0]))
			for _, blocker := range progression.SelectedTask.Blockers[1:] {
				if len(lines) >= 40 {
					break
				}
				lines = append(lines, "  "+clean(blocker))
			}
		}
	} else {
		lines = append(lines, "Current task: none")
		lines = append(lines, "Blocker: none")
	}
	lines = append(lines, "")
	// Live run with a one-line activity summary; the full detail is one
	// keystroke away on the run screen (R). A missing run is a state.
	if s.Run != nil {
		run := s.Run
		lines = append(lines, fmt.Sprintf("Run: %s · %s · %s", clean(run.RunID), clean(run.State), clean(run.RuntimeKind)))
		lines = append(lines, runBudgetLines(run)...)
		// One compact session line, not a wall of identity: R45/P15 require
		// session state on this screen, while the generation, native key
		// and allowed-next detail stay one keystroke away.
		if run.SessionID != "" {
			lines = append(lines, "Session: "+clean(run.SessionID))
		} else {
			lines = append(lines, "Session: none")
		}
		lines = append(lines, "Activity: "+clean(run.ActivitySummary))
	} else {
		lines = append(lines, "Run: no run is active")
	}
	// Cost and usage honesty: unavailable is a marker, never a zero.
	if s.Usage.Observed {
		cost := "no cost recorded"
		if s.Usage.HasCost {
			cost = fmt.Sprintf("cost %dµ%s", s.Usage.CostMicrounits, s.Usage.Currency)
		}
		lines = append(lines, fmt.Sprintf("Cost/usage: %s · in %d · out %d · %s", clean(s.Usage.Provenance), s.Usage.InputTokens, s.Usage.OutputTokens, clean(cost)))
	} else {
		lines = append(lines, "Cost/usage: unavailable (no observation)")
	}
	lines = append(lines, "")
	// Compact per-task quality status.
	lines = append(lines, "Quality per task:")
	if len(s.Tasks) == 0 {
		lines = append(lines, "  No tasks defined.")
	}
	for _, task := range s.Tasks {
		lines = append(lines, "  "+qualityRollup(task))
	}
	// The task read is bounded at 100 rows in plan order, so a project
	// whose active plan's tasks fall past that bound would otherwise
	// render "Current task: none" with no hint that anything was hidden.
	// The full set is on Tasks, which reads readiness with no LIMIT;
	// Detail reads the same bounded rows, so it is deliberately not named.
	if len(s.Tasks) >= taskWindowCap {
		lines = append(lines, fmt.Sprintf("  …task window holds the latest %d rows; the full set is on Tasks", taskWindowCap))
	}
	lines = append(lines, "")
	// Actionable request list with kind, age and blocking state. Enter
	// opens the selected entry in the Inbox through the screen stack.
	lines = append(lines, fmt.Sprintf("Actionable requests (%d · Enter opens in Inbox):", len(s.Inbox)))
	for index, entry := range s.Inbox {
		if index >= maxRenderedRequests {
			lines = append(lines, fmt.Sprintf("  …and %d more in Inbox (not selectable here)", len(s.Inbox)-maxRenderedRequests))
			break
		}
		marker := "  "
		if index == m.ovInbox {
			marker = "> "
		}
		blocking := "non-blocking"
		if entry.Blocking {
			blocking = "blocking"
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s · %s · age %s · %s", marker, clean(entry.ID), clean(entry.Kind), clean(entry.State), formatAge(entry.CreatedAt), blocking))
	}
	if len(s.Inbox) == 0 {
		lines = append(lines, "  No pending decisions.")
	}
	lines = append(lines, "", "j/k select request · [/] select queue plan · Enter opens request · R run detail")
	return lines
}
