package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	// Attestation is the human decision of record for a closed-unobserved
	// delivery, never a system proof that the effect did not occur. It is read
	// from the attestation receipt, so a later reader of delivery-status can
	// always distinguish "the system proved this" from "a person asserted this".
	Attestation string `json:"attestation,omitempty"`
}

// attestedOperationCommand is the receipt kind recorded by
// CloseUnobservedDelivery; its presence is what marks a reconciled delivery as
// human-attested rather than system-observed.
const attestedOperationCommand = "delivery.close_unobserved"

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
			result.ApprovedTargets = []string{"target_ref=" + intent.TargetRef, "approved_predecessor=" + approvedRef(intent.ExpectedRefOID), "approved_tree=" + intent.TreeOID}
		}
	case "push":
		if intent, _, err := e.loadPushIntent(ctx, operationID); err == nil {
			result.ApprovedTargets = []string{"remote_ref=" + intent.RemoteRef, "approved_predecessor=" + approvedRef(intent.ExpectedRemoteOID), "approved_head=" + intent.HeadOID}
		}
	case "draft_request":
		if intent, _, err := e.loadDraftIntent(ctx, operationID); err == nil {
			result.ApprovedTargets = []string{"project=" + intent.Project, "head=" + intent.HeadBranch + "@" + intent.HeadOID, "base=" + intent.BaseBranch}
		}
	}
	// A reconciled delivery with no observed effect was closed by a human
	// attestation. Surface that provenance from the attestation receipt so the
	// status never reads as a system-proven outcome.
	if result.State == "reconciled" && result.DeliveryState == "failed" {
		result.Attestation = e.findDeliveryAttestation(ctx, operationID)
		if result.Attestation == "" {
			result.Attestation = "closed without a recorded human attestation"
		}
	}
	return result, nil
}

// findDeliveryAttestation reads back the attestation a human recorded for this
// operation, so delivery-status carries the same provenance the command did.
func (e *Engine) findDeliveryAttestation(ctx context.Context, operationID string) string {
	var raw string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT c.result_json FROM command_receipts c
		JOIN events e ON e.command_id=c.id
		WHERE c.actor='human' AND json_extract(e.payload_json,'$.command_kind')=?
		AND json_extract(c.result_json,'$.operation_id')=?
		ORDER BY c.committed_at DESC LIMIT 1`, attestedOperationCommand, operationID).Scan(&raw); err != nil {
		return ""
	}
	var status struct {
		OperationID string `json:"operation_id"`
		Attestation string `json:"attestation"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil || status.OperationID != operationID {
		return ""
	}
	return status.Attestation
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

// ReconcileDelivery resolves a stuck executing or uncertain delivery operation
// from a fresh exact observation.
//
// Automatic closure is deliberately limited to POSITIVE proof that the
// approved effect happened (observed/succeeded). Nothing automatic ever asserts
// that an unobserved effect did not occur: no durable state can distinguish an
// executor mid-effect from one that crashed mid-effect, so a point-in-time
// observation cannot prove non-occurrence.
//
// For a push whose destination still holds the approved predecessor, the
// reconciler re-attempts the identical non-force OID:ref push, which is
// idempotent — a no-op if the first push landed, the delivery if it did not,
// and a Git rejection (leaving the operation open) if a third party moved the
// destination. No other kind is retried, and no other outcome closes
// automatically. Everything else — a ref or remote moved by a third party, or
// a hosting listing without the exact draft — is reported with its approved
// operands and left open for CloseUnobservedDelivery's explicit human
// attestation.
func (e *Engine) ReconcileDelivery(ctx context.Context, commandID, operationID string) (DeliveryStatus, error) {
	if !store.SafeID(commandID) || !store.SafeID(operationID) {
		return DeliveryStatus{}, errors.New("exact reconciliation command and operation IDs required")
	}
	var kind, state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT kind,state FROM operations WHERE id=? AND kind IN ('commit','push','draft_request')", operationID).Scan(&kind, &state); err != nil {
		return DeliveryStatus{}, errors.New("delivery operation not found")
	}
	// A claim is resumable ONLY when no closure was committed for it. A claim
	// is written as operations.state='reconciled', and so is a terminal
	// human-attested closure, which additionally moves the delivery journal to
	// a terminal state. Resuming a closed operation would re-attempt an
	// external effect on an operation the operator already decided was closed,
	// so the journal is what distinguishes an interrupted claim from a
	// finished one.
	claimed := state == "reconciled"
	if claimed {
		var deliveryState string
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", operationID).Scan(&deliveryState); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return DeliveryStatus{}, err
		} else if err == nil && (deliveryState == "succeeded" || deliveryState == "failed") {
			return DeliveryStatus{}, errors.New("delivery operation is already closed; a committed closure is never reopened by reconciliation")
		}
	}
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
			// A concurrent executor may already have observed the same effect
			// and recorded it. Report that durable truth instead of failing
			// after a successful effect, so a reported success always matches
			// the recorded state.
			if deliveryState == "succeeded" {
				closed := observation
				closed.OperationID, closed.State, closed.DeliveryState = operationID, "observed", "succeeded"
				if err := tx.QueryRowContext(ctx, "SELECT id,coalesce(external_id,''),coalesce(url,'') FROM deliveries WHERE operation_id=?", operationID).
					Scan(&closed.DeliveryID, &closed.ExternalID, &closed.URL); err != nil {
					return nil, err
				}
				if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=? AND state='reconciled'", operationID); err != nil {
					return nil, err
				}
				return closed, nil
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
		// No automatic closure can assert non-occurrence: only the explicit
		// human attestation path may record that an effect did not happen.
		return nil, errors.New("reconciliation closed automatically without proof the effect occurred; use delivery-close-unobserved to attest")
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

// CloseUnobservedDelivery is the explicit, human-attested exit for a delivery
// operation whose external effect could not be positively observed. The system
// cannot prove non-occurrence — an executor mid-effect is indistinguishable
// from one that crashed mid-effect — so this records an operator decision of
// record, never a system proof: the receipt states that a human attested, and
// carries the fresh observation it was taken against.
func (e *Engine) CloseUnobservedDelivery(ctx context.Context, commandID, operationID, attestation string) (DeliveryStatus, error) {
	if !store.SafeID(commandID) || !store.SafeID(operationID) {
		return DeliveryStatus{}, errors.New("exact attestation command and operation IDs required")
	}
	if len(attestation) < 1 || len(attestation) > store.MaxDocument || strings.TrimSpace(attestation) == "" {
		return DeliveryStatus{}, errors.New("an explicit human attestation describing the verified external state is required")
	}
	var kind, state string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT kind,state FROM operations WHERE id=? AND kind IN ('commit','push','draft_request')", operationID).Scan(&kind, &state); err != nil {
		return DeliveryStatus{}, errors.New("delivery operation not found")
	}
	if state != "executing" && state != "uncertain" {
		return DeliveryStatus{}, errors.New("only an executing or uncertain delivery can be closed by attestation")
	}
	args, _ := json.Marshal(map[string]string{"operation_id": operationID, "kind": kind, "attestation": attestation})
	receipt, err := e.DB.Command(ctx, store.Command{ID: commandID, Actor: "human", Kind: "delivery.close_unobserved", Args: args}, func(tx *store.Tx) (any, error) {
		var currentState string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM operations WHERE id=?", operationID).Scan(&currentState); err != nil ||
			(currentState != "executing" && currentState != "uncertain") {
			return nil, errors.New("delivery operation changed before attestation")
		}
		var deliveryState string
		hasRow := true
		if err := tx.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE operation_id=?", operationID).Scan(&deliveryState); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			hasRow = false
		}
		if hasRow {
			updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='failed' WHERE operation_id=? AND state IN ('pending','uncertain')", operationID)
			if err != nil {
				return nil, err
			}
			if count, err := updated.RowsAffected(); err != nil || count != 1 {
				return nil, errors.New("delivery journal changed before attestation")
			}
		}
		updated, err := tx.ExecContext(ctx, "UPDATE operations SET state='reconciled' WHERE id=? AND state IN ('executing','uncertain')", operationID)
		if err != nil {
			return nil, err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return nil, errors.New("delivery operation changed before attestation")
		}
		status := DeliveryStatus{OperationID: operationID, Kind: kind, State: "reconciled", Attestation: attestation}
		if hasRow {
			status.DeliveryState = "failed"
		}
		return status, nil
	})
	if err != nil {
		return DeliveryStatus{}, err
	}
	var closed DeliveryStatus
	if err := json.Unmarshal(receipt, &closed); err != nil {
		return DeliveryStatus{}, err
	}
	return closed, nil
}

// planDeliveryReconciliation reads the current exact external state of one
// delivery operation and decides whether the approved effect is positively
// observed. A push whose destination still holds the approved predecessor is
// re-attempted idempotently first (see retryUnobservedPush); nothing else
// closes automatically. The returned partial status carries the observed
// identity.
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
		// The ref is not at the journaled commit. A local compare-and-swap is
		// atomic, so this is either untouched or a third party moved it; the
		// difference is not provable and the plan ref must never be moved by
		// reconciliation.
		return DeliveryStatus{}, "", fmt.Errorf("plan ref is %s, not the approved commit %s; its local update is atomic, so resolve the ref yourself or attest the outcome", approvedRef(current), journaled)
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
			// The destination still holds the approved predecessor. The first
			// push either never landed or landed and was moved back; a
			// point-in-time read cannot tell. Re-attempt the identical non-force
			// OID:ref push, which is a no-op if it landed, delivers if it did
			// not, and is rejected if a third party moved the destination.
			return e.retryUnobservedPush(ctx, operationID, intent, record, env)
		}
		return DeliveryStatus{}, "", fmt.Errorf("remote ref is %s, not the approved predecessor %q or the approved head %q; a third party moved the destination, so resolve it yourself or attest the outcome",
			remote, intent.ExpectedRemoteOID, intent.HeadOID)
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
		return DeliveryStatus{}, "", errors.New("exact draft absent from a bounded listing; absence is not proof of non-delivery, so the operation stays open for manual remediation or attestation")
	}
	return DeliveryStatus{}, "", errors.New("unsupported delivery kind")
}

func approvedRef(oid string) string {
	if oid == "" {
		return "<absent>"
	}
	return oid
}

// retryUnobservedPush re-attempts the exact approved push and reports the
// result. The refspec is the deterministic approved OID and the same
// destination, without force, so the re-attempt is idempotent: Git reports
// everything up to date if the first push landed, delivers the object if it
// did not, and rejects a non-fast-forward if another writer moved the
// destination. It never overwrites and never widens the approved ref.
func (e *Engine) retryUnobservedPush(ctx context.Context, operationID string, intent PushIntent, record RepositoryRecord, env []string) (DeliveryStatus, string, error) {
	// The destination must still be the approved predecessor immediately
	// before the effect, so a ref that advanced during this read is not
	// pushed over.
	fresh, err := observedRemoteRef(ctx, record, intent.RemoteURL, intent.RemoteRef, env)
	if err != nil {
		return DeliveryStatus{}, "", err
	}
	if fresh != intent.ExpectedRemoteOID {
		// The destination advanced during this reconciliation. That is far more
		// likely the original push landing than a third party, so re-run
		// delivery-reconcile rather than attesting: attesting here is what
		// produced a false "no effect" record when the head was already there.
		if fresh == intent.HeadOID {
			return DeliveryStatus{Kind: "push", DeliveryID: pushDeliveryID(operationID), Observation: fresh}, "observed", nil
		}
		return DeliveryStatus{}, "", fmt.Errorf("remote ref changed to %s during reconciliation; it is neither the approved predecessor nor the approved head %s, so resolve the destination yourself before re-running reconcile",
			approvedRef(fresh), intent.HeadOID)
	}
	_, pushErr := isolatedRemoteGit(ctx, record, env, true, "-c", "push.default=nothing", "push", "--porcelain", "--no-verify", "--no-follow-tags",
		"--recurse-submodules=no", intent.RemoteURL, intent.HeadOID+":"+intent.RemoteRef)
	after, err := observedRemoteRef(ctx, record, intent.RemoteURL, intent.RemoteRef, env)
	if err != nil {
		return DeliveryStatus{}, "", err
	}
	if after == intent.HeadOID {
		return DeliveryStatus{Kind: "push", DeliveryID: pushDeliveryID(operationID), Observation: after}, "observed", nil
	}
	_ = pushErr
	return DeliveryStatus{}, "", fmt.Errorf("re-attempted the approved push but the destination is now %s; the approved head was not confirmed, so the operation stays open for manual resolution or attestation", approvedRef(after))
}
