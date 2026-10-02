package tui

import (
	"fmt"
)

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
			// Queue the queue-cursor plan at its displayed rank. The
			// action names the plan so the mutator binds the visible
			// selection instead of the first queueable plan.
			if len(m.snapshot.Queue) == 0 {
				return "", false
			}
			cursor := min(max(m.queueCursor, 0), len(m.snapshot.Queue)-1)
			entry := m.snapshot.Queue[cursor]
			return fmt.Sprintf("queue:%s:%d", entry.ID, entry.Rank), true
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

// moveCursor advances the focused screen's cursor. Overview's j/k select
// the actionable request when one exists and the queue plan otherwise.
func (m *model) moveCursor(delta int) {
	switch {
	case m.focused() == screenInbox && m.snapshot != nil && len(m.snapshot.Inbox) > 0:
		m.inbox = min(len(m.snapshot.Inbox)-1, max(0, m.inbox+delta))
	case m.focused() == screenOverview && m.snapshot != nil && len(m.snapshot.Inbox) > 0:
		m.ovInbox = min(len(m.snapshot.Inbox)-1, max(0, m.ovInbox+delta))
	case m.focused() == screenOverview && m.snapshot != nil && len(m.snapshot.Queue) > 0:
		m.queueCursor = min(len(m.snapshot.Queue)-1, max(0, m.queueCursor+delta))
	case (m.focused() == screenTasks || m.focused() == screenDetail) && m.snapshot != nil && len(m.snapshot.Tasks) > 0:
		m.task = min(len(m.snapshot.Tasks)-1, max(0, m.task+delta))
		m.criterion = 0
	case m.focused() == screenHistory && m.snapshot != nil && len(filteredEvents(m.snapshot.Events, m.historyFilter)) > 0:
		shown := filteredEvents(m.snapshot.Events, m.historyFilter)
		m.historyCursor = min(len(shown)-1, max(0, m.historyCursor+delta))
	default:
		m.offset = max(0, m.offset+delta)
	}
}

// stepCriterion moves the manual-criterion focus on Tasks/Detail and the
// queue-plan selection on the Overview focus. Focused, not tab: an overlay
// must not retarget the selection behind it.
func (m *model) stepCriterion(delta int) {
	if m.focused() == screenOverview && m.snapshot != nil && len(m.snapshot.Queue) > 0 {
		count := len(m.snapshot.Queue)
		m.queueCursor = (m.queueCursor + delta + count) % count
		return
	}
	if (m.focused() == screenTasks || m.focused() == screenDetail) && m.snapshot != nil && len(m.snapshot.Tasks) > 0 && len(m.snapshot.Tasks[m.task].ManualCriteria) > 0 {
		count := len(m.snapshot.Tasks[m.task].ManualCriteria)
		m.criterion = (m.criterion + delta + count) % count
	}
}
