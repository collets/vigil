package supervisor

import (
	"context"
	"database/sql"
	"errors"

	"vigil/internal/store"
)

type RunView struct {
	RunID           string            `json:"run_id"`
	RunState        string            `json:"run_state"`
	WriterState     string            `json:"writer_state"`
	GenerationID    string            `json:"generation_id"`
	GenerationState string            `json:"generation_state"`
	SubmissionState string            `json:"submission_state"`
	NativeSessionID string            `json:"native_session_id,omitempty"`
	NativeTurnID    string            `json:"native_turn_id,omitempty"`
	TaskState       string            `json:"task_state"`
	Effects         map[string]string `json:"effects"`
	AllowedNext     []string          `json:"allowed_next_commands"`
}

func (r *Runner) Inspect(ctx context.Context, prepared PreparedRun) (RunView, error) {
	view := RunView{RunID: prepared.RunID, GenerationID: prepared.GenerationID, Effects: map[string]string{}, AllowedNext: []string{}}
	err := r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT runs.state,runs.writer_state,g.state,g.submission_state,coalesce(g.native_session_id,''),coalesce(g.native_turn_id,''),tasks.state FROM runs JOIN run_generations g ON g.run_id=runs.id JOIN tasks ON tasks.id=runs.task_id WHERE runs.id=? AND g.id=?`, prepared.RunID, prepared.GenerationID).Scan(&view.RunState, &view.WriterState, &view.GenerationState, &view.SubmissionState, &view.NativeSessionID, &view.NativeTurnID, &view.TaskState)
	if err != nil {
		return view, err
	}
	rows, err := r.Engine.DB.SQL.QueryContext(ctx, "SELECT kind,state FROM execution_effects WHERE generation_id=? ORDER BY ordinal", prepared.GenerationID)
	if err != nil {
		return view, err
	}
	for rows.Next() {
		var kind, state string
		if err := rows.Scan(&kind, &state); err != nil {
			rows.Close()
			return view, err
		}
		view.Effects[kind] = state
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return view, err
	}
	rows.Close()
	switch {
	case view.SubmissionState == "uncertain":
		view.AllowedNext = []string{"inspect", "reconcile", "stop"}
	case view.RunState == "completed":
		view.AllowedNext = []string{"inspect"}
	case view.RunState == "prepared" || view.RunState == "starting" || view.RunState == "active":
		view.AllowedNext = []string{"inspect", "start", "reconcile", "stop"}
	default:
		view.AllowedNext = []string{"inspect", "reconcile"}
	}
	return view, nil
}

// Reconcile inspects the stable runtime identity and records what is proven. It
// never calls Submit or any create/start operation.
func (r *Runner) Reconcile(ctx context.Context, prepared PreparedRun) (RunView, error) {
	observation, err := r.Driver.Inspect(ctx, prepared)
	if err != nil {
		return RunView{}, err
	}
	rows, err := r.Engine.DB.SQL.QueryContext(ctx, "SELECT id,kind,state FROM execution_effects WHERE generation_id=? ORDER BY ordinal", prepared.GenerationID)
	if err != nil {
		return RunView{}, err
	}
	type effect struct{ id, kind, state string }
	var effects []effect
	for rows.Next() {
		var item effect
		if err := rows.Scan(&item.id, &item.kind, &item.state); err != nil {
			rows.Close()
			return RunView{}, err
		}
		effects = append(effects, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RunView{}, err
	}
	rows.Close()
	for _, item := range effects {
		if item.state == "observed" || item.state == "reconciled" {
			continue
		}
		if phaseObserved(item.kind, observation) {
			if err := r.observeEffect(ctx, item.id, "reconciled", observation); err != nil {
				return RunView{}, err
			}
		}
	}
	var submission string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT submission_state FROM run_generations WHERE id=?", prepared.GenerationID).Scan(&submission); err != nil {
		return RunView{}, err
	}
	if observation.SubmissionState == "delivered" {
		if err := r.setSubmission(ctx, prepared, "delivered", observation.NativeTurnID); err != nil {
			return RunView{}, err
		}
		submission = "delivered"
	} else if observation.SubmissionState == "not_attempted" && submission == "writing" {
		if err := r.setSubmission(ctx, prepared, "proven_not_delivered", ""); err != nil {
			return RunView{}, err
		}
		submission = "proven_not_delivered"
	} else if submission == "writing" || submission == "uncertain" {
		if err := r.setSubmission(ctx, prepared, "uncertain", observation.NativeTurnID); err != nil {
			return RunView{}, err
		}
		submission = "uncertain"
	}
	if observation.Terminal {
		terminalID, effectErr := r.effect(ctx, prepared, 7, "terminal_observe", map[string]string{"generation_id": prepared.GenerationID})
		if effectErr != nil {
			return RunView{}, effectErr
		}
		if err := r.observeEffect(ctx, terminalID, "reconciled", observation); err != nil {
			return RunView{}, err
		}
		if err := r.ingest(ctx, prepared, observation); err != nil {
			return RunView{}, err
		}
	}
	var resultCount int
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM execution_results WHERE run_id=?", prepared.RunID).Scan(&resultCount); err != nil {
		return RunView{}, err
	}
	if resultCount == 0 && observation.Terminal && submission == "delivered" && observation.WriterState == "contained_stopped" && len(observation.Result) != 0 {
		if _, err := r.persistResult(ctx, prepared, observation); err != nil {
			return RunView{}, err
		}
	}
	if submission == "uncertain" {
		_ = r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE runs SET state='unknown' WHERE id=? AND state!='completed'", prepared.RunID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE run_generations SET state='unknown' WHERE id=? AND state!='terminal'", prepared.GenerationID)
			return err
		})
	}
	return r.Inspect(ctx, prepared)
}

func (r *Runner) Result(ctx context.Context, runID string) (Result, error) {
	var result Result
	var changed string
	err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT schema_version,status,summary,changed_paths_json FROM execution_results WHERE run_id=?", runID).Scan(&result.SchemaVersion, &result.Status, &result.Summary, &changed)
	if errors.Is(err, sql.ErrNoRows) {
		return result, errors.New("validated execution result is not available")
	}
	if err != nil {
		return result, err
	}
	if err := store.Decode([]byte(changed), &result.ChangedPaths); err != nil {
		return result, err
	}
	return result, nil
}
