//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
	"vigil/internal/supervisor"
)

func cliPTY(t *testing.T) (*os.File, *os.File) {
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

func cliGit(t *testing.T, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
}

func cliApply(t *testing.T, engine *core.Engine, kind string, payload any) {
	t.Helper()
	var revision int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	if _, err := engine.Apply(context.Background(), core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: kind, Payload: raw}); err != nil {
		t.Fatal(kind, err)
	}
}

func setupDashboardInteractionFixture(t *testing.T) (string, string, supervisor.PreparedRun, string) {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{filepath.Join(root, ".vigil-disposable-fixture"): "agent-owned fixture\n", filepath.Join(root, "src", ".keep"): "fixture\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cliGit(t, "init", "-q", "-b", "main", root)
	cliGit(t, "-C", root, "add", ".")
	cliGit(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture")
	stateDir := filepath.Join(base, "state")
	manager, err := core.OpenManager(ctx, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := manager.Init(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := manager.Open(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"test"}, CheckDefinitions: []policy.CheckDefinition{{ID: "test", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "task", Objective: "fixture", Criteria: []policy.Criterion{{ID: "criterion", Text: "fixture"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"test"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}
	cliApply(t, engine, "project.configure", config)
	cliApply(t, engine, "profile.put", profile)
	cliApply(t, engine, "plan.put", core.Plan{ID: "plan", Title: "Fixture", Specification: "fixture", Approved: true, Tasks: []policy.Task{task}})
	cliApply(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "repo", PlanID: "plan", Root: root, BaseRef: "main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"})
	var revision int
	_ = engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err := engine.PrepareRepository(ctx, "prepare-repo", "repo", revision); err != nil {
		t.Fatal(err)
	}
	_ = engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err := engine.QueuePlan(ctx, "queue-plan", revision, "plan", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Continue(ctx, "continue-plan", revision+1); err != nil {
		t.Fatal(err)
	}
	_ = engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err := engine.Advance(ctx, "advance-plan", revision); err != nil {
		t.Fatal(err)
	}
	prepared, err := supervisor.Prepare(ctx, engine, supervisor.PrepareRequest{CommandID: "prepare-run", ExpectedProjectRevision: revision, TaskID: "task", RuntimeKind: "synthetic", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "fixture-session-record"
	if _, err := engine.DB.SQL.Exec(`INSERT INTO sessions(id,run_id,generation,harness,durable_id,runtime_id,native_home_ref,workspace_identity,profile_digest,capabilities_json) VALUES(?,?,?,?,?,?,?,?,?,'{}')`, sessionID, prepared.RunID, prepared.TransportGeneration, "fixture", "fixture-native", "fixture-native", "private:"+prepared.GenerationID, prepared.Repositories[0].Identity.Key, prepared.ProfileDigest); err != nil {
		t.Fatal(err)
	}
	engine.DB.Close()
	manager.Close()
	return stateDir, project.ID, prepared, sessionID
}

func TestDashboardSyntheticInteractionRunsPersistedClarificationThroughPTY(t *testing.T) {
	stateDir, projectID, prepared, sessionID := setupDashboardInteractionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	master, slave := cliPTY(t)
	defer master.Close()
	defer slave.Close()
	go io.Copy(io.Discard, master)
	command := NewCommand()
	command.SetIn(slave)
	command.SetOut(slave)
	command.SetErr(slave)
	command.SetArgs([]string{"--state-dir", stateDir, "dashboard", projectID, "--synthetic-interactions", "--clarification-run", prepared.RunID, "--clarification-session", sessionID, "--clarification-key", "fixture-request", "--clarification-prompt", "Which fixture color?"})
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	time.Sleep(120 * time.Millisecond)
	if _, err := master.Write([]byte("3")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := master.Write([]byte("iblue\r")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if _, err := master.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fixture dashboard did not complete")
	}
	manager, err := core.OpenManager(context.Background(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	engine, err := manager.Open(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.DB.Close()
	var resolved, delivered int
	if err := engine.DB.SQL.QueryRow(`SELECT count(*) FROM requests WHERE session_id=? AND native_request_key='fixture-request' AND state='resolved' AND json_extract(result_json,'$.delivery')='delivered'`, sessionID).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if err := engine.DB.SQL.QueryRow(`SELECT count(*) FROM events WHERE kind='fixture_native_clarification_delivered' AND json_extract(payload_json,'$.fixture_mechanics_only')=1`).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || delivered != 1 {
		t.Fatal("PTY did not execute the persisted owner route", resolved, delivered)
	}
}

func TestSyntheticInteractionAdmissionRechecksRuntimeMarkerAndIdentity(t *testing.T) {
	stateDir, projectID, prepared, _ := setupDashboardInteractionFixture(t)
	manager, err := core.OpenManager(context.Background(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	engine, err := manager.Open(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.DB.Close()
	foreign := prepared
	foreign.Repositories[0].Identity.Key = "foreign"
	if err := requireSyntheticFixture(context.Background(), engine, foreign); err == nil {
		t.Fatal("foreign physical identity admitted")
	}
	production := prepared
	production.RuntimeKind = "native"
	if err := requireSyntheticFixture(context.Background(), engine, production); err == nil {
		t.Fatal("production runtime admitted to fixture interaction path")
	}
	marker := filepath.Join(prepared.Repositories[0].Root, ".vigil-disposable-fixture")
	if err := os.Rename(marker, marker+".absent"); err != nil {
		t.Fatal(err)
	}
	if err := requireSyntheticFixture(context.Background(), engine, prepared); err == nil {
		t.Fatal("missing disposable marker admitted")
	}
}
