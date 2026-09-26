package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"vigil/internal/core"
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
	ctx                               context.Context
	load                              source
	snapshot                          *core.DashboardSnapshot
	err                               error
	loading                           bool
	mutating                          bool
	feedback                          string
	mutate                            mutator
	tab, offset, inbox, width, height int
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
			case "y":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "allow:" + m.snapshot.Inbox[m.inbox].ID
				}
			case "n":
				if m.tab == 2 && len(m.snapshot.Inbox) > 0 {
					action = "deny:" + m.snapshot.Inbox[m.inbox].ID
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
			} else {
				m.offset++
			}
		case "k", "up":
			if m.tab == 2 {
				m.inbox = max(0, m.inbox-1)
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
		for _, task := range s.Readiness.Tasks {
			lines = append(lines, fmt.Sprintf("%s · %s · revision %d", clean(task.ID), clean(task.State), task.Revision))
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
		lines = []string{"Resolve the displayed revision only: y allow once · n deny · g apply proposal · b remain blocked.", "Permission, task acceptance, manual Pass and native clarification remain distinct actions.", "Showing up to 100 pending/expired decisions.", ""}
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
		for _, task := range s.Tasks {
			lines = append(lines, "", fmt.Sprintf("Task %s · %s · remaining %dms", clean(task.ID), clean(task.State), task.BudgetRemainingMS))
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
	lines := m.lines()
	space := max(1, m.height-5)
	start := min(m.offset, max(0, len(lines)-space))
	end := min(len(lines), start+space)
	output := []string{"Vigil  " + strings.Join(tabs, "  "), status, ""}
	output = append(output, lines[start:end]...)
	output = append(output, "", fmt.Sprintf("p pause · c continue · s stop · a advance · u queue · y/n permission · g proposal · b blocked · q quit  (%d–%d/%d)", start+1, end, len(lines)))
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

func RunProject(ctx context.Context, engine *core.Engine, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	final, err := tea.NewProgram(model{ctx: ctx, load: engine.Dashboard, mutate: projectMutator(engine), loading: true, width: 80, height: 24}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	if m, ok := final.(model); ok && m.snapshot == nil {
		return m.err
	}
	return nil
}

func projectMutator(engine *core.Engine) mutator {
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
			if (decision != "allow" && decision != "deny" && decision != "apply-proposal" && decision != "remain-blocked") || !found || requestID == "" {
				return fmt.Errorf("unknown dashboard action")
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
			if decision == "remain-blocked" {
				if entry.Kind != "recovery" || entry.RunID == "" {
					return fmt.Errorf("displayed request is not a recovery choice")
				}
				_, err := (&supervisor.Runner{Engine: engine}).ChooseRecovery(ctx, supervisor.RecoveryChoiceRequest{CommandID: store.ID(), ExpectedRevision: revision, RunID: entry.RunID, Mode: "remain_blocked"}, nil)
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
