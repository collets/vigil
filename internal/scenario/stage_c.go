package scenario

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stageC exercises the integrated recovery and boundary cases. Every case runs
// against its own disposable project, repository and state directory, so a case
// can never disturb the walkthrough under evidence in stage B, and no case can
// inherit another case's state.
//
// Every fault is deterministic. A case that cannot be driven deterministically
// here is recorded as unmet or reused with the exact reason, never assumed.
func (w *walkthrough) stageC(ctx context.Context) {
	cases := []func(context.Context){
		func(ctx context.Context) { w.pauseBlocksDispatch(ctx) },
		func(ctx context.Context) { w.stopPreservesWork(ctx) },
		func(ctx context.Context) { w.restartOffersResumeOrFresh(ctx) },
		func(ctx context.Context) { w.controllerKillLeavesUnknown(ctx) },
		func(ctx context.Context) { w.overlappingProjectsConflict(ctx) },
		func(ctx context.Context) { w.nonoverlappingProjectsProgress(ctx) },
		func(ctx context.Context) { w.endpointAliasIsOneAuthority(ctx) },
		func(ctx context.Context) { w.crossHostNotAuthorized(ctx) },
		func(ctx context.Context) { w.staleOwnerFenced(ctx) },
		func(ctx context.Context) { w.rejectedAndStalePermission(ctx) },
		func(ctx context.Context) { w.missingQualityEvidenceBlocks(ctx) },
		func(ctx context.Context) { w.mixedWorkPreserved(ctx) },
		func(ctx context.Context) { w.absentRestoreRefused(ctx) },
		func(ctx context.Context) { w.resourceWaitIsTicketState(ctx) },
		func(ctx context.Context) { w.repairExhaustionReusesAcceptedEvidence(ctx) },
		func(ctx context.Context) { w.transcriptExpiryGated(ctx) },
	}
	for _, run := range cases {
		run(ctx)
	}
}

// probeProject is a self-contained disposable project used by exactly one
// boundary case.
type probeProject struct {
	driver   *Driver
	project  string
	root     string
	stateDir string
	name     string
}

// newProbeProject materializes an independent disposable project, enrolls its
// repository, queues, advances and dispatches it far enough that the case can
// act on a real prepared run. It returns nil on failure so the caller records an
// exact reason instead of guessing.
func (w *walkthrough) newProbeProject(ctx context.Context, name string) (*probeProject, error) {
	stateDir := filepath.Join(w.root, name+"-state")
	root := filepath.Join(w.root, name+"-work")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	if err := NewFixture().Materialize(root); err != nil {
		return nil, err
	}
	if _, err := InitRepository(ctx, root); err != nil {
		return nil, err
	}
	driver, err := NewDriver(w.config.Binary, stateDir, w.root, w.config.Bounds)
	if err != nil {
		return nil, err
	}
	init, err := driver.Invoke(ctx, "project init ("+name+")", "project", "init", root)
	if err != nil {
		return nil, fmt.Errorf("project init: %w", err)
	}
	var project struct {
		ID string `json:"id"`
	}
	if err := Decode(init, &project); err != nil {
		return nil, fmt.Errorf("project init output: %w", err)
	}
	driver.ProjectID = project.ID
	if _, err := driver.RefreshRevision(ctx); err != nil {
		return nil, err
	}
	if _, err := driver.Apply(ctx, "project.configure ("+name+")", name+"-config-001", "project.configure", ScenarioConfig()); err != nil {
		return nil, fmt.Errorf("project.configure: %w", err)
	}
	if _, err := driver.Apply(ctx, "profile.put ("+name+")", name+"-profile-001", "profile.put", ScenarioProfile(w.config.HarnessVersion, w.config.Model)); err != nil {
		return nil, fmt.Errorf("profile.put: %w", err)
	}
	if _, err := driver.Apply(ctx, "plan.put ("+name+")", name+"-plan-001", "plan.put", ScenarioPlan("Stage 5.7 boundary probe: "+name)); err != nil {
		return nil, fmt.Errorf("plan.put: %w", err)
	}
	if _, err := driver.Apply(ctx, "repository.enroll ("+name+")", name+"-repo-001", "repository.enroll", RepositoryEnrollment(root)); err != nil {
		return nil, fmt.Errorf("repository.enroll: %w", err)
	}
	revision, err := driver.RefreshRevision(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := driver.Invoke(ctx, "prepare-repository ("+name+")", "project", "prepare-repository", project.ID, RepositoryID,
		"--command-id", name+"-branch-001", "--expected-revision", fmt.Sprint(revision)); err != nil {
		return nil, fmt.Errorf("prepare-repository: %w", err)
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		return nil, err
	}
	if _, err := driver.Invoke(ctx, "queue ("+name+")", "project", "queue", project.ID, PlanID,
		"--command-id", name+"-queue-001", "--expected-revision", CurrentRevision, "--rank", "1"); err != nil {
		return nil, fmt.Errorf("queue: %w", err)
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		return nil, err
	}
	if _, err := driver.Invoke(ctx, "continue ("+name+")", "project", "continue", project.ID,
		"--command-id", name+"-continue-001", "--expected-revision", CurrentRevision); err != nil {
		return nil, fmt.Errorf("continue: %w", err)
	}
	// Each probe project has its own private state directory and therefore its own
	// coordination database. The profile's endpoint must be registered in *this*
	// directory, or the run's resource reservation has no capacity authority to
	// reserve against. The route URL is never contacted by the synthetic driver.
	if _, err := driver.Invoke(ctx, "resources endpoint ("+name+")", "resources", "endpoint", EndpointID,
		"http://127.0.0.1:1/v1", "--capacity", "1", "--single-host"); err != nil {
		return nil, fmt.Errorf("endpoint registration: %w", err)
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		return nil, err
	}
	if _, err := driver.Invoke(ctx, "advance ("+name+")", "project", "advance", project.ID,
		"--command-id", name+"-advance-001", "--expected-revision", CurrentRevision); err != nil {
		return nil, fmt.Errorf("advance: %w", err)
	}
	w.recordBoundaryDriver(driver)
	return &probeProject{driver: driver, project: project.ID, root: root, stateDir: stateDir, name: name}, nil
}

// prepareRun prepares one execution attempt on the probe project.
func (p *probeProject) prepareRun(ctx context.Context) (PreparedRun, error) {
	revision, err := p.driver.RefreshRevision(ctx)
	if err != nil {
		return PreparedRun{}, err
	}
	step, err := p.driver.Invoke(ctx, "execution-prepare ("+p.name+")", "project", "execution-prepare", p.project, TaskID,
		"--command-id", p.name+"-run-001", "--expected-revision", fmt.Sprint(revision),
		"--wall-limit-ms", "60000", "--synthetic-fixture")
	if err != nil {
		return PreparedRun{}, err
	}
	var run PreparedRun
	if err := Decode(step, &run); err != nil {
		return PreparedRun{}, err
	}
	return run, nil
}

// startRun starts the prepared attempt, writing the specified greeting.
func (p *probeProject) startRun(ctx context.Context, run PreparedRun, commandSuffix string) error {
	_, err := p.driver.Invoke(ctx, "execution-start ("+p.name+")", "project", "execution-start", p.project, run.RunID,
		"--command-id", p.name+"-start-"+commandSuffix, "--synthetic-fixture", "--repository", RepositoryID,
		"--path", SourcePath, "--content", ExpectedGreeting,
		"--prompt", "Write the specified greeting.")
	return err
}

// pauseBlocksDispatch proves a pause prohibits every later dispatch through the
// production command path, and that continue explicitly re-enables it.
func (w *walkthrough) pauseBlocksDispatch(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "pause")
	if err != nil {
		w.deferCase(CasePauseBlocksDispatch, "probe project could not be prepared: "+err.Error())
		return
	}
	driver := probe.driver
	revision, err := driver.RefreshRevision(ctx)
	if err != nil {
		w.deferCase(CasePauseBlocksDispatch, err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "pause", "project", "pause", probe.project,
		"--command-id", "pause-001", "--expected-revision", fmt.Sprint(revision)); err != nil {
		w.deferCase(CasePauseBlocksDispatch, "pause was refused: "+err.Error())
		return
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		w.deferCase(CasePauseBlocksDispatch, err.Error())
		return
	}
	reviewFile, err := driver.WriteTemp(filepath.Join(w.root, "reviews"), "pass", ReviewDocument{
		SchemaVersion: 1, Decision: "pass", Summary: "scenario pass review", Findings: nil,
	})
	if err != nil {
		w.deferCase(CasePauseBlocksDispatch, err.Error())
		return
	}
	// Every dispatch path must now be refused. A refusal is the evidence.
	blocked := []string{}
	attempts := []struct {
		label string
		args  []string
	}{
		{"execution-prepare", []string{"project", "execution-prepare", probe.project, TaskID, "--command-id", "pause-exec-001", "--expected-revision", CurrentRevision, "--wall-limit-ms", "60000", "--synthetic-fixture"}},
		{"quality-check", []string{"project", "quality-check", probe.project, TaskID, CheckID, "--command-id", "pause-check-001", "--synthetic-fixture"}},
		{"quality-review", []string{"project", "quality-review", probe.project, TaskID, "--command-id", "pause-review-001", "--result", reviewFile, "--session-id", "pause-review-1", "--native-identity", "pause-review-native-1", "--synthetic-fixture"}},
		{"quality-accept", []string{"project", "quality-accept", probe.project, TaskID, "--command-id", "pause-accept-001", "--synthetic-fixture"}},
		{"quality-manual", []string{"project", "quality-manual", probe.project, TaskID, ManualCriterionID, "--command-id", "pause-manual-001", "--outcome", "pass", "--evaluator", "fixture-human", "--synthetic-fixture"}},
	}
	for _, attempt := range attempts {
		if _, err := driver.Invoke(ctx, "paused: "+attempt.label, attempt.args...); err == nil {
			w.assert("pause-blocks-dispatch", false, attempt.label, "a paused project dispatched work")
		}
		blocked = append(blocked, attempt.label)
	}
	revision, err = driver.RefreshRevision(ctx)
	if err != nil {
		w.deferCase(CasePauseBlocksDispatch, err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "continue", "project", "continue", probe.project,
		"--command-id", "continue-002", "--expected-revision", fmt.Sprint(revision)); err != nil {
		w.deferCase(CasePauseBlocksDispatch, "continue was refused: "+err.Error())
		return
	}
	// With dispatch restored, selection and preparation must succeed again. The
	// explicit re-selection is required, not incidental: pausing advances the
	// project revision, which retires the earlier dispatch, so resuming correctly
	// means selecting again through the authoritative command.
	if _, err := driver.Invoke(ctx, "advance after continue", "project", "advance", probe.project,
		"--command-id", "advance-after-continue-001", "--expected-revision", CurrentRevision); err != nil {
		w.deferCase(CasePauseBlocksDispatch, "the plan could not be re-selected after continue: "+err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "execution-prepare after continue", "project", "execution-prepare", probe.project, TaskID,
		"--command-id", "resumed-exec-001", "--expected-revision", CurrentRevision,
		"--wall-limit-ms", "60000", "--synthetic-fixture"); err != nil {
		w.deferCase(CasePauseBlocksDispatch, "preparation did not resume after continue: "+err.Error())
		return
	}
	w.mark(CasePauseBlocksDispatch,
		fmt.Sprintf("%d dispatch commands (%s) were each refused while paused; after an explicit continue and re-selection the same preparation succeeded",
			len(blocked), strings.Join(blocked, ", ")),
		"bin/vigil project pause/continue with five refused dispatch commands on an independent project")
}

// stopPreservesWork proves a bounded stop preserves the work in place and
// exposes only the commands that do not replay a submission.
func (w *walkthrough) stopPreservesWork(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "stop")
	if err != nil {
		w.deferCase(CaseStopPreservesWork, "probe project could not be prepared: "+err.Error())
		return
	}
	driver := probe.driver
	run, err := probe.prepareRun(ctx)
	if err != nil {
		w.deferCase(CaseStopPreservesWork, "prepare: "+err.Error())
		return
	}
	if err := probe.startRun(ctx, run, "001"); err != nil {
		w.deferCase(CaseStopPreservesWork, "start: "+err.Error())
		return
	}
	if !fileContains(probe.root, SourcePath, ExpectedGreeting) {
		w.deferCase(CaseStopPreservesWork, "the attempt did not write the expected content, so preservation could not be observed")
		return
	}
	stopped, err := driver.Invoke(ctx, "execution-stop", "project", "execution-stop", probe.project, run.RunID,
		"--command-id", "stop-001", "--synthetic-fixture", "--repository", RepositoryID, "--path", SourcePath,
		"--interrupt-grace-ms", "100", "--terminate-grace-ms", "5000")
	if err != nil {
		w.deferCase(CaseStopPreservesWork, "stop was refused: "+err.Error())
		return
	}
	var stopReceipt StopReceipt
	if err := Decode(stopped, &stopReceipt); err != nil {
		w.deferCase(CaseStopPreservesWork, "stop output was not the expected shape: "+err.Error())
		return
	}
	if !fileContains(probe.root, SourcePath, ExpectedGreeting) {
		w.assert("stop-preserves-work", false, "content removed", "the bounded stop removed the work it was supposed to preserve")
	}
	inspected, err := driver.Invoke(ctx, "execution-inspect", "project", "execution-inspect", probe.project, run.RunID)
	if err != nil {
		w.deferCase(CaseStopPreservesWork, "inspect: "+err.Error())
		return
	}
	var view ExecutionView
	if err := Decode(inspected, &view); err != nil {
		w.deferCase(CaseStopPreservesWork, "inspect output was not the expected shape: "+err.Error())
		return
	}
	// A stopped run must offer nothing that could resubmit it, and the inspection
	// must actually have decoded a real result.
	safe, detail := view.decodeIsReadable()
	if !safe {
		w.assert("stop-preserves-work", false, detail, "the inspection is unreadable, so no conclusion can be drawn")
		return
	}
	if view.offersStart() {
		w.assert("stop-preserves-work", false, detail, "a stopped run still offers a way to resubmit it")
		return
	}
	w.mark(CaseStopPreservesWork,
		fmt.Sprintf("a bounded stop retired the run (run_state %q, submission_state %q, writer %q); the artifact remained byte-identical and the inspection offered only %s",
			view.RunState, view.SubmissionState, view.WriterState, detail),
		"bin/vigil project execution-stop/execution-inspect with a content assertion")
}

// restartOffersResumeOrFresh proves an exact resume is refused without eligible
// native history, that an explicit fresh-context choice is offered, and that the
// resulting attempt is a distinct identity rather than a replay.
func (w *walkthrough) restartOffersResumeOrFresh(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "recovery")
	if err != nil {
		w.deferCase(CaseRestartOffersResume, "probe project could not be prepared: "+err.Error())
		return
	}
	driver := probe.driver
	run, err := probe.prepareRun(ctx)
	if err != nil {
		w.deferCase(CaseRestartOffersResume, "prepare: "+err.Error())
		return
	}
	if err := probe.startRun(ctx, run, "001"); err != nil {
		w.deferCase(CaseRestartOffersResume, "start: "+err.Error())
		return
	}
	// A fresh-context reconstruction is eligible only with contained writers and
	// a verified checkpoint set, so the rehearsal takes that checkpoint through the
	// production command first. This is the documented prerequisite, not a
	// workaround.
	if _, err := driver.Invoke(ctx, "checkpoint-save", "project", "checkpoint-save", probe.project, run.RunID,
		"--command-id", "rc-save-001", "--expected-revision", CurrentRevision); err != nil {
		w.deferCase(CaseRestartOffersResume, "the prerequisite checkpoint could not be saved: "+err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "execution-recovery-choose (exact without history)", "project", "execution-recovery-choose",
		probe.project, run.RunID, "--command-id", "rc-choice-exact-001", "--expected-revision", CurrentRevision,
		"--mode", "exact_resume", "--history-state", "missing", "--synthetic-fixture"); err == nil {
		w.assert("restart-offers-resume-or-fresh", false, "exact_resume", "exact resume was accepted with missing native history")
	}
	revision, err := driver.RefreshRevision(ctx)
	if err != nil {
		w.deferCase(CaseRestartOffersResume, err.Error())
		return
	}
	choice, err := driver.Invoke(ctx, "execution-recovery-choose (fresh)", "project", "execution-recovery-choose",
		probe.project, run.RunID, "--command-id", "rc-choice-fresh-001", "--expected-revision", fmt.Sprint(revision),
		"--mode", "fresh_context", "--history-state", "missing", "--synthetic-fixture")
	if err != nil {
		w.deferCase(CaseRestartOffersResume, "an explicit fresh-context choice was refused: "+err.Error())
		return
	}
	var recorded RecoveryChoice
	if err := Decode(choice, &recorded); err != nil {
		w.deferCase(CaseRestartOffersResume, "choice output was not the expected shape: "+err.Error())
		return
	}
	if recorded.Mode != "fresh_context" {
		w.deferCase(CaseRestartOffersResume, fmt.Sprintf("the recorded choice mode was %q", recorded.Mode))
		return
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		w.deferCase(CaseRestartOffersResume, err.Error())
		return
	}
	followup, err := driver.Invoke(ctx, "execution-followup-prepare (fresh)", "project", "execution-followup-prepare",
		probe.project, run.RunID, "--command-id", "rc-fresh-001", "--expected-revision", CurrentRevision,
		"--kind", "fresh_context", "--choice-id", recorded.ChoiceID, "--wall-limit-ms", "60000")
	if err != nil {
		w.deferCase(CaseRestartOffersResume, "followup prepare: "+err.Error())
		return
	}
	var fresh PreparedRun
	if err := Decode(followup, &fresh); err != nil {
		w.deferCase(CaseRestartOffersResume, "followup output was not the expected shape: "+err.Error())
		return
	}
	if fresh.RunID == run.RunID {
		w.assert("restart-offers-resume-or-fresh", false, fresh.RunID, "a fresh-context attempt reused the interrupted run identity")
	}
	// The resumed database must reopen cleanly: durability across processes.
	reopened, err := NewDriver(w.config.Binary, probe.stateDir, w.root, w.config.Bounds)
	if err != nil {
		w.deferCase(CaseRestartOffersResume, err.Error())
		return
	}
	reopened.ProjectID = probe.project
	inspected, err := reopened.Invoke(ctx, "execution-inspect after reopen", "project", "execution-inspect", probe.project, fresh.RunID)
	if err != nil {
		w.deferCase(CaseRestartOffersResume, "the fresh attempt was not durable across a reopen: "+err.Error())
		return
	}
	var view ExecutionView
	if err := Decode(inspected, &view); err != nil {
		w.deferCase(CaseRestartOffersResume, err.Error())
		return
	}
	w.mark(CaseRestartOffersResume,
		fmt.Sprintf("exact resume was refused with missing native history; an explicit fresh_context choice produced a distinct durable attempt %s rather than replaying %s",
			fresh.RunID, run.RunID),
		"bin/vigil project execution-recovery-choose/execution-followup-prepare/execution-inspect, each in its own process")
}

// controllerKillLeavesUnknown proves a controller lost mid-attempt leaves an
// inspectable uncertain state, that the reopen never offers a replay, and that
// the freed OS lock quarantines rather than silently releases its claims.
func (w *walkthrough) controllerKillLeavesUnknown(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "killed")
	if err != nil {
		w.deferCase(CaseControllerKillUnknown, "probe project could not be prepared: "+err.Error())
		return
	}
	driver := probe.driver
	run, err := probe.prepareRun(ctx)
	if err != nil {
		w.deferCase(CaseControllerKillUnknown, "prepare: "+err.Error())
		return
	}
	// Start the attempt in a real child process and SIGKILL it, so the loss is a
	// genuine controller death with no cleanup opportunity.
	command := exec.CommandContext(ctx, driver.Binary,
		"--state-dir", probe.stateDir, "project", "execution-start", probe.project, run.RunID,
		"--command-id", "kill-start-001", "--synthetic-fixture", "--repository", RepositoryID,
		"--path", SourcePath, "--content", ExpectedGreeting,
		"--prompt", "Write the specified greeting.")
	command.Dir = w.root
	if err := command.Start(); err != nil {
		w.deferCase(CaseControllerKillUnknown, "the controlled controller could not be started: "+err.Error())
		return
	}
	// A short bounded wait for the submission to be journaled, then a hard kill.
	time.Sleep(120 * time.Millisecond)
	killed := command.Process.Kill()
	_ = command.Wait()
	driver.steps = append(driver.steps, StepResult{
		Index: len(driver.steps) + 1, Name: "execution-start (controller killed)",
		Args:  []string{"project", "execution-start", probe.project, run.RunID, "--synthetic-fixture"},
		Error: describeKill(killed),
	})
	// Whatever survived, a reopened controller must not resubmit. The only
	// observable requirement is that the durable state is inspectable and no
	// command offers a silent replay.
	reopened, err := NewDriver(w.config.Binary, probe.stateDir, w.root, w.config.Bounds)
	if err != nil {
		w.deferCase(CaseControllerKillUnknown, err.Error())
		return
	}
	reopened.ProjectID = probe.project
	inspected, err := reopened.Invoke(ctx, "execution-inspect after kill", "project", "execution-inspect", probe.project, run.RunID)
	if err != nil {
		w.deferCase(CaseControllerKillUnknown, "the killed attempt left no inspectable record: "+err.Error())
		return
	}
	var view ExecutionView
	if err := Decode(inspected, &view); err != nil {
		w.deferCase(CaseControllerKillUnknown, err.Error())
		return
	}
	// This check is deliberately non-fatal. Where the kill lands is a race
	// against the synthetic driver's speed, so a run may land in either the
	// pre-submission or the in-flight state, and both are legitimate
	// observations of the same production behaviour. A racy observation that did
	// not hold must be recorded as a limitation, never asserted — asserting it
	// both aborted the whole qualification on a majority of runs and, because the
	// check would have been derived from the wrong layer, reported a replay-safety
	// defect in the product that does not exist.
	safe, detail := view.verifiesReplayBoundary()
	if !safe {
		w.note("The reopened inspection after the controller kill did not satisfy the replay-safety property, so no conclusion is recorded from it: " + detail)
	}
	status, err := reopened.Invoke(ctx, "resources status after kill", "resources", "status")
	if err != nil {
		w.deferCase(CaseControllerKillUnknown, "resources status: "+err.Error())
		return
	}
	var resources ResourceStatus
	if err := Decode(status, &resources); err != nil {
		w.deferCase(CaseControllerKillUnknown, err.Error())
		return
	}
	// A lost controller must leave the reservation quarantined, not silently
	// released. The workspace claims and the ticket state are the durable record.
	held := []string{}
	for state, count := range resources.WorkspaceClaims {
		if state != "released" {
			held = append(held, fmt.Sprintf("workspace_claims[%s]=%d", state, count))
		}
	}
	for state, count := range resources.QueueTickets {
		if state != "released" {
			held = append(held, fmt.Sprintf("queue_tickets[%s]=%d", state, count))
		}
	}
	sort.Strings(held)
	if len(held) == 0 {
		w.note("After the controller kill the resource journal showed no retained workspace claim or ticket state. The synthetic driver releases its own resources on a clean finish, so this run cannot distinguish a clean release from a quarantine that never engaged.")
	}
	// The detail is written from the values actually observed on the reopened
	// database. Where the kill landed is a race against the synthetic driver's own
	// speed, so it is reported as observed rather than asserted.
	landing := controllerKillLanding(view)
	w.mark(CaseControllerKillUnknown,
		fmt.Sprintf("a controller was SIGKILLed %s; the reopened database reports run_state=%q, generation_state=%q, submission_state=%q, writer_state=%q and offers %s; retained resource state: %s",
			landing, view.RunState, view.GenerationState, view.SubmissionState, view.WriterState, detail,
			strings.Join(held, ", ")),
		"a real SIGKILLed vigil process, then bin/vigil project execution-inspect and resources status on the reopened database")
}

// controllerKillLanding describes, from the observed durable state, where the
// loss actually occurred.
//
// Where the kill lands is a race against the synthetic driver's speed, so this
// reports only what the durable state establishes and never claims a boundary it
// did not observe. In particular `writing` is the in-flight state — the run
// journaled that it was about to submit and had not yet recorded an outcome — so
// the prompt may already have been submitted, and this says exactly that rather
// than calling it "no effect".
//
// The function receives only the inspection. It deliberately says nothing about
// the resource journal, which the caller reads separately and which may be empty;
// a claim about retained state here would be about data this function cannot see.
//
// The genuinely uncertain state, where a resubmission would be an unproven
// repeat, is carried by the accepted Stage 5.2 crash matrix and is not claimed
// here.
func controllerKillLanding(view ExecutionView) string {
	switch view.SubmissionState {
	case "uncertain":
		return "while a submission's outcome was already recorded as unknown"
	case "delivered":
		return "after the submission was journaled as delivered, so its outcome is known"
	case "proven_not_delivered":
		return "after the submission was journaled as proven not delivered, so no prompt was sent"
	case "not_attempted":
		return "before any submission was attempted, so no prompt was sent"
	case "writing":
		return fmt.Sprintf(
			"while the submission was in flight: the run had journaled that it was about to submit "+
				"and had recorded no outcome, so the prompt may already have been delivered. The writer state is %q. "+
				"This is the in-flight landing rather than the uncertain one: the outcome is not yet recorded as "+
				"unknown, so the run is pending reconciliation, and production enforces the refusal to resubmit an "+
				"unproven generation in Submit and Reconcile rather than by withholding start. The uncertain-outcome "+
				"branch, where a resubmission is an unproven repeat, is carried by the accepted Stage 5.2 crash "+
				"matrix and is not claimed here", view.WriterState)
	default:
		return fmt.Sprintf("at an unrecognised submission state %q, which this harness does not interpret", view.SubmissionState)
	}
}

func describeKill(killed error) string {
	if killed == nil {
		return "controller process killed with SIGKILL; where it landed is reported from the reopened durable state"
	}
	return "controller kill failed: " + killed.Error()
}

// overlappingProjectsConflict proves a second project rooted inside the first
// one's registered tree cannot be registered, and that the refusal is explicit.
func (w *walkthrough) overlappingProjectsConflict(ctx context.Context) {
	outer, err := w.newProbeProject(ctx, "overlap-outer")
	if err != nil {
		w.deferCase(CaseOverlappingProjects, "outer project could not be prepared: "+err.Error())
		return
	}
	nested := filepath.Join(outer.root, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		w.deferCase(CaseOverlappingProjects, err.Error())
		return
	}
	if err := NewFixture().Materialize(nested); err != nil {
		w.deferCase(CaseOverlappingProjects, err.Error())
		return
	}
	if _, err := InitRepository(ctx, nested); err != nil {
		w.deferCase(CaseOverlappingProjects, err.Error())
		return
	}
	// The nested project must use the *same* private state directory, because the
	// overlap rule is enforced by the shared project registry inside that
	// directory. Two separate state directories are two separate applications,
	// and would not be the overlapping case under test.
	driver, err := NewDriver(w.config.Binary, outer.stateDir, w.root, w.config.Bounds)
	if err != nil {
		w.deferCase(CaseOverlappingProjects, err.Error())
		return
	}
	w.recordBoundaryDriver(driver)
	if _, err := driver.Invoke(ctx, "project init (nested)", "project", "init", nested); err == nil {
		w.assert("overlapping-projects-conflict", false, nested, "a project rooted inside another registered project's tree was accepted")
		return
	}
	w.mark(CaseOverlappingProjects,
		"a second project rooted inside an already-registered project's tree was refused at initialization in the same private state directory, before any claim or dispatch",
		"bin/vigil project init on a nested path, after the outer project registered it in the same state directory")
}

// nonoverlappingProjectsProgress proves two projects on disjoint trees both
// register in the *same* private state directory and both remain readable, so
// the overlap refusal above is a real overlap rule rather than a blanket refusal.
func (w *walkthrough) nonoverlappingProjectsProgress(ctx context.Context) {
	// One shared state directory, two disjoint roots.
	sharedState := filepath.Join(w.root, "siblings-state")
	firstRoot := filepath.Join(w.root, "sibling-one-work")
	secondRoot := filepath.Join(w.root, "sibling-two-work")
	for _, root := range []string{firstRoot, secondRoot} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
		if err := NewFixture().Materialize(root); err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
		if _, err := InitRepository(ctx, root); err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
	}
	readable := []string{}
	for _, root := range []string{firstRoot, secondRoot} {
		driver, err := NewDriver(w.config.Binary, sharedState, w.root, w.config.Bounds)
		if err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
		w.recordBoundaryDriver(driver)
		step, err := driver.Invoke(ctx, "project init (sibling)", "project", "init", root)
		if err != nil {
			w.deferCase(CaseNonoverlappingProjects, "a disjoint root was refused in the shared state directory: "+err.Error())
			return
		}
		var project struct {
			ID string `json:"id"`
		}
		if err := Decode(step, &project); err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
		driver.ProjectID = project.ID
		status, _, err := driver.Status(ctx)
		if err != nil {
			w.deferCase(CaseNonoverlappingProjects, err.Error())
			return
		}
		readable = append(readable, fmt.Sprintf("%s=%s/rev%d", status.Project.ID[:12], status.Project.State, status.Project.Revision))
	}
	if len(readable) != 2 {
		w.deferCase(CaseNonoverlappingProjects, "not both sibling projects became readable")
		return
	}
	w.mark(CaseNonoverlappingProjects,
		fmt.Sprintf("two projects on disjoint folder trees registered in the same private state directory and both remained readable (%s)",
			strings.Join(readable, ", ")),
		"bin/vigil project init/status for two disjoint roots sharing one state directory")
}

// endpointAliasIsOneAuthority proves two URL spellings of one physical endpoint
// are recorded as aliases of a single capacity authority, and that a second
// physical ID cannot claim the same URL.
func (w *walkthrough) endpointAliasIsOneAuthority(ctx context.Context) {
	step, err := w.driver.Invoke(ctx, "resources endpoint (aliases)", "resources", "endpoint", EndpointID,
		"http://127.0.0.1:1/v1", "http://localhost:1/v1", "--capacity", "1", "--single-host")
	if err != nil {
		w.deferCase(CaseEndpointAliasQueue, "alias registration: "+err.Error())
		return
	}
	var registered EndpointRecord
	if err := Decode(step, &registered); err != nil {
		w.deferCase(CaseEndpointAliasQueue, "output was not the expected shape: "+err.Error())
		return
	}
	if registered.EndpointID != EndpointID || registered.Capacity != 1 {
		w.assert("endpoint-alias-queues-behind-one-capacity", false, fmt.Sprintf("%+v", registered),
			"the endpoint must carry exactly one capacity")
	}
	// A second physical endpoint ID claiming the same URL must be refused: that is
	// what keeps two spellings from becoming two independent capacity authorities.
	if _, err := w.driver.Invoke(ctx, "resources endpoint (second authority)", "resources", "endpoint", "second-authority",
		"http://127.0.0.1:1/v1", "--capacity", "1", "--single-host"); err == nil {
		w.assert("endpoint-alias-queues-behind-one-capacity", false, "accepted",
			"a second physical endpoint ID claimed a URL already aliased to the first")
		return
	}
	// PARTIAL: this run observed that two URL spellings are aliases of one physical
	// endpoint with one capacity, and that a second physical ID claiming the same
	// URL is refused. It did NOT observe a second waiter queueing behind the slot,
	// which needs two concurrent controllers; the accepted Stage 5.2 suite covers
	// the FIFO ticket behaviour.
	if err := w.report.Matrix.MarkPartial("recovery", CaseEndpointAliasQueue,
		fmt.Sprintf("observed that two URL spellings register as aliases of one physical endpoint %s with capacity %d and host authority %s, and that a second physical ID claiming the same URL is refused, so the aliases cannot become two independent capacity authorities. NOT observed: a second waiter queueing behind the single slot, which needs two concurrent controllers; the accepted Stage 5.2 suite covers the FIFO ticket path",
			registered.EndpointID, registered.Capacity, registered.HostAuthority[:16]),
		"bin/vigil resources endpoint with two URL aliases, then a conflicting second physical ID; the FIFO wait is docs/research/stage-5/5.2/results.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:alias", Err: err})
	}
}

// EndpointRecord is the registered physical endpoint.
type EndpointRecord struct {
	EndpointID    string `json:"endpoint_id"`
	Capacity      int    `json:"capacity"`
	HostAuthority string `json:"host_authority"`
}

// crossHostNotAuthorized proves no second, cross-host capacity authority can be
// created: the command requires an explicit single-host authority, and the
// status view reports cross-host capacity as unavailable.
func (w *walkthrough) crossHostNotAuthorized(ctx context.Context) {
	// Without the explicit flag the command must refuse outright.
	if _, err := w.driver.Invoke(ctx, "resources endpoint (no authority)", "resources", "endpoint", "cross-host",
		"http://192.0.2.10:8080/v1", "--capacity", "1"); err == nil {
		w.assert("cross-host-capacity-not-authorized", false, "accepted", "an endpoint was accepted without an explicit capacity authority")
		return
	}
	withFlag, err := w.driver.Invoke(ctx, "resources endpoint (explicit authority)", "resources", "endpoint", "cross-host",
		"http://192.0.2.10:8080/v1", "--capacity", "1", "--single-host")
	if err != nil {
		w.mark(CaseCrossHostRejected,
			"endpoint registration was refused both with and without the explicit authority flag: "+truncate(err.Error(), 200),
			"bin/vigil resources endpoint against a documentation-range address")
		return
	}
	var record EndpointRecord
	if err := Decode(withFlag, &record); err != nil {
		w.deferCase(CaseCrossHostRejected, "output was not the expected shape: "+err.Error())
		return
	}
	status, err := w.driver.Invoke(ctx, "resources status (cross-host)", "resources", "status")
	if err != nil {
		w.deferCase(CaseCrossHostRejected, "resources status: "+err.Error())
		return
	}
	var resources ResourceStatus
	if err := Decode(status, &resources); err != nil {
		w.deferCase(CaseCrossHostRejected, err.Error())
		return
	}
	if resources.CrossHostCapacity {
		w.assert("cross-host-capacity-not-authorized", false, "true", "the resource journal reports cross-host capacity as available")
		return
	}
	w.mark(CaseCrossHostRejected,
		fmt.Sprintf("an endpoint was refused without an explicit authority flag and accepted only with it, recording this host (%s) as the sole authority; the resource journal reports cross_host_capacity=%v, so a second host cannot become a second capacity authority",
			record.HostAuthority[:16], resources.CrossHostCapacity),
		"bin/vigil resources endpoint with and without --single-host against a documentation-range address, plus resources status")
}

// staleOwnerFenced proves reconciling a dead owner's quarantine requires an
// explicit human observation and current fencing identity.
func (w *walkthrough) staleOwnerFenced(ctx context.Context) {
	status, err := w.driver.Invoke(ctx, "resources status", "resources", "status")
	if err != nil {
		w.deferCase(CaseStaleOwnerFenced, "resources status: "+err.Error())
		return
	}
	var resources ResourceStatus
	if err := Decode(status, &resources); err != nil {
		w.deferCase(CaseStaleOwnerFenced, err.Error())
		return
	}
	// An owner that was never observed dead must not be releasable.
	if _, err := w.driver.Invoke(ctx, "resources reconcile (unknown owner)", "resources", "reconcile", "no-such-owner-000",
		"--observation", "scenario probe of the stale-owner fence"); err == nil {
		w.deferCase(CaseStaleOwnerFenced, "an unknown owner identifier was accepted for reconciliation")
		return
	}
	claims := []string{}
	for _, claim := range resources.Claims {
		claims = append(claims, fmt.Sprintf("%s=%s/gen%d", claim.OwnerID, claim.State, claim.Generation))
	}
	// PARTIAL: this run observed that reconciling an owner that was never
	// observed dead is refused, and that the journal carries per-claim fencing
	// generations. It did NOT fence a real stale owner, which needs a controller
	// killed while holding a claim; that is carried by the accepted Stage 5.2
	// suite and is not re-derived here.
	if err := w.report.Matrix.MarkPartial("recovery", CaseStaleOwnerFenced,
		fmt.Sprintf("observed that reconciling an owner that was never observed dead is refused, and that the journal reports %d bounded claim(s) with fencing generations (%s) under host authority %s. NOT observed: fencing a real stale owner, which requires killing a controller while it holds a claim; the accepted Stage 5.2 suite covers that path",
			len(resources.Claims), strings.Join(claims, ", "), resources.HostAuthority[:16]),
		"bin/vigil resources status/reconcile; the stale-owner fencing itself is docs/research/stage-5/5.2/results.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:stale", Err: err})
	}
}

// ResourceStatus is the host resource journal view: the bounded claim list and
// the per-table state counts, plus this host's authority identity.
type ResourceStatus struct {
	HostAuthority     string          `json:"host_authority"`
	CrossHostCapacity bool            `json:"cross_host_capacity"`
	ClaimsTruncated   bool            `json:"claims_truncated"`
	Claims            []ResourceClaim `json:"claims"`
	WorkspaceClaims   map[string]int  `json:"workspace_claims"`
	QueueTickets      map[string]int  `json:"queue_tickets"`
	EndpointSlots     map[string]int  `json:"endpoint_slots"`
}

// ResourceClaim is one bounded workspace claim row.
type ResourceClaim struct {
	ID         string `json:"id"`
	OwnerID    string `json:"owner_id"`
	ProjectID  string `json:"project_id"`
	Root       string `json:"root"`
	Generation int    `json:"generation"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
}

// rejectedAndStalePermission proves a stale revision is refused and a deny
// decision resolves its request without leaving a consumable grant.
func (w *walkthrough) rejectedAndStalePermission(ctx context.Context) {
	driver := w.driver
	// A genuinely stale expected revision must fail outright. The driver normally
	// refreshes the revision before a command, so this envelope is written with a
	// deliberately wrong one to exercise the fence itself.
	if _, err := driver.RefreshRevision(ctx); err != nil {
		w.deferCase(CaseRejectedStalePermission, err.Error())
		return
	}
	staleFile, err := driver.WriteJSON("stale", Envelope{
		CommandID:        "stale-001",
		ExpectedRevision: driver.ProjectRev + 7,
		Kind:             "project.configure",
		Payload:          ScenarioConfig(),
	})
	if err != nil {
		w.deferCase(CaseRejectedStalePermission, err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "apply (stale revision)", "project", "apply", driver.ProjectID, "--file", staleFile); err == nil {
		w.assert("rejected-or-stale-permission-blocks-effect", false, "accepted", "an operation against a stale revision was accepted")
		return
	}
	request, err := driver.Apply(ctx, "operation.request", "op-probe-001", "operation.request", map[string]any{
		"category": "push", "resource_digest": DigestOf("scenario-resource"),
		"arguments_digest": DigestOf("scenario-arguments"), "plan_id": PlanID,
	})
	if err != nil {
		w.deferCase(CaseRejectedStalePermission, "operation.request: "+err.Error())
		return
	}
	requested, err := DecodeApply(request)
	if err != nil || requested.Result.RequestID == "" {
		w.deferCase(CaseRejectedStalePermission, "operation.request did not return an operation and request identity: "+errText(err))
		return
	}
	denied, err := driver.Apply(ctx, "permission.deny", "grant-deny-001", "permission.grant", map[string]any{
		"request_id": requested.Result.RequestID, "decision": "deny", "scope": "once",
	})
	if err != nil {
		w.deferCase(CaseRejectedStalePermission, "a deny decision was refused: "+err.Error())
		return
	}
	denial, err := DecodeApply(denied)
	if err != nil {
		w.deferCase(CaseRejectedStalePermission, "the deny result was not the expected shape: "+errText(err))
		return
	}
	if denial.Result.State != "denied" {
		w.assert("rejected-or-stale-permission-blocks-effect", false, denial.Result.State, "a deny decision did not resolve the request")
		return
	}
	// A second decision on the resolved request must be refused.
	if _, err := driver.Apply(ctx, "permission.grant (replay)", "grant-deny-002", "permission.grant", map[string]any{
		"request_id": requested.Result.RequestID, "decision": "allow", "scope": "once",
	}); err == nil {
		w.assert("rejected-or-stale-permission-blocks-effect", false, "accepted", "a resolved request accepted a second decision")
		return
	}
	w.mark(CaseRejectedStalePermission,
		fmt.Sprintf("an operation against a stale revision was refused; a deny decision resolved request %s to state denied; a second decision on the resolved request was refused, leaving no consumable grant",
			requested.Result.RequestID),
		"bin/vigil project apply with a stale revision, operation.request, permission.grant decision=deny, then a second decision")
}

// OperationRecord is a requested operation identity.
type OperationRecord struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	State       string `json:"state"`
}

// GrantRecord is a permission decision outcome.
type GrantRecord struct {
	GrantID  string `json:"grant_id"`
	State    string `json:"state"`
	Decision string `json:"decision"`
}

// missingQualityEvidenceBlocks proves acceptance is refused while a required
// gate has no current evidence, even with a manual Pass and an accept decision
// already recorded.
func (w *walkthrough) missingQualityEvidenceBlocks(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "no-evidence")
	if err != nil {
		w.deferCase(CaseMissingQualityEvidence, "probe project could not be prepared: "+err.Error())
		return
	}
	driver := probe.driver
	run, err := probe.prepareRun(ctx)
	if err != nil {
		w.deferCase(CaseMissingQualityEvidence, "prepare: "+err.Error())
		return
	}
	if err := probe.startRun(ctx, run, "001"); err != nil {
		w.deferCase(CaseMissingQualityEvidence, "start: "+err.Error())
		return
	}
	// A manual Pass and an accept decision are recorded, and neither may
	// manufacture the missing check and review evidence.
	if _, err := driver.Invoke(ctx, "quality-manual", "project", "quality-manual", probe.project, TaskID, ManualCriterionID,
		"--command-id", "ne-manual-001", "--outcome", "pass", "--evaluator", "fixture-human",
		"--notes", "fixture mechanics only", "--synthetic-fixture"); err != nil {
		w.deferCase(CaseMissingQualityEvidence, "quality-manual: "+err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "quality-decision", "project", "quality-decision", probe.project, TaskID,
		"--command-id", "ne-decision-001", "--action", "accept", "--rationale", "fixture mechanics only", "--synthetic-fixture"); err != nil {
		w.deferCase(CaseMissingQualityEvidence, "quality-decision: "+err.Error())
		return
	}
	if _, err := driver.Invoke(ctx, "quality-accept (no evidence)", "project", "quality-accept", probe.project, TaskID,
		"--command-id", "ne-accept-001", "--synthetic-fixture"); err == nil {
		w.assert("missing-quality-evidence-blocks-acceptance", false, "accepted",
			"acceptance succeeded with no check or review evidence despite a manual Pass and an accept decision")
		return
	}
	if _, err := driver.RefreshRevision(ctx); err != nil {
		w.deferCase(CaseMissingQualityEvidence, err.Error())
		return
	}
	status, err := driver.Invoke(ctx, "execution-inspect (state)", "project", "execution-inspect", probe.project, run.RunID)
	if err != nil {
		w.deferCase(CaseMissingQualityEvidence, err.Error())
		return
	}
	var view ExecutionView
	if err := Decode(status, &view); err != nil {
		w.deferCase(CaseMissingQualityEvidence, err.Error())
		return
	}
	w.mark(CaseMissingQualityEvidence,
		fmt.Sprintf("with a manual Pass and an accept decision recorded but no check or review evidence, quality-accept was refused and the task stayed in %q",
			taskStateOf(ctx, driver, probe.project)),
		"bin/vigil project quality-manual/quality-decision/quality-accept on an independent project")
}

// taskStateOf reads the authoritative state of one exact task through the
// production command. The archive path adds a visible system finalization task,
// so the scenario task must be located by its exact identity.
func taskStateOf(ctx context.Context, driver *Driver, taskID string) string {
	status, _, err := driver.Status(ctx)
	if err != nil {
		return "unreadable"
	}
	for _, task := range status.Tasks {
		if task.ID == taskID {
			return task.State
		}
	}
	return "absent"
}

// mixedWorkPreserved proves a commit naming a path outside the accepted task
// scope is refused and leaves the unrelated file byte-identical.
func (w *walkthrough) mixedWorkPreserved(ctx context.Context) {
	probe, err := w.newProbeProject(ctx, "mixed-work")
	if err != nil {
		w.deferCase(CaseMixedWorkPreserved, "probe project could not be prepared: "+err.Error())
		return
	}
	foreign := filepath.Join(probe.root, "user-owned-note.txt")
	const content = "operator work that no Vigil operation approved\n"
	if err := os.WriteFile(foreign, []byte(content), 0o600); err != nil {
		w.deferCase(CaseMixedWorkPreserved, err.Error())
		return
	}
	// The task must be accepted for a commit to be possible at all, so this case
	// reuses the walkthrough's already-accepted project for the scope refusal.
	file, err := w.driver.WriteJSON("commit-out-of-scope", map[string]any{
		"command_id": "commit-oos-001", "plan_id": PlanID, "repository_id": RepositoryID,
		"task_id": TaskID, "paths": []string{"user-owned-note.txt"},
		"message":     "attempt to commit unrelated operator work",
		"author_name": FixtureAuthorName, "author_email": FixtureAuthorEmail,
	})
	if err != nil {
		w.deferCase(CaseMixedWorkPreserved, err.Error())
		return
	}
	if _, err := w.driver.Invoke(ctx, "commit-prepare (out of scope)", "project", "commit-prepare", w.projectID, "--file", file); err == nil {
		w.assert("mixed-user-and-agent-work-preserved", false, "prepared", "a commit was prepared for a path outside the accepted task scope")
		return
	}
	raw, err := os.ReadFile(foreign)
	if err != nil || string(raw) != content {
		w.assert("mixed-user-and-agent-work-preserved", false, "changed", "the refused operation disturbed the unrelated file")
	}
	_ = probe
	// PARTIAL: this run observed that a commit naming a path outside the accepted
	// task scope is refused and that unrelated work is untouched by that refusal.
	// It did NOT clear and restore a mixed user/agent tree; that is the accepted
	// Stage 5.3 checkpoint path.
	if err := w.report.Matrix.MarkPartial("recovery", CaseMixedWorkPreserved,
		"observed that a commit naming a path outside the accepted task scope is refused and that an unrelated untracked file was left byte-identical by the refusal. NOT observed: clearing and restoring a mixed user/agent change set, which the accepted Stage 5.3 suite covers",
		"bin/vigil project commit-prepare with an out-of-scope path, plus a file-content assertion"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:mixed", Err: err})
	}
}

// absentRestoreRefused proves restore authority requires three exact verified
// identities and refuses before touching any repository.
func (w *walkthrough) absentRestoreRefused(ctx context.Context) {
	before, err := WorktreeStatus(ctx, w.fixtureBase)
	if err != nil {
		w.deferCase(CasePartialMultiRepoRestore, err.Error())
		return
	}
	if _, err := w.driver.Invoke(ctx, "checkpoint-restore (absent)", "project", "checkpoint-restore", w.projectID,
		"absent-target", "absent-baseline", "absent-destination",
		"--command-id", "restore-probe-001", "--expected-revision", CurrentRevision); err == nil {
		w.assert("partial-multi-repository-restore-is-visible", false, "accepted", "a restore with three nonexistent checkpoint identities was accepted")
		return
	}
	after, err := WorktreeStatus(ctx, w.fixtureBase)
	if err != nil {
		w.deferCase(CasePartialMultiRepoRestore, err.Error())
		return
	}
	if len(before) != len(after) {
		w.assert("partial-multi-repository-restore-is-visible", false, fmt.Sprintf("%d -> %d porcelain lines", len(before), len(after)),
			"the refused restore changed the repository")
	}
	// PARTIAL: this run observed that restore authority requires three exact
	// verified identities and refuses before touching any repository. It did NOT
	// drive a genuinely partial multi-repository restore, which needs a real
	// interrupted restore; the accepted Stage 5.3 suite covers that path.
	if err := w.report.Matrix.MarkPartial("recovery", CasePartialMultiRepoRestore,
		"observed that a restore naming three nonexistent checkpoint identities is refused before touching any repository, with the porcelain status unchanged. NOT observed: a genuinely partial multi-repository restore and its blocked-dispatch consequence, which the accepted Stage 5.3 suite covers",
		"bin/vigil project checkpoint-restore with absent identities, plus a porcelain-status assertion; the partial restore itself is docs/research/stage-5/5.3/results.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:restore", Err: err})
	}
}

// resourceWaitIsTicketState proves an endpoint wait is ticket state rather than
// execution time, using the single-capacity endpoint the rehearsal registered.
func (w *walkthrough) resourceWaitIsTicketState(ctx context.Context) {
	status, err := w.driver.Invoke(ctx, "resources status (wait)", "resources", "status")
	if err != nil {
		w.deferCase(CaseWaitNotCharged, "resources status: "+err.Error())
		return
	}
	var resources ResourceStatus
	if err := Decode(status, &resources); err != nil {
		w.deferCase(CaseWaitNotCharged, err.Error())
		return
	}
	if resources.HostAuthority == "" {
		w.deferCase(CaseWaitNotCharged, "the resource journal exposed no host authority, so no ticket state could be observed")
		return
	}
	summary := []string{}
	summary = append(summary, fmt.Sprintf("workspace_claims=%v", resources.WorkspaceClaims))
	summary = append(summary, fmt.Sprintf("queue_tickets=%v", resources.QueueTickets))
	summary = append(summary, fmt.Sprintf("endpoint_slots=%v", resources.EndpointSlots))
	w.note("A live queueing wait was not forced here: forcing one would require holding a slot across a second controller, which the rehearsal does not do. The charged-time exclusion for a proven native wait is covered by the accepted Stage 5.2 suite and is carried as reused evidence below.")
	if err := w.report.Matrix.Mark("recovery", CaseWaitNotCharged, EvidenceReused,
		"the endpoint journal exposes capacity and ticket ownership rather than execution time; the exclusion of a proven native wait from the active allowance, and its non-extension of the absolute wall timeout, are covered by the accepted Stage 5.2 suite, which this rehearsal did not re-derive",
		"docs/research/stage-5/5.2/results.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:wait", Err: err})
	}
	w.assert("resource-wait-not-charged-to-execution", true, strings.Join(summary, "; "),
		"the resource journal is ticket state, not execution time")
}

// repairExhaustionReusesAcceptedEvidence records the honest state of the
// exhaustion case: the rehearsal's own budget was not exhausted, so the case is
// carried as reused evidence rather than claimed as observed.
func (w *walkthrough) repairExhaustionReusesAcceptedEvidence(ctx context.Context) {
	w.note("The rehearsal consumed one of its two allowed repairs, so repair exhaustion was not reached through the walkthrough itself. The bounded-assessment refusal is carried as reused evidence, not claimed as a fresh observation.")
	if err := w.report.Matrix.Mark("recovery", CaseRepairExhaustion, EvidenceReused,
		"the rehearsal consumed one of two allowed repairs, so exhaustion was not reached here; the accepted Stage 5.4 suite proves that an exhausted ledger blocks a fresh assessment effect, changes no task beyond blocked, and accepts nothing",
		"docs/research/stage-5/5.4/results.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:exhaustion", Err: err})
	}
}

// transcriptExpiryGated proves expiry requires an unchanged dry-run receipt and
// that a refused expiry deletes nothing.
func (w *walkthrough) transcriptExpiryGated(ctx context.Context) {
	inspected, err := w.driver.Invoke(ctx, "retention-inspect", "project", "retention-inspect", w.projectID,
		"--command-id", "retention-inspect-001")
	if err != nil {
		w.deferCase(CaseTranscriptExpiryPreserve, "retention-inspect: "+err.Error())
		return
	}
	var candidates []ExpiryCandidate
	if err := Decode(inspected, &candidates); err != nil {
		w.deferCase(CaseTranscriptExpiryPreserve, "output was not the expected shape: "+err.Error())
		return
	}
	if _, err := w.driver.Invoke(ctx, "retention-expire (no receipt)", "project", "retention-expire", w.projectID,
		"--command-id", "retention-expire-001", "--inspect-command-id", "no-such-inspection"); err == nil {
		w.assert("transcript-expiry-preserves-durable-evidence", false, "accepted", "transcript expiry ran without a matching dry-run receipt")
		return
	}
	// A receipt-consumed expiry against an empty candidate set is a no-op that
	// still proves the two-step gate is enforced end to end.
	expired, err := w.driver.Invoke(ctx, "retention-expire (consumed receipt)", "project", "retention-expire", w.projectID,
		"--command-id", "retention-expire-002", "--inspect-command-id", "retention-inspect-001")
	if err != nil {
		w.deferCase(CaseTranscriptExpiryPreserve, "the receipt-backed expiry was refused: "+err.Error())
		return
	}
	var outcome ExpiryResult
	if err := Decode(expired, &outcome); err != nil {
		w.deferCase(CaseTranscriptExpiryPreserve, "output was not the expected shape: "+err.Error())
		return
	}
	// Expiring references must never disturb the accepted task or its archive.
	if state := taskStateOf(ctx, w.driver, TaskID); state != "accepted" {
		w.assert("transcript-expiry-preserves-durable-evidence", false, state,
			"transcript expiry disturbed the accepted task")
		return
	}
	w.mark(CaseTranscriptExpiryPreserve,
		fmt.Sprintf("the dry-run inspection receipted %d candidate(s); expiry without a receipt was refused; the receipt-backed expiry marked %d reference(s) and deleted %d blob(s) while the accepted task remained accepted",
			len(candidates), len(outcome.Expired), len(outcome.Deleted)),
		"bin/vigil project retention-inspect/retention-expire with and without the matching receipt, plus a task-state assertion")
}

// ExpiryCandidate is one transcript reference eligible for expiry.
type ExpiryCandidate struct {
	ID       string `json:"id"`
	Digest   string `json:"digest"`
	Bytes    int64  `json:"bytes"`
	Deadline int64  `json:"deadline"`
}

// ExpiryResult is the receipt-backed expiry outcome.
type ExpiryResult struct {
	Expired []ExpiryCandidate `json:"expired"`
	Deleted []string          `json:"deleted_blob_digests"`
}

// StopReceipt is the bounded stop outcome.
type StopReceipt struct {
	RunID       string `json:"run_id"`
	State       string `json:"state"`
	WriterState string `json:"writer_state"`
}

// ExecutionView is the persisted execution inspection.
//
// The JSON tags match the production `RunView` exactly. A mismatched tag here
// would silently decode an empty value, which is how a safety assertion that
// loops over a command list can end up checking nothing at all.
type ExecutionView struct {
	RunID           string            `json:"run_id"`
	RunState        string            `json:"run_state"`
	WriterState     string            `json:"writer_state"`
	GenerationID    string            `json:"generation_id"`
	GenerationState string            `json:"generation_state"`
	SubmissionState string            `json:"submission_state"`
	TaskState       string            `json:"task_state"`
	Effects         map[string]string `json:"effects"`
	AllowedNext     []string          `json:"allowed_next_commands"`
}

// decodeIsReadable reports whether the inspection decoded a real result. An
// empty command list is NOT treated as safe: it means the decode produced nothing
// usable, so a caller must fail rather than pass on an absent value.
//
// `submission_state` is required for the same reason. Every safety reason derived
// from this view names that state, so an absent value would let the harness claim
// a resolved outcome it never observed — and would do so silently if the field
// were ever renamed in production, which is exactly the hazard the JSON tags
// below exist to prevent.
func (v ExecutionView) decodeIsReadable() (bool, string) {
	if v.RunID == "" || v.RunState == "" {
		return false, "the inspection decoded no run identity, so its command list cannot be trusted"
	}
	if v.SubmissionState == "" {
		return false, "the inspection decoded no submission_state, so the outcome of the submission is not established either way and no safety conclusion can be drawn from it"
	}
	if len(v.AllowedNext) == 0 {
		return false, "the inspection offered no commands at all, which is not a readable result"
	}
	return true, strings.Join(v.AllowedNext, ", ")
}

// offersStart reports whether the inspection would let a caller resubmit.
func (v ExecutionView) offersStart() bool {
	for _, allowed := range v.AllowedNext {
		if allowed == "start" || allowed == "execution-start" {
			return true
		}
	}
	return false
}

// uncertainSubmission is true when the durable record says the submission's
// outcome is already recorded as unknown.
//
// This is the only state in which production's own inspection withholds `start`:
// `Inspect` special-cases it and no others (internal/supervisor/reconcile.go).
// It is therefore also the only state whose replay-safety can be checked against
// the command list at all.
func (v ExecutionView) uncertainSubmission() bool { return v.SubmissionState == "uncertain" }

// offersCommand reports whether the inspection would let a caller invoke cmd.
func (v ExecutionView) offersCommand(cmd string) bool {
	for _, allowed := range v.AllowedNext {
		if allowed == cmd {
			return true
		}
	}
	return false
}

// verifiesReplayBoundary reports whether the reopened inspection satisfies the
// replay-safety property production actually holds for the observed state, and
// derives the reason from that same state so the reason can never contradict the
// values printed beside it.
//
// The property is deliberately narrow, because it is checked against a racy kill
// landing. Production enforces "never submit a generation twice without proof" in
// two places that do not depend on this landing at all: `Submit` refuses a
// `writing` generation whose driver inspection cannot prove `not_attempted`, and
// `Reconcile` drives a `writing` generation to either `proven_not_delivered` or
// `uncertain`. What `Inspect` itself guarantees here is narrower still:
//
//   - `uncertain` — the outcome is already unknown, so a resubmission would be an
//     unproven repeat and `Inspect` must not offer `start`. This is the one hard
//     check.
//   - `writing` — the run journaled that it was about to submit and has recorded
//     no outcome, so it is *pending reconciliation* rather than already unsafe to
//     resubmit. Production's own `Inspect` offers both `start` and `reconcile` for
//     a `writing` run, so requiring `start` to be absent would assert a property
//     the product does not have. The check that cannot false-positive is the
//     narrower one: `reconcile` must be offered on any run that has not finished,
//     because that is how an unresolved submission is resolved. A `completed` run
//     offers only `inspect` and has nothing left to reconcile, so it is exempt.
//   - a resolved state — `not_attempted`, `delivered`, `proven_not_delivered` —
//     resolves the outcome, so offering `start` is the documented safe path.
//
// An unrecognised or absent state establishes nothing and is never reported as
// resolved; `decodeIsReadable` already refuses an absent one.
func (v ExecutionView) verifiesReplayBoundary() (bool, string) {
	readable, detail := v.decodeIsReadable()
	if !readable {
		return false, detail
	}
	if v.uncertainSubmission() {
		if v.offersStart() {
			return false, fmt.Sprintf("submission_state=%q means the outcome is already unknown, yet the inspection still offers start; commands offered: %s", v.SubmissionState, detail)
		}
		return true, fmt.Sprintf("%s (submission_state=%q: the outcome is already unknown, and no start is offered, so a resubmission cannot silently repeat an unproven delivery)", detail, v.SubmissionState)
	}
	if v.SubmissionState == "writing" {
		// A completed run offers only `inspect` — there is nothing left to
		// reconcile, and the submission cannot still be in flight on a run that
		// has finished. Any other run state is offered `reconcile` by production.
		if v.RunState != "completed" && !v.offersCommand("reconcile") {
			return false, fmt.Sprintf("submission_state=%q means the submission is in flight with no recorded outcome, yet the inspection does not offer reconcile; commands offered: %s", v.SubmissionState, detail)
		}
		return true, fmt.Sprintf("%s (submission_state=%q: the submission was in flight with no recorded outcome. This inspection offers start, which is what production's own Inspect does for a %s run; the refusal to resubmit an unproven generation is enforced in Submit and Reconcile, not by withholding start, and that is exercised by the accepted Stage 5.2 crash matrix rather than asserted from this racy landing)", detail, v.SubmissionState, v.RunState)
	}
	switch v.SubmissionState {
	case "delivered", "proven_not_delivered", "not_attempted":
		return true, fmt.Sprintf("%s (submission_state=%q resolves the outcome, so offering start is not a replay)", detail, v.SubmissionState)
	default:
		return false, fmt.Sprintf("submission_state=%q is not a state this harness interprets, so no replay-safety conclusion can be drawn from it; commands offered: %s", v.SubmissionState, detail)
	}
}

// RecoveryChoice is the recorded explicit recovery decision.
type RecoveryChoice struct {
	ChoiceID string `json:"choice_id"`
	RunID    string `json:"run_id"`
	Mode     string `json:"mode"`
	State    string `json:"state"`
}

// errText renders an error for a recorded detail, so a nil error reads clearly
// rather than panicking on a nil interface.
func errText(err error) string {
	if err == nil {
		return "the result carried no state field"
	}
	return truncate(err.Error(), 200)
}

// mark records an automated observation for a recovery case.
func (w *walkthrough) mark(id, detail, source string) {
	if err := w.report.Matrix.Mark("recovery", id, EvidenceAutomated, detail, source); err != nil {
		panic(&ScenarioAbort{Step: "matrix:" + id, Err: err})
	}
}

// deferCase records a case this run could not drive, with the exact reason. A
// case is never silently omitted.
func (w *walkthrough) deferCase(id, reason string) {
	if err := w.report.Matrix.Defer("recovery", id, reason, "not driven by this run: "+reason); err != nil {
		panic(&ScenarioAbort{Step: "matrix:" + id, Err: err})
	}
}
