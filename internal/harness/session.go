package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type RunID string
type AppTurnID string
type SessionID string
type TurnID string
type Generation string

type Profile struct {
	Model, Provider, Workspace string
	NativeParams               map[string]any
}
type Usage struct {
	Input  *int64   `json:"input_tokens"`
	Output *int64   `json:"output_tokens"`
	Cached *int64   `json:"cached_input_tokens"`
	Total  *int64   `json:"total_tokens"`
	Cost   *float64 `json:"cost"`
	Scope  string   `json:"scope"`
}
type Snapshot struct {
	Run          RunID      `json:"run_id"`
	Generation   Generation `json:"transport_generation"`
	Harness      string     `json:"harness"`
	RuntimeID    SessionID  `json:"runtime_session_id"`
	DurableID    SessionID  `json:"durable_session_id"`
	ThreadID     SessionID  `json:"thread_id,omitempty"`
	AppTurn      AppTurnID  `json:"application_turn_id"`
	NativeTurn   TurnID     `json:"native_turn_id,omitempty"`
	Model        string     `json:"model"`
	Provider     string     `json:"provider"`
	Workspace    string     `json:"workspace"`
	Outcome      string     `json:"outcome"`
	NativeStatus string     `json:"native_status"`
	Fault        string     `json:"fault,omitempty"`
	IdleObserved bool       `json:"native_idle_observed"`
	Quiescence   string     `json:"writer_quiescence"`
	Usage        Usage      `json:"usage"`
	Output       string     `json:"-"`
	Pending      int        `json:"pending_requests"`
	HistoryItems int        `json:"resumed_history_items,omitempty"`
}
type Request struct {
	Generation Generation      `json:"generation"`
	ID         json.RawMessage `json:"id"`
	Method     string          `json:"method"`
	AppTurn    AppTurnID       `json:"application_turn"`
	Deadline   time.Time       `json:"deadline"`
	Params     json.RawMessage `json:"-"` // potentially sensitive; never serialized into evidence
}
type Event struct {
	Sequence   uint64     `json:"sequence"`
	At         time.Time  `json:"received_at"`
	Run        RunID      `json:"run_id"`
	Generation Generation `json:"generation"`
	Kind       string     `json:"kind"`
	Method     string     `json:"method,omitempty"`
	Item       string     `json:"item_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	Bytes      int        `json:"bytes,omitempty"`
	Request    *Request   `json:"request,omitempty"`
}
type Answer struct {
	Decision string // allow-once, deny, cancel; never session/global grants
	Text     string
	Answers  map[string][]string
}

type Session struct {
	kind                                string
	t                                   *Transport
	profile                             Profile
	mu                                  sync.Mutex
	snapshot                            Snapshot
	ready, created, submitted, terminal bool
	requests                            map[string]Request
	seenRequests                        map[string]bool
	events                              chan Event
	changed                             chan struct{}
	pumpDone                            chan struct{}
	seq                                 uint64
	requestWait                         time.Duration
	fault                               error
	closing                             bool
}

func NewSession(kind string, t *Transport, profile Profile, run RunID, generation Generation, requestWait time.Duration) (*Session, error) {
	if (kind != "codex" && kind != "hermes") || requestWait <= 0 {
		return nil, errors.New("invalid session configuration")
	}
	s := &Session{kind: kind, t: t, profile: profile, requests: make(map[string]Request), seenRequests: make(map[string]bool), events: make(chan Event, t.cfg.QueueSize), changed: make(chan struct{}, 1), pumpDone: make(chan struct{}), requestWait: requestWait}
	s.snapshot = Snapshot{Run: run, Generation: generation, Harness: kind, Model: profile.Model, Provider: profile.Provider, Workspace: profile.Workspace, Outcome: "not_started", Quiescence: "unconfirmed"}
	go s.pump()
	return s, nil
}
func (s *Session) Events() <-chan Event     { return s.events }
func (s *Session) Changed() <-chan struct{} { return s.changed }
func (s *Session) Inspect() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snapshot
	out.Pending = len(s.requests)
	return out
}
func (s *Session) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.fault }
func (s *Session) touchLocked() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *Session) emitLocked(e Event) {
	s.seq++
	e.Sequence = s.seq
	e.At = time.Now().UTC()
	e.Run = s.snapshot.Run
	e.Generation = s.snapshot.Generation
	select {
	case s.events <- e:
	default:
		if s.fault == nil {
			s.fault = errors.New("normalized event queue overflow")
			s.snapshot.Fault = s.fault.Error()
			s.t.fail(s.fault)
		}
	}
	s.touchLocked()
}
func (s *Session) failLocked(err error) {
	if s.fault != nil {
		return
	}
	s.fault = err
	s.snapshot.Fault = err.Error()
	if s.submitted && !s.terminal {
		s.snapshot.Outcome = "unknown"
		s.terminal = true
	}
	s.retireLocked("cancelled")
	s.emitLocked(Event{Kind: "transport_failure", Status: err.Error()})
}
func (s *Session) retireLocked(reason string) {
	for key, request := range s.requests {
		r := request
		delete(s.requests, key)
		s.emitLocked(Event{Kind: "request_resolved", Request: &r, Status: reason})
	}
}
func (s *Session) terminalLocked(outcome, status, text string) {
	if s.terminal {
		if s.snapshot.Outcome != outcome || s.snapshot.NativeStatus != status || (text != "" && text != s.snapshot.Output) {
			s.failLocked(errors.New("conflicting terminal evidence"))
		}
		return
	}
	if !s.submitted {
		s.failLocked(errors.New("unsolicited native turn"))
		return
	}
	s.terminal = true
	s.snapshot.Outcome = outcome
	s.snapshot.NativeStatus = status
	if text != "" {
		s.snapshot.Output = text
	}
	s.retireLocked("cancelled")
	s.emitLocked(Event{Kind: "outcome", Status: outcome})
}
func (s *Session) bindTurnLocked(id string) bool {
	if id == "" {
		s.failLocked(errors.New("missing native turn identity"))
		return false
	}
	if s.snapshot.NativeTurn != "" && string(s.snapshot.NativeTurn) != id {
		s.failLocked(errors.New("unexpected native turn identity"))
		return false
	}
	s.snapshot.NativeTurn = TurnID(id)
	return true
}
func (s *Session) pump() {
	defer close(s.pumpDone)
	for {
		select {
		case m := <-s.t.Messages():
			s.handle(m)
		case <-s.t.Done():
			// Preserve buffered terminal events preceding EOF.
			for {
				select {
				case m := <-s.t.Messages():
					s.handle(m)
				default:
					s.mu.Lock()
					if !s.closing {
						s.failLocked(errors.New("harness transport lost"))
					}
					s.mu.Unlock()
					return
				}
			}
		}
	}
}
func (s *Session) handle(m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(m.ID) > 0 {
		s.requestLocked(m)
		return
	}
	if s.kind == "codex" {
		s.codexEventLocked(m)
	} else {
		s.hermesEventLocked(m)
	}
}

func (s *Session) wait(ctx context.Context, condition func() bool) error {
	for {
		s.mu.Lock()
		ok := condition()
		err := s.fault
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.changed:
		case <-s.pumpDone:
			return ErrClosed
		}
	}
}

func (s *Session) Probe(ctx context.Context) error {
	if s.kind == "hermes" {
		if err := s.wait(ctx, func() bool { return s.ready }); err != nil {
			return err
		}
		var cap struct {
			Exclusive bool `json:"per_session_exclusive_submit"`
		}
		if err := s.t.Call(ctx, "gateway.capabilities", map[string]any{}, &cap); err != nil {
			return err
		}
		if !cap.Exclusive {
			return errors.New("Hermes lacks exclusive submit capability")
		}
		var client struct {
			Requests []string `json:"server_requests"`
		}
		if err := s.t.Call(ctx, "client.capabilities", map[string]any{"server_requests": true}, &client); err != nil {
			return err
		}
		for _, required := range []string{"approval", "clarify"} {
			found := false
			for _, method := range client.Requests {
				if method == required {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("Hermes lacks %s requests", required)
			}
		}
		return nil
	}
	if err := s.t.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "vigil_spike", "version": "0.1.0"}, "capabilities": map[string]any{}}, nil); err != nil {
		return err
	}
	if err := s.t.Notify(ctx, "initialized", map[string]any{}); err != nil {
		return err
	}
	var account struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
	}
	if err := s.t.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &account); err != nil {
		return err
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		return errors.New("Codex ChatGPT authentication unavailable")
	}
	cursor := ""
	for page := 0; page < 10; page++ {
		var result struct {
			Data []struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"data"`
			Next *string `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := s.t.Call(ctx, "model/list", params, &result); err != nil {
			return err
		}
		for _, model := range result.Data {
			if model.Model == s.profile.Model || model.ID == s.profile.Model {
				return nil
			}
		}
		if result.Next == nil || *result.Next == "" {
			break
		}
		cursor = *result.Next
	}
	return errors.New("configured Codex model not advertised")
}

func (s *Session) Create(ctx context.Context) error {
	s.mu.Lock()
	already := s.created
	s.mu.Unlock()
	if already {
		return errors.New("session already created")
	}
	if s.kind == "codex" {
		var r struct {
			Model    string `json:"model"`
			Provider string `json:"modelProvider"`
			Cwd      string `json:"cwd"`
			Approval string `json:"approvalPolicy"`
			Reviewer string `json:"approvalsReviewer"`
			Sandbox  struct {
				Type    string `json:"type"`
				Network bool   `json:"networkAccess"`
			} `json:"sandbox"`
			Thread struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionId"`
			} `json:"thread"`
		}
		if err := s.t.Call(ctx, "thread/start", s.profile.NativeParams, &r); err != nil {
			return err
		}
		if r.Thread.ID == "" || r.Model != s.profile.Model || r.Provider != s.profile.Provider || r.Cwd != s.profile.Workspace || r.Approval != "on-request" || r.Reviewer != "user" || r.Sandbox.Type != "workspaceWrite" || r.Sandbox.Network {
			return errors.New("Codex effective thread configuration mismatch")
		}
		s.mu.Lock()
		s.snapshot.ThreadID = SessionID(r.Thread.ID)
		s.snapshot.RuntimeID = SessionID(r.Thread.SessionID)
		s.snapshot.DurableID = SessionID(r.Thread.ID)
		s.created = true
		s.mu.Unlock()
		return nil
	}
	var r struct {
		ID     string          `json:"session_id"`
		Stored string          `json:"stored_session_id"`
		Info   json.RawMessage `json:"info"`
	}
	if err := s.t.Call(ctx, "session.create", s.profile.NativeParams, &r); err != nil {
		return err
	}
	if r.ID == "" || r.Stored == "" {
		return errors.New("missing Hermes session identity")
	}
	s.mu.Lock()
	s.snapshot.RuntimeID = SessionID(r.ID)
	s.snapshot.DurableID = SessionID(r.Stored)
	s.mu.Unlock()
	// session.info is an event, not an RPC. Activate reads the owned session snapshot.
	for {
		var r struct {
			Info json.RawMessage `json:"info"`
		}
		if err := s.t.Call(ctx, "session.activate", map[string]any{"session_id": s.Inspect().RuntimeID, "omit_messages": true}, &r); err != nil {
			return err
		}
		if !boolField(object(r.Info), "lazy") {
			s.mu.Lock()
			err := s.validateHermesInfoLocked(r.Info)
			if err == nil {
				s.created = true
			}
			s.mu.Unlock()
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Session) Submit(ctx context.Context, turn AppTurnID, prompt string, schema any) error {
	s.mu.Lock()
	if !s.created || s.submitted || len(s.requests) > 0 || s.fault != nil {
		s.mu.Unlock()
		return errors.New("session not eligible for submission")
	}
	s.submitted = true
	s.snapshot.AppTurn = turn
	s.snapshot.Outcome = "active"
	s.snapshot.IdleObserved = false
	id := s.snapshot.ThreadID
	if s.kind == "hermes" {
		id = s.snapshot.RuntimeID
	}
	s.mu.Unlock()
	var err error
	if s.kind == "codex" {
		var r struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		err = s.t.Call(ctx, "turn/start", map[string]any{"threadId": id, "input": []any{map[string]any{"type": "text", "text": prompt}}, "outputSchema": schema}, &r)
		if err == nil {
			s.mu.Lock()
			s.bindTurnLocked(r.Turn.ID)
			s.mu.Unlock()
		}
	} else {
		var r struct {
			Status string `json:"status"`
		}
		err = s.t.Call(ctx, "prompt.submit", map[string]any{"session_id": id, "text": prompt}, &r)
		if err == nil && r.Status != "streaming" {
			err = errors.New("Hermes did not acknowledge an exclusive fresh turn")
		}
	}
	if err != nil {
		s.mu.Lock()
		s.failLocked(fmt.Errorf("submission failed or uncertain: %w", err))
		s.mu.Unlock()
	}
	return err
}

func (s *Session) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	s.retireLocked("cancelled")
	snap := s.snapshot
	s.mu.Unlock()
	if s.kind == "hermes" && snap.RuntimeID != "" {
		return s.t.Call(ctx, "session.interrupt", map[string]any{"session_id": snap.RuntimeID}, nil)
	}
	if snap.ThreadID != "" && snap.NativeTurn != "" {
		return s.t.Call(ctx, "turn/interrupt", map[string]any{"threadId": snap.ThreadID, "turnId": snap.NativeTurn}, nil)
	}
	return errors.New("no known active native turn to interrupt")
}
func (s *Session) Refresh(ctx context.Context) error {
	if s.kind != "hermes" {
		return s.Err()
	}
	snap := s.Inspect()
	var r struct {
		Info json.RawMessage `json:"info"`
	}
	if err := s.t.Call(ctx, "session.activate", map[string]any{"session_id": snap.RuntimeID, "omit_messages": true}, &r); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateHermesInfoLocked(r.Info)
}
func (s *Session) Close() {
	s.mu.Lock()
	s.closing = true
	s.retireLocked("cancelled")
	s.mu.Unlock()
	s.t.Close()
	<-s.pumpDone
}
