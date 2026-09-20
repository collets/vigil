package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fakeProtocol(mode string, m Message, send func(any)) {
	result := any(map[string]any{})
	notify := func(method string, params any) {
		b, _ := json.Marshal(params)
		send(Message{Method: method, Params: b})
	}
	info := map[string]any{"model": "fixture-model", "provider": "custom", "cwd": "/fixture", "approval_mode": "manual", "running": false, "stored_session_id": "stored-1"}
	switch m.Method {
	case "account/read":
		result = map[string]any{"account": map[string]string{"type": "chatgpt"}}
	case "model/list":
		result = map[string]any{"data": []any{map[string]string{"model": "fixture-model"}}}
	case "thread/read":
		thread := map[string]any{"id": "thread-1", "cwd": "/fixture", "modelProvider": "openai", "turns": []any{map[string]string{"id": "old-turn"}}}
		if mode == "codex-resume-corrupt" {
			thread["turns"] = []any{}
		}
		if mode == "codex-resume-workspace" {
			thread["cwd"] = "/other"
		}
		result = map[string]any{"thread": thread}
	case "thread/start", "thread/resume":
		result = map[string]any{"model": "fixture-model", "modelProvider": "openai", "cwd": "/fixture", "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandbox": map[string]any{"type": "workspaceWrite", "networkAccess": false}, "thread": map[string]string{"id": "thread-1", "sessionId": "native-1"}}
	case "turn/start":
		turn := map[string]any{"id": "turn-1", "status": "completed", "items": []any{map[string]string{"type": "agentMessage", "text": `{"summary":"ok","files":["message.txt"]}`}}}
		notify("turn/started", map[string]any{"threadId": "thread-1", "turn": map[string]string{"id": "turn-1"}})
		notify("item/commandExecution/outputDelta", map[string]string{"threadId": "thread-1", "turnId": "turn-1", "delta": "fixture output"})
		if mode == "codex-request" {
			send(Message{ID: json.RawMessage(`"approval-1"`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1"}`)})
		} else {
			notify("turn/completed", map[string]any{"threadId": "thread-1", "turn": turn})
			if mode == "codex-conflict" {
				turn["status"] = "failed"
			}
			notify("turn/completed", map[string]any{"threadId": "thread-1", "turn": turn})
		}
		result = map[string]any{"turn": map[string]string{"id": "turn-1", "status": "inProgress"}}
	case "gateway.capabilities":
		result = map[string]any{"per_session_exclusive_submit": true}
	case "client.capabilities":
		result = map[string]any{"server_requests": []string{"approval", "clarify"}}
	case "session.create":
		result = map[string]any{"session_id": "runtime-1", "stored_session_id": "stored-1", "info": info}
	case "session.activate":
		result = map[string]any{"info": info}
	case "session.resume":
		r := map[string]any{"session_id": "new-runtime", "resumed": "stored-1", "message_count": 2, "info": info, "running": false}
		if mode == "hermes-resume-corrupt" {
			r["message_count"] = 0
		}
		if mode == "hermes-resume-alias" {
			r["resumed"] = "other-stored"
		}
		if mode == "hermes-resume-auto" {
			r["auto_continue"] = map[string]int{"attempt": 1}
		}
		if mode == "hermes-resume-workspace" {
			info["cwd"] = "/other"
		}
		result = r
	case "prompt.submit":
		result = map[string]string{"status": "streaming"}
		if mode == "hermes-queued" {
			result = map[string]string{"status": "queued"}
			break
		}
		payload := map[string]any{"status": "complete", "text": `{"summary":"ok","files":["message.txt"]}`, "usage": map[string]int{"prompt": 10, "completion": 3, "total": 13}}
		if mode == "hermes-missing" {
			delete(payload, "status")
		}
		if mode == "hermes-error" {
			payload["error"] = "failed"
		}
		notify("event", map[string]any{"type": "tool.start", "session_id": "runtime-1", "payload": map[string]string{"tool_id": "t1", "name": "write_file"}})
		notify("event", map[string]any{"type": "message.complete", "session_id": "runtime-1", "payload": payload})
	}
	b, _ := json.Marshal(result)
	send(Message{ID: m.ID, Result: b})
}
func testSession(t *testing.T, mode string) *Session {
	t.Helper()
	kind := "codex"
	provider := "openai"
	if strings.HasPrefix(mode, "hermes") {
		kind = "hermes"
		provider = "custom"
	}
	tr := testTransport(t, mode, nil)
	s, err := NewSession(kind, tr, Profile{Model: "fixture-model", Provider: provider, Workspace: "/fixture", NativeParams: map[string]any{}}, "run-1", "gen-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = s.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestTerminalNormalizationAndEarlyEvents(t *testing.T) {
	for _, tc := range []struct{ mode, outcome string }{{"codex", "completed"}, {"hermes", "completed"}, {"hermes-missing", "unknown"}, {"hermes-error", "failed"}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := testSession(t, tc.mode)
			if err := s.Submit(context.Background(), "app-turn", "prompt", nil); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := s.wait(ctx, func() bool { return s.terminal }); err != nil {
				t.Fatal(err)
			}
			snap := s.Inspect()
			if snap.Outcome != tc.outcome {
				t.Fatalf("outcome=%s", snap.Outcome)
			}
			if tc.mode == "codex" && snap.NativeTurn != "turn-1" {
				t.Fatal("missing turn id")
			}
			if tc.mode == "hermes" && (snap.NativeTurn != "" || snap.RuntimeID == snap.DurableID) {
				t.Fatal("identity conflated")
			}
			if snap.Usage.Cost != nil {
				t.Fatal("invented cost")
			}
			if err := s.Submit(ctx, "second", "no", nil); err == nil {
				t.Fatal("second turn admitted")
			}
		})
	}
}
func TestConflictingTerminalAndQueuedSubmit(t *testing.T) {
	for _, mode := range []string{"codex-conflict", "hermes-queued"} {
		t.Run(mode, func(t *testing.T) {
			s := testSession(t, mode)
			_ = s.Submit(context.Background(), "app-turn", "prompt", nil)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := s.wait(ctx, func() bool { return false })
			if err == nil || err == context.DeadlineExceeded {
				t.Fatal("conflict did not fail")
			}
		})
	}
}
func TestAnswerRejectsStaleGenerationAndDuplicate(t *testing.T) {
	s := testSession(t, "codex-request")
	if err := s.Submit(context.Background(), "app-turn", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	var request Request
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case e := <-s.Events():
			if e.Request != nil {
				request = *e.Request
				break loop
			}
		case <-deadline:
			t.Fatal("missing request")
		}
	}
	stale := request
	stale.Generation = "old"
	if err := s.Answer(context.Background(), stale, Answer{Decision: "allow-once"}); err == nil {
		t.Fatal("accepted stale generation")
	}
	if err := s.Answer(context.Background(), request, Answer{Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Answer(context.Background(), request, Answer{Decision: "deny"}); err == nil {
		t.Fatal("accepted duplicate answer")
	}
}
func TestNativeAnswersNeverGrantPermanentAccess(t *testing.T) {
	for _, method := range []string{"approval", "item/commandExecution/requestApproval", "item/fileChange/requestApproval"} {
		r := Request{Method: method, Params: json.RawMessage(`{}`)}
		if _, _, err := nativeAnswer(r, Answer{Decision: "always"}); err == nil {
			t.Fatal("permanent grant admitted")
		}
		result, _, err := nativeAnswer(r, Answer{Decision: "allow-once"})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "Session") || strings.Contains(string(raw), "always") {
			t.Fatal("broad grant")
		}
	}
}
func TestRequestExpiryAndCancellation(t *testing.T) {
	s := testSession(t, "codex-request")
	_ = s.Submit(context.Background(), "app-turn", "prompt", nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.wait(ctx, func() bool { return len(s.requests) > 0 }); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	var r Request
	for key, req := range s.requests {
		req.Deadline = time.Now().Add(-time.Second)
		s.requests[key] = req
		r = req
	}
	s.mu.Unlock()
	if err := s.Answer(ctx, r, Answer{Decision: "allow-once"}); err == nil {
		t.Fatal("expired answer accepted")
	}
	if err := s.Expire(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Inspect().Pending != 0 {
		t.Fatal("expired request retained")
	}
}
