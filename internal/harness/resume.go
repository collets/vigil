package harness

import (
	"context"
	"encoding/json"
	"errors"
)

// Resume attaches only to an application-owned prior identity. The caller must
// revalidate its native-home/config fingerprints and workspace ownership first.
// A failed resume never creates a replacement session or replays a prompt.
func (s *Session) Resume(ctx context.Context, prior Snapshot) error {
	s.mu.Lock()
	if s.created || s.submitted || s.closing || s.fault != nil || prior.DurableID == "" || prior.Harness != s.kind || prior.Workspace != s.profile.Workspace || prior.Model != s.profile.Model || prior.Provider != s.profile.Provider || prior.Generation == s.snapshot.Generation {
		s.mu.Unlock()
		return errors.New("resume requires a fresh generation and matching owned identity/profile")
	}
	s.mu.Unlock()
	if s.kind == "codex" {
		var read struct {
			Thread struct {
				ID, Cwd, ModelProvider string
				Turns                  []json.RawMessage
			}
		}
		if err := s.t.Call(ctx, "thread/read", map[string]any{"threadId": prior.DurableID, "includeTurns": true}, &read); err != nil {
			return err
		}
		if read.Thread.ID != string(prior.DurableID) || read.Thread.Cwd != prior.Workspace || read.Thread.ModelProvider != prior.Provider || len(read.Thread.Turns) == 0 {
			return errors.New("resume history or original workspace/provider mismatch")
		}
		var r struct {
			Model             string
			ModelProvider     string
			Cwd               string
			ApprovalPolicy    string
			ApprovalsReviewer string
			Sandbox           struct {
				Type          string
				NetworkAccess bool
			}
			Thread struct {
				ID        string
				SessionID string
			}
		}
		if err := s.t.Call(ctx, "thread/resume", map[string]any{"threadId": prior.DurableID, "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": "workspace-write"}, &r); err != nil {
			return err
		}
		if r.Thread.ID != string(prior.DurableID) || r.Cwd != prior.Workspace || r.Model != prior.Model || r.ModelProvider != prior.Provider || r.ApprovalPolicy != "on-request" || r.ApprovalsReviewer != "user" || r.Sandbox.Type != "workspaceWrite" || r.Sandbox.NetworkAccess {
			return errors.New("resumed Codex identity/profile mismatch")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.snapshot.ThreadID = prior.DurableID
		s.snapshot.DurableID = prior.DurableID
		s.snapshot.RuntimeID = SessionID(r.Thread.SessionID)
		s.snapshot.HistoryItems = len(read.Thread.Turns)
		s.created = true
		return s.fault
	}
	var r struct {
		ID       string            `json:"session_id"`
		Resumed  string            `json:"resumed"`
		Count    int               `json:"message_count"`
		Info     json.RawMessage   `json:"info"`
		Running  bool              `json:"running"`
		Queued   json.RawMessage   `json:"queued"`
		Auto     json.RawMessage   `json:"auto_continue"`
		Requests []json.RawMessage `json:"open_requests"`
	}
	if err := s.t.Call(ctx, "session.resume", map[string]any{"session_id": prior.DurableID, "eager_build": true, "omit_messages": true, "close_on_disconnect": true}, &r); err != nil {
		return err
	}
	if r.ID == "" || r.Resumed != string(prior.DurableID) || r.Count < 1 || r.Running || present(r.Queued) || present(r.Auto) || len(r.Requests) > 0 {
		return errors.New("resumed Hermes identity/history/activity mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.RuntimeID = SessionID(r.ID)
	s.snapshot.DurableID = prior.DurableID
	if err := s.validateHermesInfoLocked(r.Info); err != nil {
		return err
	}
	s.snapshot.HistoryItems = r.Count
	s.created = true
	return s.fault
}
