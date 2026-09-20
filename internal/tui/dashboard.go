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
)

type source func(context.Context) (core.DashboardSnapshot, error)
type loaded struct {
	snapshot core.DashboardSnapshot
	err      error
}
type refresh struct{}
type model struct {
	ctx                        context.Context
	load                       source
	snapshot                   *core.DashboardSnapshot
	err                        error
	loading                    bool
	tab, offset, width, height int
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
		}
	case refresh:
		if m.loading {
			return m, tick()
		}
		m.loading = true
		return m, tea.Batch(m.fetch(), tick())
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.fetch()
			}
		case "tab", "right":
			m.tab = (m.tab + 1) % 4
			m.offset = 0
		case "shift+tab", "left":
			m.tab = (m.tab + 3) % 4
			m.offset = 0
		case "1", "2", "3", "4":
			m.tab = int(msg.String()[0] - '1')
			m.offset = 0
		case "j", "down":
			m.offset++
		case "k", "up":
			m.offset = max(0, m.offset-1)
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
		lines = []string{"Decisions are read-only here. Resolve them with project apply.", "Showing up to 100 pending/expired decisions.", ""}
		for _, entry := range s.Inbox {
			lines = append(lines, clean(entry.ID)+" · "+clean(entry.Kind)+" · "+clean(entry.State))
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
	}
	return lines
}

func (m model) View() tea.View {
	tabs := []string{"1 Overview", "2 Tasks", "3 Inbox", "4 History"}
	tabs[m.tab] = "[" + tabs[m.tab] + "]"
	status := "Persisted state · read-only"
	if m.loading {
		status += " · refreshing"
	}
	if m.err != nil && m.snapshot != nil {
		status = "Refresh failed; showing previous snapshot: " + clean(m.err.Error())
	}
	lines := m.lines()
	space := max(1, m.height-5)
	start := min(m.offset, max(0, len(lines)-space))
	end := min(len(lines), start+space)
	output := []string{"Vigil  " + strings.Join(tabs, "  "), status, ""}
	output = append(output, lines[start:end]...)
	output = append(output, "", fmt.Sprintf("Tab: view · ↑/↓: scroll · r: refresh · q: quit  (%d–%d/%d)", start+1, end, len(lines)))
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
	final, err := tea.NewProgram(model{ctx: ctx, load: engine.Dashboard, loading: true, width: 80, height: 24}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	if m, ok := final.(model); ok && m.snapshot == nil {
		return m.err
	}
	return nil
}
