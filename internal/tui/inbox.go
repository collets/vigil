package tui

import (
	"encoding/json"
	"fmt"
	"time"

	"vigil/internal/core"
)

// inboxBindings are the decision keys the Inbox screen owns. Every one binds
// the focused request ID; none fires from any other screen.
func inboxBindings() []Binding {
	return []Binding{
		{Key: "y", Action: "allow", Command: "permission.grant"},
		// No command: n resolves per focused kind (deny grants with a deny
		// decision, proposal rows reject, inputs dismiss or cancel), so no
		// single command describes it.
		{Key: "n", Action: "deny/reject/cancel", Command: ""},
		{Key: "g", Action: "apply-proposal", Command: "planning.proposal.apply"},
		{Key: "v", Action: "request-revision", Command: "planning.proposal.decide"},
		{Key: "x", Action: "exact-resume", Command: "project execution-recovery-choose"},
		{Key: "f", Action: "fresh-context", Command: "project execution-recovery-choose"},
		{Key: "b", Action: "remain-blocked", Command: "project execution-recovery-choose"},
		{Key: "i", Action: "answer", Command: "input.resolve"},
	}
}

func inboxLines(m *model, s *core.DashboardSnapshot) []string {
	lines := []string{"Resolve the displayed revision only: y allow · n deny/reject/cancel · g apply · v revise.", "Recovery: x exact resume · f fresh context · b remain blocked. Native input: i answer.", "Task acceptance and manual Pass remain distinct revision-bound actions.", "Showing up to 100 pending/expired decisions.", ""}
	for index, entry := range s.Inbox {
		marker := "  "
		if index == m.inbox {
			marker = "> "
		}
		lines = append(lines, marker+clean(entry.ID)+" · "+clean(entry.Kind)+" · "+clean(entry.State))
		lines = append(lines, fmt.Sprintf("  Plan %s · Task %s r%d · Run %s", clean(entry.PlanID), clean(entry.TaskID), entry.TaskRevision, clean(entry.RunID)))
		if entry.SessionID != "" {
			lines = append(lines, "  Session/generation: "+clean(entry.SessionID)+" / "+clean(entry.NativeRequestKey))
		}
		if entry.Deadline > 0 {
			lines = append(lines, "  Expires: "+time.UnixMilli(entry.Deadline).Format(time.RFC3339))
		}
		if entry.GrantID != "" {
			lines = append(lines, "  Grant: "+clean(entry.GrantID)+" · "+clean(entry.GrantScope)+" · origin "+clean(entry.GrantOrigin), fmt.Sprintf("  Revoked: %d", entry.GrantRevokedAt))
		}
		if entry.OperationID != "" {
			lines = append(lines, "  Operation: "+clean(entry.OperationID), "  Resource digest: "+clean(entry.ResourceDigest), "  Arguments digest: "+clean(entry.ArgumentsDigest), fmt.Sprintf("  Policy revision: %d · decision scope: once", entry.PolicyEpoch))
		}
		if entry.Kind == "approval" {
			var proposal struct {
				ID        string `json:"proposal_id"`
				Revision  int    `json:"proposal_revision"`
				Digest    string `json:"definition_digest"`
				Operation string `json:"operation"`
				Actor     string `json:"proposal_actor"`
			}
			if json.Unmarshal(entry.Context, &proposal) == nil && proposal.ID != "" {
				lines = append(lines, fmt.Sprintf("  Proposal: %s r%d · %s · author %s", clean(proposal.ID), proposal.Revision, clean(proposal.Operation), clean(proposal.Actor)), "  Definition digest: "+clean(proposal.Digest))
			}
		}
		if entry.Kind == "input" && entry.SessionID != "" {
			var clarification struct {
				Prompt json.RawMessage `json:"prompt"`
			}
			if json.Unmarshal(entry.Context, &clarification) == nil && len(clarification.Prompt) > 0 {
				lines = append(lines, "  Native prompt (untrusted): "+boundedText(string(clarification.Prompt), 1024))
			}
		}
		if entry.Kind == "recovery" {
			var recovery struct {
				Reason string `json:"reason"`
				Stage  string `json:"stage"`
			}
			if json.Unmarshal(entry.Context, &recovery) == nil && recovery.Reason != "" {
				lines = append(lines, "  Recovery reason: "+clean(recovery.Reason)+" · "+clean(recovery.Stage))
			}
		}
		var request core.OperationRequest
		if json.Unmarshal(entry.Context, &request) == nil && request.Category != "" {
			lines = append(lines, "  Action: "+clean(request.Category))
			if request.TaskID != "" {
				lines = append(lines, "  Task: "+clean(request.TaskID))
			}
		}
	}
	if len(s.Inbox) == 0 {
		lines = append(lines, "No pending decisions.")
	}
	return lines
}
