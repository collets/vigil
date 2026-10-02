package tui

import (
	"fmt"

	"vigil/internal/core"
)

// overviewBindings are the mutating keys the Overview screen owns. No other
// screen registers them, so pressing them elsewhere does nothing: that is the
// 6.1-F17 fix. stop is present but armed only through the confirmation
// dialog, which shows the exact displayed revision and run ID.
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

func overviewLines(m *model, s *core.DashboardSnapshot) []string {
	lines := []string{"> Project: " + clean(s.Readiness.Project.ID), "Root: " + clean(s.Readiness.Project.Root), fmt.Sprintf("Revision %d · %s", s.Readiness.Project.Revision, clean(s.Readiness.Project.State)), "", "Execution is unavailable until all runtime and readiness gates pass."}
	for _, issue := range s.Readiness.RuntimeIssues {
		lines = append(lines, "Runtime: "+clean(issue))
	}
	for _, issue := range s.Readiness.DefinitionIssues {
		lines = append(lines, "Definition: "+clean(issue))
	}
	lines = append(lines, "", fmt.Sprintf("%d tasks · %d pending/expired decisions", len(s.Readiness.Tasks), len(s.Inbox)))
	return lines
}
