package core

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"vigil/internal/policy"
	"vigil/internal/store"
)

type OperationRequest struct {
	Category        string `json:"category"`
	ResourceDigest  string `json:"resource_digest"`
	ArgumentsDigest string `json:"arguments_digest"`
	PlanID          string `json:"plan_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	TaskRevision    int    `json:"task_revision,omitempty"`
}
type GrantRequest struct {
	RequestID string `json:"request_id"`
	Scope     string `json:"scope"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	Decision  string `json:"decision"`
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func revisionValue(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
func validDigest(s string) bool { _, err := hex.DecodeString(s); return len(s) == 64 && err == nil }
func configAt(ctx context.Context, tx *store.Tx) (policy.Config, error) {
	var raw string
	var c policy.Config
	err := tx.QueryRowContext(ctx, "SELECT s.resolved_json FROM project_configurations c JOIN config_snapshots s ON s.id=c.config_id ORDER BY c.revision DESC LIMIT 1").Scan(&raw)
	if err != nil {
		return c, errors.New("explicit project configuration required before permissions")
	}
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}
func validateOperation(ctx context.Context, tx *store.Tx, p OperationRequest, c policy.Config) error {
	if !policy.Contains(policy.OperationCategories, p.Category) {
		return errors.New("unknown or forbidden permission category")
	}
	if policy.Contains(c.Deny, p.Category) || policy.Contains(c.Deny, "*") {
		return errors.New("project restriction denies this category")
	}
	if !validDigest(p.ResourceDigest) || !validDigest(p.ArgumentsDigest) {
		return errors.New("exact SHA-256 resource and arguments digests required")
	}
	layers := []policy.Layer{{Name: "project", Restrictions: c.Restrictions}}
	if p.TaskID != "" {
		var plan string
		var revision int
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT t.plan_id,t.revision,r.definition_json FROM tasks t JOIN task_revisions r ON r.task_id=t.id AND r.revision=t.revision WHERE t.id=?", p.TaskID).Scan(&plan, &revision, &raw); err != nil {
			return err
		}
		if p.PlanID != plan || p.TaskRevision != revision {
			return errors.New("stale or foreign operation task context")
		}
		var task policy.Task
		if err := json.Unmarshal([]byte(raw), &task); err != nil {
			return err
		}
		layers = append(layers, policy.Layer{Name: "task", Restrictions: task.Restrictions})
	} else if p.TaskRevision != 0 {
		return errors.New("task revision requires task ID")
	}
	if p.PlanID != "" {
		var raw string
		if err := tx.QueryRowContext(ctx, "SELECT r.definition_json FROM plans p JOIN plan_revisions r ON r.plan_id=p.id AND r.revision=p.revision WHERE p.id=?", p.PlanID).Scan(&raw); err != nil {
			return err
		}
		var plan Plan
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			return err
		}
		layers = append(layers, policy.Layer{Name: "plan", Restrictions: plan.Restrictions})
	}
	resolved, err := policy.Resolve(layers)
	if err != nil {
		return err
	}
	if !resolved.Restrictions.Allows("operation_categories", p.Category) {
		return errors.New("enclosing restriction denies this operation category")
	}
	return nil
}
func (e *Engine) permission(ctx context.Context, tx *store.Tx, actor Authority, cmd Envelope, epoch int) (any, error) {
	c, err := configAt(ctx, tx)
	if err != nil {
		return nil, err
	}
	now := store.Now()
	switch cmd.Kind {
	case "operation.request":
		var p OperationRequest
		if err := store.Decode(cmd.Payload, &p); err != nil {
			return nil, err
		}
		if err := validateOperation(ctx, tx, p, c); err != nil {
			return nil, err
		}
		op, request := store.ID(), store.ID()
		raw, _ := json.Marshal(p)
		if _, err := tx.ExecContext(ctx, "INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,task_id,evidence_json,created_at) VALUES(?,?,?,?,?,'prepared',?,?,?,?)", op, p.Category, p.ResourceDigest, p.ArgumentsDigest, epoch, nullable(p.PlanID), nullable(p.TaskID), string(raw), now); err != nil {
			return nil, err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO requests(id,kind,state,plan_id,task_id,task_revision,operation_id,context_json,blocking,created_at,deadline) VALUES(?,'approval','pending',?,?,?,?,?,1,?,?)", request, nullable(p.PlanID), nullable(p.TaskID), revisionValue(p.TaskRevision), op, string(raw), now, now+1800000)
		return map[string]string{"operation_id": op, "request_id": request, "state": "prepared"}, err
	case "permission.grant":
		if actor != Human {
			return nil, errors.New("only human can grant authority")
		}
		var p GrantRequest
		if err := store.Decode(cmd.Payload, &p); err != nil {
			return nil, err
		}
		if p.Decision != "allow" && p.Decision != "deny" {
			return nil, errors.New("decision must be allow or deny")
		}
		var state, op, raw string
		var deadline int64
		if err := tx.QueryRowContext(ctx, "SELECT state,operation_id,context_json,deadline FROM requests WHERE id=?", p.RequestID).Scan(&state, &op, &raw, &deadline); err != nil {
			return nil, err
		}
		if state != "pending" || deadline <= now {
			return nil, errors.New("request resolved, cancelled or expired")
		}
		var request OperationRequest
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			return nil, err
		}
		if err := validateOperation(ctx, tx, request, c); err != nil {
			return nil, err
		}
		if p.Decision == "deny" {
			if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='cancelled' WHERE id=? AND state='prepared'", op); err != nil {
				return nil, err
			}
			_, err := tx.ExecContext(ctx, "UPDATE requests SET state='denied',result_json='{"+`"decision":"deny"`+"}',resolved_at=? WHERE id=?", now, p.RequestID)
			return map[string]string{"state": "denied"}, err
		}
		if !policy.Contains([]string{"once", "task", "plan", "project_permanent"}, p.Scope) {
			return nil, errors.New("unsupported scope; global grants require shared authorization integration")
		}
		if p.Scope == "task" && request.TaskID == "" || p.Scope == "plan" && request.PlanID == "" {
			return nil, errors.New("grant scope lacks corresponding context")
		}
		if p.ExpiresAt != 0 && p.ExpiresAt <= now {
			return nil, errors.New("grant expiry must be in the future")
		}
		grant := store.ID()
		var expiry any
		if p.ExpiresAt != 0 {
			expiry = p.ExpiresAt
		} else if p.Scope == "once" {
			expiry = deadline
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO grants(id,category,scope,plan_id,task_id,resource_pattern,revision_policy_json,policy_epoch,granted_by,granted_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", grant, request.Category, p.Scope, nullable(request.PlanID), nullable(request.TaskID), request.ResourceDigest, raw, epoch, "human", now, expiry)
		if err != nil {
			return nil, err
		}
		result, _ := json.Marshal(map[string]string{"decision": "allow", "grant_id": grant})
		_, err = tx.ExecContext(ctx, "UPDATE requests SET state='resolved',grant_id=?,result_json=?,resolved_at=? WHERE id=?", grant, string(result), now, p.RequestID)
		return map[string]string{"grant_id": grant, "state": "resolved"}, err
	case "permission.revoke":
		var p struct {
			GrantID string `json:"grant_id"`
		}
		if err := store.Decode(cmd.Payload, &p); err != nil {
			return nil, err
		}
		r, err := tx.ExecContext(ctx, "UPDATE grants SET revoked_at=? WHERE id=? AND revoked_at IS NULL", now, p.GrantID)
		if err != nil {
			return nil, err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return nil, errors.New("grant missing or already revoked")
		}
		return map[string]string{"grant_id": p.GrantID, "state": "revoked"}, nil
	case "operation.start":
		if actor != Core {
			return nil, errors.New("effect start requires trusted core authority")
		}
		var p struct {
			OperationID string `json:"operation_id"`
			GrantID     string `json:"grant_id"`
		}
		if err := store.Decode(cmd.Payload, &p); err != nil {
			return nil, err
		}
		var state, raw string
		var opEpoch int
		if err := tx.QueryRowContext(ctx, "SELECT state,evidence_json,policy_epoch FROM operations WHERE id=?", p.OperationID).Scan(&state, &raw, &opEpoch); err != nil {
			return nil, err
		}
		if state != "prepared" || opEpoch != epoch {
			return nil, errors.New("operation already started, cancelled or stale; never replay")
		}
		var operation OperationRequest
		if err := json.Unmarshal([]byte(raw), &operation); err != nil {
			return nil, err
		}
		if err := validateOperation(ctx, tx, operation, c); err != nil {
			return nil, err
		}
		var category, scope, resource, grantRaw string
		var grantEpoch int
		var revoked, expires sql.NullInt64
		var consumed sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT category,scope,resource_pattern,revision_policy_json,policy_epoch,revoked_at,expires_at,reserved_operation FROM grants WHERE id=?", p.GrantID).Scan(&category, &scope, &resource, &grantRaw, &grantEpoch, &revoked, &expires, &consumed); err != nil {
			return nil, err
		}
		if revoked.Valid || expires.Valid && expires.Int64 <= now || consumed.Valid || grantEpoch != epoch || category != operation.Category || resource != operation.ResourceDigest {
			return nil, errors.New("grant revoked, expired, consumed, stale or mismatched")
		}
		// Initial grants conservatively bind the exact argument/revision context in all scopes.
		var bound OperationRequest
		if err := json.Unmarshal([]byte(grantRaw), &bound); err != nil {
			return nil, err
		}
		if bound != operation {
			return nil, errors.New("grant does not cover the operation arguments/revisions")
		}
		if scope == "once" {
			if _, err := tx.ExecContext(ctx, "UPDATE grants SET reserved_operation=? WHERE id=? AND reserved_operation IS NULL", p.OperationID, p.GrantID); err != nil {
				return nil, err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='executing' WHERE id=?", p.OperationID)
		return map[string]string{"operation_id": p.OperationID, "state": "executing"}, err
	}
	return nil, errors.New("unknown permission command")
}

type InboxEntry struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	State            string          `json:"state"`
	PlanID           string          `json:"plan_id,omitempty"`
	TaskID           string          `json:"task_id,omitempty"`
	TaskRevision     int             `json:"task_revision,omitempty"`
	RunID            string          `json:"run_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	NativeRequestKey string          `json:"native_request_key,omitempty"`
	OperationID      string          `json:"operation_id,omitempty"`
	ResourceDigest   string          `json:"resource_digest,omitempty"`
	ArgumentsDigest  string          `json:"arguments_digest,omitempty"`
	PolicyEpoch      int             `json:"policy_epoch,omitempty"`
	Blocking         bool            `json:"blocking"`
	Context          json.RawMessage `json:"context"`
	CreatedAt        int64           `json:"created_at"`
	Deadline         int64           `json:"deadline"`
	GrantID          string          `json:"grant_id,omitempty"`
	GrantOrigin      string          `json:"grant_origin,omitempty"`
	GrantScope       string          `json:"grant_scope,omitempty"`
	GrantRevokedAt   int64           `json:"grant_revoked_at,omitempty"`
}

func (e *Engine) Inbox(ctx context.Context) ([]InboxEntry, error) {
	return readInbox(ctx, e.DB.SQL)
}

type queryReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readInbox(ctx context.Context, reader queryReader) ([]InboxEntry, error) {
	rows, err := reader.QueryContext(ctx, `SELECT r.id,r.kind,r.state,coalesce(r.plan_id,''),coalesce(r.task_id,''),coalesce(r.task_revision,0),coalesce(r.run_id,''),coalesce(r.session_id,''),coalesce(r.native_request_key,''),coalesce(r.operation_id,''),coalesce(o.resource_digest,''),coalesce(o.args_digest,''),coalesce(o.policy_epoch,0),r.blocking,r.context_json,r.created_at,coalesce(r.deadline,0),coalesce(r.grant_id,''),coalesce(g.granted_by,''),coalesce(g.scope,''),coalesce(g.revoked_at,0)
		FROM requests r LEFT JOIN grants g ON g.id=r.grant_id LEFT JOIN operations o ON o.id=r.operation_id WHERE r.state='pending' ORDER BY r.created_at,r.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InboxEntry{}
	for rows.Next() {
		var entry InboxEntry
		var raw string
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.State, &entry.PlanID, &entry.TaskID, &entry.TaskRevision, &entry.RunID, &entry.SessionID, &entry.NativeRequestKey, &entry.OperationID, &entry.ResourceDigest, &entry.ArgumentsDigest, &entry.PolicyEpoch, &entry.Blocking, &raw, &entry.CreatedAt, &entry.Deadline, &entry.GrantID, &entry.GrantOrigin, &entry.GrantScope, &entry.GrantRevokedAt); err != nil {
			return nil, err
		}
		entry.Context = json.RawMessage(raw)
		if entry.Deadline > 0 && entry.Deadline <= store.Now() {
			entry.State = "expired"
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

type Event struct {
	Sequence  int64           `json:"sequence"`
	Kind      string          `json:"kind"`
	At        int64           `json:"occurred_at"`
	CommandID string          `json:"command_id"`
	Payload   json.RawMessage `json:"payload"`
}

func (e *Engine) Events(ctx context.Context, after int64) ([]Event, error) {
	return readEvents(ctx, e.DB.SQL, after, false)
}

func readEvents(ctx context.Context, reader queryReader, after int64, latest bool) ([]Event, error) {
	if after < 0 {
		return nil, errors.New("negative event cursor")
	}
	query := "SELECT sequence,kind,occurred_at,coalesce(command_id,''),payload_json FROM events WHERE sequence>? ORDER BY sequence LIMIT 100"
	if latest {
		query = "SELECT sequence,kind,occurred_at,coalesce(command_id,''),payload_json FROM events WHERE sequence>? ORDER BY sequence DESC LIMIT 100"
	}
	rows, err := reader.QueryContext(ctx, query, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		var raw string
		if err := rows.Scan(&event.Sequence, &event.Kind, &event.At, &event.CommandID, &raw); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(strings.TrimSpace(raw))
		out = append(out, event)
	}
	if latest {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, rows.Err()
}
