package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vigil/internal/boundary"
	"vigil/internal/coordinator"
)

type inspectFailureDriver struct {
	*FixtureDriver
	fail bool
}

type blockingCreateDriver struct {
	*FixtureDriver
	entered chan struct{}
	release chan struct{}
}

func (d *blockingCreateDriver) Create(ctx context.Context, prepared PreparedRun) error {
	close(d.entered)
	select {
	case <-d.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.FixtureDriver.Create(ctx, prepared)
}

func (d *inspectFailureDriver) Inspect(ctx context.Context, prepared PreparedRun) (Observation, error) {
	if d.fail {
		return Observation{}, errors.New("inspection unavailable")
	}
	return d.FixtureDriver.Inspect(ctx, prepared)
}

func TestInterruptedEffectRequiresInspectionBeforeReplay(t *testing.T) {
	fixture := setupFixture(t)
	driver := &inspectFailureDriver{FixtureDriver: fixture.driver}
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver, Fault: func(point string) error {
		if point == "after_runtime_create" {
			return errors.New("controller crash")
		}
		return nil
	}}
	if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	call := func(ctx context.Context) error { return driver.Create(ctx, fixture.prepared) }
	if err := runner.ensureDriverEffect(context.Background(), fixture.prepared, 1, "runtime_create", call); err == nil {
		t.Fatal("expected injected crash")
	}
	runner.Fault = nil
	driver.fail = true
	if err := runner.ensureDriverEffect(context.Background(), fixture.prepared, 1, "runtime_create", call); err == nil {
		t.Fatal("unavailable recovery inspection authorized replay")
	}
	if driver.Calls["create"] != 1 {
		t.Fatal("runtime create replayed", driver.Calls)
	}
}

func TestDeliveredSubmissionNeverReplaysWhenInspectionFails(t *testing.T) {
	fixture := setupFixture(t)
	fixture.driver.RelativePath = "src/.keep"
	fixture.driver.Content = []byte("fixture\n")
	driver := &inspectFailureDriver{FixtureDriver: fixture.driver}
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver, Fault: func(point string) error {
		if point == "after_terminal_observe" {
			return errors.New("controller crash")
		}
		return nil
	}}
	if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result"); err == nil {
		t.Fatal("expected injected crash")
	}
	if err := runner.ReconcileOpenSegment(context.Background(), fixture.prepared, 0); err != nil {
		t.Fatal(err)
	}
	runner.Fault = nil
	driver.fail = true
	_, _ = runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result")
	if driver.Calls["submit"] != 1 {
		t.Fatal("delivered prompt replayed", driver.Calls)
	}
}

func TestLiveReservationAuthorityCannotCloseDuringDispatch(t *testing.T) {
	fixture := setupFixture(t)
	driver := &blockingCreateDriver{FixtureDriver: fixture.driver, entered: make(chan struct{}), release: make(chan struct{})}
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
	runDone := make(chan error, 1)
	go func() {
		_, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result")
		runDone <- err
	}()
	select {
	case <-driver.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not reach the external effect")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- fixture.owner.Close() }()
	select {
	case err := <-closeDone:
		t.Fatal("owner closed while reservation authorized an external effect", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(driver.release)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

func TestFixtureWriteRejectsSymlinkAndReplacesHardlink(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		fixture := setupFixture(t)
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(victim, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, filepath.Join(fixture.driver.Root, ".git", "review-escape")); err != nil {
			t.Fatal(err)
		}
		fixture.driver.RelativePath = ".git/review-escape"
		runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: fixture.driver}
		if _, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result"); err == nil {
			t.Fatal("protected symlink target accepted")
		}
		got, err := os.ReadFile(victim)
		if err != nil || string(got) != "preserve" {
			t.Fatal("outside symlink victim changed", string(got), err)
		}
	})

	t.Run("hardlink", func(t *testing.T) {
		fixture := setupFixture(t)
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(victim, []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(fixture.driver.Root, "src", ".keep")
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(victim, target); err != nil {
			t.Fatal(err)
		}
		fixture.driver.RelativePath = "src/.keep"
		runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: fixture.driver}
		result, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result")
		if err != nil || result.Status != "completed" {
			t.Fatal(result, err)
		}
		got, err := os.ReadFile(victim)
		if err != nil || string(got) != "fixture\n" {
			t.Fatal("outside hardlink victim changed", string(got), err)
		}
	})
}

type delayedSubmitDriver struct {
	*FixtureDriver
	delay time.Duration
}

func (d *delayedSubmitDriver) Submit(_ context.Context, prepared PreparedRun, prompt string) (string, string, error) {
	time.Sleep(d.delay)
	return d.FixtureDriver.Submit(context.Background(), prepared, prompt)
}

func TestActiveBudgetBoundsSubmissionAndCompletion(t *testing.T) {
	fixture := setupFixtureWithLimits(t, 1000, 5000)
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: &delayedSubmitDriver{FixtureDriver: fixture.driver, delay: 1200 * time.Millisecond}}
	if result, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result"); err == nil {
		t.Fatal("execution completed beyond active allowance", result)
	}
	if fixture.driver.Calls["submit"] != 1 {
		t.Fatal("slow-submission regression did not reach the driver", fixture.driver.Calls)
	}
	if _, err := runner.Reconcile(context.Background(), fixture.prepared); err == nil {
		t.Fatal("reconciliation accepted an over-budget terminal result")
	}
	view, err := runner.Inspect(context.Background(), fixture.prepared)
	if err != nil || view.RunState == "completed" || view.TaskState == "checking" {
		t.Fatal(view, err)
	}
}

func TestOutcomeCommitRechecksPersistedBudget(t *testing.T) {
	fixture := setupFixtureWithLimits(t, 1000, 5000)
	hookReached := false
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: fixture.driver, Fault: func(point string) error {
		if point == "before_outcome_commit" {
			hookReached = true
			time.Sleep(1200 * time.Millisecond)
		}
		return nil
	}}
	if result, err := runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result"); err == nil {
		t.Fatal("late outcome committed beyond active allowance", result)
	}
	if !hookReached {
		t.Fatal("late-outcome regression did not reach the outcome boundary")
	}
	view, err := runner.Inspect(context.Background(), fixture.prepared)
	if err != nil || view.RunState == "completed" || view.TaskState == "checking" {
		t.Fatal(view, err)
	}
}

type stopIntentDriver struct {
	*FixtureDriver
	runner *Runner
	t      *testing.T
}

func (d *stopIntentDriver) Start(context.Context, PreparedRun) error {
	return errors.New("force containment")
}

func (d *stopIntentDriver) Stop(ctx context.Context, prepared PreparedRun) (Observation, error) {
	var count int
	if err := d.runner.Engine.DB.SQL.QueryRow("SELECT count(*) FROM execution_effects WHERE generation_id=? AND kind='containment_stop' AND state='executing'", prepared.GenerationID).Scan(&count); err != nil {
		d.t.Fatal(err)
	}
	if count != 1 {
		d.t.Error("stop invoked without durable executing containment intent")
	}
	return d.FixtureDriver.Stop(ctx, prepared)
}

func TestContainmentIntentPrecedesExternalStop(t *testing.T) {
	fixture := setupFixture(t)
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner}
	runner.Driver = &stopIntentDriver{FixtureDriver: fixture.driver, runner: &runner, t: t}
	_, _ = runner.Run(context.Background(), fixture.prepared, fixture.reservation, "write result")
}

func TestHumanWaitCheckpointRemainsExcluded(t *testing.T) {
	fixture := setupFixture(t)
	fixture.prepared.ActiveLimitMS = 50
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: fixture.driver}
	if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
	if err := runner.checkpointSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal("proven human wait exhausted active budget", err)
	}
}

type controlledAwaitDriver struct {
	*FixtureDriver
	entered chan struct{}
	release chan struct{}
}

func (d *controlledAwaitDriver) Await(ctx context.Context, prepared PreparedRun) (Observation, error) {
	close(d.entered)
	select {
	case <-d.release:
		return d.FixtureDriver.Inspect(ctx, prepared)
	case <-ctx.Done():
		return Observation{}, ctx.Err()
	}
}

func TestAwaitSuspendsAndResumesActiveDeadlineForProvenWait(t *testing.T) {
	t.Run("already_waiting_uses_wall_context", func(t *testing.T) {
		fixture := setupFixtureWithLimits(t, 100, 1000)
		driver := &controlledAwaitDriver{FixtureDriver: fixture.driver, entered: make(chan struct{}), release: make(chan struct{})}
		runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
		if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
			t.Fatal(err)
		}
		if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		began := time.Now()
		_, err := runner.awaitWithLease(ctx, fixture.prepared)
		if ctx.Err() == nil || time.Since(began) < 250*time.Millisecond {
			t.Fatal("proven wait was bounded by active rather than wall context", time.Since(began), err)
		}
	})

	t.Run("wait_suspends_deadline", func(t *testing.T) {
		fixture := setupFixtureWithLimits(t, 500, 2000)
		driver := &controlledAwaitDriver{FixtureDriver: fixture.driver, entered: make(chan struct{}), release: make(chan struct{})}
		runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
		if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := runner.awaitWithLease(context.Background(), fixture.prepared); done <- err }()
		<-driver.entered
		time.Sleep(25 * time.Millisecond)
		if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(225 * time.Millisecond)
		select {
		case err := <-done:
			t.Fatal("await ended during excluded wait", err)
		default:
		}
		if err := runner.EndHumanWait(context.Background(), fixture.prepared); err != nil {
			t.Fatal(err)
		}
		close(driver.release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("resume_restores_deadline", func(t *testing.T) {
		fixture := setupFixtureWithLimits(t, 500, 2000)
		driver := &controlledAwaitDriver{FixtureDriver: fixture.driver, entered: make(chan struct{}), release: make(chan struct{})}
		runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
		if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := runner.awaitWithLease(context.Background(), fixture.prepared); done <- err }()
		<-driver.entered
		time.Sleep(25 * time.Millisecond)
		if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
			t.Fatal(err)
		}
		time.Sleep(225 * time.Millisecond)
		if err := runner.EndHumanWait(context.Background(), fixture.prepared); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("await succeeded without terminal observation")
			}
		case <-time.After(900 * time.Millisecond):
			close(driver.release)
			t.Fatal("active deadline was not restored after proven wait")
		}
	})
}

func TestProductionQualificationBindsPreparedExecution(t *testing.T) {
	fixture := setupFixture(t)
	checkout, err := boundary.PlanCheckout(context.Background(), fixture.driver.Root)
	if err != nil {
		t.Fatal(err)
	}
	mountDigest, err := checkout.QualificationMountPlanDigest(context.Background(), "implementation")
	if err != nil {
		t.Fatal(err)
	}
	prepared := fixture.prepared
	prepared.RuntimeKind = "docker"
	request := boundary.EligibilityRequest{
		Role:   "implementation",
		Layout: checkout.QualificationLayout(),
		Inputs: boundary.QualificationInputs{
			ProfileID: prepared.ProfileID, ProfileRevision: prepared.ProfileRevision, ProfileDigest: prepared.ProfileDigest,
			EndpointAuthority: prepared.EndpointID, CapacityAuthority: coordinator.Host(), CapacityAuthorityScope: "single_host",
			RuntimeName: "docker", MountPlanDigest: mountDigest,
		},
		RequiredClaims:          []string{boundary.ClaimBoundaryExecution, boundary.ClaimProviderIdle, boundary.ClaimProductionLaunch},
		RequiredRecoveryClasses: append([]string(nil), productionRecoveryClasses...),
	}
	runner := Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: fixture.driver, Checkout: &checkout}

	roleMismatch := request
	roleMismatch.Role = "review"
	prepared.Eligibility = &roleMismatch
	if err := runner.validateProductionBinding(context.Background(), prepared); err == nil || !strings.Contains(err.Error(), "implementation profile") {
		t.Fatal("different qualification role accepted", err)
	}

	endpointMismatch := request
	endpointMismatch.Inputs.EndpointAuthority = "other-endpoint"
	prepared.Eligibility = &endpointMismatch
	if err := runner.validateProductionBinding(context.Background(), prepared); err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatal("different endpoint authority accepted", err)
	}

	mountMismatch := request
	mountMismatch.Inputs.MountPlanDigest = strings.Repeat("0", 64)
	prepared.Eligibility = &mountMismatch
	if err := runner.validateProductionBinding(context.Background(), prepared); err == nil || !strings.Contains(err.Error(), "mount plan") {
		t.Fatal("different mount plan accepted", err)
	}

	prepared.Eligibility = &request
	prepared.ExpectedRoutes = []string{"route-a"}
	if err := runner.validateProductionBinding(context.Background(), prepared); err == nil || !strings.Contains(err.Error(), "independently derived") {
		t.Fatal("production driver without an actual binding accepted", err)
	}
}
