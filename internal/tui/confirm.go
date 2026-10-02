package tui

import "fmt"

// confirmRequest is one pending destructive action. The dialog shows the
// exact displayed revision and the exact target ID, so the operator confirms
// what the display bound, not a name recalled from elsewhere.
type confirmRequest struct {
	action   string
	revision int
	targetID string
	reason   string
}

// DestructiveActions names every interface action that consumes authority or
// can lose progress and therefore requires this dialog. stop is wired in
// 6.2; checkpoint clear/restore, delivery cancel/reconcile and retention
// expire register here when their screens land in 6.6/6.7, so the rule is
// stated once and each later screen only adds its row.
var DestructiveActions = []struct {
	Action string
	Reason string
}{
	{Action: "stop", Reason: "interrupts the active run and preserves its state"},
	{Action: "checkpoint-clear", Reason: "clears agent-owned paths; only proven paths are removed"},
	{Action: "checkpoint-restore", Reason: "restores saved work over the current tree"},
	{Action: "delivery-cancel", Reason: "cancels a never-started delivery approval"},
	{Action: "delivery-reconcile", Reason: "closes a stuck delivery from a fresh observation"},
	{Action: "retention-expire", Reason: "deletes raw transcripts against the visible inspect receipt"},
}

func confirmLines(c *confirmRequest) []string {
	lines := []string{
		"Confirm " + clean(c.action) + ": " + clean(c.reason) + ".",
		fmt.Sprintf("Revision %d · target %s", c.revision, clean(c.targetID)),
	}
	// Feature 2.12: stop names its grace defaults. They are fixed here;
	// the execution screen (6.6) makes them editable.
	if c.action == "stop" {
		lines = append(lines, fmt.Sprintf("Interrupt grace %s · terminate grace %s (fixed defaults)", StopInterruptGrace, StopTerminateGrace))
	}
	return append(lines, "", "Enter/y confirms this exact action · Esc/n abandons it.")
}

// needsConfirm reports whether an action string requires the dialog. Task
// and inbox actions carry a colon suffix binding the displayed ID; the match
// is on the action head.
func needsConfirm(action string) bool {
	head := action
	for i, r := range action {
		if r == ':' {
			head = action[:i]
			break
		}
	}
	for _, row := range DestructiveActions {
		if row.Action == head {
			return true
		}
	}
	return false
}
