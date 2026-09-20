package spike

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vigil/internal/harness"
)

type Experiment struct {
	Scenario     string             `json:"scenario"`
	Passed       bool               `json:"passed"`
	Observations map[string]any     `json:"observations"`
	Sessions     []harness.Snapshot `json:"sessions,omitempty"`
}

func validScenario(s string) bool {
	switch s {
	case "resume", "interrupt", "child", "loss", "clarify", "approval-allow", "approval-deny":
		return true
	}
	return false
}

// lifecycle is deliberately separate from task acceptance and the Stage 2 edit check.
// It operates only inside an already validated, single-use prepared experiment.
func lifecycle(ctx context.Context, opt Options, m Manifest, s *harness.Session, tr *harness.Transport, spawn func(string) (*harness.Transport, *harness.Session, error), j *journal, report *Report) error {
	x := report.Experiment
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.Limits.Active)*time.Second)
	defer cancel()
	if err := s.Create(ctx); err != nil {
		return err
	}
	prompt := "Do not use tools or change files. Write a long numbered list of 2000 imaginary starship names, one per line. Start immediately."
	token := "VIGIL-" + string(s.Inspect().Run)
	switch opt.Scenario {
	case "resume":
		prompt = "Remember this exact private fixture token for a follow-up: " + token + ". Do not write files or use tools. Reply only with that token."
	case "child", "loss":
		// Self-limiting even if both the controller and native interrupt fail.
		script := `import os,time
from pathlib import Path
Path("writer.pid").write_text(str(os.getpid()))
with open("heartbeat.txt","a", buffering=1) as f:
 for i in range(150):
  f.write(str(i)+"\n")
  time.sleep(0.2)
`
		if err := os.WriteFile(filepath.Join(m.Workspace, "heartbeat.py"), []byte(script), 0600); err != nil {
			return err
		}
		prompt = "Run exactly this foreground command using your terminal tool: python3 " + filepath.Join(m.Workspace, "heartbeat.py") + " . Do not inspect files first, do not detach it or use background mode, do not modify files or run other commands. This controller-owned fixture writes only a bounded heartbeat for 30 seconds. Wait for it to finish."
	case "clarify":
		prompt = "Use your native clarification/user-input tool to ask the operator: Which fixture color should I remember, blue or green? Do not guess, use other tools, or modify files. After the answer reply with that color only."
	case "approval-allow", "approval-deny":
		if opt.Harness != "codex" {
			return errors.New("model approval scenario is Codex-specific; Hermes native paths have separate deterministic probes")
		}
		prompt = "Use exec_command to request sandbox_permissions require_escalated for exactly the harmless command printf 'vigil-approval-probe\\n'. Set justification to 'Controlled approval fixture'. Do not run any other command, read or edit files, or retry a denied command. Report the result briefly."
	}
	report.Submitted = true
	if err := s.Submit(ctx, harness.AppTurnID(token+"-first"), prompt, nil); err != nil {
		return err
	}
	triggered := false
	requested := false
	answered := false
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if s.Err() != nil {
			return s.Err()
		}
		snap := s.Inspect()
		if snap.Outcome != "active" && snap.Outcome != "not_started" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-s.Events():
			if err := recordEvent(j, opt.Output, report, e); err != nil {
				return err
			}
			if opt.Scenario == "interrupt" && e.Kind == "output" && !triggered {
				triggered = true
				at := time.Now()
				err := s.Interrupt(ctx)
				x.Observations["interrupt_acknowledged"] = err == nil
				x.Observations["interrupt_ack_seconds"] = time.Since(at).Seconds()
				if err != nil {
					return err
				}
			}
			if e.Request != nil && (e.Kind == "input_requested" || e.Kind == "approval_requested" || e.Kind == "unsupported_request") {
				requested = true
				if e.Kind == "approval_requested" {
					var p map[string]json.RawMessage
					_ = json.Unmarshal(e.Request.Params, &p)
					// This experiment's literal printf stimulus is public. Record only
					// its native choice structure and whether the exact command matches.
					var choices []json.RawMessage
					_ = json.Unmarshal(p["availableDecisions"], &choices)
					var safe []string
					for _, raw := range choices {
						var choice string
						_ = json.Unmarshal(raw, &choice)
						switch choice {
						case "accept", "decline", "cancel", "acceptForSession":
							safe = append(safe, choice)
						default:
							safe = append(safe, "structured-or-unknown-choice")
						}
					}
					x.Observations["approval_choices"] = safe
					x.Observations["exact_approval_command"] = safeApprovalFixture(e.Request.Params)
				}
				answer := harness.Answer{Decision: "deny"}
				if opt.Scenario == "clarify" && e.Kind == "input_requested" {
					answer = harness.Answer{Decision: "answer", Text: "blue"}
					var params struct {
						Questions []struct {
							QID string `json:"qid"`
							ID  string `json:"id"`
						} `json:"questions"`
					}
					if json.Unmarshal(e.Request.Params, &params) != nil || len(params.Questions) > 1 {
						return errors.New("unexpected clarification fixture")
					}
					if len(params.Questions) == 1 {
						id := params.Questions[0].QID
						if id == "" {
							id = params.Questions[0].ID
						}
						answer.Answers = map[string][]string{id: {"blue"}}
					}
				}
				if opt.Scenario == "approval-allow" && e.Kind == "approval_requested" {
					// Only this literal harmless stimulus can be allowed by the unattended probe.
					if !safeApprovalFixture(e.Request.Params) {
						return errors.New("approval stimulus differs from allowed fixture command")
					}
					answer.Decision = "allow-once"
				}
				stale := *e.Request
				stale.Generation = "retired-generation"
				x.Observations["stale_answer_rejected"] = s.Answer(ctx, stale, answer) != nil
				if err := s.Answer(ctx, *e.Request, answer); err != nil {
					return err
				}
				answered = true
				x.Observations["duplicate_answer_rejected"] = s.Answer(ctx, *e.Request, answer) != nil
			}
		case <-tick.C:
			if (opt.Scenario == "child" || opt.Scenario == "loss") && !triggered {
				if b, err := os.ReadFile(filepath.Join(m.Workspace, "heartbeat.txt")); err == nil && len(b) >= 4 {
					triggered = true
					x.Observations["writer_observed"] = true
					if opt.Scenario == "loss" {
						tr.Abort()
						<-tr.Done()
						// Wait for the session reader to consume loss before normal Close masks it.
						for s.Err() == nil {
							select {
							case <-ctx.Done():
								return ctx.Err()
							case <-time.After(10 * time.Millisecond):
							}
						}
					} else {
						at := time.Now()
						err := s.Interrupt(ctx)
						x.Observations["interrupt_acknowledged"] = err == nil
						x.Observations["interrupt_ack_seconds"] = time.Since(at).Seconds()
						if err != nil {
							return err
						}
					}
				}
			}
		}
		if opt.Scenario == "loss" && triggered {
			break
		}
	}
	x.Observations["trigger_observed"] = triggered
	x.Observations["native_request_observed"] = requested
	if opt.Harness == "hermes" && opt.Scenario != "loss" {
		if err := s.Refresh(ctx); err != nil {
			return err
		}
	}
	s.Close()
	x.Sessions = append(x.Sessions, s.Inspect())
	if opt.Scenario != "loss" && (s.Err() != nil || tr.Err() != nil) {
		return errors.New("unexpected lifecycle transport failure")
	}
	switch opt.Scenario {
	case "interrupt", "child":
		if !triggered || s.Inspect().Outcome != "interrupted" {
			return errors.New("native interruption not demonstrated")
		}
	case "loss":
		if !triggered || s.Inspect().Outcome != "unknown" {
			return errors.New("transport loss did not preserve unknown outcome")
		}
	case "clarify":
		if !answered || s.Inspect().Outcome != "completed" || !strings.Contains(strings.ToLower(s.Inspect().Output), "blue") {
			return errors.New("native clarification not demonstrated")
		}
	case "approval-allow", "approval-deny":
		if !answered {
			return errors.New("native approval not triggered")
		}
	}
	if opt.Scenario == "child" || opt.Scenario == "loss" {
		a, err := os.ReadFile(filepath.Join(m.Workspace, "heartbeat.txt"))
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		b, err := os.ReadFile(filepath.Join(m.Workspace, "heartbeat.txt"))
		if err != nil {
			return err
		}
		x.Observations["heartbeat_stable_after_close"] = string(a) == string(b)
		x.Observations["heartbeat_preserved"] = len(b) > 0
		if string(a) != string(b) {
			return errors.New("fixture writer survived owned transport close; retained until bounded script exits")
		}
	}
	if opt.Scenario == "resume" {
		prior := s.Inspect()
		if prior.Outcome != "completed" || !strings.Contains(prior.Output, token) {
			return errors.New("initial recall fixture incomplete")
		}
		t2, s2, err := spawn(string(prior.Run) + "-2")
		if err != nil {
			return err
		}
		defer s2.Close()
		if err = s2.Probe(ctx); err != nil {
			return err
		}
		if err = s2.Resume(ctx, prior); err != nil {
			return err
		}
		x.Observations["exact_durable_identity"] = s2.Inspect().DurableID == prior.DurableID
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		if s2.Inspect().Outcome != "not_started" || s2.Err() != nil {
			return errors.New("resume started unsolicited work")
		}
		x.Observations["resume_no_unsolicited_turn"] = true
		if err = s2.Submit(ctx, harness.AppTurnID(token+"-second"), "What exact private fixture token did I ask you to remember? Reply with the token only. Do not use tools or modify files.", nil); err != nil {
			return err
		}
		if err = awaitCompletion(ctx, s2, j, opt, report); err != nil {
			return err
		}
		s2.Close()
		x.Sessions = append(x.Sessions, s2.Inspect())
		if t2.Err() != nil || s2.Err() != nil || !strings.Contains(s2.Inspect().Output, token) {
			return errors.New("resumed history recall not demonstrated")
		}
		x.Observations["history_recalled"] = true
		// Unknown identity must fail on a new connection without a fallback turn.
		_, s3, err := spawn(string(prior.Run) + "-3")
		if err != nil {
			return err
		}
		defer s3.Close()
		if err = s3.Probe(ctx); err != nil {
			return err
		}
		prior.DurableID = "00000000-0000-0000-0000-000000000000"
		err = s3.Resume(ctx, prior)
		x.Observations["unknown_identity_rejected"] = err != nil
		if err == nil {
			return errors.New("unknown resume silently accepted")
		}
	}
	x.Passed = true
	fmt.Fprintf(opt.Output, "Lifecycle experiment %s passed; guarantee scope remains in the report.\n", opt.Scenario)
	return nil
}

func awaitCompletion(ctx context.Context, s *harness.Session, j *journal, opt Options, r *Report) error {
	for {
		if s.Err() != nil {
			return s.Err()
		}
		snap := s.Inspect()
		if snap.Outcome != "active" && snap.Outcome != "not_started" {
			if snap.Outcome != "completed" {
				return errors.New("continuation did not complete")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-s.Events():
			if err := recordEvent(j, opt.Output, r, e); err != nil {
				return err
			}
			if e.Request != nil && strings.HasSuffix(e.Kind, "requested") {
				return errors.New("unexpected continuation request")
			}
		case <-s.Changed():
		}
	}
}

func safeApprovalFixture(raw json.RawMessage) bool {
	var p struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	for _, cmd := range []string{"printf 'vigil-approval-probe\\n'", "printf 'vigil-approval-probe\n'"} {
		if p.Command == cmd {
			return true
		}
		for _, shell := range []string{"/bin/zsh", "/bin/bash", "/bin/sh", "/usr/bin/zsh", "/usr/bin/bash", "/usr/bin/sh"} {
			for _, quoted := range []string{"'" + strings.ReplaceAll(cmd, "'", "'\\''") + "'", "'" + strings.ReplaceAll(cmd, "'", "'\"'\"'") + "'", "\"" + strings.ReplaceAll(cmd, "\\", "\\\\") + "\""} {
				if p.Command == shell+" -lc "+quoted || p.Command == shell+" -c "+quoted {
					return true
				}
			}
		}
	}
	return false
}
