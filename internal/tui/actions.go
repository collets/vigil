package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/quality"
	"vigil/internal/store"
	"vigil/internal/supervisor"
)

// ActionStrings is the exact set of mutation action families the interface
// drives, from 6.1 §1.2. The 6.2 refactor moves where keys are bound but must
// not change what can be done: TestActionStringsUnchanged pins this set, so a
// refactor cannot quietly drop an action. Refresh ("r") is navigation, not a
// mutation, and is not listed here. Parameterized actions carry their
// displayed binding after a colon (allow:ID, queue:ID:rank); the family name
// below is what is pinned, and the mutator validates the suffix against the
// visible snapshot.
var ActionStrings = []string{
	"pause",
	"continue",
	"advance",
	"queue",
	"stop",
	"apply-proposal",
	"reject-proposal",
	"request-proposal-revision",
	"remain-blocked",
	"exact-resume",
	"fresh-context",
	"answer-clarification",
	"cancel-clarification",
	"answer-input",
	"dismiss-input",
	"allow",
	"deny",
	"human-accept",
	"manual-pass",
	"accept-task",
}

var actionSet = func() map[string]bool {
	set := make(map[string]bool, len(ActionStrings))
	for _, action := range ActionStrings {
		set[action] = true
	}
	return set
}()

// Treat persisted names/messages as text, never terminal control sequences.
func clean(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, value)
}

func boundedText(value string, limit int) string {
	value = clean(value)
	runes := []rune(value)
	if limit < 1 || len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func proposalRequest(entry core.InboxEntry) bool {
	var proposal struct {
		ID       string `json:"proposal_id"`
		Revision int    `json:"proposal_revision"`
	}
	return json.Unmarshal(entry.Context, &proposal) == nil && store.SafeID(proposal.ID) && proposal.Revision > 0
}

func projectMutator(engine *core.Engine, coordination *coordinator.Coordinator, owner *supervisor.InteractiveOwner) mutator {
	return func(ctx context.Context, s core.DashboardSnapshot, action string) error {
		revision := s.Readiness.Project.Revision
		switch action {
		case "pause":
			_, err := engine.Pause(ctx, store.ID(), revision)
			return err
		case "continue":
			_, err := engine.Continue(ctx, store.ID(), revision)
			return err
		case "advance":
			_, err := engine.Advance(ctx, store.ID(), revision)
			return err
		case "stop":
			if s.ActiveRun == "" {
				return fmt.Errorf("no active persisted run")
			}
			_, err := (&supervisor.Runner{Engine: engine}).RequestStop(ctx, supervisor.StopRequest{CommandID: store.ID(), ExpectedRevision: revision, RunID: s.ActiveRun, InterruptGrace: 2 * time.Second, TerminateGrace: 5 * time.Second})
			return err
		default:
			// Explicit queue selection from the Overview queue cursor:
			// queue:PLAN_ID:RANK binds the visibly displayed plan at
			// its displayed rank. The rank must still match the
			// snapshot, so a stale cursor cannot queue a moved plan.
			// There is no implicit-first fallback: the 6.1-F2 defect
			// was exactly such a fallback, so a bare "queue" is
			// rejected as unknown below.
			if rest, ok := strings.CutPrefix(action, "queue:"); ok {
				planID, rankText, found := strings.Cut(rest, ":")
				rank, atoiErr := strconv.Atoi(rankText)
				if !found || !store.SafeID(planID) || atoiErr != nil || rank < 0 {
					return fmt.Errorf("invalid displayed queue selection")
				}
				displayed := false
				for _, p := range s.Queue {
					if p.ID == planID && p.Rank == rank {
						displayed = true
					}
				}
				if !displayed {
					return fmt.Errorf("displayed queue selection is stale")
				}
				_, err := engine.QueuePlan(ctx, store.ID(), revision, planID, rank)
				return err
			}
			decision, requestID, found := strings.Cut(action, ":")
			if decision == "human-accept" || decision == "manual-pass" || decision == "accept-task" {
				taskID, revisionText, ok := strings.Cut(requestID, ":")
				criterionID := ""
				if decision == "manual-pass" {
					revisionText, criterionID, ok = strings.Cut(revisionText, ":")
				}
				var displayedRevision int
				_, scanErr := fmt.Sscanf(revisionText, "%d", &displayedRevision)
				if !ok || !store.SafeID(taskID) || scanErr != nil || displayedRevision < 1 {
					return fmt.Errorf("invalid displayed task action")
				}
				var planID string
				var currentRevision int
				if err := engine.DB.SQL.QueryRowContext(ctx, "SELECT plan_id,revision FROM tasks WHERE id=?", taskID).Scan(&planID, &currentRevision); err != nil {
					return err
				}
				if currentRevision != displayedRevision {
					return fmt.Errorf("displayed task revision is stale")
				}
				target := quality.Target{Kind: "task", PlanID: planID, TaskID: taskID}
				switch decision {
				case "human-accept":
					_, err := quality.RecordHumanDecision(ctx, engine, quality.HumanDecisionRequest{CommandID: store.ID(), Target: target, Action: "accept", Rationale: "Explicit acceptance from Vigil dashboard", Actor: "human", ExpectedTaskRevision: displayedRevision})
					return err
				case "manual-pass":
					if !store.SafeID(criterionID) {
						return fmt.Errorf("invalid displayed manual criterion")
					}
					_, err := quality.RecordManual(ctx, engine, quality.ManualRequest{CommandID: store.ID(), Target: target, CriterionID: criterionID, State: "pass", Evaluator: "Vigil dashboard user", Notes: "Explicit Pass entered in Vigil dashboard", Actor: "human", ExpectedTaskRevision: displayedRevision})
					return err
				case "accept-task":
					if coordination == nil {
						return fmt.Errorf("acceptance owner unavailable")
					}
					owner, err := coordination.Register(ctx)
					if err != nil {
						return err
					}
					defer owner.Close()
					_, err = (&quality.Acceptor{Engine: engine, Owner: owner}).Accept(ctx, quality.AcceptanceRequest{CommandID: store.ID(), Target: target, Actor: "core", ExpectedTaskRevision: displayedRevision})
					return err
				}
			}
			if (decision != "allow" && decision != "deny" && decision != "apply-proposal" && decision != "reject-proposal" && decision != "request-proposal-revision" && decision != "remain-blocked" && decision != "exact-resume" && decision != "fresh-context" && decision != "answer-clarification" && decision != "cancel-clarification" && decision != "answer-input" && decision != "dismiss-input") || !found || requestID == "" {
				return fmt.Errorf("unknown dashboard action")
			}
			answerText := ""
			if decision == "answer-clarification" || decision == "answer-input" {
				var encoded string
				requestID, encoded, found = strings.Cut(requestID, ":")
				decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
				if !found || decodeErr != nil || len(decoded) == 0 || len(decoded) > 4096 {
					return fmt.Errorf("invalid bounded clarification answer")
				}
				answerText = string(decoded)
			}
			if len(s.Inbox) == 0 {
				return fmt.Errorf("no displayed pending request")
			}
			var entry core.InboxEntry
			for _, candidate := range s.Inbox {
				if candidate.ID == requestID {
					entry = candidate
					break
				}
			}
			if entry.ID == "" {
				return fmt.Errorf("focused request is no longer displayed")
			}
			if decision == "apply-proposal" {
				if entry.Kind != "approval" {
					return fmt.Errorf("displayed request is not a planning approval")
				}
				var proposal struct {
					ID       string `json:"proposal_id"`
					Revision int    `json:"proposal_revision"`
				}
				if err := json.Unmarshal(entry.Context, &proposal); err != nil || !store.SafeID(proposal.ID) || proposal.Revision < 1 {
					return fmt.Errorf("displayed approval is not a closed planning proposal")
				}
				payload, _ := json.Marshal(map[string]any{"proposal_id": proposal.ID, "proposal_revision": proposal.Revision, "authorize_criteria_changes": false})
				_, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: "planning.proposal.apply", Payload: payload})
				return err
			}
			if decision == "reject-proposal" || decision == "request-proposal-revision" {
				if entry.Kind != "approval" {
					return fmt.Errorf("displayed request is not a planning approval")
				}
				var proposal struct {
					ID       string `json:"proposal_id"`
					Revision int    `json:"proposal_revision"`
				}
				if err := json.Unmarshal(entry.Context, &proposal); err != nil || !store.SafeID(proposal.ID) || proposal.Revision < 1 {
					return fmt.Errorf("displayed approval is not a closed planning proposal")
				}
				action, rationale := "reject", "Explicit rejection from Vigil dashboard"
				if decision == "request-proposal-revision" {
					action, rationale = "request_revision", "Explicit replacement revision requested from Vigil dashboard"
				}
				payload, _ := json.Marshal(map[string]any{"proposal_id": proposal.ID, "proposal_revision": proposal.Revision, "action": action, "rationale": rationale})
				_, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: "planning.proposal.decide", Payload: payload})
				return err
			}
			if decision == "remain-blocked" || decision == "exact-resume" || decision == "fresh-context" {
				if entry.Kind != "recovery" || entry.RunID == "" {
					return fmt.Errorf("displayed request is not a recovery choice")
				}
				mode := map[string]string{"remain-blocked": "remain_blocked", "exact-resume": "exact_resume", "fresh-context": "fresh_context"}[decision]
				if owner == nil {
					if mode != "remain_blocked" {
						return fmt.Errorf("live recovery owner is unavailable")
					}
					_, err := (&supervisor.Runner{Engine: engine}).ChooseRecovery(ctx, supervisor.RecoveryChoiceRequest{CommandID: store.ID(), ExpectedRevision: revision, RunID: entry.RunID, Mode: mode, DisplayedRequestID: entry.ID, ExpectedTaskRevision: entry.TaskRevision}, nil)
					return err
				}
				_, err := owner.ChooseRecovery(ctx, supervisor.RecoveryChoiceRequest{CommandID: store.ID(), ExpectedRevision: revision, RunID: entry.RunID, Mode: mode, DisplayedRequestID: entry.ID, ExpectedTaskRevision: entry.TaskRevision})
				return err
			}
			if decision == "answer-clarification" || decision == "cancel-clarification" {
				if entry.Kind != "input" || entry.SessionID == "" || entry.NativeRequestKey == "" {
					return fmt.Errorf("displayed request is not an owner-routed native clarification")
				}
				if owner == nil {
					return fmt.Errorf("live clarification owner is unavailable")
				}
				nativeDecision := "answer"
				if decision == "cancel-clarification" {
					nativeDecision = "cancel"
				}
				_, err := owner.AnswerClarification(ctx, supervisor.ClarificationAnswerRequest{CommandID: store.ID(), ExpectedRevision: revision, RequestID: entry.ID, SessionID: entry.SessionID, NativeRequestKey: entry.NativeRequestKey, Decision: nativeDecision, Answer: answerText})
				return err
			}
			if decision == "answer-input" || decision == "dismiss-input" {
				if entry.Kind != "input" || entry.SessionID != "" || entry.NativeRequestKey != "" {
					return fmt.Errorf("displayed request is not a non-native input request")
				}
				inputDecision := "answer"
				if decision == "dismiss-input" {
					inputDecision = "dismiss"
				}
				payload, _ := json.Marshal(map[string]any{"request_id": entry.ID, "decision": inputDecision, "answer": answerText})
				_, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: "input.resolve", Payload: payload})
				return err
			}
			if entry.Kind != "approval" {
				return fmt.Errorf("displayed request requires its distinct %s action", entry.Kind)
			}
			var proposalContext struct {
				ProposalID string `json:"proposal_id"`
			}
			_ = json.Unmarshal(entry.Context, &proposalContext)
			if proposalContext.ProposalID != "" {
				return fmt.Errorf("planning approval requires proposal-apply for the displayed exact revision")
			}
			payload, _ := json.Marshal(core.GrantRequest{RequestID: entry.ID, Scope: "once", Decision: decision})
			_, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: "permission.grant", Payload: payload})
			return err
		}
	}
}
