package core

import (
	"context"
	"encoding/json"
	"errors"

	"vigil/internal/coordinator"
	"vigil/internal/store"
)

type GlobalEffectLink struct {
	ProjectOperationID     string `json:"project_operation_id"`
	CoordinatorOperationID string `json:"coordinator_operation_id"`
	GlobalGrantID          string `json:"global_grant_id"`
	State                  string `json:"state"`
}

// StartGlobalEffect prepares the project-side cross-DB link, obtains the
// coordinator's serialized effect-start reservation, then rechecks local
// policy before making the project operation executable. It performs no
// external user effect itself.
func (e *Engine) StartGlobalEffect(ctx context.Context, owner *coordinator.Owner, projectOperationID, globalGrantID string) (GlobalEffectLink, error) {
	return e.startGlobalEffect(ctx, owner, projectOperationID, globalGrantID, nil)
}

func (e *Engine) startGlobalEffect(ctx context.Context, owner *coordinator.Owner, projectOperationID, globalGrantID string, fault func(string) error) (GlobalEffectLink, error) {
	var link GlobalEffectLink
	if owner == nil || owner.Coordinator == nil || !store.SafeID(projectOperationID) || !store.SafeID(globalGrantID) {
		return link, errors.New("owner, project operation and explicit global grant required")
	}
	coordinatorOperationID := store.Digest([]byte(e.ProjectID + "\x00global-effect\x00" + projectOperationID))
	link = GlobalEffectLink{ProjectOperationID: projectOperationID, CoordinatorOperationID: coordinatorOperationID, GlobalGrantID: globalGrantID, State: "prepared"}
	var operation OperationRequest
	err := e.DB.Write(ctx, func(tx *store.Tx) error {
		var raw, state string
		var operationEpoch, currentEpoch int
		if err := tx.QueryRowContext(ctx, "SELECT evidence_json,state,policy_epoch FROM operations WHERE id=?", projectOperationID).Scan(&raw, &state, &operationEpoch); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&currentEpoch); err != nil {
			return err
		}
		if state != "prepared" || operationEpoch != currentEpoch {
			return errors.New("local operation is started, cancelled or stale")
		}
		if err := json.Unmarshal([]byte(raw), &operation); err != nil {
			return err
		}
		config, err := configAt(ctx, tx)
		if err != nil {
			return err
		}
		if err := validateOperation(ctx, tx, operation, config); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO global_effect_links(project_operation_id,coordinator_operation_id,global_grant_id,state,created_at) VALUES(?,?,?,'prepared',?) ON CONFLICT(project_operation_id) DO NOTHING", projectOperationID, coordinatorOperationID, globalGrantID, store.Now())
		if err != nil {
			return err
		}
		var existingCoordinator, existingGrant, existingState string
		if err := tx.QueryRowContext(ctx, "SELECT coordinator_operation_id,global_grant_id,state FROM global_effect_links WHERE project_operation_id=?", projectOperationID).Scan(&existingCoordinator, &existingGrant, &existingState); err != nil {
			return err
		}
		if existingCoordinator != coordinatorOperationID || existingGrant != globalGrantID || existingState != "prepared" {
			return store.ErrConflict
		}
		return nil
	})
	if err != nil {
		return link, err
	}
	if fault != nil {
		if err := fault("after_local_intent"); err != nil {
			return link, err
		}
	}
	_, err = owner.AuthorizeGlobalEffect(ctx, coordinatorOperationID, projectOperationID, e.ProjectID, globalGrantID, operation.Category, operation.ResourceDigest, operation.ArgumentsDigest)
	if err != nil {
		return link, err
	}
	if fault != nil {
		if err := fault("after_coordinator_start"); err != nil {
			return link, err
		}
	}
	err = e.DB.Write(ctx, func(tx *store.Tx) error {
		var raw, state string
		var operationEpoch, currentEpoch int
		if err := tx.QueryRowContext(ctx, "SELECT evidence_json,state,policy_epoch FROM operations WHERE id=?", projectOperationID).Scan(&raw, &state, &operationEpoch); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&currentEpoch); err != nil {
			return err
		}
		if state != "prepared" || operationEpoch != currentEpoch {
			return errors.New("local policy changed after shared effect reservation; reconciliation required")
		}
		var latest OperationRequest
		if err := json.Unmarshal([]byte(raw), &latest); err != nil {
			return err
		}
		config, err := configAt(ctx, tx)
		if err != nil {
			return err
		}
		if latest != operation {
			return errors.New("local operation identity changed")
		}
		if err := validateOperation(ctx, tx, latest, config); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='executing' WHERE id=? AND state='prepared'", projectOperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE global_effect_links SET state='executing',observed_at=? WHERE project_operation_id=? AND state='prepared'", store.Now(), projectOperationID); err != nil {
			return err
		}
		result, _ := json.Marshal(map[string]string{"decision": "allow", "global_grant_id": globalGrantID, "coordinator_operation_id": coordinatorOperationID})
		_, err = tx.ExecContext(ctx, "UPDATE requests SET state='resolved',result_json=?,resolved_at=? WHERE operation_id=? AND state='pending'", string(result), store.Now(), projectOperationID)
		return err
	})
	if err != nil {
		_ = e.markGlobalEffectUncertain(ctx, projectOperationID)
		return link, err
	}
	link.State = "executing"
	return link, nil
}

func (e *Engine) markGlobalEffectUncertain(ctx context.Context, projectOperationID string) error {
	return e.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE global_effect_links SET state='uncertain',observed_at=? WHERE project_operation_id=? AND state='prepared'", store.Now(), projectOperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='prepared'", projectOperationID)
		return err
	})
}

func (e *Engine) GlobalEffectLink(ctx context.Context, projectOperationID string) (GlobalEffectLink, error) {
	var result GlobalEffectLink
	err := e.DB.SQL.QueryRowContext(ctx, "SELECT project_operation_id,coordinator_operation_id,global_grant_id,state FROM global_effect_links WHERE project_operation_id=?", projectOperationID).Scan(&result.ProjectOperationID, &result.CoordinatorOperationID, &result.GlobalGrantID, &result.State)
	return result, err
}

// ReconcileGlobalEffect never authorizes replay. A shared executing record with
// no corresponding local executing observation is retained as uncertain.
func (e *Engine) ReconcileGlobalEffect(ctx context.Context, coordinatorDB *coordinator.Coordinator, projectOperationID string) (GlobalEffectLink, error) {
	link, err := e.GlobalEffectLink(ctx, projectOperationID)
	if err != nil {
		return link, err
	}
	shared, err := coordinatorDB.GlobalEffect(ctx, link.CoordinatorOperationID)
	if err != nil {
		return link, err
	}
	if shared.ProjectOperationID != projectOperationID || shared.ProjectID != e.ProjectID || shared.GrantID != link.GlobalGrantID {
		return link, store.ErrConflict
	}
	if link.State == "prepared" && shared.State == "executing" {
		if err := e.markGlobalEffectUncertain(ctx, projectOperationID); err != nil {
			return link, err
		}
		link.State = "uncertain"
	}
	return link, nil
}
