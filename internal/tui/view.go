package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)
// unsavedOrActive arms the quit confirmation when a run the interface may
// have started is recorded. Unsent text needs no arm: while text mode is
// open every key except ctrl+c feeds the entry, so q can never meet unsent
// text — Esc abandons it locally (invariant 3) and ctrl+c is the documented
// emergency quit.
func (m model) unsavedOrActive() bool {
	return m.snapshot != nil && m.snapshot.ActiveRun != ""
}

func (m model) quitWarning() string {
	return "run " + clean(m.snapshot.ActiveRun) + " may be active · Enter/y quits anyway · Esc/n stays"
}

// confirmTarget names the exact persisted object a confirmation binds.
func (m model) confirmTarget(action string) string {
	if m.snapshot.ActiveRun != "" {
		return m.snapshot.ActiveRun
	}
	return m.snapshot.Readiness.Project.ID
}

func confirmReason(action string) string {
	for _, row := range DestructiveActions {
		if row.Action == action {
			return row.Reason
		}
	}
	return "this action changes persisted state"
}
// focusText renders the single focus of the current screen on every frame.
func (m model) focusText() string {
	if m.snapshot == nil {
		return "Focus: loading"
	}
	switch m.focused() {
	case screenOverview:
		return "Focus: Overview · " + clean(m.snapshot.Readiness.Project.ID)
	case screenTasks:
		if len(m.snapshot.Tasks) == 0 {
			return "Focus: Tasks · none"
		}
		focused := m.snapshot.Tasks[m.task]
		return fmt.Sprintf("Focus: Tasks · %s r%d", clean(focused.ID), focused.Revision)
	case screenInbox:
		if len(m.snapshot.Inbox) == 0 {
			return "Focus: Inbox · none"
		}
		return "Focus: Inbox · " + clean(m.snapshot.Inbox[m.inbox].ID)
	case screenHistory:
		shown := filteredEvents(m.snapshot.Events, m.historyFilter)
		if len(shown) == 0 {
			return "Focus: History · none"
		}
		cursor := min(max(m.historyCursor, 0), len(shown)-1)
		return fmt.Sprintf("Focus: History · event %d", shown[cursor].Sequence)
	case screenDetail:
		if len(m.snapshot.Tasks) == 0 {
			return "Focus: Detail · none"
		}
		focused := m.snapshot.Tasks[m.task]
		return fmt.Sprintf("Focus: Detail · %s r%d", clean(focused.ID), focused.Revision)
	case screenHelp:
		return "Focus: Help"
	case screenConfirm:
		return "Focus: Confirm"
	case screenProjects:
		return "Focus: Projects"
	case screenPalette:
		return "Focus: Palette"
	case screenRun:
		return "Focus: Run"
	case screenEvent:
		return "Focus: Event"
	}
	return "Focus: unknown"
}

func (m model) lines() []string {
	if m.snapshot == nil {
		if m.err != nil {
			return []string{"Unable to read project: " + clean(m.err.Error()), "Press r to retry."}
		}
		return []string{"Loading persisted project…"}
	}
	s := m.snapshot
	switch {
	case m.showPalette:
		return paletteLines(&m, paletteFilter(paletteEntries(), m.palFilter), m.palCursor)
	case m.showHelp:
		return helpLines(&m)
	case m.confirm != nil:
		return confirmLines(m.confirm)
	case m.showProjects:
		return projectsLines(&m)
	case m.showRun:
		return runDetailLines(&m, s)
	case m.showEvent:
		return eventDetailLines(&m, s)
	}
	switch m.tab {
	case 0:
		return overviewLines(&m, s)
	case 1:
		return tasksLines(&m, s)
	case 2:
		return inboxLines(&m, s)
	case 3:
		return historyLines(&m, s)
	case 4:
		return detailLines(&m, s)
	}
	return overviewLines(&m, s)
}

// tabBar renders the screen bar without ever cutting it mid-name: when the
// full bar does not fit, it degrades to the current screen plus a count.
func tabBar(m *model, width int) string {
	tabs := []string{"1 Overview", "2 Tasks", "3 Inbox", "4 History", "5 Detail"}
	tabs[m.tab] = "[" + tabs[m.tab] + "]"
	full := "Vigil  " + strings.Join(tabs, "  ")
	if ansi.StringWidth(full) <= width {
		return full
	}
	names := []string{"Overview", "Tasks", "Inbox", "History", "Detail"}
	return fmt.Sprintf("Vigil [%s] ?", names[m.tab])
}

func (m model) View() tea.View {
	status := "Persisted state · interactive controls"
	if m.loading {
		status += " · refreshing"
	}
	if m.err != nil && m.snapshot != nil {
		status = "Refresh failed; showing previous snapshot: " + clean(m.err.Error())
	}
	if m.feedback != "" {
		status += " · " + clean(m.feedback)
	}
	if m.inputRequest != "" {
		status = "Clarification input for " + clean(m.inputRequest)
	}
	status += " · " + m.focusText()
	lines := m.lines()
	space := max(1, m.height-5)
	start := min(m.offset, max(0, len(lines)-space))
	end := min(len(lines), start+space)
	output := []string{tabBar(&m, max(1, m.width)), status, ""}
	output = append(output, lines[start:end]...)
	footer := fmt.Sprintf("p/c/s control · a/u plan · inbox g/v/x/f/b/i/y/n · h/m/t quality · [/] criterion · q quit  (%d–%d/%d)", start+1, end, len(lines))
	width := max(1, m.width)
	switch {
	case m.showPalette:
		footer = bindingLine([]Binding{{Key: "Enter", Action: "run"}, {Key: "Esc", Action: "back"}}, width)
	case m.showHelp:
		footer = bindingLine(globalBindings(), width)
	case m.confirm != nil:
		// The dialog's own keys degrade like every other binding line
		// rather than being cut mid-list at narrow widths.
		footer = bindingLine([]Binding{{Key: "Enter/y", Action: "confirm"}, {Key: "Esc/n", Action: "abandon"}}, width)
	case m.showProjects:
		footer = bindingLine([]Binding{{Key: "Enter", Action: "switch"}, {Key: "j/k", Action: "move"}, {Key: "Esc", Action: "back"}}, width)
	case m.showRun:
		footer = bindingLine([]Binding{{Key: "Esc", Action: "back"}}, width)
	case m.showEvent:
		footer = bindingLine([]Binding{{Key: "Esc", Action: "back"}}, width)
	case m.focused() == screenHelp:
		footer = bindingLine(globalBindings(), width)
	default:
		// Per-screen bindings plus the two keys that must stay visible at
		// any width: help and quit. The scroll position is appended only
		// when everything still fits.
		suffix := " · ? help · q quit"
		avail := max(0, width-ansi.StringWidth(suffix))
		line := bindingLine(screenBindings(rootScreen(m.tab)), avail)
		switch {
		case line == "":
			footer = "? help · q quit"
		case strings.Contains(line, "? for all keys"):
			footer = line
		case ansi.StringWidth(line+suffix) <= width:
			footer = line + suffix
		default:
			footer = line
		}
		pos := fmt.Sprintf("  (%d–%d/%d)", start+1, end, len(lines))
		if !strings.Contains(footer, "? for all keys") && ansi.StringWidth(footer+pos) <= width {
			footer += pos
		}
	}
	if m.inputRequest != "" {
		entry := textEntry{text: m.inputText}
		footer = entry.line()
	}
	output = append(output, "", footer)
	for n, line := range output {
		output[n] = ansi.Truncate(line, max(1, m.width), "…")
	}
	if len(output) > m.height {
		output = output[:max(1, m.height)]
	}
	view := tea.NewView(strings.Join(output, "\n"))
	view.AltScreen = true
	return view
}
