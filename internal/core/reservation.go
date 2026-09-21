package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"vigil/internal/coordinator"
	"vigil/internal/store"
	"vigil/internal/workspace"
)

// Reservation is a project-side journal of host coordinator effects. It never
// authorizes native launch and never claims cross-database atomicity. A lost
// owner leaves its resources quarantined; a new owner cannot resume this intent.
type Reservation struct {
	OperationID string               `json:"operation_id"`
	OwnerID     string               `json:"owner_id"`
	RunID       string               `json:"run_id"`
	Endpoint    string               `json:"endpoint_id"`
	Root        workspace.Identity   `json:"root"`
	Roots       []workspace.Identity `json:"roots"`
	Claims      []coordinator.Claim  `json:"claims"`
	Ticket      coordinator.Ticket   `json:"ticket"`
	Phase       string               `json:"phase"`
}

// ReserveResources acquires the primary registered checkout before queuing for
// endpoint capacity. Stable coordinator IDs recover a same-owner interrupted
// attempt without duplicate claims or slots. The caller retains and closes the
// Owner; persistence failure must stop dispatch, never release by assumption.
func (e *Engine) ReserveResources(ctx context.Context, owner *coordinator.Owner, operationID, runID, endpoint string) (Reservation, error) {
	return e.reserveResources(ctx, owner, operationID, runID, endpoint, nil)
}

func (e *Engine) reserveResources(ctx context.Context, owner *coordinator.Owner, operationID, runID, endpoint string, fault func(string) error) (Reservation, error) {
	var result Reservation
	if owner == nil || owner.Coordinator == nil || owner.Coordinator.DB == nil || !store.SafeID(owner.ID) || !store.SafeID(operationID) || !store.SafeID(runID) || !store.SafeID(endpoint) {
		return result, errors.New("explicit reservation identities required")
	}
	roots, err := e.participatingRoots(ctx, owner)
	if err != nil {
		return result, err
	}
	intent := Reservation{OperationID: operationID, OwnerID: owner.ID, RunID: runID, Endpoint: endpoint, Root: roots[0], Roots: roots, Phase: "intent"}
	args, _ := json.Marshal(intent)
	_, err = e.DB.Command(ctx, store.Command{ID: "reservation:" + store.Digest([]byte(operationID)), Actor: string(Core), Kind: "resource.reserve", Args: args}, func(tx *store.Tx) (any, error) {
		if _, err := configAt(ctx, tx); err != nil {
			return nil, err
		}
		var epoch int
		if err := tx.QueryRowContext(ctx, "SELECT policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&epoch); err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(roots))
		for _, root := range roots {
			keys = append(keys, root.Key+"\x00"+root.CommonGit)
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,evidence_json,created_at) VALUES(?,'resource.reserve',?,?,?,'prepared',?,?)", operationID, store.Digest([]byte(strings.Join(keys, "\x00"))), store.Digest(args), epoch, string(args), store.Now())
		return map[string]string{"operation_id": operationID}, err
	})
	if err != nil {
		return result, err
	}
	checkpoint := func(phase string) error {
		if fault != nil {
			return fault(phase)
		}
		return nil
	}
	if err := checkpoint("after_intent"); err != nil {
		return intent, err
	}
	result, err = e.Reservation(ctx, operationID)
	if err != nil {
		return result, err
	}
	if result.OwnerID != owner.ID || result.RunID != runID || result.Endpoint != endpoint {
		return result, store.ErrConflict
	}
	if len(result.Roots) == 0 {
		result.Roots = []workspace.Identity{result.Root}
	}
	for _, root := range result.Roots {
		if err := root.Validate(); err != nil {
			return result, err
		}
	}
	// Check policy before any coordinator effect; the same check runs before
	// recording each observation. A policy race leaves inspectable ownership.
	if err := e.recordReservation(ctx, result, false); err != nil {
		return result, err
	}
	sharedID := store.Digest([]byte(e.ProjectID + "\x00" + operationID))
	claims, err := owner.Claim(ctx, e.ProjectID, sharedID, result.Roots)
	if err != nil {
		return result, err
	}
	if err := checkpoint("after_claim"); err != nil {
		return result, err
	}
	result.Claims = claims
	result.Phase = "workspace_owned"
	if err := e.recordReservation(ctx, result, false); err != nil {
		return result, err
	}
	ticket, err := owner.Enqueue(ctx, sharedID, e.ProjectID, runID, endpoint)
	if err != nil {
		return result, err
	}
	if err := checkpoint("after_enqueue"); err != nil {
		return result, err
	}
	result.Ticket = ticket
	result.Phase = "capacity_wait"
	if err := e.recordReservation(ctx, result, false); err != nil {
		return result, err
	}
	ticket, err = owner.Reserve(ctx, ticket)
	if err != nil {
		return result, err
	}
	if err := checkpoint("after_slot"); err != nil {
		return result, err
	}
	result.Ticket = ticket
	result.Phase = "reserved"
	if err := e.recordReservation(ctx, result, true); err != nil {
		return result, err
	}
	if err := checkpoint("after_observation"); err != nil {
		return result, err
	}
	return result, nil
}

func (e *Engine) participatingRoots(ctx context.Context, owner *coordinator.Owner) ([]workspace.Identity, error) {
	var projectRoot, registeredJSON string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT root FROM project WHERE id=?", e.ProjectID).Scan(&projectRoot); err != nil {
		return nil, err
	}
	if err := owner.Coordinator.DB.SQL.QueryRowContext(ctx, "SELECT identity_json FROM project_registry WHERE id=? AND root=?", e.ProjectID, projectRoot).Scan(&registeredJSON); err != nil {
		return nil, err
	}
	var registered workspace.Identity
	if err := json.Unmarshal([]byte(registeredJSON), &registered); err != nil {
		return nil, err
	}
	rows, err := e.DB.SQL.QueryContext(ctx, `SELECT rr.root_identity_json FROM repositories r JOIN repository_revisions rr ON rr.repository_id=r.id WHERE r.inclusion='participating' AND rr.revision=(SELECT max(x.revision) FROM repository_revisions x WHERE x.repository_id=r.id) ORDER BY r.root,r.id`)
	if err != nil {
		return nil, err
	}
	var roots []workspace.Identity
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		var identity workspace.Identity
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &identity); err != nil {
			rows.Close()
			return nil, err
		}
		if !seen[identity.Key] {
			roots = append(roots, identity)
			seen[identity.Key] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if !seen[registered.Key] {
		roots = append([]workspace.Identity{registered}, roots...)
	}
	if len(roots) == 0 {
		return nil, errors.New("no participating workspace roots")
	}
	for _, identity := range roots {
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		current, err := workspace.Inspect(ctx, identity.Root)
		if err != nil {
			return nil, err
		}
		if current.Key != identity.Key || current.CommonGit != identity.CommonGit || current.CommonGitPath != identity.CommonGitPath {
			return nil, errors.New("participating repository identity changed; reconcile before reserving")
		}
	}
	return roots, nil
}

func (e *Engine) Reservation(ctx context.Context, id string) (Reservation, error) {
	var result Reservation
	var raw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT evidence_json FROM operations WHERE id=? AND kind='resource.reserve'", id).Scan(&raw); err != nil {
		return result, err
	}
	err := json.Unmarshal([]byte(raw), &result)
	return result, err
}

func (e *Engine) recordReservation(ctx context.Context, r Reservation, complete bool) error {
	return e.DB.Write(ctx, func(tx *store.Tx) error {
		var epoch, current int
		var raw, state string
		if err := tx.QueryRowContext(ctx, "SELECT policy_epoch,evidence_json,state FROM operations WHERE id=? AND kind='resource.reserve'", r.OperationID).Scan(&epoch, &raw, &state); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT policy_epoch FROM project WHERE id=?", e.ProjectID).Scan(&current); err != nil {
			return err
		}
		var prior Reservation
		if err := json.Unmarshal([]byte(raw), &prior); err != nil {
			return err
		}
		if epoch != current || prior.OwnerID != r.OwnerID || (state != "prepared" && state != "executing" && state != "observed") {
			return errors.New("stale or retired resource intent; reconcile ownership")
		}
		// Retrying a completed acquisition verifies actual ownership above but
		// must not regress its durable observation to an earlier phase.
		if state == "observed" && !complete {
			return nil
		}
		next := "executing"
		if complete {
			next = "observed"
		}
		body, _ := json.Marshal(r)
		if len(body) > store.MaxDocument {
			return errors.New("reservation evidence exceeds document limit")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state=?,evidence_json=? WHERE id=?", next, string(body), r.OperationID); err != nil {
			return err
		}
		if prior.Phase != r.Phase {
			payload, _ := json.Marshal(map[string]string{"operation_id": r.OperationID, "phase": r.Phase, "owner_id": r.OwnerID})
			_, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,kind,occurred_at,payload_json) VALUES(1,'resource_observed',?,?)", store.Now(), string(payload))
			return err
		}
		return nil
	})
}

// RetireReservation records a trusted recovery observation without releasing
// host resources. Reconciliation is deliberately separate and evidence-bearing.
func (e *Engine) RetireReservation(ctx context.Context, id, ownerID string) error {
	if !store.SafeID(id) || !store.SafeID(ownerID) {
		return errors.New("reservation identity required")
	}
	return e.DB.Write(ctx, func(tx *store.Tx) error {
		var raw, state string
		if err := tx.QueryRowContext(ctx, "SELECT evidence_json,state FROM operations WHERE id=? AND kind='resource.reserve'", id).Scan(&raw, &state); err != nil {
			return err
		}
		var r Reservation
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return err
		}
		if r.OwnerID != ownerID {
			return errors.New("foreign reservation owner")
		}
		if state == "uncertain" {
			return nil
		}
		r.Phase = "uncertain"
		body, _ := json.Marshal(r)
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain',evidence_json=? WHERE id=?", string(body), id); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"operation_id": id, "owner_id": ownerID})
		_, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,kind,occurred_at,payload_json) VALUES(1,'resource_uncertain',?,?)", store.Now(), string(payload))
		return err
	})
}
