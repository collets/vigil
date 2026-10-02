package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"vigil/internal/core"
)

// historyWindowCap is the bounded event window the dashboard snapshot and
// the CLI both read. History never renders past it silently.
const historyWindowCap = 100

// filteredEvents applies the History kind filter to the snapshot window.
// Empty filter means every event.
func filteredEvents(events []core.Event, filter string) []core.Event {
	if filter == "" {
		return events
	}
	out := []core.Event{}
	for _, event := range events {
		if event.Kind == filter {
			out = append(out, event)
		}
	}
	return out
}

// historyKinds lists the distinct event kinds in the window, sorted, for
// the F filter cycle.
func historyKinds(events []core.Event) []string {
	seen := map[string]bool{}
	var kinds []string
	for _, event := range events {
		if !seen[event.Kind] {
			seen[event.Kind] = true
			kinds = append(kinds, event.Kind)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func eventLabel(event core.Event) string {
	label := event.Kind
	if event.Kind == "command_applied" {
		var payload struct {
			Kind  string `json:"command_kind"`
			Actor string `json:"actor"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Kind != "" {
			// Bounded like the detail overlay: list rows must not
			// grow with payload length.
			label = boundedText(payload.Kind, 80) + " · " + boundedText(payload.Actor, 40)
		}
	}
	return label
}

// historyLines renders the navigable, filterable event window with a
// visible cursor. Truncation is announced with the cursor to continue
// from, never silent (6.1-F20).
func historyLines(m *model, s *core.DashboardSnapshot) []string {
	shown := filteredEvents(s.Events, m.historyFilter)
	header := fmt.Sprintf("History · showing %d of %d events", len(shown), len(s.Events))
	if m.historyFilter == "" {
		header += " · filter: all (F cycles kinds)"
	} else {
		header += " · filter: " + clean(m.historyFilter) + " (F cycles, ends at all)"
	}
	lines := []string{header, ""}
	cursor := min(m.historyCursor, max(0, len(shown)-1))
	for index, event := range shown {
		marker := "  "
		if index == cursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%d · %s · %s", marker, event.Sequence, time.UnixMilli(event.At).Format("15:04:05"), clean(eventLabel(event))))
	}
	if len(shown) == 0 {
		if len(s.Events) == 0 {
			lines = append(lines, "No events recorded.")
		} else {
			lines = append(lines, "No events match the filter.")
		}
	}
	lines = append(lines, "")
	if len(s.Events) >= historyWindowCap {
		newest := s.Events[len(s.Events)-1].Sequence
		lines = append(lines, fmt.Sprintf("Event window capped at the latest %d · newest shown %d", historyWindowCap, newest))
		lines = append(lines, fmt.Sprintf("Continue with: vigil project events %s --after %d", clean(s.Readiness.Project.ID), newest))
	} else {
		lines = append(lines, fmt.Sprintf("Complete window (%d events).", len(s.Events)))
	}
	lines = append(lines, "F filter · Enter opens event")
	return lines
}

// eventDetailLines renders the opened event's sanitized payload. Only
// closed, bounded payload shapes are shown field-by-field (command_applied
// and plan_queued); anything else is withheld rather than risking an
// unbounded or credential-bearing render.
func eventDetailLines(m *model, s *core.DashboardSnapshot) []string {
	var found *core.Event
	for i, event := range s.Events {
		if event.Sequence == m.historyOpen {
			found = &s.Events[i]
			break
		}
	}
	if found == nil {
		return []string{"Event detail.", "", "Event no longer in the window.", "", "esc backs out"}
	}
	event := *found
	lines := []string{"Event detail.", "", fmt.Sprintf("%d · %s · %s", event.Sequence, time.UnixMilli(event.At).Format(time.RFC3339), clean(event.Kind))}
	if event.CommandID != "" {
		lines = append(lines, "Command: "+clean(boundedText(event.CommandID, 64)))
	}
	enriched := false
	switch event.Kind {
	case "command_applied":
		var payload struct {
			Kind  string `json:"command_kind"`
			Actor string `json:"actor"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.Kind != "" {
			lines = append(lines, "Applied: "+clean(boundedText(payload.Kind, 80))+" · actor "+clean(boundedText(payload.Actor, 40)))
			enriched = true
		}
	case "plan_queued":
		var payload struct {
			PlanID string `json:"plan_id"`
			Rank   int    `json:"rank"`
		}
		if json.Unmarshal(event.Payload, &payload) == nil && payload.PlanID != "" {
			lines = append(lines, fmt.Sprintf("Queued: %s · rank %d", clean(boundedText(payload.PlanID, 64)), payload.Rank))
			enriched = true
		}
	}
	if !enriched {
		lines = append(lines, "Payload withheld (unbounded or unclassified).")
	}
	lines = append(lines, "", "esc backs out")
	return lines
}
