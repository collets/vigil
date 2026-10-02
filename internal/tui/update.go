package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Update routes keys through the shell: overlays first, then the focused
// screen's bindings, then navigation. Mutating keys fire only on the screen
// that owns them; pressing one elsewhere does nothing and persists nothing.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
	case loaded:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.snapshot = &msg.snapshot
			m.inbox = min(m.inbox, max(0, len(msg.snapshot.Inbox)-1))
			m.task = min(m.task, max(0, len(msg.snapshot.Tasks)-1))
			m.historyCursor = min(m.historyCursor, max(0, len(msg.snapshot.Events)-1))
			if len(msg.snapshot.Tasks) == 0 || len(msg.snapshot.Tasks[m.task].ManualCriteria) == 0 {
				m.criterion = 0
			} else {
				m.criterion = min(m.criterion, len(msg.snapshot.Tasks[m.task].ManualCriteria)-1)
			}
			m.projectCursor = min(m.projectCursor, max(0, len(m.projects)-1))
		}
	case mutationResult:
		m.mutating = false
		if msg.err != nil {
			m.feedback = msg.action + " failed: " + clean(msg.err.Error())
		} else {
			m.feedback = msg.action + " succeeded"
			m.loading = true
			return m, m.fetch()
		}
	case refresh:
		if m.loading {
			return m, tick()
		}
		m.loading = true
		return m, tea.Batch(m.fetch(), tick())
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	m.offset = min(m.offset, max(0, len(m.lines())-max(1, m.height-5)))
	return m, nil
}

func (m model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Text entry swallows every key except quit and its own controls.
	if m.inputRequest != "" {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.inputRequest, m.inputAction, m.inputText = "", "", ""
			m.feedback = "clarification answer cancelled locally"
		case "backspace":
			entry := textEntry{text: m.inputText}
			entry.backspace()
			m.inputText = entry.text
		case "enter":
			answer := strings.TrimSpace(m.inputText)
			if answer == "" {
				m.feedback = "clarification answer cannot be empty"
				return m, nil
			}
			action := m.inputAction + ":" + m.inputRequest + ":" + encodeAnswer(answer)
			m.inputRequest, m.inputAction, m.inputText = "", "", ""
			m.mutating = true
			m.feedback = "answer-clarification pending"
			return m, m.act(action)
		default:
			if msg.Text != "" {
				entry := textEntry{text: m.inputText}
				entry.add(msg.Text)
				m.inputText = entry.text
			}
		}
		return m, nil
	}
	// The confirmation dialog owns its keys.
	if m.confirm != nil {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "y":
			action := m.confirm.action
			m.confirm = nil
			m.stack = nil
			m.mutating = true
			m.feedback = action + " pending"
			return m, m.act(action)
		default:
			m.confirm = nil
			m.stack = nil
			m.feedback = "confirmation abandoned locally"
			return m, nil
		}
	}
	// Armed quit owns its keys.
	if m.quitArmed {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "y":
			return m, tea.Quit
		default:
			m.quitArmed = false
			m.feedback = "quit abandoned locally"
			return m, nil
		}
	}
	// Project switcher owns its keys. Nothing falls through: while the
	// switcher is open no mutation, help or quit can fire past it.
	if m.showProjects {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			pop(&m)
			return m, nil
		case "enter":
			if len(m.projects) > 0 {
				return m, m.switchProject(m.projects[m.projectCursor].ID)
			}
			pop(&m)
			return m, nil
		case "j", "down":
			if len(m.projects) > 0 {
				m.projectCursor = min(len(m.projects)-1, m.projectCursor+1)
			}
			return m, nil
		case "k", "up":
			m.projectCursor = max(0, m.projectCursor-1)
			return m, nil
		default:
			return m, nil
		}
	}
	// Help owns only its exit key; every other key closes it first so help
	// can never swallow a mutation.
	if m.showHelp {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		default:
			pop(&m)
			if msg.String() == "esc" || msg.String() == "?" {
				return m, nil
			}
		}
	}
	if m.snapshot != nil && m.mutate != nil && !m.mutating {
		if action, ok := m.screenAction(msg.String()); ok {
			if needsConfirm(action) {
				m.confirm = &confirmRequest{action: action, revision: m.snapshot.Readiness.Project.Revision, targetID: m.confirmTarget(action), reason: confirmReason(action)}
				push(&m, screenConfirm)
				m.feedback = action + " needs confirmation"
				return m, nil
			}
			m.mutating = true
			m.feedback = action + " pending"
			return m, m.act(action)
		}
	}
	switch msg.String() {
	case "q", "ctrl+c":
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.unsavedOrActive() {
			m.quitArmed = true
			m.feedback = m.quitWarning()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if pop(&m) {
			return m, nil
		}
		m.feedback = "esc backs out · q quits"
		return m, nil
	case "?":
		if len(m.stack) != 0 {
			return m, nil
		}
		m.showHelp = true
		push(&m, screenHelp)
		return m, nil
	case "P":
		if len(m.stack) != 0 {
			return m, nil
		}
		m.showProjects = true
		m.projectCursor = 0
		push(&m, screenProjects)
		return m, nil
	case "r":
		if !m.loading {
			m.loading = true
			return m, m.fetch()
		}
	case "tab", "right":
		replace(&m, (m.tab+1)%5)
	case "shift+tab", "left":
		replace(&m, (m.tab+4)%5)
	case "1", "2", "3", "4", "5":
		replace(&m, int(msg.String()[0]-'1'))
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "pgdown":
		m.offset += max(1, m.height-5)
	case "pgup":
		m.offset = max(0, m.offset-max(1, m.height-5))
	case "home":
		m.offset = 0
	case "end":
		m.offset = len(m.lines())
	case "]":
		m.stepCriterion(1)
	case "[":
		m.stepCriterion(-1)
	}
	m.offset = min(m.offset, max(0, len(m.lines())-max(1, m.height-5)))
	return m, nil
}

// screenAction maps a key to its action string only when the focused screen
// owns that key. Project controls live on Overview; inbox decisions on
// Inbox; quality actions on Tasks and Detail. History owns no mutating key,
// so no persisted change can originate there.
func (m *model) screenAction(key string) (string, bool) {
	if m.snapshot == nil {
		return "", false
	}
	switch m.focused() {
	case screenOverview:
		switch key {
		case "p":
			return "pause", true
		case "c":
			return "continue", true
		case "a":
			return "advance", true
		case "u":
			return "queue", true
		case "s":
			return "stop", true
		}
	case screenInbox:
		if len(m.snapshot.Inbox) == 0 {
			return "", false
		}
		focused := m.snapshot.Inbox[m.inbox].ID
		switch key {
		case "g":
			return "apply-proposal:" + focused, true
		case "b":
			return "remain-blocked:" + focused, true
		case "x":
			return "exact-resume:" + focused, true
		case "f":
			return "fresh-context:" + focused, true
		case "v":
			return "request-proposal-revision:" + focused, true
		case "y":
			return "allow:" + focused, true
		case "n":
			return m.denyAction(), true
		case "i":
			return m.beginInput()
		}
	case screenTasks, screenDetail:
		if len(m.snapshot.Tasks) == 0 {
			return "", false
		}
		switch key {
		case "h", "m", "t":
			return m.taskAction(key), true
		}
	}
	return "", false
}

// denyAction resolves n against the focused request kind, exactly as before:
// native clarification cancels, plain input dismisses, proposal approvals
// reject, everything else denies.
func (m *model) denyAction() string {
	entry := m.snapshot.Inbox[m.inbox]
	switch {
	case entry.Kind == "input" && entry.SessionID != "":
		return "cancel-clarification:" + entry.ID
	case entry.Kind == "input":
		return "dismiss-input:" + entry.ID
	case entry.Kind == "approval" && proposalRequest(entry):
		return "reject-proposal:" + entry.ID
	default:
		return "deny:" + entry.ID
	}
}

// beginInput enters bounded text mode for the focused input request. It
// returns ("", false) when the focused row is not an input, so i is a
// no-op there rather than a mutation.
func (m *model) beginInput() (string, bool) {
	entry := m.snapshot.Inbox[m.inbox]
	if entry.Kind != "input" {
		return "", false
	}
	m.inputRequest = entry.ID
	m.inputAction = "answer-input"
	if entry.SessionID != "" && entry.NativeRequestKey != "" {
		m.inputAction = "answer-clarification"
	}
	m.inputText = ""
	m.feedback = "enter input answer; Enter submits, Esc cancels locally"
	return "", false
}

// taskAction binds h, m and t to the focused task at its displayed revision.
func (m *model) taskAction(key string) string {
	prefix := map[string]string{"h": "human-accept", "m": "manual-pass", "t": "accept-task"}[key]
	task := m.snapshot.Tasks[m.task]
	if key == "m" && len(task.ManualCriteria) > 0 {
		criterion := min(m.criterion, len(task.ManualCriteria)-1)
		return fmt.Sprintf("%s:%s:%d:%s", prefix, task.ID, task.Revision, task.ManualCriteria[criterion])
	}
	return fmt.Sprintf("%s:%s:%d", prefix, task.ID, task.Revision)
}

func (m *model) moveCursor(delta int) {
	switch {
	case m.focused() == screenInbox && m.snapshot != nil && len(m.snapshot.Inbox) > 0:
		m.inbox = min(len(m.snapshot.Inbox)-1, max(0, m.inbox+delta))
	case (m.tab == 1 || m.tab == 4) && m.snapshot != nil && len(m.snapshot.Tasks) > 0:
		m.task = min(len(m.snapshot.Tasks)-1, max(0, m.task+delta))
		m.criterion = 0
	case m.focused() == screenHistory && m.snapshot != nil && len(m.snapshot.Events) > 0:
		m.historyCursor = min(len(m.snapshot.Events)-1, max(0, m.historyCursor+delta))
	default:
		m.offset = max(0, m.offset+delta)
	}
}

func (m *model) stepCriterion(delta int) {
	if (m.tab == 1 || m.tab == 4) && m.snapshot != nil && len(m.snapshot.Tasks) > 0 && len(m.snapshot.Tasks[m.task].ManualCriteria) > 0 {
		count := len(m.snapshot.Tasks[m.task].ManualCriteria)
		m.criterion = (m.criterion + delta + count) % count
	}
}
