package tui

import (
	"fmt"
	"strings"

	"vigil/internal/core"
)

// tasksBindings are the quality keys the Tasks screen owns. h, m and t act
// on the focused task at its displayed revision; [ and ] move the manual
// criterion focus inside the focused task.
func tasksBindings() []Binding {
	return []Binding{
		{Key: "h", Action: "human-accept", Command: "project quality-decision"},
		{Key: "m", Action: "manual-pass", Command: "project quality-manual"},
		{Key: "t", Action: "accept-task", Command: "project quality-accept"},
		{Key: "[ ]", Action: "criterion", Command: ""},
	}
}

func tasksLines(m *model, s *core.DashboardSnapshot) []string {
	var lines []string
	for index, task := range s.Readiness.Tasks {
		marker := "  "
		if index == m.task {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s · revision %d", marker, clean(task.ID), clean(task.State), task.Revision))
		for _, issue := range task.Issues {
			lines = append(lines, "  "+clean(issue))
		}
		if len(task.RequiredChecks) > 0 {
			lines = append(lines, "  Required checks: "+clean(strings.Join(task.RequiredChecks, ", ")))
		}
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		lines = []string{"No tasks defined."}
	}
	return lines
}
