package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
)

// TestActionStringsUnchanged pins the exact set of twenty mutation action
// strings from 6.1 §1.2. The 6.2 refactor moves where keys are bound; this
// test proves it changed nothing about what can be done.
func TestActionStringsUnchanged(t *testing.T) {
	want := []string{
		"pause", "continue", "advance", "queue", "stop",
		"apply-proposal", "reject-proposal", "request-proposal-revision",
		"remain-blocked", "exact-resume", "fresh-context",
		"answer-clarification", "cancel-clarification",
		"answer-input", "dismiss-input",
		"allow", "deny",
		"human-accept", "manual-pass", "accept-task",
	}
	if len(ActionStrings) != len(want) {
		t.Fatalf("ActionStrings has %d entries, want %d", len(ActionStrings), len(want))
	}
	seen := map[string]bool{}
	for _, action := range ActionStrings {
		if seen[action] {
			t.Fatalf("duplicate action %q", action)
		}
		seen[action] = true
	}
	for _, action := range want {
		if !seen[action] {
			t.Errorf("action %q missing after refactor", action)
		}
		if !actionSet[action] {
			t.Errorf("action %q not in dispatch set", action)
		}
	}
}

func scopedModel() model {
	snapshot := core.DashboardSnapshot{
		Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 9, State: "ready"}},
		ActiveRun: "run-1",
		Inbox:     []core.InboxEntry{{ID: "req-1", Kind: "approval"}},
		Tasks:     []core.TaskDetail{{ID: "task-1", Revision: 2}},
		Events:    []core.Event{{Sequence: 1, Kind: "project.pause"}},
	}
	return model{ctx: context.Background(), snapshot: &snapshot, width: 110, height: 24, mutate: func(context.Context, core.DashboardSnapshot, string) error {
		return nil
	}}
}

// TestProjectControlsScopedToOwningScreen is the 6.1-F17 regression test:
// p, c, a, u and s must not persist anything from History, Detail, Tasks or
// Inbox. A mutate function that fails the test on any call proves it.
func TestProjectControlsScopedToOwningScreen(t *testing.T) {
	for _, tab := range []int{1, 2, 3, 4} {
		m := scopedModel()
		m.tab = tab
		m.mutate = func(context.Context, core.DashboardSnapshot, string) error {
			t.Fatalf("project control fired from tab %d", tab)
			return nil
		}
		for _, key := range []string{"p", "c", "a", "u", "s"} {
			updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
			m = updated.(model)
			if cmd != nil {
				// A command is only a mutation when the model armed one;
				// refresh ticks never occur here, so any command is a leak.
				t.Fatalf("key %q from tab %d produced a command", key, tab)
			}
		}
		if m.mutating {
			t.Fatalf("tab %d is mutating after foreign keys", tab)
		}
	}
}

// TestProjectControlsFireOnOverview proves the scoping did not disable the
// owning screen: the same keys start mutations there (stop excepted, which
// now confirms first).
func TestProjectControlsFireOnOverview(t *testing.T) {
	called := make(chan string, 8)
	m := scopedModel()
	m.tab = 0
	m.mutate = func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		called <- action
		return nil
	}
	for _, key := range []string{"p", "c", "a", "u"} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
		if cmd == nil {
			t.Fatalf("key %q on Overview did not start", key)
		}
		msg := cmd().(mutationResult)
		updated, _ = m.Update(msg)
		m = updated.(model)
	}
	for _, want := range []string{"pause", "continue", "advance", "queue"} {
		if got := <-called; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// TestStopRequiresConfirmation proves stop shows the exact displayed revision
// and run ID before anything persists, and only mutates on confirmation.
func TestStopRequiresConfirmation(t *testing.T) {
	called := make(chan string, 1)
	m := scopedModel()
	m.tab = 0
	m.mutate = func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		called <- action
		return nil
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("stop mutated without confirmation")
	}
	if m.confirm == nil {
		t.Fatal("stop did not open a confirmation")
	}
	view := m.View().Content
	if !strings.Contains(view, "Revision 9") || !strings.Contains(view, "run-1") {
		t.Fatal("confirmation does not show the exact revision and run", view)
	}
	select {
	case <-called:
		t.Fatal("mutator ran before confirmation")
	default:
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("confirming did not start the mutation")
	}
	if got := cmd().(mutationResult).action; got != "stop" {
		t.Fatalf("confirmed action %q, want stop", got)
	}
	// Abandon path: s then Esc persists nothing.
	m2 := scopedModel()
	m2.tab = 0
	m2.mutate = func(context.Context, core.DashboardSnapshot, string) error {
		t.Fatal("abandoned stop mutated")
		return nil
	}
	updated, _ = m2.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m2 = updated.(model)
	if m2.confirm == nil {
		t.Fatal("stop did not arm a confirmation to abandon")
	}
	updated, cmd = m2.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m2 = updated.(model)
	if cmd != nil || m2.confirm != nil || m2.mutating {
		t.Fatal("abandoned stop mutated or stayed armed")
	}
}

// TestEscPopsInsteadOfQuitting is the 6.1-F18 fix: esc backs out one level
// and never quits the program; q is the explicit quit, confirmed when a run
// is active or text is unsaved.
func TestEscPopsInsteadOfQuitting(t *testing.T) {
	m := scopedModel()
	m.tab = 2
	// esc at root: no quit, only a hint.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("esc quit the program")
	}
	// ? opens help; esc closes it without quitting.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(model)
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	if cmd != nil || m.showHelp {
		t.Fatal("esc did not close help cleanly")
	}
	// q with an active run arms instead of quitting.
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(model)
	if cmd != nil || !m.quitArmed {
		t.Fatal("q with an active run did not arm confirmation")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = updated.(model)
	if cmd != nil || m.quitArmed {
		t.Fatal("n did not abandon the armed quit")
	}
	// q with no run and no input quits outright.
	m.snapshot.ActiveRun = ""
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q did not quit a quiet shell")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not produce a quit message")
	}
}

// TestBindingLineNeverTruncates is the 6.1-F16 fix: at 40, 60, 80, 110 and
// 200 columns no binding string is cut mid-list and the tab bar stays
// readable. Either the full list fits, or a count plus a pointer to help
// renders instead.
func TestBindingLineNeverTruncates(t *testing.T) {
	for _, width := range []int{40, 60, 80, 110, 200} {
		for tab := 0; tab < 5; tab++ {
			m := scopedModel()
			m.tab = tab
			m.width, m.height = width, 40
			view := m.View().Content
			lines := strings.Split(view, "\n")
			bar := lines[0]
			if strings.Contains(bar, "…") {
				t.Fatalf("tab bar truncated at width %d tab %d: %q", width, tab, bar)
			}
			footer := lines[len(lines)-1]
			if strings.Contains(footer, "…") {
				t.Fatalf("binding line truncated at width %d tab %d: %q", width, tab, footer)
			}
			if !strings.Contains(footer, "?") {
				t.Fatalf("binding line at width %d tab %d has no help pointer: %q", width, tab, footer)
			}
		}
	}
}

// TestFocusRenderedOnEveryFrame proves the focus model: each screen names
// its focus on every frame, so what a key will act on is always visible.
func TestFocusRenderedOnEveryFrame(t *testing.T) {
	for tab := 0; tab < 5; tab++ {
		m := scopedModel()
		m.tab = tab
		m.width, m.height = 110, 40
		if view := m.View().Content; !strings.Contains(view, "Focus:") {
			t.Fatalf("tab %d renders no focus", tab)
		}
	}
	m := scopedModel()
	m.tab = 2
	m.width, m.height = 110, 40
	if view := m.View().Content; !strings.Contains(view, "Focus: Inbox · req-1") {
		t.Fatal("inbox focus does not name the focused request")
	}
}

// TestTextEntryIsBounded proves the clarification path keeps its 4096-byte
// cap, shows a visible counter, and refuses empty answers.
func TestTextEntryIsBounded(t *testing.T) {
	m := scopedModel()
	m.tab = 2
	m.snapshot.Inbox = []core.InboxEntry{{ID: "in-1", Kind: "input"}}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = updated.(model)
	if cmd != nil || m.inputRequest != "in-1" {
		t.Fatal("i did not enter text mode")
	}
	for i := 0; i < TextEntryLimit+100; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		m = updated.(model)
	}
	if got := len([]rune(m.inputText)); got != TextEntryLimit {
		t.Fatalf("entry holds %d runes, want exactly %d", got, TextEntryLimit)
	}
	if view := m.View().Content; !strings.Contains(view, "4096") {
		t.Fatal("no visible length counter", view)
	}
	// Empty submission is refused.
	m.inputText = "   "
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("empty answer submitted")
	}
}

// TestTextEntryEnforcesByteCap proves the 4096-byte cap is enforced in the
// entry's own unit: multibyte answers stop at 4096 bytes rather than failing
// at submit.
func TestTextEntryEnforcesByteCap(t *testing.T) {
	entry := textEntry{}
	for i := 0; i < 3000; i++ {
		entry.add("\u00e9")
	}
	if got := len(entry.text); got != 4096 {
		t.Fatalf("entry holds %d bytes, want exactly 4096", got)
	}
	if got := len([]rune(entry.text)); got != 2048 {
		t.Fatalf("entry holds %d runes, want 2048 full characters", got)
	}
}

// TestEmergencyQuitFromTextMode documents the one quit path available while
// typing: q feeds the entry, Esc abandons it, and ctrl+c quits at once.
func TestEmergencyQuitFromTextMode(t *testing.T) {
	m := scopedModel()
	m.tab = 2
	m.snapshot.Inbox = []core.InboxEntry{{ID: "in-1", Kind: "input"}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(model)
	if cmd != nil || m.inputText != "q" {
		t.Fatal("q did not feed the entry")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 0, Text: "ctrl+c"})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("ctrl+c did not quit from text mode")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c did not produce a quit message")
	}
}

// TestOverlaysNeverTruncate extends the F16 guarantee past the root tabs:
// help, confirmation and project overlays degrade through the same binding
// line instead of cutting mid-list.
func TestOverlaysNeverTruncate(t *testing.T) {
	for _, width := range []int{40, 60, 80, 110, 200} {
		open := func() model {
			m := scopedModel()
			m.width, m.height = width, 40
			return m
		}
		m := open()
		m.tab = 0
		updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		m = updated.(model)
		if lines := strings.Split(m.View().Content, "\n"); strings.Contains(lines[len(lines)-1], "\u2026") {
			t.Fatalf("confirm footer truncated at width %d", width)
		}
		m = open()
		updated, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
		m = updated.(model)
		if lines := strings.Split(m.View().Content, "\n"); strings.Contains(lines[len(lines)-1], "\u2026") {
			t.Fatalf("help footer truncated at width %d", width)
		}
		m = open()
		m.projects = []ProjectRef{{ID: "fixture", Root: "/a"}}
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
		m = updated.(model)
		if lines := strings.Split(m.View().Content, "\n"); strings.Contains(lines[len(lines)-1], "\u2026") {
			t.Fatalf("projects footer truncated at width %d", width)
		}
	}
}

// TestProjectSwitcherOffersRegisteredProjects proves checkpoint C: P lists
// registered projects and Enter switches the open one.
func TestProjectSwitcherOffersRegisteredProjects(t *testing.T) {
	m := scopedModel()
	m.tab = 0
	m.projects = []ProjectRef{{ID: "fixture", Root: "/a"}, {ID: "other", Root: "/b"}}
	switched := ""
	m.onSwitch = func(id string) (source, mutator, error) {
		switched = id
		load := func(context.Context) (core.DashboardSnapshot, error) {
			return core.DashboardSnapshot{}, nil
		}
		return load, func(context.Context, core.DashboardSnapshot, string) error { return nil }, nil
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	m = updated.(model)
	if !m.showProjects {
		t.Fatal("P did not open the switcher")
	}
	if view := m.View().Content; !strings.Contains(view, "other") {
		t.Fatal("switcher does not list the registered project")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if switched != "other" {
		t.Fatalf("switched to %q, want other", switched)
	}
	if cmd == nil {
		t.Fatal("switch did not start a fresh load")
	}
	if !m.loading {
		t.Fatal("switch did not arm loading")
	}
	// The old snapshot stays until the fresh load lands; delivering it
	// replaces the project instead of relabelling stale data.
	fresh := core.DashboardSnapshot{Readiness: core.Readiness{Project: core.Project{ID: "other", Revision: 1}}}
	updated, _ = m.Update(loaded{snapshot: fresh})
	m = updated.(model)
	if m.loading || m.snapshot.Readiness.Project.ID != "other" {
		t.Fatal("fresh load did not replace the snapshot")
	}
	if m.confirm != nil || m.showHelp || m.quitArmed || m.inputRequest != "" {
		t.Fatal("switch carried confirmation, help, quit or input state across projects")
	}
}

// TestPaletteRunsOwnedActionsOnly proves feature row 1.3 without reopening
// 6.1-F17: the palette reaches every registered action, but a screen-owned
// action runs only from its screen. From elsewhere the row names its owner.
func TestPaletteRunsOwnedActionsOnly(t *testing.T) {
	called := make(chan string, 2)
	m := scopedModel()
	m.tab = 0
	m.width, m.height = 110, 40
	m.mutate = func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		called <- action
		return nil
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m = updated.(model)
	if !m.showPalette {
		t.Fatal(": did not open the palette")
	}
	// Filter to pause and run it: Overview owns pause.
	for _, key := range []string{"p", "a", "u"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("palette Enter did not run the owned action")
	}
	if got := cmd().(mutationResult).action; got != "pause" {
		t.Fatalf("palette ran %q, want pause", got)
	}
	// From History the same row is unavailable and names its owner.
	m2 := scopedModel()
	m2.tab = 3
	m2.width, m2.height = 110, 40
	m2.mutate = func(context.Context, core.DashboardSnapshot, string) error {
		t.Fatal("palette bypassed screen scoping")
		return nil
	}
	updated, _ = m2.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m2 = updated.(model)
	for _, key := range []string{"p", "a", "u"} {
		updated, _ = m2.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m2 = updated.(model)
	}
	updated, cmd = m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 = updated.(model)
	if cmd != nil {
		t.Fatal("palette ran a foreign action")
	}
	if !strings.Contains(m2.feedback, "Overview") {
		t.Fatalf("palette did not name the owning screen: %q", m2.feedback)
	}
	// Esc backs out of the palette without quitting.
	updated, cmd = m2.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m2 = updated.(model)
	if cmd != nil || m2.showPalette {
		t.Fatal("esc did not leave the palette cleanly")
	}
}

// TestDestructiveActionsRegistered pins the confirmation registry: every
// action that consumes authority or can lose progress must confirm.
func TestDestructiveActionsRegistered(t *testing.T) {
	want := []string{"stop", "checkpoint-clear", "checkpoint-restore", "delivery-cancel", "delivery-reconcile", "retention-expire"}
	if len(DestructiveActions) != len(want) {
		t.Fatalf("DestructiveActions has %d entries, want %d", len(DestructiveActions), len(want))
	}
	for _, action := range want {
		if !needsConfirm(action) && !needsConfirm(action+":target") {
			t.Errorf("action %q needs no confirmation", action)
		}
	}
}

// TestTUIFilesStaySmall keeps the 6.1-F21 split from regressing: no
// non-test file in internal/tui may exceed roughly 400 lines.
func TestTUIFilesStaySmall(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if lines := len(strings.Split(string(raw), "\n")); lines > 400 {
			t.Errorf("%s has %d lines, want at most 400", name, lines)
		}
	}
}
