package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
)

type source func(context.Context) (core.DashboardSnapshot, error)

type loaded struct {
	snapshot core.DashboardSnapshot
	err      error
}

type refresh struct{}

type mutationResult struct {
	action string
	err    error
}

type mutator func(context.Context, core.DashboardSnapshot, string) error

// model is the shared shell state. Per-screen rendering and key ownership
// live in the screen files; this struct only carries what the shell needs to
// route input and render frames. Field names are stable: the dashboard tests
// construct model literals directly.
type model struct {
	ctx      context.Context
	load     source
	snapshot *core.DashboardSnapshot
	err      error
	loading  bool
	mutating bool
	feedback string
	mutate   mutator

	// Bounded text entry (clarification and input answers).
	inputRequest string
	inputAction  string
	inputText    string

	// Root screen and per-screen cursors. tab selects the root screen;
	// inbox, task and criterion are that screen's visible focus, and
	// historyCursor is History's. Every screen has exactly one focus that
	// is rendered on every frame, so "what will this key act on" is always
	// answerable from the display.
	tab, offset, inbox, task, criterion int
	historyCursor                      int
	// Overview cursors: queueCursor selects the ranked queue entry that
	// u acts on; ovInbox selects the actionable request that Enter
	// opens in the Inbox. Both are rendered on every Overview frame.
	queueCursor, ovInbox int
	width, height        int

	// Screen stack. Root screens replace the stack base; overlays
	// (help, confirmation, project switcher) push on top. esc pops one
	// level; quit is a distinct explicit action.
	stack []screenID

	// Overlay state.
	showHelp     bool
	showPalette  bool
	showRun      bool
	showEvent    bool
	// History state: kind filter ("" for all) and the opened event's
	// sequence for the detail overlay.
	historyFilter string
	historyOpen   int64
	palFilter    string
	palCursor    int
	confirm      *confirmRequest
	showProjects bool
	projectCursor int
	projects      []ProjectRef
	onSwitch      func(id string) (source, mutator, error)

	// Quit confirmation when a run is active or text is unsaved.
	quitArmed bool
}

func (m model) act(action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		return mutationResult{action: action, err: m.mutate(ctx, *m.snapshot, action)}
	}
}

func (m model) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 4*time.Second)
		defer cancel()
		snapshot, err := m.load(ctx)
		return loaded{snapshot, err}
	}
}

func tick() tea.Cmd { return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return refresh{} }) }

func (m model) Init() tea.Cmd { return tea.Batch(m.fetch(), tick()) }

// focused returns the screen that owns the current frame: the top overlay
// when one is open, otherwise the root screen selected by tab.
func (m model) focused() screenID {
	if len(m.stack) > 0 {
		return m.stack[len(m.stack)-1]
	}
	return rootScreen(m.tab)
}
