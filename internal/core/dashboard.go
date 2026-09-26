package core

import (
	"context"
	"database/sql"
)

type DashboardSnapshot struct {
	Readiness Readiness        `json:"readiness"`
	Queue     []PlanQueueEntry `json:"queue"`
	Tasks     []TaskDetail     `json:"task_details"`
	Plans     []PlanDetail     `json:"plan_details"`
	Inbox     []InboxEntry     `json:"inbox"`
	Events    []Event          `json:"events"`
}

type TaskDetail struct {
	ID                string   `json:"id"`
	PlanID            string   `json:"plan_id"`
	State             string   `json:"state"`
	BlockReason       string   `json:"block_reason,omitempty"`
	BudgetRemainingMS int64    `json:"budget_remaining_ms"`
	CheckOutputs      []string `json:"check_outputs"`
	BlockingFindings  int      `json:"blocking_findings"`
	Suggestions       int      `json:"suggestions"`
	ManualOutcomes    []string `json:"manual_outcomes"`
	RecoveryState     string   `json:"recovery_state,omitempty"`
	BaselineUnhealthy bool     `json:"baseline_unhealthy"`
}
type PlanDetail struct {
	ID                       string `json:"id"`
	State                    string `json:"state"`
	Rank                     int    `json:"rank"`
	ServiceBudgetRemainingMS int64  `json:"service_budget_remaining_ms"`
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
	if snapshot.Queue, err = readPlanQueue(ctx, tx); err != nil {
		return snapshot, err
	}
	if snapshot.Tasks, snapshot.Plans, err = readDashboardDetails(ctx, tx); err != nil {
		return snapshot, err
	}
	if snapshot.Inbox, err = readInbox(ctx, tx); err != nil {
		return snapshot, err
	}
	snapshot.Events, err = readEvents(ctx, tx, 0, true)
	return snapshot, err
}

func readPlanQueue(ctx context.Context, tx *sql.Tx) ([]PlanQueueEntry, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,revision,queue_rank,state FROM plans ORDER BY CASE state WHEN 'active' THEN 0 WHEN 'blocked' THEN 0 WHEN 'paused' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,queue_rank,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanQueueEntry{}
	for rows.Next() {
		var v PlanQueueEntry
		if err := rows.Scan(&v.ID, &v.Revision, &v.Rank, &v.State); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func readDashboardDetails(ctx context.Context, tx *sql.Tx) ([]TaskDetail, []PlanDetail, error) {
	plans := []PlanDetail{}
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.state,p.queue_rank,coalesce(b.active_limit_ms-b.charged_ms-b.unknown_ms,p.service_limit_ms) FROM plans p LEFT JOIN budget_ledgers b ON b.scope='plan_services' AND b.plan_id=p.id AND b.task_id IS NULL ORDER BY p.queue_rank,p.id LIMIT 100`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var v PlanDetail
		if err := rows.Scan(&v.ID, &v.State, &v.Rank, &v.ServiceBudgetRemainingMS); err != nil {
			rows.Close()
			return nil, nil, err
		}
		plans = append(plans, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	tasks := []TaskDetail{}
	rows, err = tx.QueryContext(ctx, `SELECT t.id,t.plan_id,t.state,coalesce(t.block_reason,''),coalesce(b.active_limit_ms-b.charged_ms-b.unknown_ms,t.active_limit_ms) FROM tasks t LEFT JOIN budget_ledgers b ON b.scope='task' AND b.task_id=t.id ORDER BY t.plan_id,t.rank,t.id LIMIT 100`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var v TaskDetail
		if err := rows.Scan(&v.ID, &v.PlanID, &v.State, &v.BlockReason, &v.BudgetRemainingMS); err != nil {
			rows.Close()
			return nil, nil, err
		}
		v.CheckOutputs = []string{}
		v.ManualOutcomes = []string{}
		tasks = append(tasks, v)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	for i := range tasks {
		t := &tasks[i]
		checkRows, qerr := tx.QueryContext(ctx, `SELECT c.check_id||':'||c.status||':'||c.output_artifact_id FROM check_results_v2 c JOIN quality_scopes_v2 s ON s.id=c.scope_id WHERE s.task_id=? ORDER BY c.ended_at DESC LIMIT 20`, t.ID)
		if qerr != nil {
			return nil, nil, qerr
		}
		for checkRows.Next() {
			var v string
			if err := checkRows.Scan(&v); err != nil {
				checkRows.Close()
				return nil, nil, err
			}
			t.CheckOutputs = append(t.CheckOutputs, v)
		}
		if err := checkRows.Err(); err != nil {
			checkRows.Close()
			return nil, nil, err
		}
		checkRows.Close()
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(CASE WHEN f.blocking=1 AND f.resolution='open' THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN f.blocking=0 AND f.resolution='open' THEN 1 ELSE 0 END),0) FROM quality_findings_v2 f JOIN review_results_v2 r ON r.id=f.review_id JOIN quality_scopes_v2 s ON s.id=r.scope_id WHERE s.task_id=?`, t.ID).Scan(&t.BlockingFindings, &t.Suggestions); err != nil {
			return nil, nil, err
		}
		manualRows, qerr := tx.QueryContext(ctx, `SELECT m.criterion_id||':'||m.state FROM manual_results_v2 m JOIN quality_scopes_v2 s ON s.id=m.scope_id WHERE s.task_id=? ORDER BY m.evaluated_at DESC LIMIT 20`, t.ID)
		if qerr != nil {
			return nil, nil, qerr
		}
		for manualRows.Next() {
			var v string
			if err := manualRows.Scan(&v); err != nil {
				manualRows.Close()
				return nil, nil, err
			}
			t.ManualOutcomes = append(t.ManualOutcomes, v)
		}
		if err := manualRows.Err(); err != nil {
			manualRows.Close()
			return nil, nil, err
		}
		manualRows.Close()
		var baselines int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM baseline_exceptions_v2`).Scan(&baselines); err == nil {
			t.BaselineUnhealthy = baselines > 0
		}
		_ = tx.QueryRowContext(ctx, `SELECT state FROM checkpoint_sets c JOIN runs r ON r.id=c.run_id WHERE r.task_id=? AND c.state IN('capturing','clearing','restoring','conflicted','incomplete') ORDER BY c.created_at DESC LIMIT 1`, t.ID).Scan(&t.RecoveryState)
	}
	return tasks, plans, nil
}
