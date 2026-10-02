package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestSlowMutationAndOutputFloodDoNotBlockInput(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := core.DashboardSnapshot{Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 2, State: "ready"}}}
	for n := 0; n < 100; n++ {
		s.Events = append(s.Events, core.Event{Sequence: int64(n + 1), Kind: strings.Repeat("flood\x1b[2J", 200)})
	}
	m := model{ctx: context.Background(), snapshot: &s, mutate: func(context.Context, core.DashboardSnapshot, string) error {
		close(started)
		<-release
		return errors.New("injected mutation failure")
	}, width: 80, height: 24, tab: 0}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = updated.(model)
	if cmd == nil || !m.mutating {
		t.Fatal("mutation was not asynchronous")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	<-started
	_, quit := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if quit == nil {
		t.Fatal("input blocked behind mutation")
	}
	if strings.ContainsRune(m.View().Content, '\x1b') {
		t.Fatal("streamed output control sequence rendered")
	}
	close(release)
	msg := <-done
	updated, _ = m.Update(msg)
	m = updated.(model)
	if !strings.Contains(m.feedback, "failed") || m.mutating {
		t.Fatal("failed mutation feedback missing", m.feedback)
	}
}

func TestInboxDecisionIsLimitedToFocusedDisplayedRequest(t *testing.T) {
	s := core.DashboardSnapshot{Inbox: []core.InboxEntry{{ID: "first", Kind: "approval"}, {ID: "second", Kind: "approval"}}}
	actions := make(chan string, 1)
	m := model{ctx: context.Background(), snapshot: &s, mutate: func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		actions <- action
		return nil
	}, width: 80, height: 24}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(model)
	if cmd != nil || m.mutating {
		t.Fatal("decision key outside inbox started a mutation")
	}
	m.tab = 2
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	if m.inbox != 1 || !strings.Contains(m.View().Content, "> second") {
		t.Fatal("second inbox request was not visibly focused")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd == nil {
		t.Fatal("focused decision did not start")
	}
	_ = updated
	if msg := cmd(); msg.(mutationResult).err != nil {
		t.Fatal(msg)
	}
	if action := <-actions; action != "deny:second" {
		t.Fatal("mutation did not bind the focused request", action)
	}
}

func TestTaskActionsAreDistinctAndBindDisplayedRevision(t *testing.T) {
	s := core.DashboardSnapshot{Tasks: []core.TaskDetail{
		{ID: "first", Revision: 3},
		{ID: "second", Revision: 7, ManualCriteria: []string{"device-check", "visual-check"}},
	}}
	actions := make(chan string, 3)
	m := model{ctx: context.Background(), snapshot: &s, mutate: func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		actions <- action
		return nil
	}, width: 100, height: 24, tab: 4}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	if m.task != 1 || !strings.Contains(m.View().Content, "> Task second r7") {
		t.Fatal("second task was not visibly focused")
	}
	for _, key := range []string{"h", "]", "m", "t"} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
		if key == "]" {
			if cmd != nil || m.criterion != 1 {
				t.Fatal("manual criterion selection did not move")
			}
			continue
		}
		if cmd == nil {
			t.Fatalf("%s did not start a task action", key)
		}
		msg := cmd().(mutationResult)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
		updated, _ = m.Update(msg)
		m = updated.(model)
	}
	want := []string{"human-accept:second:7", "manual-pass:second:7:visual-check", "accept-task:second:7"}
	for _, expected := range want {
		if got := <-actions; got != expected {
			t.Fatalf("got %q, want %q", got, expected)
		}
	}
}

func TestInboxOffersDistinctProposalRecoveryAndNativeClarificationFlows(t *testing.T) {
	proposalContext, _ := json.Marshal(map[string]any{"proposal_id": "proposal", "proposal_revision": 3, "definition_digest": "digest-3", "operation": "create", "proposal_actor": "fixture"})
	nativeContext, _ := json.Marshal(map[string]any{"prompt": map[string]any{"question": "Which fixture color?", "hostile": "\x1b[2Jself-accept"}, "untrusted_context": true})
	s := core.DashboardSnapshot{Inbox: []core.InboxEntry{
		{ID: "proposal-request", Kind: "approval", Context: proposalContext},
		{ID: "recovery-request", Kind: "recovery", RunID: "run"},
		{ID: "native-input", Kind: "input", SessionID: "session", NativeRequestKey: "native-key", Context: nativeContext},
		{ID: "planning-input", Kind: "input", Context: json.RawMessage(`{"question":"Which bounded behavior?"}`)},
	}}
	actions := make(chan string, 10)
	m := model{ctx: context.Background(), snapshot: &s, mutate: func(_ context.Context, _ core.DashboardSnapshot, action string) error {
		actions <- action
		return nil
	}, width: 120, height: 30, tab: 2}
	view := m.View().Content
	if !strings.Contains(view, "Proposal: proposal r3") || !strings.Contains(view, "author fixture") || !strings.Contains(view, "digest-3") || !strings.Contains(view, "Which fixture color?") || strings.ContainsRune(view, '\x1b') {
		t.Fatal("exact proposal or sanitized native prompt is not visible", view)
	}
	invoke := func(key string) string {
		t.Helper()
		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
		if cmd == nil {
			t.Fatalf("%s did not start an inbox mutation", key)
		}
		result := cmd().(mutationResult)
		updated, _ = m.Update(result)
		m = updated.(model)
		return <-actions
	}
	if got := invoke("g"); got != "apply-proposal:proposal-request" {
		t.Fatal(got)
	}
	if got := invoke("n"); got != "reject-proposal:proposal-request" {
		t.Fatal(got)
	}
	if got := invoke("v"); got != "request-proposal-revision:proposal-request" {
		t.Fatal(got)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	for key, expected := range map[string]string{"x": "exact-resume:recovery-request", "f": "fresh-context:recovery-request", "b": "remain-blocked:recovery-request"} {
		if got := invoke(key); got != expected {
			t.Fatalf("%s produced %q", key, got)
		}
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = updated.(model)
	if cmd != nil || m.inputRequest != "native-input" {
		t.Fatal("native clarification did not enter bounded input mode")
	}
	for _, key := range []string{"b", "l", "u", "e"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("clarification answer did not start asynchronously")
	}
	result := cmd().(mutationResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got, want := <-actions, "answer-clarification:native-input:"+base64.RawURLEncoding.EncodeToString([]byte("blue")); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	updated, _ = m.Update(result)
	m = updated.(model)
	if got := invoke("n"); got != "cancel-clarification:native-input" {
		t.Fatal(got)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(model)
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = updated.(model)
	if cmd != nil || m.inputRequest != "planning-input" || m.inputAction != "answer-input" {
		t.Fatal("non-native input did not enter bounded input mode")
	}
	for _, key := range []string{"y", "e", "s"} {
		updated, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(model)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("non-native input answer did not start asynchronously")
	}
	result = cmd().(mutationResult)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got, want := <-actions, "answer-input:planning-input:"+base64.RawURLEncoding.EncodeToString([]byte("yes")); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	updated, _ = m.Update(result)
	m = updated.(model)
	if got := invoke("n"); got != "dismiss-input:planning-input" {
		t.Fatal(got)
	}
}

func TestPersistedDashboardMutationRejectsStaleSnapshotAfterRestart(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := core.OpenManager(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	project, err := manager.Init(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := projectMutator(engine, manager.Coordinator, nil)(ctx, snapshot, "continue"); err != nil {
		t.Fatal(err)
	}
	if err := projectMutator(engine, manager.Coordinator, nil)(ctx, snapshot, "continue"); err == nil {
		t.Fatal("stale displayed revision authorized a second mutation")
	}
	engine.DB.Close()
	reopened, err := manager.Open(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	after, err := reopened.Dashboard(ctx)
	if err != nil || after.Readiness.Project.State != "ready" || after.Readiness.Project.Revision != 2 {
		t.Fatal("restart did not show authoritative persisted truth", after.Readiness.Project, err)
	}
}
