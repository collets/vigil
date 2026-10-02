package tui

// screenID names every surface the shell can focus. Root screens are the
// five persistent views; overlays push on top of them and esc pops back.
type screenID int

const (
	screenOverview screenID = iota
	screenTasks
	screenInbox
	screenHistory
	screenDetail
	screenHelp
	screenConfirm
	screenProjects
	screenPalette
)

// title names the screen on the tab bar and the help listing.
func (s screenID) title() string {
	switch s {
	case screenOverview:
		return "Overview"
	case screenTasks:
		return "Tasks"
	case screenInbox:
		return "Inbox"
	case screenHistory:
		return "History"
	case screenDetail:
		return "Detail"
	case screenHelp:
		return "Help"
	case screenConfirm:
		return "Confirm"
	case screenProjects:
		return "Projects"
	case screenPalette:
		return "Palette"
	}
	return "Unknown"
}

// rootScreen maps the tab index to its screen. Tab order is stable because
// tests and operators address screens by number.
func rootScreen(tab int) screenID {
	switch tab {
	case 1:
		return screenTasks
	case 2:
		return screenInbox
	case 3:
		return screenHistory
	case 4:
		return screenDetail
	default:
		return screenOverview
	}
}

// push adds an overlay screen. pop removes the top overlay and reports
// whether one was open. replace swaps the root screen, clearing overlays.
func push(m *model, s screenID) {
	m.stack = append(m.stack, s)
}

func pop(m *model) bool {
	if len(m.stack) == 0 {
		return false
	}
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	switch top {
	case screenHelp:
		m.showHelp = false
	case screenConfirm:
		m.confirm = nil
	case screenProjects:
		m.showProjects = false
	case screenPalette:
		m.showPalette = false
		m.palFilter, m.palCursor = "", 0
	}
	return true
}

func replace(m *model, tab int) {
	m.tab = tab
	m.offset = 0
	m.stack = nil
	m.showHelp = false
	m.showPalette = false
	m.palFilter, m.palCursor = "", 0
	m.confirm = nil
	m.showProjects = false
	m.quitArmed = false
}

// Binding describes one key on one screen: what it does and which product
// command it drives, so the help screen and the footer render from the same
// data rather than from two hand-kept copies.
type Binding struct {
	Key     string
	Action  string
	Command string
}

// Mutates reports whether the binding changes persisted state. Only bindings
// with Mutates=true need a revision-bound confirmation path.
type mutatingBinding struct {
	Binding
	Mutates bool
}
