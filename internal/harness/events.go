package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type fields map[string]json.RawMessage

func object(raw json.RawMessage) fields   { var d fields; _ = json.Unmarshal(raw, &d); return d }
func str(d fields, key string) string     { var s string; _ = json.Unmarshal(d[key], &s); return s }
func boolField(d fields, key string) bool { var b bool; _ = json.Unmarshal(d[key], &b); return b }
func number(d fields, key string) *int64 {
	var n int64
	if len(d[key]) == 0 || string(d[key]) == "null" || json.Unmarshal(d[key], &n) != nil || n < 0 {
		return nil
	}
	return &n
}
func present(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

func (s *Session) codexEventLocked(m Message) {
	p := object(m.Params)
	if p == nil {
		s.failLocked(errors.New("invalid Codex event params"))
		return
	}
	thread := str(p, "threadId")
	if thread != "" && s.snapshot.ThreadID != "" && thread != string(s.snapshot.ThreadID) {
		return
	}
	if id := str(p, "turnId"); id != "" && s.submitted && !s.bindTurnLocked(id) {
		return
	}
	switch m.Method {
	case "turn/started":
		t := object(p["turn"])
		if s.submitted {
			s.bindTurnLocked(str(t, "id"))
		} else {
			s.failLocked(errors.New("unsolicited Codex turn"))
		}
	case "item/agentMessage/delta", "item/reasoning/textDelta", "item/commandExecution/outputDelta":
		s.emitLocked(Event{Kind: "output", Method: m.Method, Item: str(p, "itemId"), Bytes: len(str(p, "delta"))})
	case "item/started", "item/completed":
		i := object(p["item"])
		kind := str(i, "type")
		if kind == "agentMessage" && m.Method == "item/completed" {
			s.snapshot.Output = str(i, "text")
		}
		if kind != "agentMessage" && kind != "reasoning" && kind != "userMessage" {
			s.emitLocked(Event{Kind: "tool", Method: m.Method, Item: str(i, "id"), Name: kind, Status: str(i, "status")})
		}
	case "thread/tokenUsage/updated":
		u := object(object(p["tokenUsage"])["total"])
		s.snapshot.Usage = Usage{Input: number(u, "inputTokens"), Output: number(u, "outputTokens"), Cached: number(u, "cachedInputTokens"), Total: number(u, "totalTokens"), Scope: "session_cumulative"}
		s.emitLocked(Event{Kind: "usage"})
	case "turn/completed":
		t := object(p["turn"])
		if !s.bindTurnLocked(str(t, "id")) {
			return
		}
		status := str(t, "status")
		outcome := "unknown"
		switch status {
		case "completed":
			outcome = "completed"
		case "failed":
			outcome = "failed"
		case "interrupted":
			outcome = "interrupted"
		}
		if present(t["error"]) {
			outcome = "failed"
		}
		var items []fields
		_ = json.Unmarshal(t["items"], &items)
		text := s.snapshot.Output
		for _, i := range items {
			if str(i, "type") == "agentMessage" {
				text = str(i, "text")
			}
		}
		s.terminalLocked(outcome, status, text)
	case "thread/status/changed":
		status := str(object(p["status"]), "type")
		if status == "idle" {
			s.snapshot.IdleObserved = true
			s.touchLocked()
		}
	case "serverRequest/resolved":
		s.cancelRequestLocked(p["requestId"], "native_resolved")
	case "error":
		if !boolField(p, "willRetry") {
			s.failLocked(errors.New("native Codex error"))
		}
	}
}

func (s *Session) hermesEventLocked(m Message) {
	if m.Method != "event" {
		return
	}
	p := object(m.Params)
	if p == nil {
		s.failLocked(errors.New("invalid Hermes event params"))
		return
	}
	kind := str(p, "type")
	d := object(p["payload"])
	sid := str(p, "session_id")
	if kind == "gateway.ready" {
		s.ready = true
		s.touchLocked()
		return
	}
	if sid != "" && s.snapshot.RuntimeID != "" && sid != string(s.snapshot.RuntimeID) {
		return
	}
	switch kind {
	case "message.delta", "reasoning.delta", "thinking.delta", "message.interim":
		s.emitLocked(Event{Kind: "output", Method: kind, Bytes: len(str(d, "text"))})
	case "tool.start", "tool.complete", "tool.generating":
		s.emitLocked(Event{Kind: "tool", Method: kind, Item: str(d, "tool_id"), Name: str(d, "name"), Status: kind})
	case "session.info":
		if !boolField(d, "lazy") {
			if err := s.validateHermesInfoLocked(p["payload"]); err != nil {
				s.failLocked(err)
			}
		}
	case "session.usage":
		s.hermesUsageLocked(d)
		s.emitLocked(Event{Kind: "usage"})
	case "message.complete":
		if present(d["usage"]) {
			s.hermesUsageLocked(object(d["usage"]))
		}
		status := str(d, "status")
		outcome := "unknown"
		switch status {
		case "complete":
			outcome = "completed"
		case "error":
			outcome = "failed"
		case "interrupted":
			outcome = "interrupted"
		}
		if str(d, "error") != "" || str(d, "failure_reason") != "" || boolField(d, "partial") {
			outcome = "failed"
		}
		s.terminalLocked(outcome, status, str(d, "text"))
	case "request.cancel":
		s.cancelRequestLocked(d["id"], str(d, "reason"))
	case "error":
		s.failLocked(errors.New("native Hermes session error"))
	}
}
func (s *Session) hermesUsageLocked(d fields) {
	s.snapshot.Usage = Usage{Input: number(d, "prompt"), Output: number(d, "completion"), Cached: number(d, "cache_read"), Total: number(d, "total"), Scope: "session_cumulative"}
	if s.snapshot.Usage.Input == nil {
		s.snapshot.Usage.Input = number(d, "input")
	}
	if s.snapshot.Usage.Output == nil {
		s.snapshot.Usage.Output = number(d, "output")
	}
}
func (s *Session) validateHermesInfoLocked(raw json.RawMessage) error {
	d := object(raw)
	if str(d, "model") != s.profile.Model || str(d, "provider") != s.profile.Provider || str(d, "cwd") != s.profile.Workspace || str(d, "approval_mode") != "manual" || boolField(d, "yolo") || boolField(d, "lazy") {
		return errors.New("Hermes effective session configuration mismatch")
	}
	if stored := str(d, "stored_session_id"); stored != "" && s.snapshot.DurableID != "" && stored != string(s.snapshot.DurableID) {
		return errors.New("Hermes durable session identity mismatch")
	}
	s.snapshot.IdleObserved = !boolField(d, "running")
	s.touchLocked()
	return nil
}

func (s *Session) requestLocked(m Message) {
	key, err := IDKey(m.ID)
	if err != nil {
		s.failLocked(err)
		return
	}
	if s.seenRequests[key] {
		s.failLocked(errors.New("duplicate native request ID"))
		return
	}
	if !s.submitted || s.terminal || len(s.requests) >= s.t.cfg.PendingLimit {
		s.failLocked(errors.New("native request outside active turn or over capacity"))
		s.t.fail(s.fault)
		return
	}
	p := object(m.Params)
	if p == nil {
		s.failLocked(errors.New("invalid native request params"))
		return
	}
	if s.kind == "codex" {
		if str(p, "threadId") != string(s.snapshot.ThreadID) || !s.bindTurnLocked(str(p, "turnId")) {
			s.failLocked(errors.New("native request ownership mismatch"))
			return
		}
	} else if str(p, "session_id") != string(s.snapshot.RuntimeID) {
		s.failLocked(errors.New("native request ownership mismatch"))
		return
	}
	r := Request{Generation: s.snapshot.Generation, ID: append(json.RawMessage(nil), m.ID...), Method: m.Method, AppTurn: s.snapshot.AppTurn, Deadline: time.Now().Add(s.requestWait), Params: m.Params}
	if ms := number(p, "autoResolutionMs"); ms != nil {
		deadline := time.Now().Add(time.Duration(*ms) * time.Millisecond)
		if deadline.Before(r.Deadline) {
			r.Deadline = deadline
		}
	}
	s.requests[key] = r
	s.seenRequests[key] = true
	kind := "unsupported_request"
	switch m.Method {
	case "approval", "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		kind = "approval_requested"
	case "clarify", "item/tool/requestUserInput":
		kind = "input_requested"
	}
	s.emitLocked(Event{Kind: kind, Method: m.Method, Request: &r})
}
func (s *Session) cancelRequestLocked(id json.RawMessage, reason string) {
	key, err := IDKey(id)
	if err != nil {
		return
	}
	if r, ok := s.requests[key]; ok {
		delete(s.requests, key)
		s.emitLocked(Event{Kind: "request_resolved", Request: &r, Status: reason})
	}
}

func (s *Session) Answer(ctx context.Context, ref Request, answer Answer) error {
	key, err := IDKey(ref.ID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	r, ok := s.requests[key]
	if !ok || ref.Generation != s.snapshot.Generation || ref.AppTurn != s.snapshot.AppTurn || r.Method != ref.Method || s.terminal || !time.Now().Before(r.Deadline) {
		s.mu.Unlock()
		return errors.New("stale or expired request")
	}
	result, rpcErr, err := nativeAnswer(r, answer)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.requests, key)
	s.emitLocked(Event{Kind: "request_resolved", Request: &r, Status: answer.Decision})
	s.mu.Unlock()
	if err := s.t.Respond(ctx, r.ID, result, rpcErr); err != nil {
		s.mu.Lock()
		s.failLocked(errors.New("request answer delivery uncertain"))
		s.mu.Unlock()
		return err
	}
	return nil
}

func nativeAnswer(r Request, a Answer) (any, *RPCError, error) {
	if a.Decision != "allow-once" && a.Decision != "deny" && a.Decision != "cancel" && a.Decision != "answer" {
		return nil, nil, errors.New("unsupported answer decision")
	}
	p := object(r.Params)
	switch r.Method {
	case "approval":
		choice := "deny"
		if a.Decision == "allow-once" {
			choice = "once"
		} else if a.Decision == "answer" {
			return nil, nil, errors.New("approval requires a decision")
		}
		var choices []string
		_ = json.Unmarshal(p["choices"], &choices)
		if len(choices) > 0 {
			found := false
			for _, c := range choices {
				if c == choice {
					found = true
				}
			}
			if !found {
				return nil, nil, errors.New("native choice unavailable")
			}
		}
		return map[string]any{"choice": choice}, nil, nil
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		decision := "decline"
		if a.Decision == "allow-once" {
			decision = "accept"
		}
		if a.Decision == "cancel" {
			decision = "cancel"
		}
		if a.Decision == "answer" {
			return nil, nil, errors.New("approval requires a decision")
		}
		return map[string]any{"decision": decision}, nil, nil
	case "clarify":
		if a.Decision == "cancel" || a.Decision == "deny" {
			return map[string]any{}, nil, nil
		}
		if a.Decision != "answer" {
			return nil, nil, errors.New("input requires answer or cancellation")
		}
		var questions []struct {
			ID string `json:"qid"`
		}
		_ = json.Unmarshal(p["questions"], &questions)
		if len(questions) > 0 {
			if len(a.Answers) != len(questions) {
				return nil, nil, errors.New("incomplete clarification")
			}
			answers := map[string]string{}
			for _, q := range questions {
				values, ok := a.Answers[q.ID]
				if !ok || len(values) != 1 {
					return nil, nil, errors.New("invalid clarification answer")
				}
				answers[q.ID] = values[0]
			}
			return map[string]any{"answers": answers}, nil, nil
		}
		if strings.TrimSpace(a.Text) == "" {
			return nil, nil, errors.New("empty clarification answer")
		}
		return map[string]any{"answer": a.Text}, nil, nil
	case "item/tool/requestUserInput":
		if a.Decision == "cancel" || a.Decision == "deny" {
			return nil, &RPCError{Code: -32800, Message: "request cancelled by controller"}, nil
		}
		if a.Decision != "answer" {
			return nil, nil, errors.New("input requires answer or cancellation")
		}
		var questions []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(p["questions"], &questions)
		if len(questions) == 0 || len(a.Answers) != len(questions) {
			return nil, nil, errors.New("incomplete user input")
		}
		answers := map[string]any{}
		for _, q := range questions {
			v, ok := a.Answers[q.ID]
			if !ok || len(v) == 0 {
				return nil, nil, errors.New("invalid user input answer")
			}
			answers[q.ID] = map[string]any{"answers": v}
		}
		return map[string]any{"answers": answers}, nil, nil
	default:
		return nil, &RPCError{Code: -32601, Message: "unsupported request"}, nil
	}
}

// Expire invalidates UI references before sending narrow denials. Call periodically
// while waiting on a human; this spike's noninteractive runner answers immediately.
func (s *Session) Expire(ctx context.Context) error {
	s.mu.Lock()
	var expired []Request
	for key, r := range s.requests {
		if !time.Now().Before(r.Deadline) {
			expired = append(expired, r)
			delete(s.requests, key)
			s.emitLocked(Event{Kind: "request_resolved", Request: &r, Status: "expired"})
		}
	}
	s.mu.Unlock()
	for _, r := range expired {
		result, rpcErr, err := nativeAnswer(r, Answer{Decision: "deny"})
		if err != nil {
			return err
		}
		if err = s.t.Respond(ctx, r.ID, result, rpcErr); err != nil {
			return err
		}
	}
	if len(expired) > 0 {
		return s.Interrupt(ctx)
	}
	return nil
}
