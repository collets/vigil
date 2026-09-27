package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vigil/internal/store"
)

// QueueReceipt is the authoritative persisted position of one plan. Equal
// ranks are permitted and are resolved by stable plan ID.
type QueueReceipt struct {
	PlanID   string `json:"plan_id"`
	State    string `json:"state"`
	Rank     int    `json:"rank"`
	Revision int    `json:"project_revision"`
	Repeated bool   `json:"repeated,omitempty"`
}

// DispatchDecision is a typed scheduling result. It never launches an effect;
// the selected phase handler must recheck this authority at effect start.
type DispatchDecision struct {
	PlanID   string   `json:"plan_id,omitempty"`
	TaskID   string   `json:"task_id,omitempty"`
	Phase    string   `json:"phase,omitempty"`
	State    string   `json:"state"`
	Blockers []string `json:"blockers"`
	Revision int      `json:"project_revision"`
	Repeated bool     `json:"repeated,omitempty"`
}

type PlanQueueEntry struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Rank     int    `json:"rank"`
	State    string `json:"state"`
}

func (e *Engine) PlanQueue(ctx context.Context) ([]PlanQueueEntry, error) {
	rows, err := e.DB.SQL.QueryContext(ctx, `SELECT id,revision,queue_rank,state FROM plans ORDER BY
		CASE state WHEN 'active' THEN 0 WHEN 'blocked' THEN 0 WHEN 'paused' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,
		queue_rank,id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanQueueEntry{}
	for rows.Next() {
		var item PlanQueueEntry
		if err := rows.Scan(&item.ID, &item.Revision, &item.Rank, &item.State); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (e *Engine) QueuePlan(ctx context.Context, commandID string, expectedRevision int, planID string, rank int) (QueueReceipt, error) {
	var result QueueReceipt
	if e == nil || e.DB == nil || !store.SafeID(commandID) || !store.SafeID(planID) || expectedRevision < 1 || rank < 0 {
		return result, errors.New("valid command, plan, revision and nonnegative rank required")
	}
	args, _ := json.Marshal(map[string]any{"project_id": e.ProjectID, "plan_id": planID, "rank": rank, "expected_revision": expectedRevision})
	command := store.Command{ID: commandID, Actor: string(Human), Kind: "plan.queue", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &result)
			result.Repeated = true
		}
		return result, err
	}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		var projectState string
		if err := tx.QueryRowContext(ctx, "SELECT revision,state FROM project WHERE id=?", e.ProjectID).Scan(&revision, &projectState); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", expectedRevision, revision)
		}
		if projectState == "recovering" || projectState == "quarantined" {
			return nil, errors.New("recovery or quarantine prevents queue mutation")
		}
		var state string
		var accepted sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT p.state,r.accepted_at FROM plans p JOIN plan_revisions r ON r.plan_id=p.id AND r.revision=p.revision WHERE p.id=?`, planID).Scan(&state, &accepted); err != nil {
			return nil, err
		}
		if !accepted.Valid {
			return nil, errors.New("exact accepted plan revision required before queueing")
		}
		if state != "draft" && state != "ready" && state != "queued" {
			return nil, errors.New("active, terminal, or paused plan cannot be requeued")
		}
		var tasks int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE plan_id=?", planID).Scan(&tasks); err != nil {
			return nil, err
		}
		if tasks == 0 {
			return nil, errors.New("plan has no tasks")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='queued',queue_rank=? WHERE id=?", rank, planID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(map[string]any{"plan_id": planID, "rank": rank, "automatic_advance": false})
		if _, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,command_id,kind,occurred_at,payload_json) VALUES(1,?,'plan_queued',?,?)", commandID, store.Now(), string(payload)); err != nil {
			return nil, err
		}
		return QueueReceipt{PlanID: planID, State: "queued", Rank: rank, Revision: revision + 1}, nil
	})
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(receipt, &result)
	return result, err
}

func (e *Engine) Advance(ctx context.Context, commandID string, expectedRevision int) (DispatchDecision, error) {
	var result DispatchDecision
	if e == nil || e.DB == nil || !store.SafeID(commandID) || expectedRevision < 1 {
		return result, errors.New("valid command and expected revision required")
	}
	args, _ := json.Marshal(map[string]any{"project_id": e.ProjectID, "expected_revision": expectedRevision})
	command := store.Command{ID: commandID, Actor: string(Human), Kind: "workflow.advance", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		if err == nil {
			err = json.Unmarshal(receipt, &result)
			result.Repeated = true
		}
		return result, err
	}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		var projectState string
		if err := tx.QueryRowContext(ctx, "SELECT revision,state FROM project WHERE id=?", e.ProjectID).Scan(&revision, &projectState); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", expectedRevision, revision)
		}
		if projectState != "ready" {
			return nil, errors.New("project must be explicitly continued before scheduling")
		}
		readiness, err := e.readiness(ctx, tx)
		if err != nil {
			return nil, err
		}
		if readiness.Project.Revision != revision {
			return nil, errors.New("project readiness revision changed during scheduling")
		}
		eligible := map[string]bool{}
		issues := map[string][]string{}
		for _, task := range readiness.Tasks {
			eligible[task.ID] = len(task.Issues) == 0
			issues[task.ID] = append([]string(nil), task.Issues...)
		}
		var automatic int
		if err := tx.QueryRowContext(ctx, "SELECT automatic_plan_advance FROM workflow_controls WHERE singleton=1").Scan(&automatic); err != nil {
			return nil, err
		}
		if automatic != 0 {
			return nil, errors.New("automatic plan advancement is unsupported")
		}
		var planID, state string
		err = tx.QueryRowContext(ctx, `SELECT id,state FROM plans WHERE state IN('active','blocked') ORDER BY queue_rank,id LIMIT 1`).Scan(&planID, &state)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `SELECT id,state FROM plans WHERE state='queued' ORDER BY queue_rank,id LIMIT 1`).Scan(&planID, &state)
			if errors.Is(err, sql.ErrNoRows) {
				return DispatchDecision{State: "idle", Blockers: []string{"no queued plan"}, Revision: revision}, nil
			}
			if err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE id=? AND state='queued'", planID); err != nil {
				return nil, err
			}
			state = "active"
		} else if err != nil {
			return nil, err
		}
		var unsafe int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN tasks t ON t.id=r.task_id WHERE t.plan_id=? AND r.state IN('prepared','starting','active','waiting_input','stopping')`, planID).Scan(&unsafe); err != nil {
			return nil, err
		}
		if unsafe != 0 {
			return DispatchDecision{PlanID: planID, State: "waiting", Blockers: []string{"an execution context is already active"}, Revision: revision}, nil
		}
		var staleDispatchID, stalePlanID, staleTaskID string
		var staleRevision int
		err = tx.QueryRowContext(ctx, `SELECT id,plan_id,task_id,project_revision FROM workflow_dispatches WHERE state='selected' AND project_revision!=?`, revision).Scan(&staleDispatchID, &stalePlanID, &staleTaskID, &staleRevision)
		if err == nil {
			updated, updateErr := tx.ExecContext(ctx, `UPDATE workflow_dispatches SET state='retired' WHERE id=? AND state='selected' AND project_revision=?`, staleDispatchID, staleRevision)
			if updateErr != nil {
				return nil, updateErr
			}
			if count, countErr := updated.RowsAffected(); countErr != nil || count != 1 {
				return nil, errors.New("stale selected dispatch changed while retiring")
			}
			payload, _ := json.Marshal(map[string]any{"dispatch_id": staleDispatchID, "plan_id": stalePlanID, "task_id": staleTaskID, "selected_project_revision": staleRevision, "current_project_revision": revision, "reason": "project_revision_changed"})
			if _, eventErr := tx.ExecContext(ctx, "INSERT INTO events(schema_version,command_id,kind,occurred_at,payload_json) VALUES(1,?,'dispatch_retired',?,?)", commandID, store.Now(), string(payload)); eventErr != nil {
				return nil, eventErr
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workflow_dispatches WHERE state='selected'`).Scan(&unsafe); err != nil {
			return nil, err
		}
		if unsafe != 0 {
			return DispatchDecision{PlanID: planID, State: "waiting", Blockers: []string{"a selected dispatch has not been consumed"}, Revision: revision}, nil
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN tasks t ON t.id=r.task_id WHERE t.plan_id=? AND t.state='blocked' AND r.writer_state!='contained_stopped'`, planID).Scan(&unsafe); err != nil {
			return nil, err
		}
		if unsafe != 0 {
			return DispatchDecision{PlanID: planID, State: "blocked", Blockers: []string{"blocked work has unresolved writer or checkout preservation"}, Revision: revision}, nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT t.id,t.rank FROM tasks t WHERE t.plan_id=? AND t.state IN('draft','ready')
		 AND NOT EXISTS(SELECT 1 FROM task_dependencies d JOIN tasks dep ON dep.id=d.dependency_id WHERE d.task_id=t.id AND dep.state!='accepted')
		 AND NOT EXISTS(SELECT 1 FROM quality_effects_v2 q JOIN quality_scopes_v2 s ON s.id=q.scope_id WHERE s.task_id=t.id AND q.state IN('executing','uncertain'))
		 AND NOT EXISTS(SELECT 1 FROM budget_ledgers b WHERE b.scope='task' AND b.task_id=t.id AND b.charged_ms+b.unknown_ms>=b.active_limit_ms)
		 ORDER BY t.rank,t.id`, planID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		blockers := []string{}
		for rows.Next() {
			var id string
			var rank int
			if err := rows.Scan(&id, &rank); err != nil {
				return nil, err
			}
			if eligible[id] {
				decision := DispatchDecision{PlanID: planID, TaskID: id, Phase: "implementation", State: "selected", Blockers: []string{}, Revision: revision}
				if _, err := tx.ExecContext(ctx, `INSERT INTO workflow_dispatches(id,selection_command_id,plan_id,task_id,phase,project_revision,state,selected_at) VALUES(?,?,?,?,?,?, 'selected',?)`, store.ID(), commandID, planID, id, decision.Phase, revision, store.Now()); err != nil {
					return nil, err
				}
				payload, _ := json.Marshal(decision)
				if _, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,command_id,kind,occurred_at,payload_json) VALUES(1,?,'dispatch_selected',?,?)", commandID, store.Now(), string(payload)); err != nil {
					return nil, err
				}
				return decision, nil
			}
			for _, issue := range issues[id] {
				blockers = append(blockers, id+": "+issue)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(blockers) == 0 {
			blockers = append(blockers, "no dependency-ready eligible task")
		}
		return DispatchDecision{PlanID: planID, State: "blocked", Blockers: blockers, Revision: revision}, nil
	})
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(receipt, &result)
	return result, err
}

type stateReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// EnsureDispatchAllowed is the shared fail-closed gate for implementation,
// check, review, supervisor and finalization effect preparation.
func EnsureDispatchAllowed(ctx context.Context, reader stateReader, projectID string) error {
	var state string
	if err := reader.QueryRowContext(ctx, "SELECT state FROM project WHERE id=?", projectID).Scan(&state); err != nil {
		return err
	}
	if state != "ready" {
		return fmt.Errorf("project state %s prevents new dispatch", state)
	}
	return nil
}
