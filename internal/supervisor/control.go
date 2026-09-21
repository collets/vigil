package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vigil/internal/core"
	"vigil/internal/store"
)

type StopReceipt struct {
	ControlID      string `json:"control_id"`
	RunID          string `json:"run_id"`
	GenerationID   string `json:"generation_id"`
	State          string `json:"state"`
	InterruptState string `json:"interrupt_state"`
	WriterState    string `json:"writer_state"`
	InferenceState string `json:"inference_state"`
	Repeated       bool   `json:"repeated,omitempty"`
}

func (r *Runner) Stop(ctx context.Context, prepared PreparedRun, commandID string, interruptGrace, terminateGrace time.Duration) (StopReceipt, error) {
	return r.stop(ctx, prepared, commandID, "stop", interruptGrace, terminateGrace)
}

func (r *Runner) Shutdown(ctx context.Context, prepared PreparedRun, commandID string, interruptGrace, terminateGrace time.Duration) (StopReceipt, error) {
	return r.stop(ctx, prepared, commandID, "foreground_shutdown", interruptGrace, terminateGrace)
}

func (r *Runner) stop(ctx context.Context, prepared PreparedRun, commandID, kind string, interruptGrace, terminateGrace time.Duration) (StopReceipt, error) {
	var receipt StopReceipt
	if r == nil || r.Engine == nil || r.Engine.DB == nil || r.Driver == nil || !store.SafeID(commandID) {
		return receipt, errors.New("persisted run, driver and valid command ID required")
	}
	if interruptGrace < 0 || interruptGrace > 30*time.Second || terminateGrace <= 0 || terminateGrace > 30*time.Second {
		return receipt, errors.New("bounded interrupt and termination grace required")
	}
	controlID := store.Digest([]byte(prepared.GenerationID + "\x00control\x00" + kind))
	effectID := store.Digest([]byte(prepared.GenerationID + "\x00containment_stop"))
	actor := string(core.Human)
	if kind != "stop" {
		actor = string(core.Core)
	}
	args, _ := json.Marshal(map[string]any{"run_id": prepared.RunID, "generation_id": prepared.GenerationID, "kind": kind, "interrupt_grace_ms": interruptGrace.Milliseconds(), "terminate_grace_ms": terminateGrace.Milliseconds()})
	_, found, err := r.Engine.DB.Receipt(ctx, store.Command{ID: commandID, Actor: actor, Kind: "execution." + kind, Args: args})
	if err != nil {
		return receipt, err
	}
	if !found {
		_, err = r.Engine.DB.Command(ctx, store.Command{ID: commandID, Actor: actor, Kind: "execution." + kind, Args: args}, func(tx *store.Tx) (any, error) {
			var runState string
			if err := tx.QueryRowContext(ctx, "SELECT state FROM runs WHERE id=?", prepared.RunID).Scan(&runState); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='paused',revision=revision+1 WHERE id=?", r.Engine.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='paused' WHERE id=? AND state IN('active','blocked')", prepared.PlanID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE run_id=? AND state='pending'", store.Now(), prepared.RunID); err != nil {
				return nil, err
			}
			if runState != "completed" && runState != "failed" && runState != "interrupted" {
				if _, err := tx.ExecContext(ctx, "UPDATE runs SET state='stopping' WHERE id=?", prepared.RunID); err != nil {
					return nil, err
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE tasks SET state='stopped',block_reason='stopped by explicit control' WHERE id=? AND state!='accepted'", prepared.TaskID); err != nil {
				return nil, err
			}
			intent, _ := json.Marshal(map[string]string{"control_id": controlID, "kind": kind})
			if _, err := tx.ExecContext(ctx, `INSERT INTO execution_effects(id,run_id,generation_id,ordinal,kind,state,stable_identity,intent_json,prepared_at) VALUES(?,?,?,10,'containment_stop','prepared',?,?,?) ON CONFLICT(id) DO NOTHING`, effectID, prepared.RunID, prepared.GenerationID, prepared.RuntimeResourceID, string(intent), store.Now()); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO execution_controls(id,kind,run_id,generation_id,state,interrupt_state,writer_state,inference_state,prepared_at,observation_json) VALUES(?,?,?,?,'prepared','pending','unconfirmed','unknown',?,'{}') ON CONFLICT(id) DO NOTHING`, controlID, kind, prepared.RunID, prepared.GenerationID, store.Now()); err != nil {
				return nil, err
			}
			return map[string]string{"control_id": controlID, "state": "prepared"}, nil
		})
		if err != nil {
			return receipt, err
		}
	}
	if err := r.loadStopReceipt(ctx, controlID, &receipt); err != nil {
		return receipt, err
	}
	r.cancelActiveExecution(prepared.GenerationID)
	receipt.Repeated = found
	if receipt.State == "observed" {
		if err := r.awaitExecutionRetirement(ctx, prepared.GenerationID, terminateGrace); err != nil {
			return receipt, err
		}
		return receipt, nil
	}
	if receipt.State == "executing" || receipt.State == "uncertain" {
		return receipt, errors.New("stop outcome is unresolved; external effects will not be replayed")
	}
	var effectState string
	if err := r.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM execution_effects WHERE id=?", effectID).Scan(&effectState); err != nil {
		return receipt, err
	}
	if effectState == "observed" || effectState == "reconciled" {
		observation, inspectErr := r.Driver.Inspect(ctx, prepared)
		if inspectErr != nil || observation.WriterState != "contained_stopped" {
			return receipt, errors.New("prior containment is recorded but current writer safety cannot be proven")
		}
		if retireErr := r.awaitExecutionRetirement(ctx, prepared.GenerationID, terminateGrace); retireErr != nil {
			return receipt, retireErr
		}
		return r.finishStop(ctx, controlID, effectID, "unsupported", observation, nil, found)
	}
	if effectState == "executing" || effectState == "uncertain" {
		return receipt, errors.New("prior containment outcome is unresolved; external effects will not be replayed")
	}
	if err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE execution_controls SET state='executing' WHERE id=? AND state='prepared'", controlID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE execution_effects SET state='executing' WHERE id=? AND state IN('prepared','cancelled')", effectID)
		return err
	}); err != nil {
		return receipt, err
	}
	interruptState := "unsupported"
	if driver, ok := r.Driver.(InterruptDriver); ok {
		interruptState = "sent"
		interruptCtx, cancel := context.WithTimeout(ctx, maxDuration(interruptGrace, time.Millisecond))
		if err := driver.Interrupt(interruptCtx, prepared); err != nil {
			interruptState = "failed"
		}
		cancel()
	}
	if interruptGrace > 0 {
		deadline := time.Now().Add(interruptGrace)
		for time.Now().Before(deadline) {
			observation, inspectErr := r.Driver.Inspect(ctx, prepared)
			if inspectErr == nil && observation.WriterState == "contained_stopped" {
				retireErr := r.awaitExecutionRetirement(ctx, prepared.GenerationID, terminateGrace)
				return r.finishStop(ctx, controlID, effectID, interruptState, observation, retireErr, found)
			}
			select {
			case <-ctx.Done():
				return r.finishStop(context.Background(), controlID, effectID, interruptState, Observation{}, ctx.Err(), found)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), terminateGrace)
	observation, stopErr := r.Driver.Stop(stopCtx, prepared)
	cancel()
	if retireErr := r.awaitExecutionRetirement(context.Background(), prepared.GenerationID, terminateGrace); stopErr == nil {
		stopErr = retireErr
	}
	return r.finishStop(context.Background(), controlID, effectID, interruptState, observation, stopErr, found)
}

func (r *Runner) cancelActiveExecution(generationID string) {
	r.executionMu.Lock()
	active, ok := r.activeExecutions[generationID]
	r.executionMu.Unlock()
	if ok {
		active.cancel()
	}
}

func (r *Runner) awaitExecutionRetirement(ctx context.Context, generationID string, limit time.Duration) error {
	retireCtx, cancel := context.WithTimeout(ctx, maxDuration(limit, time.Millisecond))
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var executing int
		if err := r.Engine.DB.SQL.QueryRowContext(retireCtx, `SELECT count(*) FROM execution_effects WHERE generation_id=? AND kind!='containment_stop' AND state='executing'`, generationID).Scan(&executing); err != nil {
			return err
		}
		r.executionMu.Lock()
		active, local := r.activeExecutions[generationID]
		r.executionMu.Unlock()
		if executing == 0 && !local {
			return nil
		}
		if local {
			select {
			case <-active.done:
				continue
			default:
			}
		}
		select {
		case <-retireCtx.Done():
			return errors.New("stopped writer but dispatcher retirement remains unconfirmed")
		case <-ticker.C:
		}
	}
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (r *Runner) loadStopReceipt(ctx context.Context, id string, receipt *StopReceipt) error {
	return r.Engine.DB.SQL.QueryRowContext(ctx, `SELECT id,run_id,generation_id,state,interrupt_state,writer_state,inference_state FROM execution_controls WHERE id=?`, id).Scan(&receipt.ControlID, &receipt.RunID, &receipt.GenerationID, &receipt.State, &receipt.InterruptState, &receipt.WriterState, &receipt.InferenceState)
}

func (r *Runner) finishStop(ctx context.Context, controlID, effectID, interruptState string, observation Observation, stopErr error, repeated bool) (StopReceipt, error) {
	state := "uncertain"
	if stopErr == nil && observation.WriterState == "contained_stopped" {
		state = "observed"
	}
	raw, _ := json.Marshal(map[string]any{"observation": observation, "error": errorString(stopErr)})
	err := r.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE execution_controls SET state=?,interrupt_state=?,writer_state=?,observation_json=?,observed_at=? WHERE id=?`, state, interruptState, writerStateOrUnconfirmed(observation.WriterState), string(raw), store.Now(), controlID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE execution_effects SET state=?,observation_json=?,observed_at=? WHERE id=?`, map[bool]string{true: "observed", false: "uncertain"}[state == "observed"], string(raw), store.Now(), effectID); err != nil {
			return err
		}
		runState := "unknown"
		generationState := "unknown"
		if state == "observed" {
			runState, generationState = "interrupted", "contained"
		}
		var runID string
		if err := tx.QueryRowContext(ctx, "SELECT run_id FROM execution_controls WHERE id=?", controlID).Scan(&runID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE runs SET state=?,writer_state=?,ended_at=? WHERE id=? AND state NOT IN('completed','failed')`, runState, writerStateOrUnconfirmed(observation.WriterState), store.Now(), runID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE run_generations SET state=? WHERE id=(SELECT generation_id FROM execution_controls WHERE id=?) AND state!='terminal'`, generationState, controlID)
		return err
	})
	var receipt StopReceipt
	if loadErr := r.loadStopReceipt(context.Background(), controlID, &receipt); loadErr != nil {
		if err == nil {
			err = loadErr
		}
	}
	receipt.Repeated = repeated
	if stopErr != nil && err == nil {
		err = fmt.Errorf("bounded stop failed: %w", stopErr)
	}
	if state != "observed" && err == nil {
		err = errors.New("writer containment remains unconfirmed")
	}
	return receipt, err
}

func writerStateOrUnconfirmed(state string) string {
	if state == "observed_stopped" || state == "contained_stopped" {
		return state
	}
	return "unconfirmed"
}
