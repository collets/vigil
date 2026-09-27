package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vigil/internal/core"
	"vigil/internal/store"
)

var ErrBudgetExhausted = errors.New("cumulative execution budget exhausted")

var processMonotonicOrigin = time.Now()

func monotonicNow() int64 { return time.Since(processMonotonicOrigin).Nanoseconds() }

func excludedBudgetCategory(category string) bool {
	return category == "human_wait" || category == "resource_wait"
}

func (r *Runner) startSegment(ctx context.Context, prepared PreparedRun) error {
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var ledgerID string
		var charged, unknown, limit, runUsed int64
		if err := tx.QueryRowContext(ctx, "SELECT id,charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&ledgerID, &charged, &unknown, &limit); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_ms+unknown_ms),0) FROM active_segments WHERE run_id=? AND ended_at IS NOT NULL", prepared.RunID).Scan(&runUsed); err != nil {
			return err
		}
		if charged+unknown >= limit || runUsed >= prepared.ActiveLimitMS {
			return ErrBudgetExhausted
		}
		now, mono := store.Now(), monotonicNow()
		_, err := tx.ExecContext(ctx, `INSERT INTO active_segments(id,run_id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,wall_started_at,wall_checkpoint_at) VALUES(?,?,?,'active',?,?,?,?)`, store.ID(), prepared.RunID, ledgerID, mono, mono, now, now)
		return err
	})
}

func (r *Runner) closeSegment(ctx context.Context, prepared PreparedRun) error {
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var id, ledger, category string
		var started, checkpoint, priorCharged int64
		err := tx.QueryRowContext(ctx, "SELECT id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,charged_ms FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&id, &ledger, &category, &started, &checkpoint, &priorCharged)
		if err != nil {
			return err
		}
		nowMono, nowWall := monotonicNow(), store.Now()
		if nowMono < checkpoint || checkpoint < started {
			return errors.New("monotonic budget clock regressed")
		}
		charged := (nowMono - started) / int64(time.Millisecond)
		if charged < priorCharged {
			charged = priorCharged
		}
		if category == "human_wait" || category == "resource_wait" {
			charged = 0
		}
		if _, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,ended_at=?,charged_ms=? WHERE id=?", nowMono, nowWall, nowWall, charged, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE budget_ledgers SET charged_ms=charged_ms+?,revision=revision+1,updated_at=? WHERE id=?", charged, nowWall, ledger)
		return err
	})
}

func (r *Runner) checkpointSegment(ctx context.Context, prepared PreparedRun) error {
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var id, ledger, category string
		var started, priorCharged, ledgerCharged, ledgerUnknown, limit, runClosed int64
		if err := tx.QueryRowContext(ctx, `SELECT s.id,s.ledger_id,s.category,s.monotonic_started_ns,s.charged_ms,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM active_segments s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.run_id=? AND s.ended_at IS NULL`, prepared.RunID).Scan(&id, &ledger, &category, &started, &priorCharged, &ledgerCharged, &ledgerUnknown, &limit); err != nil {
			return err
		}
		nowMono, nowWall := monotonicNow(), store.Now()
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_ms+unknown_ms),0) FROM active_segments WHERE run_id=? AND ended_at IS NOT NULL", prepared.RunID).Scan(&runClosed); err != nil {
			return err
		}
		if nowMono < started {
			return errors.New("monotonic budget clock regressed")
		}
		if excludedBudgetCategory(category) {
			_, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,charged_ms=0 WHERE id=?", nowMono, nowWall, id)
			return err
		}
		charged := (nowMono - started) / int64(time.Millisecond)
		if charged < priorCharged {
			charged = priorCharged
		}
		if ledgerCharged+ledgerUnknown+charged >= limit || runClosed+charged >= prepared.ActiveLimitMS {
			return ErrBudgetExhausted
		}
		_, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,charged_ms=? WHERE id=?", nowMono, nowWall, charged, id)
		_ = ledger
		return err
	})
}

func (r *Runner) activeBudgetStatus(ctx context.Context, prepared PreparedRun) (time.Duration, bool, error) {
	var category string
	var started, priorCharged, ledgerCharged, ledgerUnknown, limit, runClosed int64
	err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT s.category,s.monotonic_started_ns,s.charged_ms,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM active_segments s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.run_id=? AND s.ended_at IS NULL`, prepared.RunID).Scan(&category, &started, &priorCharged, &ledgerCharged, &ledgerUnknown, &limit)
	if err != nil {
		return 0, false, err
	}
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(sum(charged_ms+unknown_ms),0) FROM active_segments WHERE run_id=? AND ended_at IS NOT NULL", prepared.RunID).Scan(&runClosed); err != nil {
		return 0, false, err
	}
	current := int64(0)
	if !excludedBudgetCategory(category) {
		now := monotonicNow()
		if now < started {
			return 0, false, errors.New("monotonic budget clock regressed")
		}
		current = (now - started) / int64(time.Millisecond)
		if current < priorCharged {
			current = priorCharged
		}
	}
	ledgerRemaining := limit - ledgerCharged - ledgerUnknown - current
	runRemaining := prepared.ActiveLimitMS - runClosed - current
	remaining := ledgerRemaining
	if runRemaining < remaining {
		remaining = runRemaining
	}
	if remaining <= 0 {
		return 0, false, ErrBudgetExhausted
	}
	return time.Duration(remaining) * time.Millisecond, excludedBudgetCategory(category), nil
}

func (r *Runner) remainingActive(ctx context.Context, prepared PreparedRun) (time.Duration, error) {
	remaining, _, err := r.activeBudgetStatus(ctx, prepared)
	return remaining, err
}

func (r *Runner) activeCallContext(ctx context.Context, prepared PreparedRun) (context.Context, context.CancelFunc, error) {
	if err := r.checkpointSegment(ctx, prepared); err != nil {
		return nil, nil, err
	}
	remaining, excluded, err := r.activeBudgetStatus(ctx, prepared)
	if err != nil {
		return nil, nil, err
	}
	if excluded {
		return nil, nil, errors.New("active driver call cannot start during an excluded wait")
	}
	callCtx, cancel := context.WithTimeout(ctx, remaining)
	return callCtx, cancel, nil
}

// ReconcileOpenSegment charges a conservative wall-clock bound for a segment
// whose process-local monotonic clock was lost. Without proven termination the
// gap is retained as unknown and still reduces remaining allowance.
func (r *Runner) ReconcileOpenSegment(ctx context.Context, prepared PreparedRun, provenTerminationAt int64) error {
	now := store.Now()
	if provenTerminationAt < 0 || provenTerminationAt > now {
		return errors.New("invalid termination observation")
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var id, ledger, category string
		var wallStarted, wallCheckpoint, priorCharged int64
		err := tx.QueryRowContext(ctx, "SELECT id,ledger_id,category,wall_started_at,wall_checkpoint_at,charged_ms FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&id, &ledger, &category, &wallStarted, &wallCheckpoint, &priorCharged)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		end := provenTerminationAt
		unknown := int64(0)
		if end == 0 {
			end = now
			unknown = end - wallCheckpoint
		}
		if end < wallCheckpoint {
			end = wallCheckpoint
		}
		if excludedBudgetCategory(category) {
			if _, err := tx.ExecContext(ctx, "UPDATE active_segments SET ended_at=?,wall_checkpoint_at=?,charged_ms=0,unknown_ms=0 WHERE id=?", end, end, id); err != nil {
				return err
			}
			return nil
		}
		conservative := end - wallStarted
		if conservative < priorCharged {
			conservative = priorCharged
		}
		charged := conservative
		if unknown > 0 {
			charged = wallCheckpoint - wallStarted
			if charged < priorCharged {
				charged = priorCharged
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE active_segments SET ended_at=?,wall_checkpoint_at=?,charged_ms=?,unknown_ms=? WHERE id=?", end, end, charged, unknown, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE budget_ledgers SET charged_ms=charged_ms+?,unknown_ms=unknown_ms+?,revision=revision+1,updated_at=? WHERE id=?", charged, unknown, now, ledger)
		return err
	})
}

// BeginHumanWait excludes time only after a trusted native waiting proof.
func (r *Runner) BeginHumanWait(ctx context.Context, prepared PreparedRun, nativeWaitingProof bool) error {
	if !nativeWaitingProof {
		return errors.New("human wait exclusion requires native waiting proof")
	}
	return r.transitionSegment(ctx, prepared, "active", "human_wait")
}

func (r *Runner) EndHumanWait(ctx context.Context, prepared PreparedRun) error {
	var category string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT category FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&category); err != nil {
		return err
	}
	if category != "human_wait" {
		return errors.New("run is not in a proven human wait")
	}
	return r.transitionSegment(ctx, prepared, "human_wait", "active")
}

func (r *Runner) transitionSegment(ctx context.Context, prepared PreparedRun, from, to string) error {
	err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var id, ledger, category string
		var started, checkpoint, priorCharged, ledgerCharged, ledgerUnknown, limit, runClosed int64
		if err := tx.QueryRowContext(ctx, `SELECT s.id,s.ledger_id,s.category,s.monotonic_started_ns,s.monotonic_checkpoint_ns,s.charged_ms,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM active_segments s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.run_id=? AND s.ended_at IS NULL`, prepared.RunID).Scan(&id, &ledger, &category, &started, &checkpoint, &priorCharged, &ledgerCharged, &ledgerUnknown, &limit); err != nil {
			return err
		}
		if category != from {
			return fmt.Errorf("budget segment is %s, expected %s", category, from)
		}
		nowMono, nowWall := monotonicNow(), store.Now()
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_ms+unknown_ms),0) FROM active_segments WHERE run_id=? AND ended_at IS NOT NULL", prepared.RunID).Scan(&runClosed); err != nil {
			return err
		}
		if nowMono < checkpoint || checkpoint < started {
			return errors.New("monotonic budget clock regressed")
		}
		charged := int64(0)
		if !excludedBudgetCategory(category) {
			charged = (nowMono - started) / int64(time.Millisecond)
			if charged < priorCharged {
				charged = priorCharged
			}
		}
		if to == "active" && (ledgerCharged+ledgerUnknown+charged >= limit || runClosed+charged >= prepared.ActiveLimitMS) {
			return ErrBudgetExhausted
		}
		if _, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,ended_at=?,charged_ms=? WHERE id=?", nowMono, nowWall, nowWall, charged, id); err != nil {
			return err
		}
		if charged > 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE budget_ledgers SET charged_ms=charged_ms+?,revision=revision+1,updated_at=? WHERE id=?", charged, nowWall, ledger); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO active_segments(id,run_id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,wall_started_at,wall_checkpoint_at) VALUES(?,?,?,?,?,?,?,?)`, store.ID(), prepared.RunID, ledger, to, nowMono, nowMono, nowWall, nowWall)
		return err
	})
	if err == nil {
		r.signalBudgetChange()
	}
	return err
}

func (r *Runner) budgetChangeChannel() <-chan struct{} {
	r.budgetMu.Lock()
	defer r.budgetMu.Unlock()
	if r.budgetChanged == nil {
		r.budgetChanged = make(chan struct{})
	}
	return r.budgetChanged
}

func (r *Runner) signalBudgetChange() {
	r.budgetMu.Lock()
	defer r.budgetMu.Unlock()
	if r.budgetChanged == nil {
		r.budgetChanged = make(chan struct{})
		return
	}
	close(r.budgetChanged)
	r.budgetChanged = make(chan struct{})
}

// enforceCompletionBudget runs inside the outcome transaction. It uses the
// persisted ledger/run limits and includes live active time since the last
// checkpoint, so neither reconciliation nor late persistence can turn an
// exhausted attempt into successful task progression.
func (r *Runner) enforceCompletionBudget(ctx context.Context, tx *store.Tx, prepared PreparedRun) error {
	var charged, unknown, ledgerLimit, runLimit, runClosed int64
	if err := tx.QueryRowContext(ctx, `SELECT l.charged_ms,l.unknown_ms,l.active_limit_ms,r.active_limit_ms FROM budget_ledgers l JOIN runs r ON r.task_id=l.task_id WHERE l.scope='task' AND r.id=? AND r.task_id=?`, prepared.RunID, prepared.TaskID).Scan(&charged, &unknown, &ledgerLimit, &runLimit); err != nil {
		return err
	}
	current := int64(0)
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_ms+unknown_ms),0) FROM active_segments WHERE run_id=? AND ended_at IS NOT NULL", prepared.RunID).Scan(&runClosed); err != nil {
		return err
	}
	var category string
	var started, priorCharged int64
	err := tx.QueryRowContext(ctx, "SELECT category,monotonic_started_ns,charged_ms FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&category, &started, &priorCharged)
	if err == nil && !excludedBudgetCategory(category) {
		now := monotonicNow()
		if now < started {
			return errors.New("monotonic budget clock regressed")
		}
		current = (now - started) / int64(time.Millisecond)
		if current < priorCharged {
			current = priorCharged
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	total := charged + unknown + current
	if total >= ledgerLimit || runClosed+current >= runLimit {
		return ErrBudgetExhausted
	}
	return nil
}

func persistBudgetExhaustion(ctx context.Context, engine *core.Engine, prepared PreparedRun, stage string) error {
	if engine == nil || engine.DB == nil || stage == "" || len(stage) > 128 {
		return errors.New("invalid budget exhaustion evidence")
	}
	return engine.DB.Write(ctx, func(tx *store.Tx) error {
		var ledgerID string
		var charged, unknown, limit int64
		if err := tx.QueryRowContext(ctx, "SELECT id,charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&ledgerID, &charged, &unknown, &limit); err != nil {
			return err
		}
		id := store.Digest([]byte(prepared.RunID + "\x00" + stage))
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_exhaustions(id,ledger_id,run_id,task_id,stage,charged_ms,unknown_ms,limit_ms,state,created_at) VALUES(?,?,?,?,?,?,?,?,'pending_supervisor',?) ON CONFLICT(run_id,stage) DO NOTHING`, id, ledgerID, prepared.RunID, prepared.TaskID, stage, charged, unknown, limit, store.Now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='blocked',block_reason='cumulative execution budget exhausted; bounded supervisor assessment pending' WHERE id=? AND state!='accepted'", prepared.TaskID); err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM requests WHERE task_id=? AND kind='recovery' AND state='pending'", prepared.TaskID).Scan(&existing); err != nil {
			return err
		}
		if existing == 0 {
			contextJSON, _ := json.Marshal(map[string]any{"reason": "budget_exhausted", "stage": stage, "charged_ms": charged, "unknown_ms": unknown, "limit_ms": limit})
			now := store.Now()
			deadline := now + int64((30*time.Minute)/time.Millisecond)
			if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,run_id,context_json,blocking,created_at,deadline) SELECT ?,'recovery','pending',plan_id,id,revision,? ,?,1,?,? FROM tasks WHERE id=?`, store.ID(), prepared.RunID, string(contextJSON), now, deadline, prepared.TaskID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE project SET state='paused' WHERE id=?", engine.ProjectID)
		return err
	})
}
