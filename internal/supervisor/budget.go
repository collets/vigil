package supervisor

import (
	"context"
	"database/sql"
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
		var id, ledger string
		var started, priorCharged, ledgerCharged, ledgerUnknown, limit int64
		if err := tx.QueryRowContext(ctx, `SELECT s.id,s.ledger_id,s.monotonic_started_ns,s.charged_ms,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM active_segments s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.run_id=? AND s.ended_at IS NULL`, prepared.RunID).Scan(&id, &ledger, &started, &priorCharged, &ledgerCharged, &ledgerUnknown, &limit); err != nil {
			return err
		}
		nowMono, nowWall := monotonicNow(), store.Now()
		if nowMono < started {
			return errors.New("monotonic budget clock regressed")
		}
		charged := (nowMono - started) / int64(time.Millisecond)
		if charged < priorCharged {
			charged = priorCharged
		}
		if ledgerCharged+ledgerUnknown+charged >= limit || charged >= prepared.ActiveLimitMS {
			return errors.New("active execution budget exhausted")
		}
		_, err := tx.ExecContext(ctx, "UPDATE active_segments SET monotonic_checkpoint_ns=?,wall_checkpoint_at=?,charged_ms=? WHERE id=?", nowMono, nowWall, charged, id)
		_ = ledger
		return err
	})
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
		var id, ledger string
		var wallStarted, wallCheckpoint, priorCharged int64
		err := tx.QueryRowContext(ctx, "SELECT id,ledger_id,wall_started_at,wall_checkpoint_at,charged_ms FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&id, &ledger, &wallStarted, &wallCheckpoint, &priorCharged)
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
	if err := r.closeSegment(ctx, prepared); err != nil {
		return err
	}
	return r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		var ledger string
		if err := tx.QueryRowContext(ctx, "SELECT id FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&ledger); err != nil {
			return err
		}
		now, mono := store.Now(), monotonicNow()
		_, err := tx.ExecContext(ctx, `INSERT INTO active_segments(id,run_id,ledger_id,category,monotonic_started_ns,monotonic_checkpoint_ns,wall_started_at,wall_checkpoint_at) VALUES(?,?,?,'human_wait',?,?,?,?)`, store.ID(), prepared.RunID, ledger, mono, mono, now, now)
		return err
	})
}

func (r *Runner) EndHumanWait(ctx context.Context, prepared PreparedRun) error {
	var category string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT category FROM active_segments WHERE run_id=? AND ended_at IS NULL", prepared.RunID).Scan(&category); err != nil {
		return err
	}
	if category != "human_wait" {
		return errors.New("run is not in a proven human wait")
	}
	if err := r.closeSegment(ctx, prepared); err != nil {
		return err
	}
	return r.startSegment(ctx, prepared)
}
