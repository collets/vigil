package supervisor

import (
	"context"
	"errors"
	"time"

	"vigil/internal/store"
)

var processMonotonicOrigin = time.Now()

func monotonicNow() int64 { return time.Since(processMonotonicOrigin).Nanoseconds() }

func (r *Runner) startSegment(ctx context.Context, prepared PreparedRun) error {
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var ledgerID string
		var charged, unknown, limit int64
		if err := tx.QueryRowContext(ctx, "SELECT id,charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&ledgerID, &charged, &unknown, &limit); err != nil {
			return err
		}
		if charged+unknown >= limit || charged+unknown >= prepared.ActiveLimitMS {
			return errors.New("execution budget exhausted")
		}
		now, mono := store.Now(), monotonicNow()
		_, err := tx.ExecContext(ctx, `INSERT INTO active_segments(id,run_id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,wall_started_at,wall_checkpoint_at) VALUES(?,?,?,'active',?,?,?,?)`, store.ID(), prepared.RunID, ledgerID, mono, mono, now, now)
		return err
	})
}

func (r *Runner) closeSegment(ctx context.Context, prepared PreparedRun) error {
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var id, ledger string
		var started, checkpoint, priorCharged int64
		err := tx.QueryRowContext(ctx, "SELECT id,ledger_id,monotonic_started_ns,monotonic_checkpoint_ns,charged_ms FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&id, &ledger, &started, &checkpoint, &priorCharged)
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
		if _, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,ended_at=?,charged_ms=? WHERE id=?", nowMono, nowWall, nowWall, charged, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE budget_ledgers SET charged_ms=charged_ms+?,revision=revision+1,updated_at=? WHERE id=?", charged, nowWall, ledger)
		return err
	})
}
