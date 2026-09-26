// Package tools exposes bounded application handlers to native model sessions.
// Session, project, role and run authority are injected by the server.
package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"vigil/internal/artifacts"
	"vigil/internal/core"
	"vigil/internal/store"
)

const MaxPayload = 64 << 10
const MaxList = 100
const MaxExcerpt = 32 << 10
const MaxAffected = 50

type Authority struct {
	Role            string `json:"role"`
	RunID           string `json:"run_id,omitempty"`
	NativeSessionID string `json:"native_session_id"`
	Generation      string `json:"generation"`
}
type Session struct {
	ID              string   `json:"id"`
	ProjectID       string   `json:"project_id"`
	Role            string   `json:"role"`
	RunID           string   `json:"run_id,omitempty"`
	NativeSessionID string   `json:"native_session_id"`
	Generation      string   `json:"generation"`
	Capabilities    []string `json:"capabilities"`
}
type Handler struct{ Engine *core.Engine }

var capabilities = map[string][]string{
	"implementation": {"project.read", "plan.read", "task.read", "artifact.read", "task.report_blocked"},
	"review":         {"project.read", "plan.read", "task.read", "artifact.read"},
	"supervisor":     {"project.read", "plan.read", "task.read", "artifact.read", "plan.reorder", "plan.propose_change"},
	"planning":       {"project.read", "plan.read", "task.read", "artifact.read", "plan.propose_change"},
	"finalization":   {"project.read", "plan.read", "task.read", "artifact.read"},
}

func (h *Handler) OpenSession(ctx context.Context, commandID string, a Authority) (Session, error) {
	var out Session
	if h == nil || h.Engine == nil || h.Engine.DB == nil || !store.SafeID(commandID) || !store.SafeID(a.NativeSessionID) || !store.SafeID(a.Generation) {
		return out, errors.New("valid server-injected session authority required")
	}
	caps, ok := capabilities[a.Role]
	if !ok {
		return out, errors.New("unsupported tool role")
	}
	if (a.Role == "implementation" || a.Role == "review") && a.RunID == "" {
		return out, errors.New("run-bound role requires injected run authority")
	}
	if a.RunID != "" {
		if err := h.validateRunAuthority(ctx, a.RunID, a.Role, a.NativeSessionID, a.Generation); err != nil {
			return out, err
		}
	}
	out = Session{ID: store.Digest([]byte(commandID + "\x00tool-session")), ProjectID: h.Engine.ProjectID, Role: a.Role, RunID: a.RunID, NativeSessionID: a.NativeSessionID, Generation: a.Generation, Capabilities: append([]string(nil), caps...)}
	args, _ := json.Marshal(a)
	receipt, err := h.Engine.DB.Command(ctx, store.Command{ID: commandID, Actor: "core", Kind: "tool.session.open", Args: args}, func(tx *store.Tx) (any, error) {
		raw, _ := json.Marshal(caps)
		_, err := tx.ExecContext(ctx, `INSERT INTO tool_sessions(id,project_id,role,run_id,native_session_id,generation,capabilities_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, out.ID, h.Engine.ProjectID, a.Role, nullable(a.RunID), a.NativeSessionID, a.Generation, string(raw), store.Now())
		return out, err
	})
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(receipt, &out)
	return out, err
}

func (h *Handler) Capabilities(ctx context.Context, sessionID string) ([]string, error) {
	var raw string
	var project string
	var retired int64
	if h == nil || h.Engine == nil || h.Engine.DB == nil || !store.SafeID(sessionID) {
		return nil, errors.New("valid tool session required")
	}
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, `SELECT project_id,capabilities_json,coalesce(retired_at,0) FROM tool_sessions WHERE id=?`, sessionID).Scan(&project, &raw, &retired); err != nil || project != h.Engine.ProjectID || retired != 0 {
		return nil, errors.New("foreign or retired tool session")
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) AuditCall(ctx context.Context, sessionID string, requestID json.RawMessage, name string, input, result []byte) error {
	if len(requestID) == 0 || len(requestID) > 256 {
		return errors.New("bounded native tool request identity required")
	}
	var role string
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, `SELECT role FROM tool_sessions WHERE id=? AND retired_at IS NULL`, sessionID).Scan(&role); err != nil {
		return errors.New("active injected tool session required")
	}
	args, _ := json.Marshal(map[string]string{"session_id": sessionID, "request_id_digest": store.Digest(requestID), "tool": name, "input_digest": store.Digest(input), "result_digest": store.Digest(result)})
	commandID := store.Digest([]byte(sessionID + "\x00" + string(requestID) + "\x00" + name))
	_, err := h.Engine.DB.Command(ctx, store.Command{ID: commandID, Actor: "model:" + role, Kind: "tool.call.audit", Args: args}, func(*store.Tx) (any, error) {
		return map[string]string{"session_id": sessionID, "tool": name, "result_digest": store.Digest(result)}, nil
	})
	return err
}

func (h *Handler) RetireSession(ctx context.Context, commandID, sessionID, generation string) error {
	if h == nil || h.Engine == nil || h.Engine.DB == nil || !store.SafeID(commandID) || !store.SafeID(sessionID) || !store.SafeID(generation) {
		return errors.New("exact tool session retirement required")
	}
	args, _ := json.Marshal(map[string]string{"session_id": sessionID, "generation": generation})
	_, err := h.Engine.DB.Command(ctx, store.Command{ID: commandID, Actor: "core", Kind: "tool.session.retire", Args: args}, func(tx *store.Tx) (any, error) {
		result, err := tx.ExecContext(ctx, `UPDATE tool_sessions SET retired_at=? WHERE id=? AND generation=? AND retired_at IS NULL`, store.Now(), sessionID, generation)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return nil, errors.New("tool session is missing, stale or already retired")
		}
		return map[string]string{"session_id": sessionID, "state": "retired"}, nil
	})
	return err
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (h *Handler) Call(ctx context.Context, sessionID, name string, input []byte) (json.RawMessage, error) {
	if len(input) > MaxPayload {
		return nil, errors.New("tool payload exceeds 64 KiB")
	}
	var session Session
	var caps string
	var retired int64
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, `SELECT id,project_id,role,coalesce(run_id,''),native_session_id,generation,capabilities_json,coalesce(retired_at,0) FROM tool_sessions WHERE id=?`, sessionID).Scan(&session.ID, &session.ProjectID, &session.Role, &session.RunID, &session.NativeSessionID, &session.Generation, &caps, &retired); err != nil {
		return nil, errors.New("unknown tool session")
	}
	if session.ProjectID != h.Engine.ProjectID || retired != 0 {
		return nil, errors.New("foreign or retired tool session")
	}
	if session.RunID != "" {
		if err := h.validateRunAuthority(ctx, session.RunID, session.Role, session.NativeSessionID, session.Generation); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal([]byte(caps), &session.Capabilities); err != nil {
		return nil, err
	}
	allowed := false
	for _, cap := range session.Capabilities {
		if cap == name {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("tool is not allowed for this role")
	}
	var value any
	var err error
	switch name {
	case "project.read":
		value, err = h.projectRead(ctx, session, input)
	case "plan.read":
		value, err = h.planRead(ctx, session, input)
	case "task.read":
		value, err = h.taskRead(ctx, input)
	case "artifact.read":
		value, err = h.artifactRead(ctx, input)
	case "task.report_blocked":
		value, err = h.reportBlocked(ctx, session, input)
	case "plan.reorder":
		value, err = h.reorder(ctx, input)
	case "plan.propose_change":
		value, err = h.propose(ctx, session, input)
	default:
		err = errors.New("unknown tool")
	}
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) validateRunAuthority(ctx context.Context, runID, role, nativeSessionID, generation string) error {
	var persistedRole, runState, generationState, persistedSession string
	err := h.Engine.DB.SQL.QueryRowContext(ctx, `SELECT r.role,r.state,g.state,coalesce(g.native_session_id,'') FROM runs r JOIN run_generations g ON g.run_id=r.id WHERE r.id=? AND g.transport_generation=?`, runID, generation).Scan(&persistedRole, &runState, &generationState, &persistedSession)
	if err != nil {
		return errors.New("foreign, missing or stale run generation")
	}
	if persistedRole != role || persistedSession != nativeSessionID {
		return errors.New("run authority does not match tool session")
	}
	if (runState != "active" && runState != "waiting_input") || generationState != "active" {
		return errors.New("run generation is not active")
	}
	return nil
}

func cursor(session, kind string, offset int) string {
	plain := strconv.Itoa(offset)
	sig := store.Digest([]byte(session + "\x00" + kind + "\x00" + plain))[:16]
	return base64.RawURLEncoding.EncodeToString([]byte(plain + ":" + sig))
}
func parseCursor(value, session, kind string) (int, error) {
	if value == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, errors.New("invalid opaque cursor")
	}
	parts := strings.Split(string(b), ":")
	if len(parts) != 2 {
		return 0, errors.New("invalid opaque cursor")
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n < 0 || parts[1] != store.Digest([]byte(session + "\x00" + kind + "\x00" + parts[0]))[:16] {
		return 0, errors.New("invalid opaque cursor")
	}
	return n, nil
}

type pageInput struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func page(raw []byte, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return store.Decode(raw, target)
}

func (h *Handler) projectRead(ctx context.Context, s Session, raw []byte) (any, error) {
	var in pageInput
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if in.Limit == 0 {
		in.Limit = MaxList
	}
	if in.Limit < 1 || in.Limit > MaxList {
		return nil, errors.New("list limit must be 1–100")
	}
	offset, err := parseCursor(in.Cursor, s.ID, "project")
	if err != nil {
		return nil, err
	}
	rows, err := h.Engine.DB.SQL.QueryContext(ctx, `SELECT id,plan_id,revision,state,rank,coalesce(block_reason,'') FROM tasks ORDER BY plan_id,rank,id LIMIT ? OFFSET ?`, in.Limit+1, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, plan, state, reason string
		var revision, rank int
		if err := rows.Scan(&id, &plan, &revision, &state, &rank, &reason); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "plan_id": plan, "revision": revision, "state": state, "rank": rank, "block_reason": reason})
	}
	next := ""
	if len(items) > in.Limit {
		items = items[:in.Limit]
		next = cursor(s.ID, "project", offset+in.Limit)
	}
	var project core.Project
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, "SELECT id,root,revision,state FROM project").Scan(&project.ID, &project.Root, &project.Revision, &project.State); err != nil {
		return nil, err
	}
	return map[string]any{"project": project, "tasks": items, "next_cursor": next}, rows.Err()
}

func (h *Handler) planRead(ctx context.Context, s Session, raw []byte) (any, error) {
	var in struct {
		ID       string `json:"id"`
		Revision int    `json:"revision"`
		Cursor   string `json:"cursor,omitempty"`
		Limit    int    `json:"limit,omitempty"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if !store.SafeID(in.ID) || in.Revision < 1 {
		return nil, errors.New("known plan ID and revision required")
	}
	if in.Limit == 0 {
		in.Limit = MaxList
	}
	if in.Limit < 1 || in.Limit > MaxList {
		return nil, errors.New("list limit must be 1–100")
	}
	offset, err := parseCursor(in.Cursor, s.ID, "plan:"+in.ID)
	if err != nil {
		return nil, err
	}
	var definition string
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, "SELECT definition_json FROM plan_revisions WHERE plan_id=? AND revision=?", in.ID, in.Revision).Scan(&definition); err != nil {
		return nil, errors.New("foreign, missing or stale plan")
	}
	var plan core.Plan
	if err := json.Unmarshal([]byte(definition), &plan); err != nil {
		return nil, err
	}
	end := offset + in.Limit
	if offset > len(plan.Tasks) {
		return nil, errors.New("cursor beyond plan")
	}
	if end > len(plan.Tasks) {
		end = len(plan.Tasks)
	}
	next := ""
	if end < len(plan.Tasks) {
		next = cursor(s.ID, "plan:"+in.ID, end)
	}
	tasks := plan.Tasks[offset:end]
	plan.Tasks = nil
	return map[string]any{"plan": plan, "tasks": tasks, "next_cursor": next}, nil
}

func (h *Handler) taskRead(ctx context.Context, raw []byte) (any, error) {
	var in struct {
		ID       string `json:"id"`
		Revision int    `json:"revision"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	var definition, plan, state string
	var current int
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, `SELECT r.definition_json,t.plan_id,t.state,t.revision FROM tasks t JOIN task_revisions r ON r.task_id=t.id AND r.revision=? WHERE t.id=?`, in.Revision, in.ID).Scan(&definition, &plan, &state, &current); err != nil || current != in.Revision {
		return nil, errors.New("foreign, missing or stale task")
	}
	var task any
	if err := json.Unmarshal([]byte(definition), &task); err != nil {
		return nil, err
	}
	return map[string]any{"plan_id": plan, "state": state, "revision": current, "definition": task}, nil
}

func (h *Handler) artifactRead(ctx context.Context, raw []byte) (any, error) {
	var in struct {
		ID     string `json:"id"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if !store.SafeID(in.ID) || in.Offset < 0 || in.Length < 1 || in.Length > MaxExcerpt {
		return nil, errors.New("owned artifact ID, nonnegative offset and 1–32768 length required")
	}
	repo, err := artifacts.New(h.Engine.DB)
	if err != nil {
		return nil, err
	}
	b, err := repo.Read(ctx, in.ID)
	if err != nil {
		return nil, errors.New("artifact unavailable, corrupt or foreign")
	}
	if in.Offset > len(b) {
		return nil, errors.New("artifact offset beyond end")
	}
	end := in.Offset + in.Length
	if end > len(b) {
		end = len(b)
	}
	return map[string]any{"artifact_id": in.ID, "offset": in.Offset, "bytes": end - in.Offset, "excerpt": string(b[in.Offset:end]), "eof": end == len(b)}, nil
}

func (h *Handler) reportBlocked(ctx context.Context, s Session, raw []byte) (any, error) {
	var in struct {
		CommandID        string   `json:"command_id"`
		TaskID           string   `json:"task_id"`
		ExpectedRevision int      `json:"expected_task_revision"`
		Reason           string   `json:"reason"`
		MissingFacts     []string `json:"missing_facts"`
		EvidenceIDs      []string `json:"evidence_ids"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if !store.SafeID(in.CommandID) || !store.SafeID(in.TaskID) || in.ExpectedRevision < 1 || in.Reason == "" || len(in.Reason) > 4096 || len(in.MissingFacts) > 50 || len(in.EvidenceIDs) > 50 {
		return nil, errors.New("invalid bounded blocked observation")
	}
	for _, id := range in.EvidenceIDs {
		var n int
		if !store.SafeID(id) || h.Engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM artifacts WHERE id=?", id).Scan(&n) != nil || n != 1 {
			return nil, errors.New("foreign evidence ID")
		}
	}
	args, _ := json.Marshal(in)
	receipt, err := h.Engine.DB.Command(ctx, store.Command{ID: in.CommandID, Actor: "model:" + s.Role, Kind: "task.report_blocked", Args: args}, func(tx *store.Tx) (any, error) {
		var revision int
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT revision,state FROM tasks WHERE id=?", in.TaskID).Scan(&revision, &state); err != nil || revision != in.ExpectedRevision {
			return nil, errors.New("foreign or stale task")
		}
		id := store.ID()
		missing, _ := json.Marshal(in.MissingFacts)
		evidence, _ := json.Marshal(in.EvidenceIDs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO blocked_observations VALUES(?,?,?,?,?,?,?,?)`, id, s.ID, in.TaskID, revision, in.Reason, string(missing), string(evidence), store.Now()); err != nil {
			return nil, err
		}
		contextJSON, _ := json.Marshal(map[string]any{"observation_id": id, "reason": in.Reason, "missing_facts": in.MissingFacts, "evidence_ids": in.EvidenceIDs, "run_id": s.RunID, "session_id": s.NativeSessionID, "generation": s.Generation})
		requestID := store.ID()
		if _, err := tx.ExecContext(ctx, `INSERT INTO requests(id,kind,state,task_id,task_revision,run_id,context_json,blocking,created_at) VALUES(?,'input','pending',?,?,?,?,1,?)`, requestID, in.TaskID, revision, nullable(s.RunID), string(contextJSON), store.Now()); err != nil {
			return nil, err
		}
		return map[string]any{"observation_id": id, "request_id": requestID, "task_state": state}, nil
	})
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(receipt, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) reorder(ctx context.Context, raw []byte) (any, error) {
	var in struct {
		CommandID               string   `json:"command_id"`
		ExpectedProjectRevision int      `json:"expected_project_revision"`
		PlanID                  string   `json:"plan_id"`
		ExpectedPlanRevision    int      `json:"expected_plan_revision"`
		Tasks                   []string `json:"tasks"`
		Reason                  string   `json:"reason"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if len(in.Tasks) > MaxAffected || in.Reason == "" {
		return nil, errors.New("bounded reason and at most 50 tasks required")
	}
	var current int
	if err := h.Engine.DB.SQL.QueryRowContext(ctx, "SELECT revision FROM plans WHERE id=?", in.PlanID).Scan(&current); err != nil || current != in.ExpectedPlanRevision {
		return nil, errors.New("foreign or stale plan")
	}
	payload, _ := json.Marshal(map[string]any{"plan_id": in.PlanID, "tasks": in.Tasks})
	result, err := h.Engine.Apply(ctx, core.Supervisor, core.Envelope{CommandID: in.CommandID, ExpectedRevision: in.ExpectedProjectRevision, Kind: "plan.reorder", Payload: payload})
	return result, err
}

func (h *Handler) propose(ctx context.Context, s Session, raw []byte) (any, error) {
	var in struct {
		CommandID               string               `json:"command_id"`
		ExpectedProjectRevision int                  `json:"expected_project_revision"`
		Proposal                core.ProposalRequest `json:"proposal"`
	}
	if err := page(raw, &in); err != nil {
		return nil, err
	}
	if len(in.Proposal.AffectedTasks) > MaxAffected {
		return nil, errors.New("proposal affects more than 50 tasks")
	}
	proposal, err := h.Engine.CreateModelProposal(ctx, in.CommandID, in.ExpectedProjectRevision, in.Proposal, s.Role)
	if err != nil {
		return nil, err
	}
	return proposal, nil
}
