package tui

import (
	"context"
	"fmt"
	"io"

	"agent-control/internal/storage"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type databaseResult struct {
	version string
	err     error
}

type model struct {
	ctx      context.Context
	database string
	spinner  spinner.Model
	result   *databaseResult
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		version, err := storage.Version(m.ctx, m.database)
		return databaseResult{version: version, err: err}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" || msg.String() == "esc" {
			return m, tea.Quit
		}
	case databaseResult:
		m.result = &msg
		return m, nil
	}
	if m.result == nil {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() tea.View {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render("agent-control")
	status := m.spinner.View() + " Connecting to SQLite..."
	if m.result != nil {
		status = fmt.Sprintf("SQLite %s connected.", m.result.version)
		if m.result.err != nil {
			status = "SQLite error: " + m.result.err.Error()
		}
	}
	view := tea.NewView(title + "\n\nHello, world!\n\n" + status + "\n\nPress q, Esc, or Ctrl+C to quit.\n")
	view.AltScreen = true
	return view
}

func Run(ctx context.Context, database string, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := spinner.New()
	s.Spinner = spinner.Dot
	final, err := tea.NewProgram(model{ctx: ctx, database: database, spinner: s},
		tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	if m, ok := final.(model); ok && m.result != nil {
		return m.result.err
	}
	return nil
}
