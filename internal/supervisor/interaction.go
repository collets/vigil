package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/store"
)

const maxClarificationBytes = 4096

// ClarificationDelivery is an application-owned live native-session capability.
// Implementations must bind the opaque key to the exact in-memory native request;
// project or UI data never reconstructs a provider request handle.
type ClarificationDelivery interface {
	DeliverClarification(context.Context, NativeClarificationDelivery) error
	InspectClarification(context.Context, NativeClarificationDelivery) (string, error)
}

type NativeClarificationDelivery struct {
	RunID               string `json:"run_id"`
	GenerationID        string `json:"generation_id"`
	TransportGeneration string `json:"transport_generation"`
	SessionID           string `json:"session_id"`
	NativeRequestKey    string `json:"native_request_key"`
	Decision            string `json:"decision"`
	Answer              string `json:"answer,omitempty"`
}

type PersistClarificationRequest struct {
	CommandID        string          `json:"command_id"`
	ExpectedRevision int             `json:"expected_revision"`
	Prepared         PreparedRun     `json:"prepared"`
	SessionID        string          `json:"session_id"`
	NativeRequestKey string          `json:"native_request_key"`
	Prompt           json.RawMessage `json:"prompt"`
	Deadline         int64           `json:"deadline"`
}

type ClarificationAnswerRequest struct {
	CommandID        string `json:"command_id"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	SessionID        string `json:"session_id"`
	NativeRequestKey string `json:"native_request_key"`
	Decision         string `json:"decision"`
	Answer           string `json:"answer,omitempty"`
}

type ClarificationReceipt struct {
	RequestID string `json:"request_id"`
	State     string `json:"state"`
	Delivery  string `json:"delivery"`
	Repeated  bool   `json:"repeated,omitempty"`
}

type ClarificationReconcileRequest struct {
	CommandID        string `json:"command_id"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
}

// InteractiveOwner holds the live owner-only capabilities needed by dashboard
// recovery and native clarification actions. A dashboard without this object can
// display persisted requests but cannot manufacture an inspector or delivery path.
type InteractiveOwner struct {
	Engine         *core.Engine
	Owner          *coordinator.Owner
	Recovery       RecoveryInspector
	Clarifications ClarificationDelivery
}

func (o *InteractiveOwner) validate() error {
	if o == nil || o.Engine == nil || o.Engine.DB == nil || o.Owner == nil || o.Owner.ID == "" {
		return errors.New("live interactive owner is unavailable")
	}
	return o.Owner.ValidateLive()
}

func (o *InteractiveOwner) ChooseRecovery(ctx context.Context, request RecoveryChoiceRequest) (RecoveryChoiceReceipt, error) {
	if err := o.validate(); err != nil {
		return RecoveryChoiceReceipt{}, err
	}
	var receipt RecoveryChoiceReceipt
	err := o.Owner.HoldControl(func() error {
		if request.Mode != "remain_blocked" && o.Recovery == nil {
			return errors.New("owner recovery inspector is unavailable")
		}
		var err error
		receipt, err = (&Runner{Engine: o.Engine}).ChooseRecovery(ctx, request, o.Recovery)
		return err
	})
	return receipt, err
}

func (o *InteractiveOwner) PersistClarification(ctx context.Context, request PersistClarificationRequest) (string, error) {
	if err := o.validate(); err != nil {
		return "", err
	}
	var requestID string
	err := o.Owner.HoldControl(func() error {
		var err error
		requestID, err = o.persistClarification(ctx, request)
		return err
	})
	return requestID, err
}

func (o *InteractiveOwner) persistClarification(ctx context.Context, request PersistClarificationRequest) (string, error) {
	if !store.SafeID(request.CommandID) || request.ExpectedRevision < 1 || !store.SafeID(request.Prepared.RunID) || !store.SafeID(request.Prepared.GenerationID) || !store.SafeID(request.SessionID) || !store.SafeID(request.NativeRequestKey) || request.Deadline <= store.Now() || len(request.Prompt) == 0 || len(request.Prompt) > core.MaxPlanningDocument || !json.Valid(request.Prompt) {
		return "", errors.New("valid bounded exact native clarification required")
	}
	args, _ := json.Marshal(map[string]any{
		"run_id": request.Prepared.RunID, "generation_id": request.Prepared.GenerationID,
		"session_id": request.SessionID, "native_request_key": request.NativeRequestKey,
		"prompt_digest": store.Digest(request.Prompt), "deadline": request.Deadline,
	})
	command := store.Command{ID: request.CommandID, Actor: string(core.Core), Kind: "native.clarification.persist", Args: args}
	receipt, err := o.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision, taskRevision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", o.Engine.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != request.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
		}
		var planID, taskID, runtimeKind, transportGeneration string
		if err := tx.QueryRowContext(ctx, `SELECT r.plan_id,r.task_id,g.runtime_kind,g.transport_generation,t.revision FROM sessions s JOIN run_generations g ON g.id=? AND g.run_id=s.run_id AND g.transport_generation=s.generation JOIN runs r ON r.id=g.run_id JOIN tasks t ON t.id=r.task_id WHERE s.id=? AND s.run_id=?`, request.Prepared.GenerationID, request.SessionID, request.Prepared.RunID).Scan(&planID, &taskID, &runtimeKind, &transportGeneration, &taskRevision); err != nil {
			return nil, err
		}
		if planID != request.Prepared.PlanID || taskID != request.Prepared.TaskID || runtimeKind != request.Prepared.RuntimeKind || transportGeneration != request.Prepared.TransportGeneration {
			return nil, errors.New("clarification prepared run is foreign or stale")
		}
		requestID := store.ID()
		contextJSON, _ := json.Marshal(map[string]any{
			"generation_id":        request.Prepared.GenerationID,
			"transport_generation": request.Prepared.TransportGeneration,
			"owner_id":             o.Owner.ID,
			"prompt":               json.RawMessage(request.Prompt),
			"untrusted_context":    true,
		})
		if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,run_id,session_id,native_request_key,context_json,blocking,created_at,deadline) VALUES(?,'input','pending',?,?,?,?,?,?,?,1,?,?)`, requestID, request.Prepared.PlanID, request.Prepared.TaskID, taskRevision, request.Prepared.RunID, request.SessionID, request.NativeRequestKey, string(contextJSON), store.Now(), request.Deadline); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", o.Engine.ProjectID); err != nil {
			return nil, err
		}
		return map[string]string{"request_id": requestID}, nil
	})
	if err != nil {
		return "", err
	}
	var out map[string]string
	if err := json.Unmarshal(receipt, &out); err != nil || !store.SafeID(out["request_id"]) {
		return "", errors.New("invalid clarification persistence receipt")
	}
	return out["request_id"], nil
}

func (o *InteractiveOwner) AnswerClarification(ctx context.Context, request ClarificationAnswerRequest) (ClarificationReceipt, error) {
	if err := o.validate(); err != nil {
		return ClarificationReceipt{}, err
	}
	var receipt ClarificationReceipt
	err := o.Owner.HoldControl(func() error {
		var err error
		receipt, err = o.answerClarification(ctx, request)
		return err
	})
	return receipt, err
}

func (o *InteractiveOwner) answerClarification(ctx context.Context, request ClarificationAnswerRequest) (ClarificationReceipt, error) {
	var out ClarificationReceipt
	if o.Clarifications == nil {
		return out, errors.New("owner clarification delivery is unavailable")
	}
	request.Answer = strings.TrimSpace(request.Answer)
	if !store.SafeID(request.CommandID) || request.ExpectedRevision < 1 || !store.SafeID(request.RequestID) || !store.SafeID(request.SessionID) || !store.SafeID(request.NativeRequestKey) || (request.Decision != "answer" && request.Decision != "cancel") || (request.Decision == "answer" && (request.Answer == "" || len(request.Answer) > maxClarificationBytes)) || (request.Decision == "cancel" && request.Answer != "") {
		return out, errors.New("valid exact clarification answer or cancellation required")
	}
	args, _ := json.Marshal(map[string]any{"request_id": request.RequestID, "session_id": request.SessionID, "native_request_key": request.NativeRequestKey, "decision": request.Decision, "answer_digest": store.Digest([]byte(request.Answer)), "expected_revision": request.ExpectedRevision})
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "native.clarification.answer", Args: args}
	if receipt, repeated, err := o.Engine.DB.Receipt(ctx, command); err != nil {
		return out, err
	} else if repeated {
		if err := json.Unmarshal(receipt, &out); err != nil {
			return out, err
		}
		var state, raw string
		if err := o.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state,coalesce(result_json,'{}') FROM requests WHERE id=?", request.RequestID).Scan(&state, &raw); err != nil {
			return out, err
		}
		var result struct {
			Delivery string `json:"delivery"`
		}
		_ = json.Unmarshal([]byte(raw), &result)
		out.State, out.Delivery, out.Repeated = state, result.Delivery, true
		if result.Delivery != "delivered" {
			return out, errors.New("prior clarification delivery is not proven; automatic replay disabled")
		}
		return out, nil
	}
	preparedReceipt, err := o.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", o.Engine.ProjectID).Scan(&revision); err != nil {
			return nil, err
		}
		if revision != request.ExpectedRevision {
			return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
		}
		var state, kind, runID, sessionID, nativeKey, contextRaw string
		var deadline int64
		var resultEmpty int
		if err := tx.QueryRowContext(ctx, `SELECT state,kind,coalesce(run_id,''),coalesce(session_id,''),coalesce(native_request_key,''),coalesce(deadline,0),context_json,result_json IS NULL FROM requests WHERE id=?`, request.RequestID).Scan(&state, &kind, &runID, &sessionID, &nativeKey, &deadline, &contextRaw, &resultEmpty); err != nil {
			return nil, err
		}
		var persistedContext struct {
			OwnerID             string `json:"owner_id"`
			GenerationID        string `json:"generation_id"`
			TransportGeneration string `json:"transport_generation"`
		}
		if json.Unmarshal([]byte(contextRaw), &persistedContext) != nil || persistedContext.OwnerID != o.Owner.ID {
			return nil, errors.New("native clarification belongs to a different owner")
		}
		if state != "pending" || kind != "input" || sessionID != request.SessionID || nativeKey != request.NativeRequestKey || runID == "" || deadline <= store.Now() || resultEmpty != 1 {
			return nil, errors.New("displayed native clarification is stale, expired or foreign")
		}
		var generationState string
		if err := tx.QueryRowContext(ctx, `SELECT g.state FROM run_generations g JOIN sessions s ON s.id=? AND s.run_id=g.run_id AND s.generation=g.transport_generation WHERE g.id=? AND g.run_id=? AND g.transport_generation=? AND g.ordinal=(SELECT max(latest.ordinal) FROM run_generations latest WHERE latest.run_id=g.run_id)`, request.SessionID, persistedContext.GenerationID, runID, persistedContext.TransportGeneration).Scan(&generationState); err != nil {
			return nil, errors.New("native clarification generation is no longer current")
		}
		if generationState == "terminal" || generationState == "contained" || generationState == "failed" {
			return nil, errors.New("native clarification generation is no longer answerable")
		}
		resultJSON, _ := json.Marshal(map[string]any{"delivery": "prepared", "decision": request.Decision, "answer_digest": store.Digest([]byte(request.Answer)), "owner_id": o.Owner.ID})
		result, err := tx.ExecContext(ctx, "UPDATE requests SET result_json=? WHERE id=? AND state='pending' AND result_json IS NULL", string(resultJSON), request.RequestID)
		if err != nil {
			return nil, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return nil, errors.New("clarification answer was concurrently claimed")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", o.Engine.ProjectID); err != nil {
			return nil, err
		}
		return ClarificationReceipt{RequestID: request.RequestID, State: "pending", Delivery: "prepared"}, nil
	})
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(preparedReceipt, &out); err != nil {
		return out, err
	}
	var runID string
	if err := o.Engine.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(run_id,'') FROM requests WHERE id=?", request.RequestID).Scan(&runID); err != nil {
		return out, err
	}
	var persistedContext struct {
		GenerationID        string `json:"generation_id"`
		TransportGeneration string `json:"transport_generation"`
	}
	var contextRaw string
	if err := o.Engine.DB.SQL.QueryRowContext(ctx, "SELECT context_json FROM requests WHERE id=?", request.RequestID).Scan(&contextRaw); err != nil || json.Unmarshal([]byte(contextRaw), &persistedContext) != nil {
		return out, errors.New("clarification delivery context is unavailable")
	}
	deliveryErr := o.Clarifications.DeliverClarification(ctx, NativeClarificationDelivery{RunID: runID, GenerationID: persistedContext.GenerationID, TransportGeneration: persistedContext.TransportGeneration, SessionID: request.SessionID, NativeRequestKey: request.NativeRequestKey, Decision: request.Decision, Answer: request.Answer})
	deliveryState, requestState := "delivered", "resolved"
	if deliveryErr != nil {
		deliveryState, requestState = "uncertain", "cancelled"
	}
	writeErr := o.Engine.DB.Write(context.WithoutCancel(ctx), func(tx *store.Tx) error {
		resultJSON, _ := json.Marshal(map[string]any{"delivery": deliveryState, "decision": request.Decision, "answer_digest": store.Digest([]byte(request.Answer)), "owner_id": o.Owner.ID})
		result, err := tx.ExecContext(context.WithoutCancel(ctx), "UPDATE requests SET state=?,result_json=?,resolved_at=? WHERE id=? AND state='pending'", requestState, string(resultJSON), store.Now(), request.RequestID)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			return errors.New("clarification request changed during external delivery")
		}
		_, err = tx.ExecContext(context.WithoutCancel(ctx), "UPDATE project SET revision=revision+1 WHERE id=?", o.Engine.ProjectID)
		return err
	})
	out.State, out.Delivery = requestState, deliveryState
	if writeErr != nil {
		return out, fmt.Errorf("clarification delivery outcome persistence failed: %w", writeErr)
	}
	if deliveryErr != nil {
		return out, fmt.Errorf("clarification delivery uncertain; automatic replay disabled: %w", deliveryErr)
	}
	return out, nil
}

// ReconcileClarification requires the live delivery adapter to inspect the exact
// native generation/request after a crash or uncertain transport result. It never
// accepts a caller-declared outcome and never replays an answer. Independently
// proven non-delivery only makes the original request answerable again.
func (o *InteractiveOwner) ReconcileClarification(ctx context.Context, request ClarificationReconcileRequest) (ClarificationReceipt, error) {
	var out ClarificationReceipt
	if err := o.validate(); err != nil {
		return out, err
	}
	err := o.Owner.HoldControl(func() error {
		if !store.SafeID(request.CommandID) || !store.SafeID(request.RequestID) || request.ExpectedRevision < 1 || o.Clarifications == nil {
			return errors.New("valid explicit clarification reconciliation required")
		}
		var contextRaw, runID, sessionID, nativeKey string
		if err := o.Engine.DB.SQL.QueryRowContext(ctx, "SELECT context_json,coalesce(run_id,''),coalesce(session_id,''),coalesce(native_request_key,'') FROM requests WHERE id=?", request.RequestID).Scan(&contextRaw, &runID, &sessionID, &nativeKey); err != nil {
			return err
		}
		var contextBinding struct {
			OwnerID             string `json:"owner_id"`
			GenerationID        string `json:"generation_id"`
			TransportGeneration string `json:"transport_generation"`
		}
		if json.Unmarshal([]byte(contextRaw), &contextBinding) != nil || !store.SafeID(contextBinding.OwnerID) || !store.SafeID(contextBinding.GenerationID) || contextBinding.TransportGeneration == "" || runID == "" || sessionID == "" || nativeKey == "" {
			return errors.New("clarification reconciliation binding is unavailable")
		}
		reconcile := func() error {
			observation, err := o.Clarifications.InspectClarification(ctx, NativeClarificationDelivery{RunID: runID, GenerationID: contextBinding.GenerationID, TransportGeneration: contextBinding.TransportGeneration, SessionID: sessionID, NativeRequestKey: nativeKey})
			if err != nil {
				return err
			}
			if observation != "delivered" && observation != "proven_not_delivered" {
				return errors.New("clarification delivery remains uncertain")
			}
			args, _ := json.Marshal(request)
			receipt, err := o.Engine.DB.Command(ctx, store.Command{ID: request.CommandID, Actor: string(core.Core), Kind: "native.clarification.reconcile", Args: args}, func(tx *store.Tx) (any, error) {
				var revision int
				if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", o.Engine.ProjectID).Scan(&revision); err != nil {
					return nil, err
				}
				if revision != request.ExpectedRevision {
					return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
				}
				var state, raw, contextRaw string
				if err := tx.QueryRowContext(ctx, "SELECT state,coalesce(result_json,'{}'),context_json FROM requests WHERE id=?", request.RequestID).Scan(&state, &raw, &contextRaw); err != nil {
					return nil, err
				}
				var prior struct {
					Delivery string `json:"delivery"`
				}
				if json.Unmarshal([]byte(raw), &prior) != nil || (prior.Delivery != "prepared" && prior.Delivery != "uncertain") || (state != "pending" && state != "cancelled") {
					return nil, errors.New("clarification request is not owner-reconcilable")
				}
				if observation == "proven_not_delivered" {
					if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='pending',result_json=NULL,resolved_at=NULL,context_json=json_set(context_json,'$.owner_id',?) WHERE id=?", o.Owner.ID, request.RequestID); err != nil {
						return nil, err
					}
					out = ClarificationReceipt{RequestID: request.RequestID, State: "pending", Delivery: "proven_not_delivered"}
				} else {
					resultJSON, _ := json.Marshal(map[string]any{"delivery": "delivered", "reconciled": true, "owner_id": o.Owner.ID})
					if _, err := tx.ExecContext(ctx, "UPDATE requests SET state='resolved',result_json=?,resolved_at=? WHERE id=?", string(resultJSON), store.Now(), request.RequestID); err != nil {
						return nil, err
					}
					out = ClarificationReceipt{RequestID: request.RequestID, State: "resolved", Delivery: "delivered"}
				}
				if _, err := tx.ExecContext(ctx, "UPDATE project SET revision=revision+1 WHERE id=?", o.Engine.ProjectID); err != nil {
					return nil, err
				}
				return out, nil
			})
			if err != nil {
				return err
			}
			return json.Unmarshal(receipt, &out)
		}
		if contextBinding.OwnerID != o.Owner.ID {
			return o.Owner.Coordinator.FenceRetiredOwner(ctx, contextBinding.OwnerID, reconcile)
		}
		return reconcile()
	})
	return out, err
}
