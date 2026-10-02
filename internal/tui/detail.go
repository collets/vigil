package tui

import (
	"fmt"

	"vigil/internal/core"
)

// detailBindings mirrors the Tasks screen's quality keys: both act on the
// focused task at its displayed revision.
func detailBindings() []Binding {
	return []Binding{
		{Key: "h", Action: "human-accept", Command: "project quality-decision"},
		{Key: "m", Action: "manual-pass", Command: "project quality-manual"},
		{Key: "t", Action: "accept-task", Command: "project quality-accept"},
		{Key: "[ ]", Action: "criterion", Command: ""},
	}
}

func detailLines(m *model, s *core.DashboardSnapshot) []string {
	lines := []string{"Authoritative plan/task evidence details.", "Detailed diffs remain external.", ""}
	for _, plan := range s.Plans {
		lines = append(lines, fmt.Sprintf("Plan %s · %s · rank %d · services remaining %dms", clean(plan.ID), clean(plan.State), plan.Rank, plan.ServiceBudgetRemainingMS))
	}
	for index, task := range s.Tasks {
		marker := "  "
		if index == m.task {
			marker = "> "
		}
		lines = append(lines, "", fmt.Sprintf("%sTask %s r%d · %s · remaining %dms", marker, clean(task.ID), task.Revision, clean(task.State), task.BudgetRemainingMS))
		if task.BlockReason != "" {
			lines = append(lines, "  Blocker: "+clean(task.BlockReason))
		}
		lines = append(lines, fmt.Sprintf("  Findings: %d blocking · %d suggestions", task.BlockingFindings, task.Suggestions))
		if task.BaselineUnhealthy {
			lines = append(lines, "  Baseline health: unhealthy exception present")
		}
		for _, v := range task.CheckOutputs {
			lines = append(lines, "  Check output: "+clean(v))
		}
		for _, v := range task.ManualOutcomes {
			lines = append(lines, "  Manual: "+clean(v))
		}
		if len(task.ManualCriteria) > 0 {
			for criterionIndex, criterionID := range task.ManualCriteria {
				marker := "  "
				if index == m.task && criterionIndex == m.criterion {
					marker = "> "
				}
				lines = append(lines, "  "+marker+"manual criterion: "+clean(criterionID))
			}
		}
		if task.RecoveryState != "" {
			lines = append(lines, "  Recovery quarantine: "+clean(task.RecoveryState))
		}
	}
	return lines
}
