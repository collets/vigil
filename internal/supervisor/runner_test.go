package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
)

type fixture struct {
	manager     *core.Manager
	engine      *core.Engine
	project     core.Project
	prepared    PreparedRun
	reservation core.Reservation
	owner       *coordinator.Owner
	driver      *FixtureDriver
}

func command(t *testing.T, engine *core.Engine, kind string, payload any) {
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

func git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
}

func setupFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vigil-disposable-fixture"), []byte("agent-owned fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", ".keep"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, "init", "-q", "-b", "main", root)
	git(t, "-C", root, "add", ".")
	git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture")
	manager, err := core.OpenManager(ctx, filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := manager.Init(ctx, root)
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	engine, err := manager.Open(ctx, project.ID)
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.DB.Close(); manager.Close() })
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"test"}, CheckDefinitions: []policy.CheckDefinition{{ID: "test", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "task", Objective: "Write a deterministic fixture result", Criteria: []policy.Criterion{{ID: "c1", Text: "result exists"}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"test"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}
	command(t, engine, "project.configure", config)
	command(t, engine, "profile.put", profile)
	command(t, engine, "plan.put", core.Plan{ID: "plan", Title: "Fixture", Specification: "Create one deterministic file", Approved: true, Tasks: []policy.Task{task}})
	command(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "repo", PlanID: "plan", Root: root, BaseRef: "main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"})
	var revision int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareRepository(ctx, "prepare-branch", "repo", revision); err != nil {
		t.Fatal(err)
	}
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(ctx, engine, PrepareRequest{CommandID: "prepare-run", ExpectedProjectRevision: revision, TaskID: "task", RuntimeKind: "synthetic", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Coordinator.Endpoint(ctx, "fixture-endpoint", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	owner, err := manager.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	reservation, err := engine.ReserveResources(ctx, owner, "resources", prepared.RunID, "fixture-endpoint")
	if err != nil {
		t.Fatal(err)
	}
	driver := &FixtureDriver{RepositoryID: "repo", Root: root, RelativePath: "src/result.txt", Content: []byte("persisted execution\n")}
	return fixture{manager, engine, project, prepared, reservation, owner, driver}
}

func TestOnePersistedSyntheticExecutionStopsAtChecking(t *testing.T) {
	fixture := setupFixture(t)
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	result, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "Write the fixture result")
	if err != nil || result.Status != "completed" || len(result.ChangedPaths) != 1 {
		t.Fatal(result, err)
	}
	view, err := runner.Inspect(context.Background(), fixture.prepared)
	if err != nil || view.RunState != "completed" || view.TaskState != "checking" || view.SubmissionState != "delivered" {
		t.Fatal(view, err)
	}
	if fixture.driver.Calls["submit"] != 1 {
		t.Fatal("submission count", fixture.driver.Calls)
	}
	var accepted int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM acceptances").Scan(&accepted); err != nil || accepted != 0 {
		t.Fatal("execution accepted task", accepted, err)
	}
	stored, err := runner.Result(context.Background(), fixture.prepared.RunID)
	if err != nil || stored.Summary != result.Summary {
		t.Fatal(stored, err)
	}
}

func TestUncertainCreateAndSubmissionNeverDuplicateEffects(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		fixture := setupFixture(t)
		fixture.driver.CreateUncertain = true
		runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
		if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "fixture"); err == nil {
			t.Fatal("uncertain create accepted")
		}
		fixture.driver.CreateUncertain = false
		view, err := runner.Reconcile(context.Background(), fixture.prepared)
		if err != nil || view.Effects["runtime_create"] != "reconciled" || fixture.driver.Calls["create"] != 1 {
			t.Fatal(view, fixture.driver.Calls, err)
		}
	})
	t.Run("submission", func(t *testing.T) {
		fixture := setupFixture(t)
		fixture.driver.SubmitUncertain = true
		runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
		if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "fixture"); err == nil {
			t.Fatal("uncertain submission accepted")
		}
		view, err := runner.Reconcile(context.Background(), fixture.prepared)
		if err != nil || view.SubmissionState != "uncertain" || fixture.driver.Calls["submit"] != 1 {
			t.Fatal(view, fixture.driver.Calls, err)
		}
		if _, err = runner.Run(context.Background(), fixture.prepared, fixture.reservation, "fixture"); err == nil {
			t.Fatal("uncertain submission replayed")
		}
		if fixture.driver.Calls["submit"] != 1 {
			t.Fatal("duplicate prompt", fixture.driver.Calls)
		}
	})
}

func TestCrashAfterTerminalReconcilesResultWithoutSubmissionReplay(t *testing.T) {
	fixture := setupFixture(t)
	injected := errors.New("crash before result persistence")
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver, Fault: func(point string) error {
		if point == "before_result_persist" {
			return injected
		}
		return nil
	}}
	if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "fixture"); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	var count int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM execution_results").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	reopened, err := fixture.manager.Open(context.Background(), fixture.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	reconciler := Runner{Engine: reopened, Driver: fixture.driver}
	view, err := reconciler.Reconcile(context.Background(), fixture.prepared)
	if err != nil || view.RunState != "completed" || view.TaskState != "checking" {
		t.Fatal(view, err)
	}
	if fixture.driver.Calls["submit"] != 1 {
		t.Fatal("terminal recovery replayed prompt", fixture.driver.Calls)
	}
}

func TestPersistenceFailureTriggersBoundedContainment(t *testing.T) {
	fixture := setupFixture(t)
	if _, err := fixture.engine.DB.SQL.Exec(`CREATE TRIGGER fixture_event_write_failure BEFORE INSERT ON normalized_run_events BEGIN SELECT RAISE(FAIL,'injected event persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "fixture"); err == nil {
		t.Fatal("persistence failure accepted")
	}
	view, err := runner.Inspect(context.Background(), fixture.prepared)
	if err != nil || view.WriterState != "contained_stopped" || fixture.driver.Calls["stop"] != 1 {
		t.Fatal(view, fixture.driver.Calls, err)
	}
	var results int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM execution_results").Scan(&results); err != nil || results != 0 {
		t.Fatal(results, err)
	}
}

func TestProductionPreparationFailsWithoutQualification(t *testing.T) {
	fixture := setupFixture(t)
	var revision int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(context.Background(), fixture.engine, PrepareRequest{CommandID: "production", ExpectedProjectRevision: revision, TaskID: "task", RuntimeKind: "docker", WallLimitMS: 60000})
	if err == nil {
		t.Fatal("production run prepared without exact qualification")
	}
}
