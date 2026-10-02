package tui

import (
	"fmt"
	"time"

	"vigil/internal/core"
)

// runDetailLines renders the active run as its own screen: identity,
// budgets, allowed next commands and bounded sanitized activity. The main
// screen carries only the one-line summary; this screen is one keystroke
// (R) away. A missing run renders explicitly, never as a blank.
func runDetailLines(m *model, s *core.DashboardSnapshot) []string {
	if s.Run == nil {
		return []string{"Run detail.", "", "Run: no run is active", "", "esc backs out"}
	}
	run := s.Run
	lines := []string{"Run detail.", ""}
	lines = append(lines, fmt.Sprintf("Run: %s · %s · %s", clean(run.RunID), clean(run.State), clean(run.RuntimeKind)))
	lines = append(lines, fmt.Sprintf("Task: %s · Plan: %s", clean(run.TaskID), clean(run.PlanID)))
	lines = append(lines, fmt.Sprintf("Generation: %s · %s", clean(run.GenerationID), clean(run.GenerationState)))
	if run.SessionID != "" {
		lines = append(lines, "Session: "+clean(run.SessionID))
	} else {
		lines = append(lines, "Session: none")
	}
	if run.NativeRequestKey != "" {
		lines = append(lines, "Native request key: "+clean(run.NativeRequestKey))
	} else {
		lines = append(lines, "Native request key: none")
	}
	if !run.BudgetObserved {
		lines = append(lines, "Budgets: unavailable (no recorded segment)")
	} else {
		lines = append(lines, fmt.Sprintf("This run: active %dms of %dms · unknown %dms · wall %dms of %dms (recorded at last checkpoint)", run.ActiveChargedMS, run.ActiveLimitMS, run.UnknownMS, run.WallConsumedMS, run.WallLimitMS))
		if run.TaskBudgetObserved {
			lines = append(lines, fmt.Sprintf("This task (all attempts): %dms of %dms", run.TaskChargedMS, run.TaskLimitMS))
		} else {
			lines = append(lines, "This task (all attempts): unavailable (no recorded ledger)")
		}
	}
	if len(run.AllowedNext) == 0 {
		lines = append(lines, "Allowed next: none")
	} else {
		lines = append(lines, "Allowed next: "+clean(joinWords(run.AllowedNext)))
	}
	lines = append(lines, "")
	lines = append(lines, "Recent activity (sanitized event kinds):")
	if len(run.Activity) == 0 {
		lines = append(lines, "  no recorded activity")
	}
	for _, item := range run.Activity {
		lines = append(lines, fmt.Sprintf("  %d · %s · %s", item.Sequence, time.UnixMilli(item.At).Format("15:04:05"), clean(item.Label)))
	}
	lines = append(lines, "")
	if s.Usage.Observed {
		lines = append(lines, fmt.Sprintf("Cost/usage (%s): in %d · out %d", clean(s.Usage.Provenance), s.Usage.InputTokens, s.Usage.OutputTokens))
	} else {
		lines = append(lines, "Cost/usage: unavailable (no observation)")
	}
	lines = append(lines, "", "esc backs out")
	return lines
}

func joinWords(words []string) string {
	out := ""
	for i, word := range words {
		if i > 0 {
			out += ", "
		}
		out += word
	}
	return out
}
