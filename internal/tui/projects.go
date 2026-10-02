package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
)

// ProjectRef is one registered project the shell can switch to. The
// dashboard PROJECT_ID argument still selects the opening project; the
// switcher only offers what the registry already holds.
type ProjectRef struct {
	ID   string
	Root string
}

func projectsLines(m *model) []string {
	lines := []string{"Registered projects. Enter switches, Esc backs out.", ""}
	if len(m.projects) == 0 {
		lines = append(lines, "  (one project open; the registry holds no others)")
		if m.snapshot != nil {
			lines = append(lines, fmt.Sprintf("  Current: %s", clean(m.snapshot.Readiness.Project.ID)))
		}
		return lines
	}
	for index, p := range m.projects {
		marker := "  "
		if index == m.projectCursor {
			marker = "> "
		}
		current := ""
		if m.snapshot != nil && p.ID == m.snapshot.Readiness.Project.ID {
			current = " · current"
		}
		lines = append(lines, fmt.Sprintf("%s%s%s", marker, clean(p.ID), current), "    "+clean(p.Root))
	}
	return lines
}

// switchProject swaps the open project without restarting the shell. It
// clears every overlay, confirmation, quit arm and unsent input, because all
// of those bind the previous project's revision: carrying a confirmation
// across would fire an old action against a new engine. The old snapshot is
// kept until the fresh load lands, so a failed open never blanks the
// display. The returned command starts that load; without it the shell would
// sit on stale data labelled with the new project.
func (m *model) switchProject(id string) tea.Cmd {
	if m.onSwitch == nil {
		m.feedback = "project switching is unavailable in this session"
		return nil
	}
	load, mutate, err := m.onSwitch(id)
	if err != nil {
		m.feedback = "switch failed: " + clean(err.Error())
		return nil
	}
	m.load = load
	m.mutate = mutate
	m.showProjects = false
	m.showHelp = false
	m.showPalette = false
	m.palFilter, m.palCursor = "", 0
	m.confirm = nil
	m.quitArmed = false
	m.inputRequest, m.inputAction, m.inputText = "", "", ""
	m.stack = nil
	m.inbox, m.task, m.criterion, m.historyCursor, m.projectCursor, m.offset = 0, 0, 0, 0, 0, 0
	m.queueCursor, m.ovInbox = 0, 0
	m.historyFilter, m.historyOpen = "", 0
	m.showRun, m.showEvent = false, false
	m.loading = true
	m.feedback = "switched to " + clean(id)
	return m.fetch()
}

// coreProjects converts a manager listing into switcher entries.
func coreProjects(projects []core.Project) []ProjectRef {
	refs := make([]ProjectRef, 0, len(projects))
	for _, p := range projects {
		refs = append(refs, ProjectRef{ID: p.ID, Root: p.Root})
	}
	return refs
}
