package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// globalBindings lists keys that work on every root screen. They are rendered
// in the help screen and in the degraded footer, never truncated mid-list.
func globalBindings() []Binding {
	return []Binding{
		{Key: "1-5", Action: "screen", Command: ""},
		{Key: "tab", Action: "next screen", Command: ""},
		{Key: "j/k", Action: "move focus", Command: ""},
		{Key: "?", Action: "help", Command: ""},
		{Key: ":", Action: "palette", Command: ""},
		{Key: "P", Action: "projects", Command: ""},
		{Key: "r", Action: "refresh", Command: ""},
		{Key: "q", Action: "quit", Command: ""},
		{Key: "esc", Action: "back", Command: ""},
		{Key: "ctrl+c", Action: "quit now", Command: ""},
	}
}

// screenBindings returns the mutating bindings owned by a root screen.
func screenBindings(s screenID) []Binding {
	switch s {
	case screenOverview:
		return overviewBindings()
	case screenTasks:
		return tasksBindings()
	case screenInbox:
		return inboxBindings()
	case screenDetail:
		return detailBindings()
	default:
		return nil
	}
}

// bindingLine renders one screen's bindings as key-plus-action pairs. It
// never truncates mid-binding: when the full list does not fit, it degrades
// to a count plus a pointer to help. That is the 6.1-F16 fix. The command
// each action maps to lives in the help screen, where nothing is truncated.
func bindingLine(bindings []Binding, width int) string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, b.Key+" "+b.Action)
	}
	full := strings.Join(parts, " · ")
	if ansi.StringWidth(full) <= width {
		return full
	}
	return fmt.Sprintf("%d bindings · ? for all keys", len(bindings))
}

// helpLines lists the full binding set for the focused screen, its parents
// and the globals. Nothing here is truncated: the help screen scrolls.
func helpLines(m *model) []string {
	focused := m.focused()
	if focused == screenHelp {
		focused = rootScreen(m.tab)
	}
	lines := []string{"Keys for " + focused.title() + " (focused screen).", ""}
	for _, b := range screenBindings(focused) {
		line := "  " + b.Key + "  " + b.Action
		if b.Command != "" {
			line += "  → " + b.Command
		}
		lines = append(lines, line)
	}
	if len(screenBindings(focused)) == 0 {
		lines = append(lines, "  (no mutating keys on this screen)")
	}
	if focused == screenOverview {
		lines = append(lines, "", "Overview navigation (no persistence).", "")
		lines = append(lines, "  R  run detail  → project execution-inspect")
		lines = append(lines, "  Enter  open request in Inbox")
		lines = append(lines, "  [ ]  select queue plan")
		lines = append(lines, "  j/k  select request")
	}
	if focused == screenHistory {
		lines = append(lines, "", "History navigation (no persistence).", "")
		lines = append(lines, "  F  filter by kind  → project events --kind")
		lines = append(lines, "  Enter  open event detail")
		lines = append(lines, "  j/k  move event cursor")
	}
	lines = append(lines, "", "Global keys (every screen).", "")
	for _, b := range globalBindings() {
		lines = append(lines, "  "+b.Key+"  "+b.Action)
	}
	lines = append(lines, "", "esc backs out one level · q quits explicitly.")
	lines = append(lines, "", "Command palette (:) runs every registered action above,")
	lines = append(lines, "filtered as you type, but only from the screen that owns it.")
	lines = append(lines, "", entryPolicyLine())
	return lines
}
