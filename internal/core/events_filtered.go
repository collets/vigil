package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var errNegativeCursor = errors.New("negative event cursor")
var errNegativeLimit = errors.New("negative event limit")

// EventsFiltered reads persisted events after a sequence cursor with an
// optional exact kind filter. Zero limit means the default window of 100;
// larger limits clamp to 100; negative limits are rejected like negative
// cursors. It returns the window in ascending sequence order, whether the
// window truncated, and the cursor to continue from (the last returned
// sequence, or after when empty).
func (e *Engine) EventsFiltered(ctx context.Context, after int64, kind string, limit int) ([]Event, bool, int64, error) {
	return readEventsFiltered(ctx, e.DB.SQL, after, kind, limit)
}

func readEventsFiltered(ctx context.Context, reader queryReader, after int64, kind string, limit int) ([]Event, bool, int64, error) {
	if after < 0 {
		return nil, false, after, errNegativeCursor
	}
	if limit < 0 {
		return nil, false, after, errNegativeLimit
	}
	if limit == 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if kind == "" {
		rows, err = reader.QueryContext(ctx, "SELECT sequence,kind,occurred_at,coalesce(command_id,''),payload_json FROM events WHERE sequence>? ORDER BY sequence LIMIT ?", after, limit+1)
	} else {
		rows, err = reader.QueryContext(ctx, "SELECT sequence,kind,occurred_at,coalesce(command_id,''),payload_json FROM events WHERE sequence>? AND kind=? ORDER BY sequence LIMIT ?", after, kind, limit+1)
	}
	if err != nil {
		return nil, false, after, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		var raw string
		if err := rows.Scan(&event.Sequence, &event.Kind, &event.At, &event.CommandID, &raw); err != nil {
			return nil, false, after, err
		}
		event.Payload = []byte(strings.TrimSpace(raw))
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, false, after, err
	}
	cursor := after
	if len(out) > 0 {
		cursor = out[len(out)-1].Sequence
	}
	if len(out) > limit {
		out = out[:limit]
		return out, true, out[len(out)-1].Sequence, nil
	}
	return out, false, cursor, nil
}
