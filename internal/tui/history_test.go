package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/core"
)

func historyModel(events []core.Event) model {
	snapshot := core.DashboardSnapshot{
		Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 3, State: "paused"}},
		Events:    events,
	}
	return model{ctx: context.Background(), snapshot: &snapshot, width: 120, height: 40, tab: 3}
}

func commandEvent(seq int64, kind, actor string) core.Event {
	payload, _ := json.Marshal(map[string]string{"command_kind": kind, "actor": actor})
	return core.Event{Sequence: seq, Kind: "command_applied", At: 1728000000000 + seq*1000, Payload: payload}
}

func queuedEvent(seq int64, plan string, rank int) core.Event {
	payload, _ := json.Marshal(map[string]any{"plan_id": plan, "rank": rank})
	return core.Event{Sequence: seq, Kind: "plan_queued", At: 1728000000000 + seq*1000, Payload: payload}
}

// TestHistoryFilterCyclesKinds proves History is filterable: F walks the
// window's kinds and returns to all, resetting the cursor each time.
func TestHistoryFilterCyclesKinds(t *testing.T) {
	events := []core.Event{commandEvent(1, "project.pause", "human"), queuedEvent(2, "plan-a", 0), commandEvent(3, "project.continue", "human")}
	m := historyModel(events)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
	m = updated.(model)
	if m.historyFilter != "command_applied" {
		t.Fatalf("filter is %q, want first kind", m.historyFilter)
	}
	if m.historyCursor != 0 {
		t.Fatal("filter change did not reset the cursor")
	}
	if view := m.View().Content; !strings.Contains(view, "showing 2 of 3 events") || !strings.Contains(view, "filter: command_applied") {
		t.Fatal("filtered window wrong:\n" + view)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
	m = updated.(model)
	if m.historyFilter != "plan_queued" {
		t.Fatalf("filter is %q, want second kind", m.historyFilter)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
	m = updated.(model)
	if m.historyFilter != "" {
		t.Fatalf("filter is %q, want all", m.historyFilter)
	}
}

// TestHistoryFilterScopedToHistory proves F fires only on the History
// focus and never mutates: it is navigation, like R.
func TestHistoryFilterScopedToHistory(t *testing.T) {
	for _, tab := range []int{0, 1, 2, 4} {
		m := historyModel([]core.Event{commandEvent(1, "project.pause", "human")})
		m.tab = tab
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
		m = updated.(model)
		if cmd != nil || m.historyFilter != "" {
			t.Fatalf("F acted from tab %d", tab)
		}
	}
}

// TestHistoryOpensEventDetail proves History is navigable and openable:
// Enter carries the cursor event into a sanitized detail overlay, and esc
// backs out to the list.
func TestHistoryOpensEventDetail(t *testing.T) {
	events := []core.Event{commandEvent(1, "project.pause", "human"), queuedEvent(2, "plan-a", 0)}
	m := historyModel(events)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || !m.showEvent {
		t.Fatal("Enter did not open the event")
	}
	view := m.View().Content
	for _, want := range []string{"Event detail.", "Queued: plan-a · rank 0", "Focus: Event"} {
		if !strings.Contains(view, want) {
			t.Fatalf("event screen missing %q:\n%s", want, view)
		}
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	if cmd != nil || m.showEvent {
		t.Fatal("esc did not close the event")
	}
}

// TestHistoryEnrichesClosedPayloads proves the whitelist: command_applied
// and plan_queued render their closed scalar fields, while anything else is
// withheld rather than risking an unbounded or credential-bearing render.
func TestHistoryEnrichesClosedPayloads(t *testing.T) {
	events := []core.Event{
		commandEvent(1, "project.pause", "human"),
		{Sequence: 2, Kind: "dispatch_selected", At: 1728000002000, Payload: json.RawMessage(`{"plan_id":"plan-a","task_id":"t1","secret":"s3cr3t-token-value"}`)},
	}
	m := historyModel(events)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || !m.showEvent {
		t.Fatal("Enter did not open the first event")
	}
	if view := m.View().Content; !strings.Contains(view, "Applied: project.pause · actor human") {
		t.Fatal("command_applied not enriched:\n" + view)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	view := m.View().Content
	if !strings.Contains(view, "Payload withheld (unbounded or unclassified).") {
		t.Fatal("unclassified payload not withheld:\n" + view)
	}
	if strings.Contains(view, "s3cr3t-token-value") {
		t.Fatal("credential-bearing payload rendered:\n" + view)
	}
}

// TestHistoryAnnouncesWindowCap is the 6.1-F20 regression test: the capped
// window names the cap and the cursor to continue from.
func TestHistoryAnnouncesWindowCap(t *testing.T) {
	events := make([]core.Event, 0, 100)
	for seq := int64(1); seq <= 100; seq++ {
		events = append(events, commandEvent(seq, fmt.Sprintf("op-%d", seq%3), "human"))
	}
	m := historyModel(events)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m = updated.(model)
	view := m.View().Content
	if !strings.Contains(view, "capped at the latest 100") || !strings.Contains(view, "--after 100") {
		t.Fatal("cap not announced with cursor:\n" + view)
	}
	short := historyModel(events[:3])
	if view := short.View().Content; !strings.Contains(view, "Complete window (3 events).") {
		t.Fatal("complete window not stated:\n" + view)
	}
}

// TestPaletteHistoryRows proves the palette discovers History navigation
// without bypassing its focus: rows run from History and name it elsewhere.
func TestPaletteHistoryRows(t *testing.T) {
	m := historyModel([]core.Event{commandEvent(1, "project.pause", "human"), queuedEvent(2, "plan-a", 0)})
	updated, _ := m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m = updated.(model)
	for _, key := range []string{"o", "p", "e", "n"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if !m.showEvent {
		t.Fatalf("palette did not open the event: %q", m.feedback)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 0x1b, Text: "esc"})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m = updated.(model)
	for _, key := range []string{"f", "i", "l", "t", "e", "r"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if m.historyFilter != "command_applied" {
		t.Fatalf("palette did not filter: %q", m.historyFilter)
	}
	m2 := historyModel([]core.Event{commandEvent(1, "project.pause", "human")})
	m2.tab = 0
	updated, _ = m2.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m2 = updated.(model)
	for _, key := range []string{"o", "p", "e", "n"} {
		updated, _ = m2.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m2 = updated.(model)
	}
	updated, cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 = updated.(model)
	if cmd != nil || m2.showEvent {
		t.Fatal("palette bypassed history scope")
	}
	if !strings.Contains(m2.feedback, "History") {
		t.Fatalf("palette did not name the owning screen: %q", m2.feedback)
	}
}

// TestHistoryKeysFromHelpDismissFirst pins the shared dismiss-first rule:
// with help open on History, F closes help and then applies (help never
// swallows a key).
func TestHistoryKeysFromHelpDismissFirst(t *testing.T) {
	m := historyModel([]core.Event{commandEvent(1, "project.pause", "human")})
	updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(model)
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
	m = updated.(model)
	if m.showHelp {
		t.Fatal("F did not dismiss help")
	}
	if m.historyFilter != "command_applied" {
		t.Fatalf("F did not apply after dismiss: %q", m.historyFilter)
	}
}
// TestHistoryOwnsNoProjectControls proves the 6.2 scoping still holds with
// the new History keys: p/c/a/u/s persist nothing from History, while F
// and Enter navigate without mutating.
func TestHistoryOwnsNoProjectControls(t *testing.T) {
	m := historyModel([]core.Event{commandEvent(1, "project.pause", "human")})
	m.mutate = func(_ context.Context, _ core.DashboardSnapshot, _ string) error {
		t.Fatal("project control fired from History")
		return nil
	}
	for _, key := range []string{"p", "c", "a", "u", "s"} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
		if cmd != nil {
			t.Fatalf("key %q from History produced a command", key)
		}
	}
	if m.mutating {
		t.Fatal("History is mutating after control keys")
	}
}
