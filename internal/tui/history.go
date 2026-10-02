package tui

import (
	"encoding/json"
	"fmt"
	"time"

	"vigil/internal/core"
)

// historyLines renders the event window with a visible cursor: every screen
// has exactly one focus, and History's is the selected event. Detail,
// filter and the truncation notice are 6.3's; the cursor is 6.2's focus
// model, not 6.3's feature.
func historyLines(m *model, s *core.DashboardSnapshot) []string {
	lines := []string{"Latest 100 persisted events (oldest first).", ""}
	for index, event := range s.Events {
		label := event.Kind
		if event.Kind == "command_applied" {
			var payload struct {
				Kind  string `json:"command_kind"`
				Actor string `json:"actor"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.Kind != "" {
				label = payload.Kind + " · " + payload.Actor
			}
		}
		marker := "  "
		if index == m.historyCursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%d · %s · %s", marker, event.Sequence, time.UnixMilli(event.At).Format("15:04:05"), clean(label)))
	}
	if len(s.Events) == 0 {
		lines = append(lines, "No events recorded.")
	}
	return lines
}
