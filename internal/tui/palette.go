package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// paletteEntry is one runnable row in the command palette: every registered
// action plus navigation. owner names the screen that may run it; empty
// means runnable from anywhere.
type paletteEntry struct {
	keys    string
	action  string
	command string
	owner   screenID
	global  bool
}

// paletteEntries lists every registered action, so the palette reaches the
// same set the parity register classifies rather than a hand-kept subset.
func paletteEntries() []paletteEntry {
	var entries []paletteEntry
	for _, b := range overviewBindings() {
		entries = append(entries, paletteEntry{keys: keyFor("overview", b.Action), action: b.Action, command: b.Command, owner: screenOverview})
	}
	for _, b := range inboxBindings() {
		entries = append(entries, paletteEntry{keys: keyFor("inbox", b.Action), action: b.Action, command: b.Command, owner: screenInbox})
	}
	for _, b := range tasksBindings() {
		if b.Command == "" {
			continue
		}
		entries = append(entries, paletteEntry{keys: keyFor("tasks", b.Action), action: b.Action, command: b.Command, owner: screenTasks})
	}
	for _, screen := range []screenID{screenOverview, screenTasks, screenInbox, screenHistory, screenDetail} {
		entries = append(entries, paletteEntry{keys: fmt.Sprint(int(screen) + 1), action: "go " + screen.title(), command: "", global: true})
	}
	// Run detail is Overview navigation, not a mutation: it opens from
	// the Overview focus only, like every other screen-owned row.
	entries = append(entries, paletteEntry{keys: "R", action: "run detail", command: "project execution-inspect", owner: screenOverview})
	entries = append(entries,
		paletteEntry{keys: "?", action: "help", command: "", global: true},
		paletteEntry{keys: "P", action: "projects", command: "", global: true},
		paletteEntry{keys: "r", action: "refresh", command: "", global: true},
		paletteEntry{keys: "q", action: "quit", command: "", global: true},
	)
	return entries
}

func keyFor(screen, action string) string {
	switch action {
	case "pause":
		return "p"
	case "continue":
		return "c"
	case "advance":
		return "a"
	case "queue":
		return "u"
	case "stop (confirm)":
		return "s"
	case "allow":
		return "y"
	case "deny/reject/cancel":
		return "n"
	case "apply-proposal":
		return "g"
	case "request-revision":
		return "v"
	case "exact-resume":
		return "x"
	case "fresh-context":
		return "f"
	case "remain-blocked":
		return "b"
	case "answer":
		return "i"
	case "human-accept":
		return "h"
	case "manual-pass":
		return "m"
	case "accept-task":
		return "t"
	}
	return ""
}

// paletteFilter returns the entries matching the filter, in stable order.
func paletteFilter(entries []paletteEntry, filter string) []paletteEntry {
	if filter == "" {
		return entries
	}
	var matched []paletteEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.keys+" "+entry.action+" "+entry.command), strings.ToLower(filter)) {
			matched = append(matched, entry)
		}
	}
	return matched
}

// paletteAvailable reports whether an entry may run from the current focus.
// Screen-owned actions run only on their screen with something focused;
// running them past the focus would reintroduce the cross-screen mutation
// the scoping exists to prevent.
func (m *model) paletteAvailable(entry paletteEntry) bool {
	if entry.global {
		return true
	}
	if m.focused() != entry.owner && rootScreen(m.tab) != entry.owner {
		return false
	}
	if m.snapshot == nil {
		return false
	}
	switch entry.owner {
	case screenInbox:
		return len(m.snapshot.Inbox) > 0
	case screenTasks:
		return len(m.snapshot.Tasks) > 0
	case screenOverview:
		return true
	}
	return false
}

func paletteLines(m *model, entries []paletteEntry, cursor int) []string {
	lines := []string{"Command palette. Type to filter, Enter runs, Esc backs out.", ""}
	if len(entries) == 0 {
		return append(lines, "  (no match)")
	}
	for index, entry := range entries {
		marker := "  "
		if index == cursor {
			marker = "> "
		}
		 runnable := ""
		if !m.paletteAvailable(entry) {
			runnable = " · on " + entry.owner.title() + " only"
		}
		line := fmt.Sprintf("%s%s  %s", marker, entry.keys, entry.action)
		if entry.command != "" {
			line += "  → " + entry.command
		}
		lines = append(lines, line+runnable)
	}
	return lines
}

// updatePalette owns the palette's keys while it is open and reports
// whether it handled the key. Filter text is capped so the overlay cannot
// grow without bound; Enter runs the cursor row.
func (m *model) updatePalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.showPalette {
		switch msg.String() {
		case "ctrl+c":
			return *m, tea.Quit, true
		case "esc":
			pop(m)
			return *m, nil, true
		case "enter":
			updated, cmd := m.runPalette()
			return updated, cmd, true
		case "j", "down":
			entries := paletteFilter(paletteEntries(), m.palFilter)
			m.palCursor = min(len(entries)-1, max(0, m.palCursor+1))
			return *m, nil, true
		case "k", "up":
			m.palCursor = max(0, m.palCursor-1)
			return *m, nil, true
		case "backspace":
			if runes := []rune(m.palFilter); len(runes) > 0 {
				m.palFilter = string(runes[:len(runes)-1])
			}
			m.palCursor = 0
			return *m, nil, true
		default:
			if msg.Text != "" && len([]rune(m.palFilter)) < 256 {
				m.palFilter += msg.Text
				m.palCursor = 0
			}
			return *m, nil, true
		}
	}
	return *m, nil, false
}

// runPalette executes the palette cursor row. Navigation entries run from
// anywhere; screen-owned actions run only when the current focus owns them,
// exactly as if their key had been pressed — the palette discovers keys, it
// does not bypass their scope. Unavailable rows explain where they live.
func (m *model) runPalette() (tea.Model, tea.Cmd) {
	entries := paletteFilter(paletteEntries(), m.palFilter)
	if m.palCursor < 0 || m.palCursor >= len(entries) {
		return *m, nil
	}
	entry := entries[m.palCursor]
	if !m.paletteAvailable(entry) {
		m.feedback = entry.action + " runs on " + entry.owner.title()
		return *m, nil
	}
	pop(m)
	cp := *m
	return cp.updateKey(tea.KeyPressMsg{Code: rune(0), Text: entry.keys})
}
