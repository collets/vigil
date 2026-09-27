package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/quality"
	"vigil/internal/store"
	"vigil/internal/supervisor"
)

type source func(context.Context) (core.DashboardSnapshot, error)
type loaded struct {
	snapshot core.DashboardSnapshot
	err      error
}
type refresh struct{}
type mutationResult struct {
	action string
	err    error
}
type mutator func(context.Context, core.DashboardSnapshot, string) error
type model struct {
	ctx                                     context.Context
	load                                    source
	snapshot                                *core.DashboardSnapshot
	err                                     error
	loading                                 bool
	mutating                                bool
	feedback                                string
	mutate                                  mutator
	inputRequest                            string
	inputText                               string
	tab, offset, inbox, task, width, height int
}

func (m model) act(action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		defer cancel()
		return mutationResult{action: action, err: m.mutate(ctx, *m.snapshot, action)}
	}
}

func (m model) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 4*time.Second)
		defer cancel()
		snapshot, err := m.load(ctx)
		return loaded{snapshot, err}
	}
}
func tick() tea.Cmd           { return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return refresh{} }) }
func (m model) Init() tea.Cmd { return tea.Batch(m.fetch(), tick()) }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
	case loaded:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.snapshot = &msg.snapshot
			m.inbox = min(m.inbox, max(0, len(msg.snapshot.Inbox)-1))
			m.task = min(m.task, max(0, len(msg.snapshot.Tasks)-1))
		}
	case mutationResult:
		m.mutating = false
		if msg.err != nil {
			m.feedback = msg.action + " failed: " + clean(msg.err.Error())
		} else {
			m.feedback = msg.action + " succeeded"
			m.loading = true
			return m, m.fetch()
		}
	case refresh:
		if m.loading {
			return m, tick()
		}
		m.loading = true
		return m, tea.Batch(m.fetch(), tick())
	case tea.KeyPressMsg:
		if m.inputRequest != "" {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.inputRequest, m.inputText = "", ""
				m.feedback = "clarification answer cancelled locally"
			case "backspace":
				if runes := []rune(m.inputText); len(runes) > 0 {
					m.inputText = string(runes[:len(runes)-1])
				}
			case "enter":
				answer := strings.TrimSpace(m.inputText)
				if answer == "" {
					m.feedback = "clarification answer cannot be empty"
					return m, nil
				}
				action := "answer-clarification:" + m.inputRequest + ":" + base64.RawURLEncoding.EncodeToString([]byte(answer))
				m.inputRequest, m.inputText = "", ""
				m.mutating = true
				m.feedback = "answer-clarification pending"
				return m, m.act(action)
			default:
				if msg.Text != "" && len(m.inputText)+len(msg.Text) <= 4096 {
					m.inputText += msg.Text
				}
			}
			return m, nil
		}
		if m.snapshot != nil && m.mutate != nil && !m.mutating {
			action := ""
			switch msg.String() {
			case "p":
				action = "pause"
			case "c":
				action = "continue"
			case "a":
				action = "advance"
			case "u":
				action = "queue"
			case "s":
				action = "stop"
			case "g":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "apply-proposal:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "b":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "remain-blocked:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "x":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "exact-resume:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "f":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "fresh-context:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "v":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "request-proposal-revision:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "i":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					entry := m.snapshot.Inbox[m.inbox]
					if entry.Kind == "input" && entry.SessionID != "" && entry.NativeRequestKey != "" {
						m.inputRequest = entry.ID
						m.inputText = ""
						m.feedback = "enter clarification answer; Enter submits, Esc cancels locally"
						return m, nil
					}
				}
			case "h", "m", "t":
				if (m.tab == 1 || m.tab == 4) && len(m.snapshot.Tasks) > 0 {
					prefix := map[string]string{"h": "human-accept", "m": "manual-pass", "t": "accept-task"}[msg.String()]
					task := m.snapshot.Tasks[m.task]
					if msg.String() == "m" {
						if len(task.ManualCriteria) > 0 {
							action = fmt.Sprintf("%s:%s:%d:%s", prefix, task.ID, task.Revision, task.ManualCriteria[0])
						}
					} else {
						action = fmt.Sprintf("%s:%s:%d", prefix, task.ID, task.Revision)
					}
				}
			case "y":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "allow:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "n":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					entry := m.snapshot.Inbox[m.inbox]
					switch {
					case entry.Kind == "input" && entry.SessionID != "":
						action = "cancel-clarification:" + entry.ID
					case entry.Kind == "approval" && proposalRequest(entry):
						action = "reject-proposal:" + entry.ID
					default:
						action = "deny:" + entry.ID
					}
				}
			}
			if action != "" {
				m.mutating = true
				m.feedback = action + " pending"
				return m, m.act(action)
			}
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.fetch()
			}
		case "tab", "right":
			m.tab = (m.tab + 1) % 5
			m.offset = 0
		case "shift+tab", "left":
			m.tab = (m.tab + 4) % 5
			m.offset = 0
		case "1", "2", "3", "4", "5":
			m.tab = int(msg.String()[0] - '1')
			m.offset = 0
		case "j", "down":
			if m.tab == 2 && m.snapshot != nil && len(m.snapshot.Inbox) > 0 {
				m.inbox = min(len(m.snapshot.Inbox)-1, m.inbox+1)
			} else if (m.tab == 1 || m.tab == 4) && m.snapshot != nil && len(m.snapshot.Tasks) > 0 {
				m.task = min(len(m.snapshot.Tasks)-1, m.task+1)
			} else {
				m.offset++
			}
		case "k", "up":
			if m.tab == 2 {
				m.inbox = max(0, m.inbox-1)
			} else if m.tab == 1 || m.tab == 4 {
				m.task = max(0, m.task-1)
			} else {
				m.offset = max(0, m.offset-1)
			}
		case "pgdown":
			m.offset += max(1, m.height-5)
		case "pgup":
			m.offset = max(0, m.offset-max(1, m.height-5))
		case "home":
			m.offset = 0
		case "end":
			m.offset = len(m.lines())
		}
	}
	m.offset = min(m.offset, max(0, len(m.lines())-max(1, m.height-5)))
	return m, nil
}

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

func (m model) lines() []string {
	if m.snapshot == nil {
		if m.err != nil {
			return []string{"Unable to read project: " + clean(m.err.Error()), "Press r to retry."}
		}
		return []string{"Loading persisted project…"}
	}
	s := m.snapshot
	var lines []string
	switch m.tab {
	case 0:
		lines = []string{"Project: " + clean(s.Readiness.Project.ID), "Root: " + clean(s.Readiness.Project.Root), fmt.Sprintf("Revision %d · %s", s.Readiness.Project.Revision, clean(s.Readiness.Project.State)), "", "Execution is unavailable until all runtime and readiness gates pass."}
		for _, issue := range s.Readiness.RuntimeIssues {
			lines = append(lines, "Runtime: "+clean(issue))
		}
		for _, issue := range s.Readiness.DefinitionIssues {
			lines = append(lines, "Definition: "+clean(issue))
		}
		lines = append(lines, "", fmt.Sprintf("%d tasks · %d pending/expired decisions", len(s.Readiness.Tasks), len(s.Inbox)))
	case 1:
		for index, task := range s.Readiness.Tasks {
			marker := "  "
			if index == m.task {
				marker = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s · %s · revision %d", marker, clean(task.ID), clean(task.State), task.Revision))
			for _, issue := range task.Issues {
				lines = append(lines, "  "+clean(issue))
			}
			if len(task.RequiredChecks) > 0 {
				lines = append(lines, "  Required checks: "+clean(strings.Join(task.RequiredChecks, ", ")))
			}
			lines = append(lines, "")
		}
		if len(lines) == 0 {
			lines = []string{"No tasks defined."}
		}
	case 2:
		lines = []string{"Resolve the displayed revision only: y allow · n deny/reject/cancel · g apply · v revise.", "Recovery: x exact resume · f fresh context · b remain blocked. Native input: i answer.", "Task acceptance and manual Pass remain distinct revision-bound actions.", "Showing up to 100 pending/expired decisions.", ""}
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
				}
				if json.Unmarshal(entry.Context, &proposal) == nil && proposal.ID != "" {
					lines = append(lines, fmt.Sprintf("  Proposal: %s r%d · %s", clean(proposal.ID), proposal.Revision, clean(proposal.Operation)), "  Definition digest: "+clean(proposal.Digest))
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
	case 3:
		lines = []string{"Latest 100 persisted events (oldest first).", ""}
		for _, event := range s.Events {
			label := event.Kind
			if event.Kind == "command_applied" {
				var payload struct {
					Kind  string `json:"command_kind"`
					Actor string `json:"actor"`
				}
				if json.Unmarshal(event.Payload, &payload) == nil && payload.Kind != "" {
					label = payload.Kind + " · " + payload.Actor
				}
			}
			lines = append(lines, fmt.Sprintf("%d · %s · %s", event.Sequence, time.UnixMilli(event.At).Format("15:04:05"), clean(label)))
		}
		if len(s.Events) == 0 {
			lines = append(lines, "No events recorded.")
		}
	case 4:
		lines = []string{"Authoritative plan/task evidence details.", "Detailed diffs remain external.", ""}
		for _, plan := range s.Plans {
			lines = append(lines, fmt.Sprintf("Plan %s · %s · rank %d · services remaining %dms", clean(plan.ID), clean(plan.State), plan.Rank, plan.ServiceBudgetRemainingMS))
		}
		for index, task := range s.Tasks {
			marker := "  "
			if index == m.task {
				marker = "> "
			}
			lines = append(lines, "", fmt.Sprintf("%sTask %s r%d · %s · remaining %dms", marker, clean(task.ID), task.Revision, clean(task.State), task.BudgetRemainingMS))
			if task.BlockReason != "" {
				lines = append(lines, "  Blocker: "+clean(task.BlockReason))
			}
			lines = append(lines, fmt.Sprintf("  Findings: %d blocking · %d suggestions", task.BlockingFindings, task.Suggestions))
			if task.BaselineUnhealthy {
				lines = append(lines, "  Baseline health: unhealthy exception present")
			}
			for _, v := range task.CheckOutputs {
				lines = append(lines, "  Check output: "+clean(v))
			}
			for _, v := range task.ManualOutcomes {
				lines = append(lines, "  Manual: "+clean(v))
			}
			if len(task.ManualCriteria) > 0 {
				lines = append(lines, "  m manual Pass target: "+clean(task.ManualCriteria[0]))
			}
			if task.RecoveryState != "" {
				lines = append(lines, "  Recovery quarantine: "+clean(task.RecoveryState))
			}
		}
	}
	return lines
}

func (m model) View() tea.View {
	tabs := []string{"1 Overview", "2 Tasks", "3 Inbox", "4 History", "5 Detail"}
	tabs[m.tab] = "[" + tabs[m.tab] + "]"
	status := "Persisted state · interactive controls"
	if m.loading {
		status += " · refreshing"
	}
	if m.err != nil && m.snapshot != nil {
		status = "Refresh failed; showing previous snapshot: " + clean(m.err.Error())
	}
	if m.feedback != "" {
		status += " · " + clean(m.feedback)
	}
	if m.inputRequest != "" {
		status = "Clarification input for " + clean(m.inputRequest)
	}
	lines := m.lines()
	space := max(1, m.height-5)
	start := min(m.offset, max(0, len(lines)-space))
	end := min(len(lines), start+space)
	output := []string{"Vigil  " + strings.Join(tabs, "  "), status, ""}
	output = append(output, lines[start:end]...)
	footer := fmt.Sprintf("p/c/s control · a/u plan · inbox g/v/x/f/b/i/y/n · h/m/t quality · q quit  (%d–%d/%d)", start+1, end, len(lines))
	if m.inputRequest != "" {
		footer = "Answer: " + clean(m.inputText) + "  (Enter submit · Esc cancel locally · Ctrl+C quit)"
	}
	output = append(output, "", footer)
	for n, line := range output {
		output[n] = ansi.Truncate(line, max(1, m.width), "…")
	}
	if len(output) > m.height {
		output = output[:max(1, m.height)]
	}
	view := tea.NewView(strings.Join(output, "\n"))
	view.AltScreen = true
	return view
}

func RunProject(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, input io.Reader, output io.Writer) error {
	return RunProjectWithOwner(ctx, engine, coordination, nil, input, output)
}

func RunProjectWithOwner(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, owner *supervisor.InteractiveOwner, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	final, err := tea.NewProgram(model{ctx: ctx, load: engine.Dashboard, mutate: projectMutator(engine, coordination, owner), loading: true, width: 80, height: 24}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	if m, ok := final.(model); ok && m.snapshot == nil {
		return m.err
	}
	return nil
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
		case "queue":
			for _, p := range s.Queue {
				if p.State == "draft" || p.State == "ready" {
					_, err := engine.QueuePlan(ctx, store.ID(), revision, p.ID, p.Rank)
					return err
				}
			}
			return fmt.Errorf("no queueable plan")
		case "stop":
			if s.ActiveRun == "" {
				return fmt.Errorf("no active persisted run")
			}
			_, err := (&supervisor.Runner{Engine: engine}).RequestStop(ctx, supervisor.StopRequest{CommandID: store.ID(), ExpectedRevision: revision, RunID: s.ActiveRun, InterruptGrace: 2 * time.Second, TerminateGrace: 5 * time.Second})
			return err
		default:
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
			if (decision != "allow" && decision != "deny" && decision != "apply-proposal" && decision != "reject-proposal" && decision != "request-proposal-revision" && decision != "remain-blocked" && decision != "exact-resume" && decision != "fresh-context" && decision != "answer-clarification" && decision != "cancel-clarification") || !found || requestID == "" {
				return fmt.Errorf("unknown dashboard action")
			}
			answerText := ""
			if decision == "answer-clarification" {
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
