package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/checkpoint"
	"vigil/internal/store"
)

func prepareInterruptedRecovery(t *testing.T, fixture fixture) checkpoint.SaveReceipt {
	t.Helper()
	nativeSession := "fixture-session-" + fixture.prepared.GenerationID
	if _, err := fixture.engine.DB.SQL.Exec(`INSERT INTO sessions(id,run_id,generation,harness,durable_id,runtime_id,native_turn_id,native_home_ref,workspace_identity,profile_digest,capabilities_json) VALUES(?,?,?,?,?,?,?,?,?,?,'{"resume":true}')`, store.ID(), fixture.prepared.RunID, fixture.prepared.TransportGeneration, fixture.prepared.ProfileID, nativeSession, nativeSession, "turn-old", "private:"+fixture.prepared.GenerationID, fixture.prepared.Repositories[0].Identity.Key, fixture.prepared.ProfileDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE run_generations SET native_session_id=?,native_turn_id='turn-old',state='contained',submission_state='delivered' WHERE id=?", nativeSession, fixture.prepared.GenerationID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE runs SET state='interrupted',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), fixture.prepared.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE tasks SET state='blocked',block_reason='interrupted recovery fixture' WHERE id=?", fixture.prepared.TaskID); err != nil {
		t.Fatal(err)
	}
	manager, err := checkpoint.NewManager(fixture.engine)
	if err != nil {
		t.Fatal(err)
	}
	repository := fixture.prepared.Repositories[0]
	receipt, err := manager.Save(context.Background(), checkpoint.SaveRequest{CommandID: "recovery-save", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Repositories: []checkpoint.RepositorySpec{{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: fixture.prepared.Task.Scope}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.driver.Verifier = CheckpointVerifierPair{
		Verify: func(ctx context.Context, checkpointID string) error {
			_, verifyErr := manager.VerifySet(ctx, checkpointID)
			return verifyErr
		},
		VerifyCurrent: manager.VerifyCheckpointCurrent,
	}
	return receipt
}

func exactHistory(fixture fixture) HistoryObservation {
	return HistoryObservation{State: "readable", RecoveryClass: "interrupted", NativeHomeRef: "private:" + fixture.prepared.GenerationID, NativeSessionID: "fixture-session-" + fixture.prepared.GenerationID, ProfileDigest: fixture.prepared.ProfileDigest, WorkspaceIdentity: fixture.prepared.Repositories[0].Identity.Key, TransportGeneration: fixture.prepared.TransportGeneration, Qualification: "synthetic"}
}

func TestRecoveryClassificationFailsClosedForMissingCorruptAndMismatchedHistory(t *testing.T) {
	for _, state := range []string{"missing", "corrupt"} {
		t.Run(state, func(t *testing.T) {
			fixture := setupFixture(t)
			prepareInterruptedRecovery(t, fixture)
			driver := fixture.driver
			history := exactHistory(fixture)
			history.State = state
			driver.History = &history
			runner := Runner{Engine: fixture.engine, Driver: driver}
			eligibility, _, err := runner.RecoveryEligibility(context.Background(), fixture.prepared, driver)
			if err != nil || eligibility.ExactResume || !eligibility.FreshContext {
				t.Fatal(eligibility, err)
			}
			choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "bad-exact-" + state, ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "exact_resume"}, driver)
			if err == nil || choice.State != "ineligible" {
				t.Fatal("missing/corrupt history became exact resume", choice, err)
			}
			var runs int
			if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM runs").Scan(&runs); err != nil || runs != 1 {
				t.Fatal("ineligible resume created replacement attempt", runs, err)
			}
		})
	}
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	history.ProfileDigest = strings.Repeat("0", 64)
	fixture.driver.History = &history
	eligibility, _, err := (&Runner{Engine: fixture.engine}).RecoveryEligibility(context.Background(), fixture.prepared, fixture.driver)
	if err != nil || eligibility.ExactResume {
		t.Fatal("mismatched profile identity became exact resume", eligibility, err)
	}
}

func TestExplicitRemainBlockedCompletesWithoutReplacementAttempt(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	history.State = "corrupt"
	fixture.driver.History = &history
	receipt, err := (&Runner{Engine: fixture.engine}).ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "remain-blocked", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "remain_blocked"}, fixture.driver)
	if err != nil || receipt.State != "consumed" {
		t.Fatal(receipt, err)
	}
	var runs int
	var projectState string
	_ = fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM runs").Scan(&runs)
	_ = fixture.engine.DB.SQL.QueryRow("SELECT state FROM project").Scan(&projectState)
	if runs != 1 || projectState != "paused" {
		t.Fatal("continued blocking changed execution authority", runs, projectState)
	}
}

type completedResumeDriver struct{ *FixtureDriver }

func (d *completedResumeDriver) Resume(ctx context.Context, prepared PreparedRun, nativeSessionID string) error {
	if err := d.FixtureDriver.Resume(ctx, prepared, nativeSessionID); err != nil {
		return err
	}
	if err := confinedReplace(d.Root, d.RelativePath, d.Content); err != nil {
		return err
	}
	result, _ := json.Marshal(Result{SchemaVersion: 1, Status: "completed", Summary: "exact resumed fixture completed", ChangedPaths: []string{d.RepositoryID + ":" + filepath.ToSlash(d.RelativePath)}})
	d.Restore(Observation{Exists: true, Started: true, Attached: true, NativeSessionID: nativeSessionID, NativeTurnID: "turn-old", SubmissionState: "delivered", Terminal: true, Outcome: "completed", Result: result, WriterState: "contained_stopped"})
	return nil
}

func TestExactResumeUsesNewGenerationWithoutPromptReplay(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	fixture.driver.History = &history
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "choose-exact", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "exact_resume"}, fixture.driver)
	if err != nil || choice.State != "eligible" {
		t.Fatal(choice, err)
	}
	resumeRevision := projectRevision(t, fixture)
	prepared, err := runner.PrepareExactResume(context.Background(), ExactResumeRequest{CommandID: "prepare-exact", ExpectedRevision: resumeRevision, ChoiceID: choice.ChoiceID})
	if err != nil || !prepared.ExactResume || prepared.GenerationID == fixture.prepared.GenerationID || prepared.ResumeNativeSession != history.NativeSessionID {
		t.Fatal(prepared, err)
	}
	repeated, err := runner.PrepareExactResume(context.Background(), ExactResumeRequest{CommandID: "prepare-exact", ExpectedRevision: resumeRevision, ChoiceID: choice.ChoiceID})
	if err != nil || repeated.GenerationID != prepared.GenerationID {
		t.Fatal("exact-resume preparation was not idempotent", repeated, err)
	}
	driver := &completedResumeDriver{FixtureDriver: &FixtureDriver{RepositoryID: fixture.driver.RepositoryID, Root: fixture.driver.Root, RelativePath: fixture.driver.RelativePath, Content: fixture.driver.Content, History: &history}}
	runner = Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
	if _, err := runner.Run(context.Background(), prepared, fixture.reservation, "replacement prompt"); err == nil || !strings.Contains(err.Error(), "refuses a replacement prompt") {
		t.Fatal("exact resume accepted replacement instructions", err)
	}
	result, err := runner.Run(context.Background(), prepared, fixture.reservation, "")
	if err != nil || result.Status != "completed" || driver.Calls["resume"] != 1 || driver.Calls["submit"] != 0 || driver.Calls["native_create"] != 0 {
		t.Fatal(result, driver.Calls, err)
	}
	var generations int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM run_generations WHERE run_id=?", prepared.RunID).Scan(&generations); err != nil || generations != 2 {
		t.Fatal("exact resume did not retain generation history", generations, err)
	}
}

func TestExactResumeUsesCheckpointedPartialWorkspace(t *testing.T) {
	fixture := setupFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.driver.Root, "src", "result.txt"), []byte("partial agent work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	fixture.driver.History = &history
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "choose-partial-exact", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "exact_resume"}, fixture.driver)
	if err != nil || !choice.Eligibility.ExactResume {
		t.Fatal(choice, err)
	}
	prepared, err := runner.PrepareExactResume(context.Background(), ExactResumeRequest{CommandID: "prepare-partial-exact", ExpectedRevision: projectRevision(t, fixture), ChoiceID: choice.ChoiceID})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPrepared(context.Background(), fixture.engine, prepared.RunID)
	if err != nil || !snapshotEqualValue(loaded.Repositories, prepared.Repositories) {
		t.Fatal("generation recovery baseline was not durable", err)
	}
	driver := &completedResumeDriver{FixtureDriver: &FixtureDriver{RepositoryID: fixture.driver.RepositoryID, Root: fixture.driver.Root, RelativePath: fixture.driver.RelativePath, Content: fixture.driver.Content, History: &history}}
	runner = Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
	if _, err := runner.Run(context.Background(), loaded, fixture.reservation, ""); err != nil {
		t.Fatal("checkpointed partial workspace could not resume", err)
	}
}

func TestExactResumeRevalidatesNativeHistoryAtDispatch(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	fixture.driver.History = &history
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "choose-history-change", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "exact_resume"}, fixture.driver)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := runner.PrepareExactResume(context.Background(), ExactResumeRequest{CommandID: "prepare-history-change", ExpectedRevision: projectRevision(t, fixture), ChoiceID: choice.ChoiceID})
	if err != nil {
		t.Fatal(err)
	}
	history.State = "corrupt"
	driver := &completedResumeDriver{FixtureDriver: &FixtureDriver{RepositoryID: fixture.driver.RepositoryID, Root: fixture.driver.Root, RelativePath: fixture.driver.RelativePath, Content: fixture.driver.Content, History: &history}}
	runner = Runner{Engine: fixture.engine, Owner: fixture.owner, Driver: driver}
	if _, err := runner.Run(context.Background(), prepared, fixture.reservation, ""); err == nil || !strings.Contains(err.Error(), "history identity") {
		t.Fatal("changed native history reached resume", err)
	}
	if driver.Calls["resume"] != 0 {
		t.Fatal("resume started before history revalidation", driver.Calls)
	}
}

func TestFreshContextIsNewAttemptAndKeepsCumulativeLedger(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	history.State = "missing"
	fixture.driver.History = &history
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE budget_ledgers SET charged_ms=55000 WHERE scope='task' AND task_id=?", fixture.prepared.TaskID); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "choose-fresh", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "fresh_context"}, fixture.driver)
	if err != nil || choice.State != "eligible" || choice.ContextArtifactID == "" {
		t.Fatal(choice, err)
	}
	repository, err := artifacts.New(fixture.engine.DB)
	if err != nil {
		t.Fatal(err)
	}
	contextBytes, err := repository.Read(context.Background(), choice.ContextArtifactID)
	if err != nil || !strings.Contains(string(contextBytes), "new recorded attempt") || !strings.Contains(string(contextBytes), `"charged_ms":55000`) {
		t.Fatal("fresh context omitted recovery facts", string(contextBytes), err)
	}
	prepared, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "prepare-fresh", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "fresh_context", ChoiceID: choice.ChoiceID, WallLimitMS: 60000, Verifier: fixture.driver})
	if err != nil || prepared.RunID == fixture.prepared.RunID || prepared.AttemptKind != "continuation" {
		t.Fatal(prepared, err)
	}
	var ledgers int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM budget_ledgers WHERE scope='task' AND task_id=?", prepared.TaskID).Scan(&ledgers); err != nil || ledgers != 1 {
		t.Fatal("fresh reconstruction reset cumulative ledger", ledgers, err)
	}
	runner = Runner{Engine: fixture.engine}
	if err := runner.startSegment(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	remaining, err := runner.remainingActive(context.Background(), prepared)
	if err != nil || remaining > 5*time.Second || remaining <= 0 {
		t.Fatal("fresh attempt reset charged cumulative allowance", remaining, err)
	}
	if err := runner.closeSegment(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := fixture.engine.DB.SQL.QueryRow("SELECT mode FROM recovery_attempt_links WHERE choice_id=?", choice.ChoiceID).Scan(&mode); err != nil || mode != "fresh_context" {
		t.Fatal("fresh attempt disguised as native resume", mode, err)
	}
}

func TestFreshContextPreparationReverifiesBoundCheckpoint(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	history.State = "missing"
	fixture.driver.History = &history
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	choice, err := runner.ChooseRecovery(context.Background(), RecoveryChoiceRequest{CommandID: "choose-corrupt-fresh", ExpectedRevision: projectRevision(t, fixture), RunID: fixture.prepared.RunID, Mode: "fresh_context"}, fixture.driver)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := checkpoint.NewManager(fixture.engine)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.VerifySet(context.Background(), choice.Eligibility.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(manager.Store.Dir, "blobs", manifest.Repositories[0].IndexBlob)
	if err := os.WriteFile(blob, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "reject-corrupt-fresh", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "fresh_context", ChoiceID: choice.ChoiceID, WallLimitMS: 60000, Verifier: fixture.driver})
	if err == nil || !strings.Contains(err.Error(), "no longer verifies") {
		t.Fatal("corrupt bound checkpoint started fresh attempt", err)
	}
	var runs int
	if err := fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM runs").Scan(&runs); err != nil || runs != 1 {
		t.Fatal("failed checkpoint verification progressed task", runs, err)
	}
}

func TestRetryKindsAndBudgetExhaustionRemainDistinct(t *testing.T) {
	t.Run("repair_requires_repair_state_and_has_own_allowance", func(t *testing.T) {
		fixture := setupFixture(t)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE runs SET state='failed',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), fixture.prepared.RunID)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE run_generations SET state='contained',submission_state='delivered' WHERE id=?", fixture.prepared.GenerationID)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE tasks SET state='needs_repair' WHERE id=?", fixture.prepared.TaskID)
		prepared, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "repair", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "repair", WallLimitMS: 60000})
		if err != nil || prepared.AttemptKind != "repair" {
			t.Fatal(prepared, err)
		}
		if _, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "repair-again", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "repair", WallLimitMS: 60000}); err == nil {
			t.Fatal("repair allowance reset")
		}
	})
	t.Run("infrastructure_requires_no_delivery", func(t *testing.T) {
		fixture := setupFixture(t)
		if _, err := fixture.engine.DB.SQL.Exec("UPDATE runs SET state='failed',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), fixture.prepared.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.DB.SQL.Exec("UPDATE run_generations SET state='contained',submission_state='proven_not_delivered' WHERE id=?", fixture.prepared.GenerationID); err != nil {
			t.Fatal(err)
		}
		prepared, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "infra", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "infrastructure", WallLimitMS: 60000})
		if err != nil || prepared.AttemptKind != "infrastructure" {
			t.Fatal(prepared, err)
		}
		if _, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "infra-again", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "infrastructure", WallLimitMS: 60000}); err == nil {
			t.Fatal("infrastructure retry allowance reset")
		}
	})
	t.Run("infrastructure_requires_contained_source_writer", func(t *testing.T) {
		fixture := setupFixture(t)
		if _, err := fixture.engine.DB.SQL.Exec("UPDATE runs SET state='failed',writer_state='unconfirmed' WHERE id=?", fixture.prepared.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "unsafe-infra", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "infrastructure", WallLimitMS: 60000}); err == nil || !strings.Contains(err.Error(), "writer containment") {
			t.Fatal("unresolved source writer created infrastructure authority", err)
		}
	})
	t.Run("delivered_prompt_blocks_infrastructure_retry", func(t *testing.T) {
		fixture := setupFixture(t)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE runs SET state='failed',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), fixture.prepared.RunID)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE run_generations SET state='contained',submission_state='delivered' WHERE id=?", fixture.prepared.GenerationID)
		if _, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "bad-infra", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "infrastructure", WallLimitMS: 60000}); err == nil || !strings.Contains(err.Error(), "no delivered prompt") {
			t.Fatal("delivered prompt became infrastructure retry", err)
		}
	})
	t.Run("exhaustion_persists_without_progress", func(t *testing.T) {
		fixture := setupFixture(t)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE runs SET state='failed',writer_state='contained_stopped',ended_at=? WHERE id=?", store.Now(), fixture.prepared.RunID)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE run_generations SET state='contained',submission_state='proven_not_delivered' WHERE id=?", fixture.prepared.GenerationID)
		_, _ = fixture.engine.DB.SQL.Exec("UPDATE budget_ledgers SET charged_ms=active_limit_ms WHERE scope='task' AND task_id=?", fixture.prepared.TaskID)
		_, err := PrepareFollowup(context.Background(), fixture.engine, FollowupRequest{CommandID: "exhausted", ExpectedRevision: projectRevision(t, fixture), SourceRunID: fixture.prepared.RunID, Kind: "infrastructure", WallLimitMS: 60000})
		if !errors.Is(err, ErrBudgetExhausted) {
			t.Fatal("budget exhaustion was not explicit", err)
		}
		var attempts, evidence int
		var taskState string
		_ = fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM runs").Scan(&attempts)
		_ = fixture.engine.DB.SQL.QueryRow("SELECT count(*) FROM budget_exhaustions WHERE state='pending_supervisor'").Scan(&evidence)
		_ = fixture.engine.DB.SQL.QueryRow("SELECT state FROM tasks WHERE id=?", fixture.prepared.TaskID).Scan(&taskState)
		if attempts != 1 || evidence != 1 || taskState != "blocked" {
			t.Fatal("exhaustion advanced or lost evidence", attempts, evidence, taskState)
		}
	})
}

func TestRecoveryRequiresIndependentWriterContainment(t *testing.T) {
	fixture := setupFixture(t)
	prepareInterruptedRecovery(t, fixture)
	history := exactHistory(fixture)
	fixture.driver.History = &history
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE runs SET writer_state='unconfirmed' WHERE id=?", fixture.prepared.RunID); err != nil {
		t.Fatal(err)
	}
	eligibility, _, err := (&Runner{Engine: fixture.engine}).RecoveryEligibility(context.Background(), fixture.prepared, fixture.driver)
	if err != nil || eligibility.ExactResume || eligibility.FreshContext {
		t.Fatal("native transport evidence substituted for writer containment", eligibility, err)
	}
}

func TestProvenWaitStillHonorsWallDeadline(t *testing.T) {
	fixture := setupFixtureWithLimits(t, 50, 80)
	runner := Runner{Engine: fixture.engine, Driver: fixture.driver}
	if err := runner.startSegment(context.Background(), fixture.prepared); err != nil {
		t.Fatal(err)
	}
	if err := runner.BeginHumanWait(context.Background(), fixture.prepared, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(fixture.prepared.WallLimitMS)*time.Millisecond)
	defer cancel()
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("wall deadline disappeared during proven wait", ctx.Err())
	}
}
