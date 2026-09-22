package quality_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vigil/internal/checks"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/quality"
	"vigil/internal/review"
	"vigil/internal/store"
	"vigil/internal/supervisor"
)

func TestQualityCheckHelper(t *testing.T) {
	if os.Getenv("VIGIL_CHECK_HELPER") != "1" {
		return
	}
	action := os.Args[len(os.Args)-1]
	switch action {
	case "pass":
		_ = os.MkdirAll("build", 0700)
		_ = os.WriteFile("build/result.txt", []byte("ok\n"), 0600)
		fmt.Println("PASS")
	case "fail":
		content, _ := os.ReadFile(os.Getenv("VIGIL_CONTROL"))
		fmt.Print(string(content))
		os.Exit(3)
	case "timeout":
		time.Sleep(2 * time.Second)
	case "overflow":
		fmt.Print(strings.Repeat("x", 4096))
		os.Exit(2)
	case "missing":
		fmt.Println("no required output")
	default:
		os.Exit(4)
	}
}

type reviewFunc func(context.Context, review.Manifest) ([]byte, error)

func (f reviewFunc) Review(ctx context.Context, manifest review.Manifest) ([]byte, error) {
	return f(ctx, manifest)
}

type assessorFunc func(context.Context, quality.Scope) ([]byte, error)

func (f assessorFunc) Assess(ctx context.Context, scope quality.Scope) ([]byte, error) {
	return f(ctx, scope)
}

type fixture struct {
	t                 *testing.T
	base, root, state string
	manager           *core.Manager
	engine            *core.Engine
	project           core.Project
	owner             *coordinator.Owner
	prepared          supervisor.PreparedRun
	reservation       core.Reservation
	definition        policy.CheckDefinition
}

func git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
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

func setupQuality(t *testing.T, action string, mutate func(*policy.CheckDefinition)) *fixture {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "work")
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{".vigil-disposable-fixture": "fixture\n", "src/input.txt": "input\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, "init", "-q", "-b", "main", root)
	git(t, "-C", root, "add", ".")
	git(t, "-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	definition := policy.CheckDefinition{ID: "quality-check", Argv: []string{executable, "-test.run=^TestQualityCheckHelper$", "--", action}, Cwd: ".", Environment: []policy.EnvironmentVariable{{Name: "VIGIL_CHECK_HELPER", Value: "1"}}, TimeoutMS: 5000, MaxOutputBytes: 1024}
	if action == "pass" || action == "missing" {
		definition.RequiredOutputs = []string{"build/result.txt"}
	}
	if mutate != nil {
		mutate(&definition)
	}
	manager, err := core.OpenManager(ctx, state)
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
	t.Cleanup(func() { _ = engine.DB.Close(); _ = manager.Close() })
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"quality-check"}, CheckDefinitions: []policy.CheckDefinition{definition}, TaskLimitMS: 120000, AttemptLimitMS: 30000, RepairLimit: 2, SupervisorProfile: "local", ApprovalMode: "supervised", HumanAcceptance: true, BlockingSeverity: "high"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "task", Objective: "write result", Criteria: []policy.Criterion{{ID: "automatic", Text: "result exists"}, {ID: "device", Text: "fixture human verifies", Manual: true}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"quality-check"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 2}
	plan := core.Plan{ID: "plan", Title: "quality fixture", Specification: "exercise Stage 5.4", Approved: true, Tasks: []policy.Task{task}, Criteria: []policy.Criterion{{ID: "plan-manual", Text: "fixture plan verification", Manual: true}}, Checks: []string{"quality-check"}, Reviewer: "local", HumanAcceptanceRequired: true}
	command(t, engine, "project.configure", config)
	command(t, engine, "profile.put", profile)
	command(t, engine, "plan.put", plan)
	command(t, engine, "repository.enroll", core.RepositoryEnrollment{ID: "repo", PlanID: "plan", Root: root, BaseRef: "main", PlanBranch: "vigil/quality", DirtyChoice: "clean"})
	var revision int
	_ = engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err = engine.PrepareRepository(ctx, store.ID(), "repo", revision); err != nil {
		t.Fatal(err)
	}
	_ = engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	prepared, err := supervisor.Prepare(ctx, engine, supervisor.PrepareRequest{CommandID: store.ID(), ExpectedProjectRevision: revision, TaskID: "task", RuntimeKind: "synthetic", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Coordinator.Endpoint(ctx, "fixture-endpoint", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	owner, err := manager.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	reservation, err := engine.ReserveResources(ctx, owner, store.ID(), prepared.RunID, "fixture-endpoint")
	if err != nil {
		t.Fatal(err)
	}
	driver := &supervisor.FixtureDriver{RepositoryID: "repo", Root: root, RelativePath: "src/result.txt", Content: []byte("implemented\n")}
	runner := supervisor.Runner{Engine: engine, Owner: owner, Driver: driver}
	if _, err = runner.Run(ctx, prepared, reservation, "implement fixture"); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, base: base, root: root, state: state, manager: manager, engine: engine, project: project, owner: owner, prepared: prepared, reservation: reservation, definition: definition}
}

func (f *fixture) target() quality.Target {
	return quality.Target{Kind: "task", PlanID: "plan", TaskID: "task"}
}
func (f *fixture) runCheck(ctx context.Context, hook func(string) error) (checks.Result, error) {
	runner := checks.Runner{Engine: f.engine, Owner: f.owner, Hook: hook}
	return runner.Run(ctx, checks.Request{CommandID: store.ID(), Target: f.target(), CheckID: "quality-check", Actor: "fixture"})
}
func reviewDocument(decision string, findings []review.Finding) []byte {
	raw, _ := json.Marshal(review.Document{SchemaVersion: 1, Decision: decision, Summary: "fixture review", Findings: findings})
	return raw
}
func (f *fixture) runReview(reviewer review.Reviewer, session string) (review.Result, error) {
	runner := review.Runner{Engine: f.engine, Owner: f.owner, Reviewer: reviewer}
	return runner.Run(context.Background(), review.Request{CommandID: store.ID(), Target: f.target(), Actor: "fixture", SessionID: session, NativeIdentity: "native-" + session})
}

func (f *fixture) releaseExecutionReservation(t *testing.T) {
	t.Helper()
	if err := f.owner.FinishTicket(context.Background(), f.reservation.Ticket, "contained_stopped"); err != nil {
		t.Fatal(err)
	}
	for _, claim := range f.reservation.Claims {
		if err := f.owner.Release(context.Background(), claim, "contained_stopped"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestActualCheckOutcomesAndTamper(t *testing.T) {
	tests := []struct {
		name, action, want string
		mutate             func(*policy.CheckDefinition)
		cancel             bool
	}{{"pass", "pass", "pass", nil, false}, {"failure", "fail", "fail", func(d *policy.CheckDefinition) {
		control := filepath.Join(t.TempDir(), "control")
		_ = os.WriteFile(control, []byte("failure-a\n"), 0600)
		d.Environment = append(d.Environment, policy.EnvironmentVariable{Name: "VIGIL_CONTROL", Value: control})
	}, false}, {"timeout", "timeout", "timeout", func(d *policy.CheckDefinition) { d.TimeoutMS = 30 }, false}, {"interrupted", "timeout", "interrupted", nil, true}, {"missing-output", "missing", "missing_output", nil, false}, {"overflow", "overflow", "output_overflow", func(d *policy.CheckDefinition) { d.MaxOutputBytes = 32 }, false}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setupQuality(t, test.action, test.mutate)
			ctx := context.Background()
			var cancel context.CancelFunc
			var hook func(string) error
			if test.cancel {
				ctx, cancel = context.WithCancel(ctx)
				hook = func(stage string) error {
					if stage == "after_effect_start" {
						go func() { time.Sleep(30 * time.Millisecond); cancel() }()
					}
					return nil
				}
			}
			result, err := f.runCheck(ctx, hook)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.want {
				t.Fatalf("status %s want %s", result.Status, test.want)
			}
			if test.want == "pass" {
				gates, err := quality.EvaluateChecks(context.Background(), f.engine, mustScope(t, f))
				if err != nil || !gates.Satisfied {
					t.Fatal(gates, err)
				}
				blob := filepath.Join(filepath.Dir(f.engine.DB.Path), "artifacts", "blobs", result.OutputArtifactDigest)
				if err := os.WriteFile(blob, []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
				gates, err = quality.EvaluateChecks(context.Background(), f.engine, mustScope(t, f))
				if err != nil || gates.Satisfied {
					t.Fatal("tampered artifact passed", gates, err)
				}
			}
		})
	}
}

func TestSourceMutationAndPausePreventDispatch(t *testing.T) {
	f := setupQuality(t, "pass", nil)
	result, err := f.runCheck(context.Background(), func(stage string) error {
		if stage == "before_source_recheck" {
			return os.WriteFile(filepath.Join(f.root, "src", "host-race.txt"), []byte("race"), 0600)
		}
		return nil
	})
	if err != nil || result.Status != "source_mutated" {
		t.Fatal(result, err)
	}
	f2 := setupQuality(t, "pass", nil)
	var revision int
	_ = f2.engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err = f2.engine.Pause(context.Background(), store.ID(), revision); err != nil {
		t.Fatal(err)
	}
	if _, err = f2.runCheck(context.Background(), nil); err == nil {
		t.Fatal("paused project dispatched check")
	}
	reviewer := reviewFunc(func(context.Context, review.Manifest) ([]byte, error) { return reviewDocument("pass", nil), nil })
	if _, err = f2.runReview(reviewer, "paused-review"); err == nil {
		t.Fatal("paused project dispatched reviewer")
	}
}

func TestBaselineExceptionDoesNotCoverNewFailure(t *testing.T) {
	control := filepath.Join(t.TempDir(), "control")
	_ = os.WriteFile(control, []byte("known-failure\n"), 0600)
	f := setupQuality(t, "fail", func(d *policy.CheckDefinition) {
		d.Environment = append(d.Environment, policy.EnvironmentVariable{Name: "VIGIL_CONTROL", Value: control})
	})
	first, err := f.runCheck(context.Background(), nil)
	if err != nil || first.Status != "fail" {
		t.Fatal(first, err)
	}
	if _, err = quality.AuthorizeBaseline(context.Background(), f.engine, quality.BaselineRequest{CommandID: store.ID(), Target: f.target(), CheckID: "quality-check", FailureIdentities: first.FailureIdentities, Rationale: "fixture-known baseline", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	gates, err := quality.EvaluateChecks(context.Background(), f.engine, mustScope(t, f))
	if err != nil || !gates.Satisfied || gates.Checks[0].Status != "accepted_baseline" {
		t.Fatal(gates, err)
	}
	// Prepare a bounded repair without changing the source, then the same check
	// reports the known failure plus one new identity.
	f.releaseExecutionReservation(t)
	follow, err := supervisor.PrepareFollowup(context.Background(), f.engine, supervisor.FollowupRequest{CommandID: store.ID(), ExpectedRevision: projectRevision(t, f.engine), SourceRunID: f.prepared.RunID, Kind: "repair", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := f.engine.ReserveResources(context.Background(), f.owner, store.ID(), follow.RunID, "fixture-endpoint")
	if err != nil {
		t.Fatal(err)
	}
	driver := &supervisor.FixtureDriver{RepositoryID: "repo", Root: f.root, RelativePath: "src/result.txt", Content: []byte("implemented\n")}
	runner := supervisor.Runner{Engine: f.engine, Owner: f.owner, Driver: driver}
	if _, err = runner.Run(context.Background(), follow, reservation, "repair fixture"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(control, []byte("known-failure\nnew-failure\n"), 0600)
	second, err := f.runCheck(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	gates, err = quality.EvaluateChecks(context.Background(), f.engine, mustScope(t, f))
	if err != nil || gates.Satisfied || second.Status != "fail" {
		t.Fatal("new failure was covered", gates, second, err)
	}
}

func TestReviewValidationWriteDenialAndSuggestion(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		f := setupQuality(t, "pass", nil)
		if _, err := f.runCheck(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		_, err := f.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) {
			return []byte(`{"schema_version":1,"decision":"pass","summary":"x","findings":[],"unknown":true}`), nil
		}), "malformed")
		if !errors.Is(err, review.ErrInvalidReview) {
			t.Fatal(err)
		}
	})
	t.Run("write-denied", func(t *testing.T) {
		f := setupQuality(t, "pass", nil)
		if _, err := f.runCheck(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		result, err := f.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) {
			_ = os.WriteFile(filepath.Join(f.root, "src", "reviewer-write.txt"), []byte("forbidden"), 0600)
			return reviewDocument("pass", nil), nil
		}), "writer")
		if !errors.Is(err, review.ErrInvalidReview) || result.Status != "write_denied" {
			t.Fatal(result, err)
		}
	})
	t.Run("suggestion", func(t *testing.T) {
		f := setupQuality(t, "pass", nil)
		if _, err := f.runCheck(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		finding := review.Finding{ID: "suggest-1", Severity: "suggestion", RepositoryID: "repo", Path: "src/result.txt", Line: 1, Evidence: "style", Recommendation: "optional cleanup"}
		result, err := f.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) {
			return reviewDocument("pass", []review.Finding{finding}), nil
		}), "suggestion")
		if err != nil || result.Status != "pass" || result.Findings[0].Blocking {
			t.Fatal(result, err)
		}
		gate, err := quality.EvaluateReview(context.Background(), f.engine, mustScope(t, f))
		if err != nil || !gate.Satisfied {
			t.Fatal(gate, err)
		}
	})
}

func TestCompleteRepairFreshReviewAndAcceptanceCycle(t *testing.T) {
	control := filepath.Join(t.TempDir(), "control")
	_ = os.WriteFile(control, []byte("blocking\n"), 0600)
	f := setupQuality(t, "fail", func(d *policy.CheckDefinition) {
		d.Environment = append(d.Environment, policy.EnvironmentVariable{Name: "VIGIL_CONTROL", Value: control})
	})
	if result, err := f.runCheck(context.Background(), nil); err != nil || result.Status != "fail" {
		t.Fatal(result, err)
	}
	// Repair is a Stage 5.3 implementation attempt and retains the task ledger.
	f.releaseExecutionReservation(t)
	var chargedBefore int64
	_ = f.engine.DB.SQL.QueryRow("SELECT charged_ms FROM budget_ledgers WHERE scope='task' AND task_id='task'").Scan(&chargedBefore)
	follow, err := supervisor.PrepareFollowup(context.Background(), f.engine, supervisor.FollowupRequest{CommandID: store.ID(), ExpectedRevision: projectRevision(t, f.engine), SourceRunID: f.prepared.RunID, Kind: "repair", WallLimitMS: 60000})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := f.engine.ReserveResources(context.Background(), f.owner, store.ID(), follow.RunID, "fixture-endpoint")
	if err != nil {
		t.Fatal(err)
	}
	driver := &supervisor.FixtureDriver{RepositoryID: "repo", Root: f.root, RelativePath: "src/result.txt", Content: []byte("repaired\n")}
	if _, err = (&supervisor.Runner{Engine: f.engine, Owner: f.owner, Driver: driver}).Run(context.Background(), follow, reservation, "repair"); err != nil {
		t.Fatal(err)
	}
	// The check definition remains fixed; changing the out-of-source fixture
	// control makes the repaired check pass only after a fresh execution.
	_ = os.WriteFile(control, []byte(""), 0600) // helper still exits nonzero, so use an explicit config revision to a passing argv.
	var configRaw string
	_ = f.engine.DB.SQL.QueryRow(`SELECT s.resolved_json FROM project_configurations pc JOIN config_snapshots s ON s.id=pc.config_id ORDER BY pc.revision DESC LIMIT 1`).Scan(&configRaw)
	var config policy.Config
	_ = json.Unmarshal([]byte(configRaw), &config)
	config.CheckDefinitions[0].Argv[len(config.CheckDefinitions[0].Argv)-1] = "pass"
	config.CheckDefinitions[0].RequiredOutputs = []string{"build/result.txt"}
	command(t, f.engine, "project.configure", config)
	var revision int
	_ = f.engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision)
	if _, err = f.engine.Continue(context.Background(), store.ID(), revision); err != nil {
		t.Fatal(err)
	}
	passed, err := f.runCheck(context.Background(), nil)
	if err != nil || passed.Status != "pass" {
		t.Fatal(passed, err)
	}
	// Restart the project database before review to prove durable recovery.
	if err = f.engine.DB.Close(); err != nil {
		t.Fatal(err)
	}
	f.engine, err = f.manager.Open(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	finding := review.Finding{ID: "suggestion", Severity: "suggestion", RepositoryID: "repo", Evidence: "optional", Recommendation: "consider later"}
	reviewResult, err := f.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) {
		return reviewDocument("pass", []review.Finding{finding}), nil
	}), "fresh-after-repair")
	if err != nil || reviewResult.Status != "pass" {
		t.Fatal(reviewResult, err)
	}
	if _, err = quality.RecordManual(context.Background(), f.engine, quality.ManualRequest{CommandID: store.ID(), Target: f.target(), CriterionID: "device", State: "pass", Evaluator: "fixture-human", Notes: "verified disposable fixture", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	if _, err = quality.RecordHumanDecision(context.Background(), f.engine, quality.HumanDecisionRequest{CommandID: store.ID(), Target: f.target(), Action: "accept", Rationale: "fixture acceptance only", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	acceptor := quality.Acceptor{Engine: f.engine, Owner: f.owner}
	if _, err = acceptor.Accept(context.Background(), quality.AcceptanceRequest{CommandID: store.ID(), Target: f.target(), Actor: "fixture_core"}); err != nil {
		t.Fatal(err)
	}
	planTarget := quality.Target{Kind: "plan", PlanID: "plan"}
	checkRunner := checks.Runner{Engine: f.engine, Owner: f.owner}
	if result, err := checkRunner.Run(context.Background(), checks.Request{CommandID: store.ID(), Target: planTarget, CheckID: "quality-check", Actor: "fixture"}); err != nil || result.Status != "pass" {
		t.Fatal(result, err)
	}
	reviewRunner := review.Runner{Engine: f.engine, Owner: f.owner, Reviewer: reviewFunc(func(context.Context, review.Manifest) ([]byte, error) { return reviewDocument("pass", nil), nil })}
	if _, err = reviewRunner.Run(context.Background(), review.Request{CommandID: store.ID(), Target: planTarget, Actor: "fixture", SessionID: "plan-review", NativeIdentity: "native-plan-review"}); err != nil {
		t.Fatal(err)
	}
	if _, err = quality.RecordManual(context.Background(), f.engine, quality.ManualRequest{CommandID: store.ID(), Target: planTarget, CriterionID: "plan-manual", State: "pass", Evaluator: "fixture-human", Notes: "plan fixture verified", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	if _, err = quality.RecordHumanDecision(context.Background(), f.engine, quality.HumanDecisionRequest{CommandID: store.ID(), Target: planTarget, Action: "accept", Rationale: "fixture plan acceptance", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	if _, err = acceptor.Accept(context.Background(), quality.AcceptanceRequest{CommandID: store.ID(), Target: planTarget, Actor: "fixture_core"}); err != nil {
		t.Fatal(err)
	}
	var taskState, planState string
	_ = f.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id='task'").Scan(&taskState)
	_ = f.engine.DB.SQL.QueryRow("SELECT state FROM plans WHERE id='plan'").Scan(&planState)
	if taskState != "accepted" || planState != "finalizing" {
		t.Fatal(taskState, planState)
	}
	var chargedAfter int64
	_ = f.engine.DB.SQL.QueryRow("SELECT charged_ms FROM budget_ledgers WHERE scope='task' AND task_id='task'").Scan(&chargedAfter)
	if chargedAfter < chargedBefore {
		t.Fatal("repair reset cumulative budget")
	}
	var deliveries int
	_ = f.engine.DB.SQL.QueryRow("SELECT count(*) FROM deliveries").Scan(&deliveries)
	if deliveries != 0 {
		t.Fatal("acceptance authorized delivery")
	}
}

func TestStaleCriteriaProfileAndAcceptanceRace(t *testing.T) {
	f := setupQuality(t, "pass", nil)
	if _, err := f.runCheck(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) { return reviewDocument("pass", nil), nil }), "stale-review"); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.RecordManual(context.Background(), f.engine, quality.ManualRequest{CommandID: store.ID(), Target: f.target(), CriterionID: "device", State: "pass", Evaluator: "fixture", Notes: "pass", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	if _, err := quality.RecordHumanDecision(context.Background(), f.engine, quality.HumanDecisionRequest{CommandID: store.ID(), Target: f.target(), Action: "accept", Rationale: "fixture", Actor: "fixture_human"}); err != nil {
		t.Fatal(err)
	}
	command(t, f.engine, "task.criteria.revise", map[string]any{"task_id": "task", "criteria": []policy.Criterion{{ID: "automatic", Text: "changed requirement"}, {ID: "device", Text: "fixture human verifies", Manual: true}}, "reason": "test stale retirement"})
	current, err := quality.Observe(context.Background(), f.engine, f.target())
	if err != nil {
		t.Fatal(err)
	}
	if err = quality.Persist(context.Background(), f.engine, current); err != nil {
		t.Fatal(err)
	}
	if err = quality.DetectAndRecordStaleness(context.Background(), f.engine, current); err != nil {
		t.Fatal(err)
	}
	var stale int
	_ = f.engine.DB.SQL.QueryRow("SELECT count(*) FROM evidence_staleness_v2 WHERE reason='criteria'").Scan(&stale)
	if stale == 0 {
		t.Fatal("criteria change did not record staleness")
	}
	// A fresh fixture proves repository mutation between gate evaluation and the
	// final acceptance observation records a raced attempt.
	r := setupQuality(t, "pass", nil)
	if _, err = r.runCheck(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.runReview(reviewFunc(func(context.Context, review.Manifest) ([]byte, error) { return reviewDocument("pass", nil), nil }), "race-review"); err != nil {
		t.Fatal(err)
	}
	_, _ = quality.RecordManual(context.Background(), r.engine, quality.ManualRequest{CommandID: store.ID(), Target: r.target(), CriterionID: "device", State: "pass", Evaluator: "fixture", Notes: "pass", Actor: "fixture_human"})
	_, _ = quality.RecordHumanDecision(context.Background(), r.engine, quality.HumanDecisionRequest{CommandID: store.ID(), Target: r.target(), Action: "accept", Rationale: "fixture", Actor: "fixture_human"})
	acceptor := quality.Acceptor{Engine: r.engine, Owner: r.owner, Hook: func(string) error {
		return os.WriteFile(filepath.Join(r.root, "src", "accept-race.txt"), []byte("race"), 0600)
	}}
	if _, err = acceptor.Accept(context.Background(), quality.AcceptanceRequest{CommandID: store.ID(), Target: r.target(), Actor: "fixture_core"}); err == nil {
		t.Fatal("acceptance race passed")
	}
	var raced int
	_ = r.engine.DB.SQL.QueryRow("SELECT count(*) FROM quality_acceptance_attempts_v2 WHERE outcome='raced'").Scan(&raced)
	if raced != 1 {
		t.Fatal("race attempt missing", raced)
	}
}

func TestDefinitionProfileFreshnessAndUncertainRestart(t *testing.T) {
	t.Run("configuration-and-profile", func(t *testing.T) {
		f := setupQuality(t, "pass", nil)
		if _, err := f.runCheck(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		prior := mustScope(t, f)
		var configRaw string
		_ = f.engine.DB.SQL.QueryRow(`SELECT s.resolved_json FROM project_configurations pc JOIN config_snapshots s ON s.id=pc.config_id ORDER BY pc.revision DESC LIMIT 1`).Scan(&configRaw)
		var config policy.Config
		_ = json.Unmarshal([]byte(configRaw), &config)
		config.CheckDefinitions[0].MaxOutputBytes = 2048
		command(t, f.engine, "project.configure", config)
		current, err := quality.Observe(context.Background(), f.engine, f.target())
		if err != nil {
			t.Fatal(err)
		}
		reasons := quality.StaleReasons(prior, current)
		if !containsString(reasons, "configuration") || !containsString(reasons, "check_set") {
			t.Fatal("missing check invalidation", reasons)
		}
		profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture-v2", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
		command(t, f.engine, "profile.put", profile)
		newer, err := quality.Observe(context.Background(), f.engine, f.target())
		if err != nil {
			t.Fatal(err)
		}
		if !containsString(quality.StaleReasons(current, newer), "reviewer_profile") {
			t.Fatal("profile revision did not stale evidence")
		}
	})
	t.Run("uncertain-effect", func(t *testing.T) {
		f := setupQuality(t, "pass", nil)
		request := checks.Request{CommandID: store.ID(), Target: f.target(), CheckID: "quality-check", Actor: "fixture"}
		runner := checks.Runner{Engine: f.engine, Owner: f.owner, Hook: func(stage string) error {
			if stage == "after_effect_start" {
				return errors.New("fixture crash")
			}
			return nil
		}}
		if _, err := runner.Run(context.Background(), request); err == nil {
			t.Fatal("crash hook did not interrupt")
		}
		if err := f.engine.DB.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := f.manager.Open(context.Background(), f.project.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.engine = reopened
		ids, err := quality.RecoverUnfinished(context.Background(), f.engine)
		if err != nil || len(ids) != 1 {
			t.Fatal(ids, err)
		}
		if _, err = (&checks.Runner{Engine: f.engine, Owner: f.owner}).Run(context.Background(), request); err == nil {
			t.Fatal("uncertain check effect replayed")
		}
	})
}

func TestBoundedSupervisorAssessmentCannotAcceptOrOverspend(t *testing.T) {
	f := setupQuality(t, "fail", func(d *policy.CheckDefinition) {
		control := filepath.Join(t.TempDir(), "control")
		_ = os.WriteFile(control, []byte("failure\n"), 0600)
		d.Environment = append(d.Environment, policy.EnvironmentVariable{Name: "VIGIL_CONTROL", Value: control})
	})
	if _, err := f.runCheck(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	// A labeled fixture simulates persisted repair-count exhaustion; the real
	// path derives this state from completed Stage 5.3 repair attempts.
	if _, err := f.engine.DB.SQL.Exec("UPDATE tasks SET repair_limit=0 WHERE id='task'"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(quality.AssessmentDocument{SchemaVersion: 1, Action: "revise_or_split", Rationale: "fixture assessment only"})
	assessment, err := quality.RunAssessment(context.Background(), f.engine, f.owner, assessorFunc(func(context.Context, quality.Scope) ([]byte, error) { return raw, nil }), quality.AssessmentRequest{CommandID: store.ID(), Target: f.target(), SourceKind: "repair_exhaustion", Actor: "fixture"})
	if err != nil || assessment.Action != "revise_or_split" {
		t.Fatal(assessment, err)
	}
	var state string
	_ = f.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id='task'").Scan(&state)
	if state != "blocked" {
		t.Fatal("assessment changed task beyond blocked", state)
	}
	var accepted int
	_ = f.engine.DB.SQL.QueryRow("SELECT count(*) FROM quality_acceptances_v2").Scan(&accepted)
	if accepted != 0 {
		t.Fatal("assessment accepted partial work")
	}
	// Once the same task ledger is exhausted, no assessment effect may start.
	if _, err = f.engine.DB.SQL.Exec("UPDATE budget_ledgers SET charged_ms=active_limit_ms WHERE scope='task' AND task_id='task'"); err != nil {
		t.Fatal(err)
	}
	before := 0
	_ = f.engine.DB.SQL.QueryRow("SELECT count(*) FROM quality_effects_v2 WHERE kind='supervisor_assessment'").Scan(&before)
	if _, err = quality.RunAssessment(context.Background(), f.engine, f.owner, assessorFunc(func(context.Context, quality.Scope) ([]byte, error) { return raw, nil }), quality.AssessmentRequest{CommandID: store.ID(), Target: f.target(), SourceKind: "repair_exhaustion", Actor: "fixture"}); err == nil {
		t.Fatal("assessment overspent exhausted ledger")
	}
	after := 0
	_ = f.engine.DB.SQL.QueryRow("SELECT count(*) FROM quality_effects_v2 WHERE kind='supervisor_assessment'").Scan(&after)
	if after != before {
		t.Fatal("exhausted assessment created another effect")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mustScope(t *testing.T, f *fixture) quality.Scope {
	t.Helper()
	scope, err := quality.Observe(context.Background(), f.engine, f.target())
	if err != nil {
		t.Fatal(err)
	}
	if err = quality.Persist(context.Background(), f.engine, scope); err != nil {
		t.Fatal(err)
	}
	return scope
}
func projectRevision(t *testing.T, engine *core.Engine) int {
	t.Helper()
	var revision int
	if err := engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return revision
}
