package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"vigil/internal/core"
)

func TestPendingReadDoesNotBlockQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, finished := make(chan struct{}), make(chan struct{})
	m := model{ctx: ctx, loading: true, width: 80, height: 24, load: func(ctx context.Context) (core.DashboardSnapshot, error) {
		close(started)
		<-ctx.Done()
		return core.DashboardSnapshot{}, ctx.Err()
	}}
	go func() { defer close(finished); m.fetch()() }()
	<-started
	_, quit := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if quit == nil {
		t.Fatal("quit blocked behind read")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("missing quit message")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("read did not cancel")
	}
}

func TestDashboardRetainsSnapshotAndSanitizesTerminalText(t *testing.T) {
	m := model{ctx: context.Background(), width: 80, height: 24}
	snapshot := core.DashboardSnapshot{Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 7, Root: "/work/\x1b]52;c;danger\a\x1b[2Jname"}, RuntimeIssues: []string{"not qualified"}}}
	updated, _ := m.Update(loaded{snapshot: snapshot})
	m = updated.(model)
	updated, _ = m.Update(loaded{err: errors.New("temporary failure")})
	m = updated.(model)
	view := m.View().Content
	if strings.ContainsRune(view, '\x1b') || !strings.Contains(view, "Revision 7") || !strings.Contains(view, "previous snapshot") {
		t.Fatal("unsafe or misleading view", view)
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 12, Height: 4})
	m = updated.(model)
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) > 4 {
		t.Fatal("view exceeds terminal height")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 12 {
			t.Fatal("view exceeds terminal width")
		}
	}
}
