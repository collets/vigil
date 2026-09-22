package quality

import (
	"context"
	"encoding/json"
	"errors"

	"vigil/internal/core"
	"vigil/internal/store"
)

func RemainingBudgetMS(ctx context.Context, engine *core.Engine, scope Scope) (int64, error) {
	var charged, unknown, limit int64
	var err error
	if scope.Target.Kind == "task" {
		err = engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='task' AND plan_id=? AND task_id=?", scope.Target.PlanID, scope.Target.TaskID).Scan(&charged, &unknown, &limit)
	} else {
		err = engine.DB.SQL.QueryRowContext(ctx, "SELECT charged_ms,unknown_ms,active_limit_ms FROM budget_ledgers WHERE scope='plan_services' AND plan_id=? AND task_id IS NULL", scope.Target.PlanID).Scan(&charged, &unknown, &limit)
	}
	if err != nil {
		return 0, err
	}
	remaining := limit - charged - unknown
	if remaining <= 0 {
		return 0, errors.New("cumulative quality budget exhausted")
	}
	return remaining, nil
}

// AuthorityRevision is the transaction fence for every mutable quality input.
func AuthorityRevision(ctx context.Context, engine *core.Engine) (int64, error) {
	var revision int64
	err := engine.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM quality_authority_v2 WHERE singleton=1").Scan(&revision)
	return revision, err
}

func CheckAuthorityRevision(ctx context.Context, tx *store.Tx, expected int64) error {
	var revision int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM quality_authority_v2 WHERE singleton=1").Scan(&revision); err != nil {
		return err
	}
	if revision != expected {
		return errors.New("quality authority changed before acceptance commit")
	}
	return nil
}

// EnsureTargetDispatchable prevents a new quality effect while any prior
// effect for the same task/plan remains prepared, executing or uncertain.
func EnsureTargetDispatchable(ctx context.Context, tx *store.Tx, target Target) error {
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM quality_effects_v2 e JOIN quality_scopes_v2 s ON s.id=e.scope_id WHERE s.target_kind=? AND s.plan_id=? AND coalesce(s.task_id,'')=? AND e.state IN('prepared','executing','uncertain')`, target.Kind, target.PlanID, target.TaskID).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved != 0 {
		return errors.New("a prior quality effect for this target is unresolved")
	}
	return nil
}

func StartBudgetSegment(ctx context.Context, tx *store.Tx, scope Scope, effectID string, started int64) error {
	query := "SELECT id FROM budget_ledgers WHERE scope='task' AND plan_id=? AND task_id=?"
	args := []any{scope.Target.PlanID, scope.Target.TaskID}
	if scope.Target.Kind == "plan" {
		query = "SELECT id FROM budget_ledgers WHERE scope='plan_services' AND plan_id=? AND task_id IS NULL"
		args = []any{scope.Target.PlanID}
	}
	var ledgerID string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&ledgerID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO quality_budget_segments_v2(effect_id,ledger_id,state,started_at) VALUES(?,?,'active',?)`, effectID, ledgerID, started)
	return err
}

func EffectStartedAt(ctx context.Context, engine *core.Engine, effectID string) (int64, error) {
	var started int64
	err := engine.DB.SQL.QueryRowContext(ctx, "SELECT started_at FROM quality_budget_segments_v2 WHERE effect_id=?", effectID).Scan(&started)
	return started, err
}

func elapsed(started, ended int64) int64 {
	if ended <= started {
		return 1
	}
	return ended - started
}

func WouldExhaust(ctx context.Context, engine *core.Engine, effectID string, ended int64) (bool, error) {
	var started, charged, unknown, limit int64
	err := engine.DB.SQL.QueryRowContext(ctx, `SELECT s.started_at,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM quality_budget_segments_v2 s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.effect_id=? AND s.state='active'`, effectID).Scan(&started, &charged, &unknown, &limit)
	if err != nil {
		return false, err
	}
	return charged+unknown+elapsed(started, ended) >= limit, nil
}

// FinishBudgetSegment charges the whole external-effect interval, including
// preparation/copy/observation/artifact work, exactly once in the terminal
// transaction. Callers must obtain ended immediately before this call while
// holding that transaction; an earlier subprocess/callback timestamp is not
// terminal authority.
func FinishBudgetSegment(ctx context.Context, tx *store.Tx, effectID string, ended int64) (int64, bool, error) {
	var ledgerID, state string
	var started, charged, unknown, limit int64
	if err := tx.QueryRowContext(ctx, `SELECT s.ledger_id,s.state,s.started_at,l.charged_ms,l.unknown_ms,l.active_limit_ms FROM quality_budget_segments_v2 s JOIN budget_ledgers l ON l.id=s.ledger_id WHERE s.effect_id=?`, effectID).Scan(&ledgerID, &state, &started, &charged, &unknown, &limit); err != nil {
		return 0, false, err
	}
	if state != "active" {
		return 0, false, errors.New("quality budget segment is not active")
	}
	duration := elapsed(started, ended)
	exhausted := charged+unknown+duration >= limit
	if _, err := tx.ExecContext(ctx, "UPDATE budget_ledgers SET charged_ms=charged_ms+?,revision=revision+1,updated_at=? WHERE id=?", duration, ended, ledgerID); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE quality_budget_segments_v2 SET state='charged',ended_at=?,charged_ms=? WHERE effect_id=? AND state='active'", ended, duration, effectID); err != nil {
		return 0, false, err
	}
	return duration, exhausted, nil
}

// MarkEffectUncertain conservatively charges an unobserved active interval as
// unknown and leaves both effect and assessment source non-replayable.
func MarkEffectUncertain(ctx context.Context, engine *core.Engine, effectID, reason string) error {
	return engine.DB.Write(ctx, func(tx *store.Tx) error {
		var ledgerID, state string
		var started int64
		if err := tx.QueryRowContext(ctx, "SELECT ledger_id,state,started_at FROM quality_budget_segments_v2 WHERE effect_id=?", effectID).Scan(&ledgerID, &state, &started); err != nil {
			return err
		}
		if state != "active" {
			return nil
		}
		now := store.Now()
		unknown := elapsed(started, now)
		if _, err := tx.ExecContext(ctx, "UPDATE budget_ledgers SET unknown_ms=unknown_ms+?,revision=revision+1,updated_at=? WHERE id=?", unknown, now, ledgerID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE quality_budget_segments_v2 SET state='uncertain',ended_at=?,unknown_ms=? WHERE effect_id=? AND state='active'", now, unknown, effectID); err != nil {
			return err
		}
		observation, _ := json.Marshal(map[string]string{"reason": reason})
		if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='uncertain',observation_json=?,observed_at=? WHERE id=? AND state='executing'", string(observation), now, effectID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE quality_assessment_sources_v2 SET state='uncertain',observed_at=? WHERE effect_id=? AND state='reserved'", now, effectID)
		return err
	})
}
