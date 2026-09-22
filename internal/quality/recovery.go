package quality

import (
	"context"
	"encoding/json"

	"vigil/internal/core"
	"vigil/internal/store"
)

// RecoverUnfinished records the conservative restart interpretation of
// external quality effects. An executing effect has an unknown outcome and is
// made uncertain; it is never reset to prepared or automatically replayed.
func RecoverUnfinished(ctx context.Context, engine *core.Engine) ([]string, error) {
	var ids []string
	err := engine.DB.Write(ctx, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM quality_effects_v2 WHERE state='executing' ORDER BY prepared_at,id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		observation, _ := json.Marshal(map[string]string{"reason": "controller restart with no terminal quality observation"})
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "UPDATE quality_effects_v2 SET state='uncertain',observation_json=?,observed_at=? WHERE id=? AND state='executing'", string(observation), store.Now(), id); err != nil {
				return err
			}
		}
		return nil
	})
	return ids, err
}
