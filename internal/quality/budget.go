package quality

import (
	"context"
	"errors"

	"vigil/internal/core"
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
