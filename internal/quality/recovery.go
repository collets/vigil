package quality

import (
	"context"
	"encoding/json"
	"errors"

	"vigil/internal/core"
	"vigil/internal/store"
)

// RecoverUnfinished records the conservative restart interpretation of
// external quality effects. An executing effect has an unknown outcome and is
// made uncertain; it is never reset to prepared or automatically replayed.
func RecoverUnfinished(ctx context.Context, engine *core.Engine) ([]string, error) {
	var ids []string
	err := engine.DB.Write(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT e.id,s.ledger_id,s.started_at FROM quality_effects_v2 e JOIN quality_budget_segments_v2 s ON s.effect_id=e.id AND s.state='active' WHERE e.state='executing' ORDER BY e.prepared_at,e.id`)
		if err != nil {
			return err
		}
		type unfinished struct {
			id, ledgerID string
			started      int64
		}
		var effects []unfinished
		for rows.Next() {
			var effect unfinished
			if err := rows.Scan(&effect.id, &effect.ledgerID, &effect.started); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, effect.id)
			effects = append(effects, effect)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		observation, _ := json.Marshal(map[string]string{"reason": "controller restart with no terminal quality observation"})
		for _, effect := range effects {
			now := store.Now()
			unknown := elapsed(effect.started, now)
			update, err := tx.ExecContext(ctx, "UPDATE budget_ledgers SET unknown_ms=unknown_ms+?,revision=revision+1,updated_at=? WHERE id=?", unknown, now, effect.ledgerID)
			if err != nil {
				return err
			}
			if count, _ := update.RowsAffected(); count != 1 {
				return errors.New("quality budget ledger missing during recovery")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE quality_budget_segments_v2 SET state='uncertain',ended_at=?,unknown_ms=? WHERE effect_id=? AND state='active'", now, unknown, effect.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='uncertain',observation_json=?,observed_at=? WHERE id=? AND state='executing'", string(observation), now, effect.id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE quality_assessment_sources_v2 SET state='uncertain',observed_at=? WHERE effect_id=? AND state='reserved'", now, effect.id); err != nil {
				return err
			}
		}
		return nil
	})
	return ids, err
}
