//go:build linux

package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/unix"
	"vigil/internal/core"
)

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}

func TestProjectDashboardPTYExercisesProposalRecoveryAndClarificationChoices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proposalContext, _ := json.Marshal(map[string]any{"proposal_id": "proposal", "proposal_revision": 1})
	snapshot := core.DashboardSnapshot{Readiness: core.Readiness{Project: core.Project{ID: "fixture", Revision: 7, State: "paused"}}, Inbox: []core.InboxEntry{
		{ID: "proposal-request", Kind: "approval", Context: proposalContext},
		{ID: "recovery-request", Kind: "recovery", RunID: "run"},
		{ID: "native-input", Kind: "input", SessionID: "session", NativeRequestKey: "native-key"},
	}}
	actions := make(chan string, 16)
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()
	go io.Copy(io.Discard, master)
	done := make(chan error, 1)
	go func() {
		initial := model{ctx: ctx, snapshot: &snapshot, tab: 2, width: 120, height: 30, load: func(context.Context) (core.DashboardSnapshot, error) { return snapshot, nil }, mutate: func(_ context.Context, _ core.DashboardSnapshot, action string) error {
			actions <- action
			return nil
		}}
		_, err := tea.NewProgram(initial, tea.WithContext(ctx), tea.WithInput(slave), tea.WithOutput(slave)).Run()
		done <- err
	}()
	time.Sleep(80 * time.Millisecond)
	writeAndWant := func(keys, want string) {
		t.Helper()
		if _, err := master.Write([]byte(keys)); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-actions:
			if got != want {
				t.Fatalf("keys %q produced %q, want %q", keys, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("keys %q did not produce an action", keys)
		}
		time.Sleep(20 * time.Millisecond)
	}
	writeAndWant("g", "apply-proposal:proposal-request")
	writeAndWant("n", "reject-proposal:proposal-request")
	writeAndWant("v", "request-proposal-revision:proposal-request")
	if _, err := master.Write([]byte("\x1b[B")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	writeAndWant("x", "exact-resume:recovery-request")
	writeAndWant("f", "fresh-context:recovery-request")
	writeAndWant("b", "remain-blocked:recovery-request")
	if _, err := master.Write([]byte("\x1b[B")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	writeAndWant("iblue\r", "answer-clarification:native-input:"+base64.RawURLEncoding.EncodeToString([]byte("blue")))
	writeAndWant("n", "cancel-clarification:native-input")
	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("PTY workflow did not exit")
	}
}

func TestProjectDashboardAcceptsInputThroughPTY(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
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
	defer engine.DB.Close()
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()
	go io.Copy(io.Discard, master)
	done := make(chan error, 1)
	go func() { done <- RunProject(ctx, engine, manager.Coordinator, slave, slave) }()
	time.Sleep(50 * time.Millisecond)
	if _, err = master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("PTY input did not stop the dashboard")
	}
}
