package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"vigil/internal/store"
)

type DispatchControl struct {
	ProjectID string `json:"project_id"`
	State     string `json:"state"`
	Revision  int    `json:"revision"`
	Repeated  bool   `json:"repeated,omitempty"`
}

func (e *Engine) Pause(ctx context.Context, commandID string, expectedRevision int) (DispatchControl, error) {
	return e.dispatchControl(ctx, commandID, expectedRevision, "project.pause")
}

func (e *Engine) Continue(ctx context.Context, commandID string, expectedRevision int) (DispatchControl, error) {
	return e.dispatchControl(ctx, commandID, expectedRevision, "project.continue")
}

func (e *Engine) dispatchControl(ctx context.Context, commandID string, expectedRevision int, kind string) (DispatchControl, error) {
	var result DispatchControl
	if e == nil || e.DB == nil || !store.SafeID(commandID) || expectedRevision < 1 {
		return result, errors.New("valid command ID and expected revision required")
	}
	if kind != "project.pause" && kind != "project.continue" {
		return result, errors.New("invalid dispatch control")
	}
	args, _ := json.Marshal(map[string]any{"project_id": e.ProjectID, "expected_revision": expectedRevision})
	if receipt, found, err := e.DB.Receipt(ctx, store.Command{ID: commandID, Actor: string(Human), Kind: kind, Args: args}); err != nil {
		return result, err
	} else if found {
		if err := json.Unmarshal(receipt, &result); err != nil {
			return result, err
		}
		result.Repeated = true
		return result, nil
	}
	receipt, err := e.DB.Command(ctx, store.Command{ID: commandID, Actor: string(Human), Kind: kind, Args: args}, func(tx *store.Tx) (any, error) {
		var revision int
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT revision,state FROM project WHERE id=?", e.ProjectID).Scan(&revision, &state); err != nil {
			return nil, err
		}
		if revision != expectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", expectedRevision, revision)
		}
		if kind == "project.continue" {
			if state != "paused" {
				return nil, errors.New("only a paused project can continue")
			}
			var unsafe int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE state='stopping' OR (state='unknown' AND writer_state!='contained_stopped')`).Scan(&unsafe); err != nil {
				return nil, err
			}
			if unsafe != 0 {
				return nil, errors.New("dispatch remains disabled while execution recovery is unresolved")
			}
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM checkpoint_sets WHERE state IN('capturing','clearing','restoring','conflicted','incomplete')`).Scan(&unsafe); err != nil {
				return nil, err
			}
			if unsafe != 0 {
				return nil, errors.New("dispatch remains disabled while checkpoint recovery is unresolved")
			}
			var exhausted int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM budget_ledgers WHERE charged_ms+unknown_ms>=active_limit_ms`).Scan(&exhausted); err != nil {
				return nil, err
			}
			if exhausted != 0 {
				return nil, errors.New("dispatch remains disabled because a cumulative budget is exhausted")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='ready',revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='active' WHERE state='paused'"); err != nil {
				return nil, err
			}
			state = "ready"
		} else {
			if state == "quarantined" || state == "recovering" {
				return nil, errors.New("recovery or quarantine cannot be relabeled as a pause")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='paused',revision=revision+1 WHERE id=?", e.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='paused' WHERE state IN('active','blocked')"); err != nil {
				return nil, err
			}
			state = "paused"
		}
		return DispatchControl{ProjectID: e.ProjectID, State: state, Revision: revision + 1}, nil
	})
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(receipt, &result)
	return result, err
}
