package core

import (
	"context"
	"database/sql"
)

type DashboardSnapshot struct {
	Readiness Readiness    `json:"readiness"`
	Inbox     []InboxEntry `json:"inbox"`
	Events    []Event      `json:"events"`
}

// Dashboard uses one SQLite snapshot so readiness, pending decisions and recent
// history cannot describe different committed project revisions.
func (e *Engine) Dashboard(ctx context.Context) (DashboardSnapshot, error) {
	var snapshot DashboardSnapshot
	tx, err := e.DB.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	if snapshot.Readiness, err = e.readiness(ctx, tx); err != nil {
		return snapshot, err
	}
	if snapshot.Inbox, err = readInbox(ctx, tx); err != nil {
		return snapshot, err
	}
	snapshot.Events, err = readEvents(ctx, tx, 0, true)
	return snapshot, err
}
