package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"vigil/internal/store"
)

// resolveInput records a human response to a non-native input request. Native
// clarification remains owned by the supervisor session adapter.
func (e *Engine) resolveInput(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var request struct {
		RequestID string `json:"request_id"`
		Decision  string `json:"decision"`
		Answer    string `json:"answer,omitempty"`
	}
	if err := store.Decode(cmd.Payload, &request); err != nil {
		return nil, err
	}
	request.Answer = strings.TrimSpace(request.Answer)
	if !store.SafeID(request.RequestID) || (request.Decision != "answer" && request.Decision != "dismiss") {
		return nil, errors.New("valid input request and answer or dismiss decision required")
	}
	if request.Decision == "answer" && (request.Answer == "" || len(request.Answer) > 4096) {
		return nil, errors.New("bounded nonempty input answer required")
	}
	if request.Decision == "dismiss" && request.Answer != "" {
		return nil, errors.New("dismiss input decision cannot include an answer")
	}
	var state, kind, taskID, sessionID, nativeKey, contextRaw string
	var taskRevision int
	if err := tx.QueryRowContext(ctx, `SELECT state,kind,coalesce(task_id,''),coalesce(task_revision,0),coalesce(session_id,''),coalesce(native_request_key,''),context_json FROM requests WHERE id=?`, request.RequestID).Scan(&state, &kind, &taskID, &taskRevision, &sessionID, &nativeKey, &contextRaw); err != nil {
		return nil, err
	}
	if state != "pending" || kind != "input" || sessionID != "" || nativeKey != "" {
		return nil, errors.New("input request is stale or requires its native owner")
	}
	if taskID != "" {
		var current int
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM tasks WHERE id=?", taskID).Scan(&current); err != nil || current != taskRevision {
			return nil, errors.New("input request task revision is stale")
		}
	}
	var contextValue struct {
		ProposalID       string `json:"proposal_id"`
		ProposalRevision int    `json:"proposal_revision"`
	}
	if err := json.Unmarshal([]byte(contextRaw), &contextValue); err != nil {
		return nil, errors.New("input request context is invalid")
	}
	resultJSON, _ := json.Marshal(map[string]any{"decision": request.Decision, "answer": request.Answer, "actor": "human", "request_id": request.RequestID})
	requestState := "resolved"
	if request.Decision == "dismiss" {
		requestState = "denied"
	}
	updated, err := tx.ExecContext(ctx, `UPDATE requests SET state=?,result_json=?,resolved_at=? WHERE id=? AND state='pending'`, requestState, string(resultJSON), store.Now(), request.RequestID)
	if err != nil {
		return nil, err
	}
	if count, countErr := updated.RowsAffected(); countErr != nil || count != 1 {
		return nil, errors.New("input request changed while resolving")
	}
	proposalState := ""
	if contextValue.ProposalID != "" {
		if !store.SafeID(contextValue.ProposalID) || contextValue.ProposalRevision < 1 {
			return nil, errors.New("input request has invalid proposal binding")
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM planning_proposals WHERE id=? AND revision=?`, contextValue.ProposalID, contextValue.ProposalRevision).Scan(&state); err != nil {
			return nil, err
		}
		if state != "proposed" {
			return nil, errors.New("input request proposal is no longer pending")
		}
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM requests WHERE kind='input' AND state='pending' AND json_extract(context_json,'$.proposal_id')=? AND json_extract(context_json,'$.proposal_revision')=?`, contextValue.ProposalID, contextValue.ProposalRevision).Scan(&pending); err != nil {
			return nil, err
		}
		if request.Decision == "dismiss" || pending == 0 {
			proposalState = "stale"
			if _, err := tx.ExecContext(ctx, `UPDATE planning_proposals SET state='stale' WHERE id=? AND revision=? AND state='proposed'`, contextValue.ProposalID, contextValue.ProposalRevision); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE requests SET state='cancelled',resolved_at=? WHERE state='pending' AND json_extract(context_json,'$.proposal_id')=? AND json_extract(context_json,'$.proposal_revision')=?`, store.Now(), contextValue.ProposalID, contextValue.ProposalRevision); err != nil {
				return nil, err
			}
		}
	}
	return map[string]any{"request_id": request.RequestID, "state": requestState, "proposal_state": proposalState}, nil
}
