package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExactResumeAndRejectedSubstitutions(t *testing.T) {
	for _, mode := range []string{"codex-resume", "hermes-resume", "codex-resume-corrupt", "codex-resume-workspace", "hermes-resume-corrupt", "hermes-resume-alias", "hermes-resume-auto", "hermes-resume-workspace"} {
		t.Run(mode, func(t *testing.T) {
			kind, provider, id := "codex", "openai", SessionID("thread-1")
			if strings.HasPrefix(mode, "hermes") {
				kind, provider, id = "hermes", "custom", "stored-1"
			}
			tr := testTransport(t, mode, nil)
			s, err := NewSession(kind, tr, Profile{Model: "fixture-model", Provider: provider, Workspace: "/fixture"}, "run", "new", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = s.Probe(ctx); err != nil {
				t.Fatal(err)
			}
			prior := Snapshot{Harness: kind, DurableID: id, Generation: "old", Model: "fixture-model", Provider: provider, Workspace: "/fixture"}
			err = s.Resume(ctx, prior)
			valid := mode == "codex-resume" || mode == "hermes-resume"
			if valid {
				if err != nil {
					t.Fatal(err)
				}
				snap := s.Inspect()
				if snap.DurableID != id || snap.HistoryItems < 1 || snap.Outcome != "not_started" {
					t.Fatal("resume lost identity or started work")
				}
			}
			if !valid {
				if err == nil {
					t.Fatal("invalid resume accepted")
				}
				if err = s.Submit(ctx, "new-turn", "must not submit", nil); err == nil {
					t.Fatal("dispatch allowed after invalid resume")
				}
			}
		})
	}
}

func TestNarrowOfferedDecisionsAndClarification(t *testing.T) {
	r := Request{Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"availableDecisions":["decline"]}`)}
	if _, _, err := nativeAnswer(r, Answer{Decision: "allow-once"}); err == nil {
		t.Fatal("unoffered approval accepted")
	}
	if _, _, err := nativeAnswer(r, Answer{Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	r.Params = json.RawMessage(`{"availableDecisions":["accept",{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":["printf"]}},"cancel"]}`)
	result, rpcErr, err := nativeAnswer(r, Answer{Decision: "deny"})
	if err != nil || rpcErr != nil || result.(map[string]any)["decision"] != "cancel" {
		t.Fatal("denial did not choose offered narrow cancellation")
	}
	r = Request{Method: "clarify", Params: json.RawMessage(`{"questions":[{"qid":"color","question":"Color?"}]}`)}
	if _, _, err := nativeAnswer(r, Answer{Decision: "answer", Answers: map[string][]string{"color": {"blue"}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := nativeAnswer(r, Answer{Decision: "answer", Answers: map[string][]string{"wrong": {"blue"}}}); err == nil {
		t.Fatal("foreign question accepted")
	}
}

func TestNativeCancelRetiresRequest(t *testing.T) {
	s := testSession(t, "codex-request")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Submit(ctx, "turn", "prompt", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.wait(ctx, func() bool { return len(s.requests) == 1 }); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	var r Request
	for _, v := range s.requests {
		r = v
	}
	s.mu.Unlock()
	s.handle(Message{Method: "serverRequest/resolved", Params: json.RawMessage(`{"requestId":"approval-1"}`)})
	if s.Inspect().Pending != 0 {
		t.Fatal("cancelled request remained")
	}
	if err := s.Answer(ctx, r, Answer{Decision: "allow-once"}); err == nil {
		t.Fatal("cancelled request accepted late answer")
	}
}
