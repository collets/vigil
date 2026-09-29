package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"vigil/internal/store"
)

type DeliveryStatus struct {
	OperationID   string `json:"operation_id"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	DeliveryID    string `json:"delivery_id,omitempty"`
	DeliveryState string `json:"delivery_state,omitempty"`
	ExternalID    string `json:"external_id,omitempty"`
	URL           string `json:"url,omitempty"`
	Observation   string `json:"observation,omitempty"`
	// ApprovedTargets are the exact comparison operands an operator needs to
	// resolve a diverged observation: the ref or destination the operation was
	// approved against, and the object it would have produced.
	ApprovedTargets []string `json:"approved_targets,omitempty"`
}

func (e *Engine) DeliveryStatus(ctx context.Context, operationID string) (DeliveryStatus, error) {
	result := DeliveryStatus{OperationID: operationID}
	if !store.SafeID(operationID) {
		return result, errors.New("operation ID required")
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT kind,state FROM operations WHERE id=? AND kind IN ('commit','push','draft_request')", operationID).Scan(&result.Kind, &result.State); err != nil {
		return DeliveryStatus{}, errors.New("delivery operation not found")
	}
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT id,state,coalesce(external_id,''),coalesce(url,'') FROM deliveries WHERE operation_id=?", operationID).
		Scan(&result.DeliveryID, &result.DeliveryState, &result.ExternalID, &result.URL); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DeliveryStatus{}, err
	}
	// Surface the approved comparison operands so an operator can resolve a
	// diverged observation without re-deriving them from the intent.
	switch result.Kind {
	case "commit":
		if intent, _, err := e.loadCommitIntent(ctx, operationID); err == nil {
			result.ApprovedTargets = []string{"target_ref=" + intent.TargetRef, "approved_predecessor=" + intent.ExpectedRefOID, "approved_tree=" + intent.TreeOID}
		}
	case "push":
		if intent, _, err := e.loadPushIntent(ctx, operationID); err == nil {
			result.ApprovedTargets = []string{"remote_ref=" + intent.RemoteRef, "approved_predecessor=" + intent.ExpectedRemoteOID, "approved_head=" + intent.HeadOID}
		}
	case "draft_request":
		if intent, _, err := e.loadDraftIntent(ctx, operationID); err == nil {
			result.ApprovedTargets = []string{"project=" + intent.Project, "head=" + intent.HeadBranch + "@" + intent.HeadOID, "base=" + intent.BaseBranch}
		}
	}
	return result, nil
}

// Cancelling a prepared operation cannot discard an external effect: no grant
// has been consumed and no delivery row may exist. Executing or uncertain
// operations require exact effect reconciliation and are never discarded here.
func (e *Engine) CancelPreparedDelivery(ctx context.Context, commandID, operationID string) (DeliveryStatus, error) {
	if !store.SafeID(commandID) || !store.SafeID(operationID) {
		return DeliveryStatus{}, errors.New("exact cancellation command and operation IDs required")
	}
	args, _ := json.Marshal(map[string]string{"operation_id": operationID})
	receipt, err := e.DB.Command(ctx, store.Command{ID: commandID, Actor: "human", Kind: "delivery.cancel", Args: args}, func(tx *store.Tx) (any, error) {
		var kind, state string
		if err := tx.QueryRowContext(ctx, "SELECT kind,state FROM operations WHERE id=? AND kind IN ('commit','push','draft_request')", operationID).Scan(&kind, &state); err != nil || state != "prepared" {
			return nil, errors.New("only a prepared, never-started delivery can be cancelled")
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE operation_id=?", operationID).Scan(&count); err != nil || count != 0 {
			return nil, errors.New("delivery effect journal exists; cancellation is unsafe")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='cancelled' WHERE id=? AND state='prepared'", operationID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='cancelled',resolved_at=? WHERE operation_id=? AND state='pending'", store.Now(), operationID); err != nil {
			return nil, err
		}
		return DeliveryStatus{OperationID: operationID, Kind: kind, State: "cancelled"}, nil
	})
	if err != nil {
		return DeliveryStatus{}, err
	}
	var result DeliveryStatus
	if err := json.Unmarshal(receipt, &result); err != nil {
		return DeliveryStatus{}, err
	}
	return result, nil
}

// ReconcileDelivery closes a stuck executing or uncertain delivery operation
// with a fresh exact observation. It closes only when the approved end state
// was reached (observed/succeeded) or the approved prior state still holds
// (reconciled/failed: provably no net effect). Any other observation — a ref
// or remote that moved, or a hosting listing without the exact draft — is
// reported and left open, because absence is not proof of non-delivery and a
// moved destination is a human decision. Reconciliation itself has no external
// effect: it never moves refs, pushes or POSTs.
func (e *Engine) ReconcileDelivery(ctx context.Context, commandID, operationID string) (DeliveryStatus, error) {
	if !store.SafeID(commandID) || !store.SafeID(operationID) {
		return DeliveryStatus{}, errors.New("exact reconciliation command and operation IDs required")
	}
	var kind, state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT kind,state FROM operations WHERE id=? AND kind IN ('commit','push','draft_request')", operationID).Scan(&kind, &state); err != nil {
		return DeliveryStatus{}, errors.New("delivery operation not found")
	}
	// A prior claim without a committed closure (an interrupted or blocked
	// reconciliation) is resumable: the claim is the only thing standing
	// between the observation and a concurrent effect.
	claimed := state == "reconciled"
	if !claimed && state != "executing" && state != "uncertain" {
		return DeliveryStatus{}, errors.New("only an executing or uncertain delivery can be reconciled; prepared deliveries are cancelled instead")
	}
	if !claimed {
		// Claim first. Marking the operation reconciled blocks every executor,
		// which only act on prepared/executing/uncertain, so no new effect can
		// start while the external state is observed and the closure committed.
		claim := e.DB.Write(ctx, func(tx *store.Tx) error {
			updated, err := tx.ExecContext(ctx, "UPDATE operations SET state='reconciled' WHERE id=? AND state IN ('executing','uncertain')", operationID)
			if err != nil {
				return err
			}
			if count, err := updated.RowsAffected(); err != nil || count != 1 {
				return errors.New("delivery operation changed before reconciliation claimed")
			}
			return nil
		})
		if claim != nil {
			return DeliveryStatus{}, claim
		}
	}
	observation, closeAs, err := e.planDeliveryReconciliation(ctx, kind, operationID)
	if err != nil {
		// A blocked observation releases the claim so the operation is exactly
		// as executable as it was found: the operator resolves the underlying
		// ref or hosting request, then reconciles again.
		if releaseErr := e.DB.Write(ctx, func(tx *store.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='reconciled'", operationID)
			return err
		}); releaseErr != nil {
			return DeliveryStatus{}, fmt.Errorf("%w (the claimed operation could not be released: %v)", err, releaseErr)
		}
		return DeliveryStatus{}, err
	}
	args, _ := json.Marshal(map[string]string{"operation_id": operationID, "kind": kind, "observed": observation.Observation, "close_as": closeAs})
	receipt, err := e.DB.Command(ctx, store.Command{ID: commandID, Actor: "human", Kind: "delivery.reconcile", Args: args}, func(tx *store.Tx) (any, error) {
		var claimedState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", operationID).Scan(&claimedState); err != nil || claimedState != "reconciled" {
			return nil, errors.New("reconciliation claim was lost before closure")
		}
		var deliveryState string
		hasRow := true
		if err := tx.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", operationID).Scan(&deliveryState); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			hasRow = false
		}
		if closeAs == "observed" {
			if !hasRow {
				return nil, errors.New("observed reconciliation requires the delivery journal")
			}
			updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='succeeded',external_id=?,url=? WHERE operation_id=? AND state IN ('pending','uncertain')",
				observation.ExternalID, observation.URL, operationID)
			if err != nil {
				return nil, err
			}
			if count, err := updated.RowsAffected(); err != nil || count != 1 {
				return nil, errors.New("delivery journal changed before reconciliation")
			}
			if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=?", operationID); err != nil {
				return nil, err
			}
			closed := observation
			closed.OperationID, closed.State, closed.DeliveryState = operationID, "observed", "succeeded"
			return closed, nil
		}
		if hasRow {
			updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='failed' WHERE operation_id=? AND state IN ('pending','uncertain')", operationID)
			if err != nil {
				return nil, err
			}
			if count, err := updated.RowsAffected(); err != nil || count != 1 {
				return nil, errors.New("delivery journal changed before reconciliation closure")
			}
		}
		closed := observation
		closed.OperationID, closed.State, closed.DeliveryState = operationID, "reconciled", "failed"
		return closed, nil
	})
	if err != nil {
		return DeliveryStatus{}, err
	}
	var reconciled DeliveryStatus
	if err := json.Unmarshal(receipt, &reconciled); err != nil {
		return DeliveryStatus{}, err
	}
	return reconciled, nil
}

// observeDeliveryForReconciliation reads the current exact external state of
// one delivery operation and decides the only two provable closures. The
// returned partial status carries the observed identity.
func (e *Engine) planDeliveryReconciliation(ctx context.Context, kind, operationID string) (DeliveryStatus, string, error) {
	switch kind {
	case "commit":
		intent, _, err := e.loadCommitIntent(ctx, operationID)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		record, err := e.Repository(ctx, intent.RepositoryID)
		if err != nil || record.PlanID != intent.PlanID {
			return DeliveryStatus{}, "", errors.New("enrolled commit repository required")
		}
		current := ""
		if output, err := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", "--verify", "--end-of-options", intent.TargetRef+"^{commit}"); err == nil {
			current = output
		}
		var journaled string
		_ = e.DB.SQL.QueryRowContext(ctx, "SELECT head_oid FROM deliveries WHERE operation_id=?", operationID).Scan(&journaled)
		if journaled != "" && journaled != intent.ParentOID && current == journaled {
			return DeliveryStatus{Kind: kind, DeliveryID: commitDeliveryID(operationID), Observation: current}, "observed", nil
		}
		if current == intent.ExpectedRefOID {
			observed := "absent"
			if current != "" {
				observed = current
			}
			return DeliveryStatus{Kind: kind, DeliveryID: commitDeliveryID(operationID), Observation: observed}, "reconciled", nil
		}
		if current == "" {
			return DeliveryStatus{}, "", errors.New("plan ref disappeared; resolve the ref before reconciling")
		}
		return DeliveryStatus{}, "", fmt.Errorf("plan ref is %s, not the approved predecessor %q or the approved commit %q; resolve the ref before reconciling", current, intent.ExpectedRefOID, journaled)
	case "push":
		intent, _, err := e.loadPushIntent(ctx, operationID)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		record, env, err := e.pushContext(ctx, intent, false)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		remote, err := observedRemoteRef(ctx, record, intent.RemoteURL, intent.RemoteRef, env)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		if remote == intent.HeadOID {
			return DeliveryStatus{Kind: kind, DeliveryID: pushDeliveryID(operationID), Observation: remote}, "observed", nil
		}
		if remote == intent.ExpectedRemoteOID {
			observed := "absent"
			if remote != "" {
				observed = remote
			}
			return DeliveryStatus{Kind: kind, DeliveryID: pushDeliveryID(operationID), Observation: observed}, "reconciled", nil
		}
		if remote == "" {
			return DeliveryStatus{}, "", errors.New("remote ref disappeared; resolve the remote before reconciling")
		}
		return DeliveryStatus{}, "", fmt.Errorf("remote ref is %s, not the approved predecessor %q or the approved head %q; resolve the remote before reconciling", remote, intent.ExpectedRemoteOID, intent.HeadOID)
	case "draft_request":
		intent, _, err := e.loadDraftIntent(ctx, operationID)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		adapter, err := newHostingAdapter(hostingSpec{Provider: intent.Provider, APIBase: intent.APIBase, Project: intent.Project,
			HeadRef: intent.HeadBranch, HeadOID: intent.HeadOID, BaseRef: intent.BaseBranch,
			Title: intent.Title, Body: intent.Body, OperationID: operationID, CredentialRef: intent.CredentialRef, Fixture: intent.Fixture})
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		hosted, found, err := adapter.List(ctx)
		if err != nil {
			return DeliveryStatus{}, "", err
		}
		if found {
			return DeliveryStatus{Kind: kind, DeliveryID: draftDeliveryID(operationID), ExternalID: hosted.ExternalID, URL: hosted.URL,
				Observation: hosted.ExternalID}, "observed", nil
		}
		return DeliveryStatus{}, "", errors.New("exact draft absent from a bounded listing; absence is not proof of non-delivery, so the operation stays open for manual remediation")
	}
	return DeliveryStatus{}, "", errors.New("unsupported delivery kind")
}
