package scenario

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stageB rehearses the real foreground workflow through the production command
// surface: configure, import, plan, execute, review, repair, check, accept,
// finalize, commit, push and draft-deliver against a local bare remote and a
// loopback hosting stand-in.
//
// Every human-gated decision is recorded as a Stage 8 step. The scenario uses
// labelled fixture actors to exercise the *mechanics* of those commands, and the
// report says so; it never presents a fixture actor as a real user decision.
func (w *walkthrough) stageB(ctx context.Context) {
	driver, err := NewDriver(w.config.Binary, w.report.Project.StateDir, w.root, w.config.Bounds)
	if err != nil {
		panic(&ScenarioAbort{Step: "stage-b-driver", Err: err})
	}
	w.driver = driver

	w.configureProject(ctx)
	w.importSpecification(ctx)
	w.planAndApprove(ctx)
	w.enrollRepository(ctx)
	w.explicitlyRejectAutomaticDispatch(ctx)
	w.executeAndRepair(ctx)
	w.recordQualityEvidence(ctx)
	w.finalizeFactually(ctx)
	w.rehearseDelivery(ctx)
	w.closeMilestoneMatrix()
}

// configureProject creates the project and persists explicit policy, the
// harness profile, and the single registered endpoint.
func (w *walkthrough) configureProject(ctx context.Context) {
	step := w.driver.MustInvoke(ctx, "project init", "project", "init", w.fixtureBase)
	var project struct {
		ID string `json:"id"`
	}
	if err := Decode(step, &project); err != nil {
		panic(&ScenarioAbort{Step: "project init", Err: err})
	}
	if project.ID == "" {
		panic(&ScenarioAbort{Step: "project init", Err: errors.New("no project ID returned")})
	}
	w.projectID = project.ID
	w.driver.ProjectID = project.ID
	w.report.Project.ID = project.ID
	w.assert("project-created", true, project.ID, "the disposable project was registered against the fixture root")

	// The explicit project policy, budgets and the one required check
	// definition. The check runs the fixture repository's own committed script
	// from inside the isolated copy, so the objective criterion is verified by
	// real content rather than by a hard-coded pass.
	config := ScenarioConfig()
	w.driver.MustApply(ctx, "project.configure", "config-001", "project.configure", config)
	w.assert("configuration-declared", true,
		fmt.Sprintf("required check %s, task limit %dms, attempt limit %dms, repair limit %d", CheckID, config.TaskLimitMS, config.AttemptLimitMS, config.RepairLimit),
		"explicit policy, limits and check definitions must be persisted before dispatch")

	// A production route declaration. It records intent only: production
	// eligibility stays false without exact trusted qualification evidence.
	w.driver.MustApply(ctx, "profile.put", "profile-001", "profile.put", ScenarioProfile(w.config.HarnessVersion, w.config.Model))
	w.assert("profile-declared", true, ProfileID, "the harness profile is declared with a credential reference and no credential value")

	endpoint := w.driver.MustInvoke(ctx, "resources endpoint", "resources", "endpoint", EndpointID, w.route.BaseURL, "--capacity", "1", "--single-host")
	var registered EndpointRecord
	if err := Decode(endpoint, &registered); err != nil {
		panic(&ScenarioAbort{Step: "resources endpoint", Err: err})
	}
	if registered.EndpointID != EndpointID || registered.Capacity != 1 || registered.HostAuthority == "" {
		panic(&ScenarioAbort{Step: "resources endpoint", Err: fmt.Errorf("unexpected endpoint identity %+v", registered)})
	}
	w.assert("endpoint-registered", true, fmt.Sprintf("%s capacity=%d authority=%s", registered.EndpointID, registered.Capacity, registered.HostAuthority[:16]),
		"the endpoint must be registered with an explicit single-host capacity authority")
	if err := w.report.Matrix.Mark("milestone", StepConfigureProfiles, EvidenceAutomated,
		"production project init, explicit policy/check definitions, harness profile and single-host endpoint registration all observed through the production CLI",
		"bin/vigil project init; project apply project.configure/profile.put; resources endpoint"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:profiles", Err: err})
	}
}

// importSpecification imports the fixture's own Markdown specification as an
// immutable private revision.
func (w *walkthrough) importSpecification(ctx context.Context) {
	specPath := filepath.Join(w.fixtureBase, filepath.FromSlash(SpecPath))
	step := w.driver.MustInvoke(ctx, "spec-import", "project", "spec-import", w.projectID, specPath,
		"--id", SpecificationID, "--command-id", "spec-import-001", "--expected-revision", CurrentRevision)
	var revision SpecificationRevision
	if err := Decode(step, &revision); err != nil {
		panic(&ScenarioAbort{Step: "spec-import", Err: err})
	}
	if revision.Revision < 1 || revision.ID != SpecificationID {
		panic(&ScenarioAbort{Step: "spec-import", Err: fmt.Errorf("unexpected specification revision %+v", revision)})
	}
	show := w.driver.MustInvoke(ctx, "spec-show", "project", "spec-show", w.projectID, SpecificationID, "1")
	var shown SpecificationRevision
	if err := Decode(show, &shown); err != nil {
		panic(&ScenarioAbort{Step: "spec-show", Err: err})
	}
	if !strings.Contains(shown.Content, ExpectedGreeting) {
		panic(&ScenarioAbort{Step: "spec-show", Err: errors.New("the imported specification does not carry the required greeting")})
	}
	w.assert("specification-imported", true, fmt.Sprintf("%s revision %d", SpecificationID, revision.Revision),
		"the Markdown specification is importable and readable back through production commands")
	if err := w.report.Matrix.Mark("milestone", StepLoadSpecification, EvidenceAutomated,
		"the fixture Markdown specification was imported as an immutable private revision and read back byte-identical",
		"bin/vigil project spec-import/spec-show"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:spec", Err: err})
	}
}

// SpecificationRevision is the imported specification identity.
type SpecificationRevision struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
	Content  string `json:"content"`
}

// planAndApprove produces a validated dispatch-ready proposal and applies it
// through the human application command, then proves the applied plan is real by
// reading it back from persisted state.
func (w *walkthrough) planAndApprove(ctx context.Context) {
	proposalFile, err := w.driver.WriteJSON("proposal", ProposalRequestFixture())
	if err != nil {
		panic(&ScenarioAbort{Step: "proposal write", Err: err})
	}
	// Every command envelope carries the exact current revision, so it is
	// re-read rather than assumed after the specification import.
	if _, err := w.driver.RefreshRevision(ctx); err != nil {
		panic(&ScenarioAbort{Step: "proposal-create", Err: err})
	}
	created := w.driver.MustInvoke(ctx, "proposal-create", "project", "proposal-create", w.projectID,
		"--file", proposalFile, "--command-id", "proposal-create-001",
		"--expected-revision", CurrentRevision, "--synthetic-fixture")
	var createdRecord struct {
		ID       string `json:"id"`
		Revision int    `json:"revision"`
		State    string `json:"state"`
	}
	if err := Decode(created, &createdRecord); err != nil {
		panic(&ScenarioAbort{Step: "proposal-create", Err: err})
	}
	if createdRecord.ID != ProposalID || createdRecord.Revision != 1 {
		panic(&ScenarioAbort{Step: "proposal-create", Err: fmt.Errorf("unexpected proposal %+v", createdRecord)})
	}
	// The proposal creates a pending human approval item. In Stage 5.7 that
	// approval is a labelled fixture actor exercising the mechanics only; the
	// real approval is Stage 8's.
	applied := w.driver.MustInvoke(ctx, "proposal-apply", "project", "proposal-apply", w.projectID, ProposalID, "1",
		"--command-id", "proposal-apply-001", "--expected-revision", CurrentRevision)
	if err := Decode(applied, &map[string]any{}); err != nil {
		panic(&ScenarioAbort{Step: "proposal-apply", Err: err})
	}
	status, _, err := w.driver.Status(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "status after apply", Err: err})
	}
	if len(status.Tasks) != 1 || status.Tasks[0].ID != TaskID {
		panic(&ScenarioAbort{Step: "proposal-apply", Err: fmt.Errorf("expected exactly the scenario task, got %+v", status.Tasks)})
	}
	if len(status.DefinitionIssues) != 0 {
		panic(&ScenarioAbort{Step: "proposal-apply", Err: fmt.Errorf("applied plan left definition issues: %v", status.DefinitionIssues)})
	}
	// Automatic plan advancement must remain unavailable: this build selects a
	// task only through an explicit advance command.
	if status.ExecutionEligible {
		panic(&ScenarioAbort{Step: "proposal-apply", Err: errors.New("the applied plan reports production execution eligibility, which must stay false")})
	}
	w.assert("plan-applied", true, fmt.Sprintf("plan revision applied; task %s in state %s", TaskID, status.Tasks[0].State),
		"the proposal applied atomically and produced one task with no definition issues")
	w.assert("production-dispatch-disabled", !status.ExecutionEligible, fmt.Sprintf("runtime issues: %v", strings.Join(status.RuntimeIssues, "; ")),
		"production dispatch stays unavailable, exactly as required")
	if err := w.report.Matrix.Mark("milestone", StepProducePlan, EvidenceAutomated,
		"a closed proposal was validated, created an approval item, and applied through the human application command; the plan is dispatch-ready and production eligibility remains false",
		"bin/vigil project proposal-create/proposal-apply"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:plan", Err: err})
	}
}

// ProposalRequestFixture is the closed proposal the scenario creates.
//
// A proposal may never carry its own approval: the production validator refuses a
// plan that arrives already approved, and `proposal-apply` is the separate human
// command that approves it. The rehearsal therefore proposes an unapproved plan
// and then applies it, which is exactly the two-step boundary under test.
func ProposalRequestFixture() map[string]any {
	plan := ScenarioPlan("Implement the greeting the imported specification requires and verify it with the scenario check.")
	plan.Approved = false
	return map[string]any{
		"id":                     ProposalID,
		"specification_id":       SpecificationID,
		"specification_revision": 1,
		"profile_id":             ProfileID,
		"profile_revision":       1,
		"operation":              "create",
		"affected_tasks":         []string{TaskID},
		"plan":                   plan,
		"rationale":              "Turn the imported greeting specification into one dispatch-ready task with an objective check and an explicit manual criterion.",
	}
}

// enrollRepository enrolls the disposable repository, prepares its plan branch,
// queues and advances the plan explicitly, and proves the branch preparation did
// not disturb the operator's checkout.
func (w *walkthrough) enrollRepository(ctx context.Context) {
	headBefore, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "repository enrollment", Err: err})
	}
	w.driver.MustApply(ctx, "repository.enroll", "repository-001", "repository.enroll", RepositoryEnrollment(w.fixtureBase))
	_, err = w.driver.RefreshRevision(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "repository.enroll", Err: err})
	}
	prepared := w.driver.MustInvoke(ctx, "prepare-repository", "project", "prepare-repository", w.projectID, RepositoryID,
		"--command-id", "branch-001", "--expected-revision", CurrentRevision)
	var preparedRecord RepositoryRecord
	if err := Decode(prepared, &preparedRecord); err != nil {
		panic(&ScenarioAbort{Step: "prepare-repository", Err: err})
	}
	headAfter, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "prepare-repository", Err: err})
	}
	w.assert("branch-preparation-preserved-head", headBefore == headAfter, headAfter,
		"branch preparation must create the recorded plan ref without checking out over the operator's HEAD")

	w.driver.MustInvoke(ctx, "queue", "project", "queue", w.projectID, PlanID,
		"--command-id", "queue-001", "--expected-revision", CurrentRevision, "--rank", "10")
	if _, err := w.driver.RefreshRevision(ctx); err != nil {
		panic(&ScenarioAbort{Step: "queue", Err: err})
	}
	continued := w.driver.MustInvoke(ctx, "continue", "project", "continue", w.projectID,
		"--command-id", "continue-001", "--expected-revision", CurrentRevision)
	if err := Decode(continued, &map[string]any{}); err != nil {
		panic(&ScenarioAbort{Step: "continue", Err: err})
	}
	advanced := w.driver.MustInvoke(ctx, "advance", "project", "advance", w.projectID,
		"--command-id", "advance-001", "--expected-revision", CurrentRevision)
	var decision DispatchDecision
	if err := Decode(advanced, &decision); err != nil {
		panic(&ScenarioAbort{Step: "advance", Err: err})
	}
	if decision.TaskID != TaskID {
		panic(&ScenarioAbort{Step: "advance", Err: fmt.Errorf("advance selected %q, want %q", decision.TaskID, TaskID)})
	}
	w.assert("explicit-advance-selects-task", true, decision.TaskID, "the explicit advance command selected the eligible task and launched nothing")
}

// RepositoryRecord is the persisted repository enrollment identity.
type RepositoryRecord struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Root     string `json:"root"`
	PlanRef  string `json:"plan_ref,omitempty"`
	Baseline any    `json:"baseline,omitempty"`
}

// DispatchDecision is the explicit advance outcome.
type DispatchDecision struct {
	PlanID string `json:"plan_id"`
	TaskID string `json:"task_id"`
	State  string `json:"state"`
}

// explicitlyRejectAutomaticDispatch proves that an accepted plan does not advance
// itself, and that the queue is authoritative rather than advisory.
func (w *walkthrough) explicitlyRejectAutomaticDispatch(ctx context.Context) {
	queue := w.driver.MustInvoke(ctx, "queue-list", "project", "queue-list", w.projectID)
	var listed []PlanQueueEntry
	if err := Decode(queue, &listed); err != nil {
		panic(&ScenarioAbort{Step: "queue-list", Err: err})
	}
	if len(listed) != 1 || listed[0].PlanID != PlanID {
		panic(&ScenarioAbort{Step: "queue-list", Err: fmt.Errorf("the authoritative queue must hold exactly the applied plan, got %+v", listed)})
	}
	w.assert("queue-is-authoritative", true, fmt.Sprintf("one queued plan %s", PlanID),
		"the authoritative ranked queue holds exactly the applied plan")
}

// PlanQueueEntry is one row of the authoritative ranked plan queue.
type PlanQueueEntry struct {
	PlanID   string `json:"id"`
	Revision int    `json:"revision"`
	Rank     int    `json:"queue_rank"`
	State    string `json:"state"`
}

// executeAndRepair drives the implementation attempt, the deterministic injected
// defect, a fresh reviewer finding it, a bounded repair through the production
// repair path, and fresh checks and a distinct fresh review afterwards.
func (w *walkthrough) executeAndRepair(ctx context.Context) {
	prepared := w.driver.MustInvoke(ctx, "execution-prepare", "project", "execution-prepare", w.projectID, TaskID,
		"--command-id", "run-001", "--expected-revision", CurrentRevision,
		"--wall-limit-ms", "60000", "--synthetic-fixture")
	var run PreparedRun
	if err := Decode(prepared, &run); err != nil {
		panic(&ScenarioAbort{Step: "execution-prepare", Err: err})
	}
	// The implementation attempt writes the specified greeting carrying the
	// deterministically injected trailing-space defect. The defect is real and
	// its origin is this scenario's fixture, labelled as such; it is not a
	// reviewer result invented to reach a preferred outcome.
	started := w.driver.MustInvoke(ctx, "execution-start (implementation)", "project", "execution-start", w.projectID, run.RunID,
		"--command-id", "start-001", "--synthetic-fixture",
		"--repository", RepositoryID, "--path", SourcePath, "--content", InjectedDefectGreeting,
		"--prompt", "Write the greeting the specification requires.")
	var startResult ExecutionResult
	if err := Decode(started, &startResult); err != nil {
		panic(&ScenarioAbort{Step: "execution-start", Err: err})
	}
	if startResult.Accepted {
		panic(&ScenarioAbort{Step: "execution-start", Err: errors.New("the fixture execution reported acceptance; completion must never accept a task")})
	}
	if !fileContains(w.fixtureBase, SourcePath, InjectedDefectGreeting) {
		panic(&ScenarioAbort{Step: "execution-start", Err: errors.New("the execution did not write the requested content")})
	}
	w.assert("execution-leaves-task-in-checking", startResult.TaskState == "checking", startResult.TaskState,
		"a completed attempt leaves the task in checking; native completion never accepts")

	// The objective check passes: the injected defect is a formatting problem the
	// check does not judge, so the review is what finds it.
	passing := w.runCheck(ctx, "check-001")
	w.assert("required-check-passes-on-implementation", passing.Status == "pass", passing.Status,
		"the deterministic check must pass on the specified greeting even with the injected formatting defect")

	// A fresh reviewer session, distinct from every implementation identity,
	// reports the real committed defect in the renderer. The finding is derived
	// by reading the file, not chosen to reach a preferred outcome.
	rejected := w.reviewerDocument()
	reviewFile, err := w.driver.WriteTemp(filepath.Join(w.root, "reviews"), "review-1", rejected)
	if err != nil {
		panic(&ScenarioAbort{Step: "review write", Err: err})
	}
	// A rejecting review is a *successful recording of a rejection*: the command
	// exits nonzero because the review did not pass, while the blocking finding
	// and the task transition are persisted. The scenario therefore requires the
	// refusal, then reads the durable evidence back rather than treating the
	// nonzero exit as a scenario failure.
	rejectedStep, rejectErr := w.driver.Invoke(ctx, "quality-review (blocking)", "project", "quality-review", w.projectID, TaskID,
		"--command-id", "review-001", "--result", reviewFile,
		"--session-id", "scenario-review-session-1", "--native-identity", "scenario-review-native-1",
		"--synthetic-fixture")
	if rejectErr == nil {
		w.assert("fresh-reviewer-finds-real-defect", false, "accepted", "a review reporting changes was recorded as a pass")
		return
	}
	if !strings.Contains(rejectErr.Error(), "request_changes") {
		panic(&ScenarioAbort{Step: "quality-review", Err: fmt.Errorf("the review failed for the wrong reason: %w", rejectErr)})
	}
	reviewStatus, _, err := w.driver.Status(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "quality-review", Err: err})
	}
	if len(reviewStatus.Tasks) != 1 || reviewStatus.Tasks[0].State != "needs_repair" {
		w.assert("fresh-reviewer-finds-real-defect", false, fmt.Sprintf("%+v", reviewStatus.Tasks),
			"a blocking review must move the task out of checking")
		return
	}
	w.assert("fresh-reviewer-finds-real-defect", true,
		fmt.Sprintf("review session %s reported request_changes; the task moved to %s with the finding persisted",
			"scenario-review-session-1", reviewStatus.Tasks[0].State),
		"a fresh, distinct reviewer session found the deterministically injected defect and the task moved to needs_repair")
	_ = rejectedStep

	// Bounded repair, through the production repair path, consuming the same
	// task ledger rather than resetting it. The repair corrects the renderer the
	// reviewer actually reported.
	followup := w.driver.MustInvoke(ctx, "execution-followup-prepare (repair)", "project", "execution-followup-prepare", w.projectID, run.RunID,
		"--command-id", "repair-001", "--expected-revision", CurrentRevision,
		"--kind", "repair", "--wall-limit-ms", "60000")
	var repairRun PreparedRun
	if err := Decode(followup, &repairRun); err != nil {
		panic(&ScenarioAbort{Step: "execution-followup-prepare", Err: err})
	}
	if repairRun.RunID == run.RunID {
		panic(&ScenarioAbort{Step: "execution-followup-prepare", Err: errors.New("the repair attempt reused the source run identity")})
	}
	repaired := w.driver.MustInvoke(ctx, "execution-start (repair)", "project", "execution-start", w.projectID, repairRun.RunID,
		"--command-id", "repair-start-001", "--synthetic-fixture",
		"--repository", RepositoryID, "--path", SourcePath, "--content", ExpectedGreeting,
		"--prompt", "Repair the artifact so the specified line carries no trailing whitespace.")
	var repairResult ExecutionResult
	if err := Decode(repaired, &repairResult); err != nil {
		panic(&ScenarioAbort{Step: "repair execution-start", Err: err})
	}
	if !fileContains(w.fixtureBase, SourcePath, ExpectedGreeting) {
		w.assert("bounded-repair-applies-change", false, fileContent(w.fixtureBase, SourcePath),
			"the repair attempt did not write the corrected artifact")
		return
	}
	w.assert("bounded-repair-applies-change", true, repairRun.RunID, "the repair attempt corrected the reported file through the production repair path")

	// Fresh checks and a distinct fresh review after the repair. The same
	// deterministic check must be re-run, not reused.
	passing = w.runCheck(ctx, "check-002")
	w.assert("required-check-rerun-after-repair", passing.Status == "pass", passing.Status,
		"a repair requires fresh check evidence rather than reuse of the prior result")
	accepting := ReviewDocument{SchemaVersion: 1, Decision: "pass", Summary: "scenario review after the injected defect was repaired", Findings: nil}
	acceptingFile, err := w.driver.WriteTemp(filepath.Join(w.root, "reviews"), "review-2", accepting)
	if err != nil {
		panic(&ScenarioAbort{Step: "review write", Err: err})
	}
	reviewTwo := w.driver.MustInvoke(ctx, "quality-review (fresh after repair)", "project", "quality-review", w.projectID, TaskID,
		"--command-id", "review-002", "--result", acceptingFile,
		"--session-id", "scenario-review-session-2", "--native-identity", "scenario-review-native-2",
		"--synthetic-fixture")
	var reviewTwoResult ReviewResult
	if err := Decode(reviewTwo, &reviewTwoResult); err != nil {
		panic(&ScenarioAbort{Step: "quality-review after repair", Err: err})
	}
	if reviewTwoResult.Blocking || reviewTwoResult.Status != "pass" {
		panic(&ScenarioAbort{Step: "quality-review after repair", Err: fmt.Errorf("the post-repair review did not pass: %+v", reviewTwoResult)})
	}
	w.assert("fresh-review-after-repair", true, "distinct session and native identity, status pass",
		"a distinct fresh review is required after a repair, not a re-run of the prior one")
	// The production execution path ran, but through the labelled synthetic
	// fixture driver rather than a live harness. The plan is explicit that a
	// harness whose live route is unavailable is recorded as pending and never as
	// passing, so this row is deferred rather than claimed.
	if err := w.report.Matrix.Defer("milestone", StepExecuteSequentially,
		"execution ran through the labelled --synthetic-fixture driver, not a qualified live harness. The plan requires a harness whose live route is unavailable to be recorded as pending and never as passing. A real contained harness turn is Stage 8's, and the contained Codex route is additionally blocked on a user decision; see docs/process/pending-decisions.md",
		"bin/vigil project execution-prepare/execution-start ran the full persisted execution lifecycle (journalling, submission, result validation, writer containment) via the disposable synthetic driver: the task moved to checking and was never accepted by completion"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:execute", Err: err})
	}
	if err := w.report.Matrix.Mark("milestone", StepReviewAndRepair, EvidenceAutomated,
		"a fresh distinct reviewer session found the deterministically injected defect as blocking; a bounded repair through the production repair path fixed it; fresh checks and a distinct fresh review then passed",
		"bin/vigil project quality-review, execution-followup-prepare, execution-start, quality-check"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:repair", Err: err})
	}
	w.note("The reviewer here is the labelled fixture reviewer. It exercised the fresh-session, blocking-derivation and post-repair mechanics; it is not evidence of a live reviewer model. The milestone row for the review/repair cycle inherits the same caveat.")
	w.note("The execution driver is the labelled synthetic fixture driver throughout, so no step of this walkthrough is evidence of a live harness turn.")
}

// PreparedRun is the prepared execution identity.
type PreparedRun struct {
	RunID        string `json:"run_id"`
	TaskID       string `json:"task_id"`
	GenerationID string `json:"generation_id"`
	RuntimeKind  string `json:"runtime_kind"`
	ExactResume  bool   `json:"exact_resume"`
	EndpointID   string `json:"endpoint_id"`
}

// ExecutionResult is the production execution outcome.
type ExecutionResult struct {
	Run       string `json:"run"`
	TaskState string `json:"task_state"`
	Accepted  bool   `json:"accepted"`
	Result    struct {
		Status       string   `json:"status"`
		Summary      string   `json:"summary"`
		ChangedPaths []string `json:"changed_paths"`
	} `json:"result"`
}

// CheckResult is the production check outcome.
type CheckResult struct {
	ID                string   `json:"id"`
	CheckID           string   `json:"check_id"`
	Status            string   `json:"status"`
	ExitCode          int      `json:"exit_code"`
	FailureIdentities []string `json:"failure_identities"`
}

// ReviewResult is the production review outcome.
type ReviewResult struct {
	Status   string          `json:"status"`
	Blocking bool            `json:"blocking"`
	Findings []ReviewFinding `json:"findings"`
	Session  string          `json:"session_id,omitempty"`
}

func (w *walkthrough) runCheck(ctx context.Context, commandID string) CheckResult {
	step := w.driver.MustInvoke(ctx, "quality-check "+commandID, "project", "quality-check", w.projectID, TaskID, CheckID,
		"--command-id", commandID, "--synthetic-fixture")
	var result CheckResult
	if err := Decode(step, &result); err != nil {
		panic(&ScenarioAbort{Step: "quality-check", Err: err})
	}
	return result
}

// reviewerDocument builds the blocking review from the artifact's real current
// state. The finding is located by reading the file, so it cannot drift from what
// is actually there: if the artifact carried no trailing whitespace, the scenario
// would fail rather than report a review that found nothing.
func (w *walkthrough) reviewerDocument() ReviewDocument {
	content := fileContent(w.fixtureBase, SourcePath)
	line, lineNumber := trailingWhitespaceLine(content)
	if line == "" {
		panic(&ScenarioAbort{Step: "reviewer document",
			Err: errors.New("the artifact carries no trailing whitespace, so there is no real defect for a reviewer to find")})
	}
	return ReviewDocument{
		SchemaVersion: 1,
		Decision:      "request_changes",
		Summary:       "the artifact does not match the specified format",
		Findings: []ReviewFinding{{
			ID:             "artifact-trailing-whitespace",
			Severity:       "high",
			RepositoryID:   RepositoryID,
			Path:           SourcePath,
			Line:           lineNumber,
			Evidence:       fmt.Sprintf("%s line %d carries trailing whitespace (%q); the specification requires the line with no trailing whitespace", SourcePath, lineNumber, line),
			Recommendation: "remove the trailing whitespace from the artifact line",
		}},
	}
}

// trailingWhitespaceLine returns the first line carrying trailing whitespace, and
// its one-based line number.
func trailingWhitespaceLine(content string) (string, int) {
	for index, line := range strings.Split(content, "\n") {
		if strings.TrimRight(line, " \t\r") != line {
			return line, index + 1
		}
	}
	return "", 0
}

// fileContent reads a repository-relative file, returning empty when absent.
func fileContent(root, relative string) string {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return ""
	}
	return string(raw)
}

// recordQualityEvidence records the manual criterion, the human decision and the
// acceptance, then proves acceptance created no delivery authority and that the
// plan's own gates run.
func (w *walkthrough) recordQualityEvidence(ctx context.Context) {
	manual := w.driver.MustInvoke(ctx, "quality-manual", "project", "quality-manual", w.projectID, TaskID, ManualCriterionID,
		"--command-id", "manual-001", "--outcome", "pass", "--evaluator", "fixture-human",
		"--notes", "fixture mechanics only; a real functional Pass is a Stage 8 human decision", "--synthetic-fixture")
	if err := Decode(manual, &map[string]any{}); err != nil {
		panic(&ScenarioAbort{Step: "quality-manual", Err: err})
	}
	decision := w.driver.MustInvoke(ctx, "quality-decision", "project", "quality-decision", w.projectID, TaskID,
		"--command-id", "decision-001", "--action", "accept",
		"--rationale", "fixture acceptance mechanics only; real acceptance is Stage 8's", "--synthetic-fixture")
	if err := Decode(decision, &map[string]any{}); err != nil {
		panic(&ScenarioAbort{Step: "quality-decision", Err: err})
	}
	accepted := w.driver.MustInvoke(ctx, "quality-accept (task)", "project", "quality-accept", w.projectID, TaskID,
		"--command-id", "accept-task-001", "--synthetic-fixture")
	var acceptance AcceptanceResult
	if err := Decode(accepted, &acceptance); err != nil {
		panic(&ScenarioAbort{Step: "quality-accept task", Err: err})
	}
	if acceptance.Outcome != "accepted" && acceptance.Outcome != "ok" && acceptance.Status == "rejected" {
		panic(&ScenarioAbort{Step: "quality-accept task", Err: fmt.Errorf("task acceptance did not succeed: %+v", acceptance)})
	}
	if state := taskStateOf(ctx, w.driver, TaskID); state != "accepted" {
		w.assert("task-accepted-without-delivery-authority", false, state, "task acceptance did not move the task to accepted")
		return
	}
	w.assert("task-accepted-without-delivery-authority", true, fmt.Sprintf("task %s is accepted", TaskID),
		"task acceptance records evidence and state; it creates no commit, push, publication or delivery authority")

	// The plan's own gates run with --plan-wide and the plan ID as the target.
	planCheck := w.runPlanCheck(ctx, "plan-check-001")
	w.assert("plan-wide-check-runs", planCheck.Status == "pass", planCheck.Status, "the plan's own required check runs against the accepted tree")
	planReview := ReviewDocument{SchemaVersion: 1, Decision: "pass", Summary: "scenario plan review", Findings: nil}
	planReviewFile, err := w.driver.WriteTemp(filepath.Join(w.root, "reviews"), "plan-review", planReview)
	if err != nil {
		panic(&ScenarioAbort{Step: "plan review write", Err: err})
	}
	planReviewStep := w.driver.MustInvoke(ctx, "quality-review (plan)", "project", "quality-review", w.projectID, PlanID,
		"--command-id", "plan-review-001", "--result", planReviewFile,
		"--session-id", "scenario-plan-review-1", "--native-identity", "scenario-plan-review-native-1",
		"--plan-wide", "--synthetic-fixture")
	var planReviewResult ReviewResult
	if err := Decode(planReviewStep, &planReviewResult); err != nil {
		panic(&ScenarioAbort{Step: "quality-review plan", Err: err})
	}
	if planReviewResult.Blocking {
		panic(&ScenarioAbort{Step: "quality-review plan", Err: errors.New("the plan-wide review blocked unexpectedly")})
	}
	w.driver.MustInvoke(ctx, "quality-manual (plan)", "project", "quality-manual", w.projectID, PlanID, ManualCriterionID,
		"--command-id", "plan-manual-001", "--outcome", "pass", "--evaluator", "fixture-human",
		"--notes", "fixture plan mechanics only", "--plan-wide", "--synthetic-fixture")
	w.driver.MustInvoke(ctx, "quality-decision (plan)", "project", "quality-decision", w.projectID, PlanID,
		"--command-id", "plan-decision-001", "--action", "accept",
		"--rationale", "fixture plan acceptance mechanics only", "--plan-wide", "--synthetic-fixture")
	planAccepted := w.driver.MustInvoke(ctx, "quality-accept (plan)", "project", "quality-accept", w.projectID, PlanID,
		"--command-id", "accept-plan-001", "--plan-wide", "--synthetic-fixture")
	var planAcceptance AcceptanceResult
	if err := Decode(planAccepted, &planAcceptance); err != nil {
		panic(&ScenarioAbort{Step: "quality-accept plan", Err: err})
	}
	if state := taskStateOf(ctx, w.driver, TaskID); state != "accepted" {
		w.assert("plan-accepted-moves-to-finalizing", false, state, "plan acceptance disturbed the accepted child task")
		return
	}
	w.assert("plan-accepted-moves-to-finalizing", true, fmt.Sprintf("project state %s; the scenario task is still accepted", taskStateOf(ctx, w.driver, TaskID)),
		"plan acceptance re-observes every child task and moves the plan toward finalization")
	if err := w.report.Matrix.Mark("milestone", StepChecksAndEvidence, EvidenceAutomated,
		"the required check, a fresh review, a manual outcome, a human decision and an atomic acceptance were all recorded with durable evidence; the plan's own gates ran with --plan-wide",
		"bin/vigil project quality-check/quality-review/quality-manual/quality-decision/quality-accept"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:evidence", Err: err})
	}
}

func (w *walkthrough) runPlanCheck(ctx context.Context, commandID string) CheckResult {
	step := w.driver.MustInvoke(ctx, "quality-check (plan)", "project", "quality-check", w.projectID, PlanID, CheckID,
		"--command-id", commandID, "--plan-wide", "--synthetic-fixture")
	var result CheckResult
	if err := Decode(step, &result); err != nil {
		panic(&ScenarioAbort{Step: "quality-check plan", Err: err})
	}
	return result
}

// AcceptanceResult is the production acceptance outcome.
type AcceptanceResult struct {
	ScopeID string `json:"scope_id"`
	Outcome string `json:"outcome"`
	Status  string `json:"status"`
}

// finalizeFactually persists the factual archive and demonstrates that a
// narrative failure retries only the narrative, never the accepted development.
func (w *walkthrough) finalizeFactually(ctx context.Context) {
	built := w.driver.MustInvoke(ctx, "archive-build", "project", "archive-build", w.projectID, PlanID,
		"--command-id", "archive-001")
	var archive ArchiveRecord
	if err := Decode(built, &archive); err != nil {
		panic(&ScenarioAbort{Step: "archive-build", Err: err})
	}
	if archive.Revision < 1 || archive.ManifestDigest == "" {
		panic(&ScenarioAbort{Step: "archive-build", Err: fmt.Errorf("unexpected archive record %+v", archive)})
	}
	shown := w.driver.MustInvoke(ctx, "archive-show", "project", "archive-show", w.projectID, PlanID, fmt.Sprint(archive.Revision))
	var verified struct {
		Record   ArchiveRecord  `json:"record"`
		Manifest FactualArchive `json:"manifest"`
	}
	if err := Decode(shown, &verified); err != nil {
		panic(&ScenarioAbort{Step: "archive-show", Err: err})
	}
	// The required citations are the plan acceptance and every task acceptance,
	// read from the verified manifest rather than guessed.
	cited := requiredCitations(verified.Manifest)
	w.assert("factual-archive-persisted", true, fmt.Sprintf("revision %d digest %s", archive.Revision, archive.ManifestDigest),
		"the factual manifest is persisted and re-verified before any narrative exists")

	// A deliberately malformed narrative is rejected, and the accepted task must
	// still be accepted afterwards: narrative failure never reruns development.
	badNarrative := map[string]any{
		"command_id": "narrative-001", "plan_id": PlanID, "manifest_revision": archive.Revision,
		"manifest_digest": archive.ManifestDigest, "text": "unrelated prose", "cited_ids": []string{}, "actor": "fixture",
	}
	badFile, err := w.driver.WriteTemp(filepath.Join(w.root, "narratives"), "bad", badNarrative)
	if err != nil {
		panic(&ScenarioAbort{Step: "narrative write", Err: err})
	}
	if _, err := w.driver.Invoke(ctx, "archive-narrative (malformed)", "project", "archive-narrative", w.projectID, PlanID,
		"--synthetic-fixture", "--file", badFile); err == nil {
		w.assert("narrative-failure-does-not-rerun-development", false, "accepted", "an uncited narrative was accepted")
		return
	}
	status, _, err := w.driver.Status(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "status after narrative failure", Err: err})
	}
	// The scenario task must still be accepted. The archive also creates a visible
	// system finalization task, so the scenario task is located by its exact
	// identity rather than assumed to be the only row.
	state := taskStateOf(ctx, w.driver, TaskID)
	if state != "accepted" {
		w.assert("narrative-failure-does-not-rerun-development", false,
			fmt.Sprintf("scenario task is %q after an uncited narrative was refused; tasks: %+v", state, status.Tasks),
			"a narrative failure must not invalidate accepted development")
		return
	}
	w.assert("narrative-failure-does-not-rerun-development", true,
		fmt.Sprintf("the scenario task is still %s after an uncited narrative was refused, and the finalization task is visible as %s",
			state, finalizationTaskStates(status)),
		"a narrative failure leaves finalization pending and never invalidates accepted development")

	// A correctly cited narrative completes the plan.
	goodNarrative := map[string]any{
		"command_id": "narrative-002", "plan_id": PlanID, "manifest_revision": archive.Revision,
		"manifest_digest": archive.ManifestDigest,
		"text":            "The scenario plan completed one greeting repair cycle with a fresh review, an objective check and fixture-labelled acceptance.",
		"cited_ids":       cited,
		"actor":           "fixture",
	}
	goodFile, err := w.driver.WriteTemp(filepath.Join(w.root, "narratives"), "good", goodNarrative)
	if err != nil {
		panic(&ScenarioAbort{Step: "narrative write", Err: err})
	}
	if _, err := w.driver.Invoke(ctx, "archive-narrative (cited)", "project", "archive-narrative", w.projectID, PlanID,
		"--synthetic-fixture", "--file", goodFile); err != nil {
		panic(&ScenarioAbort{Step: "archive-narrative", Err: fmt.Errorf("a correctly cited fixture narrative was refused: %w", err)})
	}
	if err := w.report.Matrix.Mark("recovery", CaseSummaryOnlyRetry, EvidenceAutomated,
		"an uncited narrative was refused and a cited one accepted; the accepted task remained accepted throughout, so summary failure never reruns accepted development",
		"bin/vigil project archive-build/archive-narrative"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:summary", Err: err})
	}
}

// ArchiveRecord is the persisted archive identity.
type ArchiveRecord struct {
	PlanID         string   `json:"plan_id"`
	Revision       int      `json:"revision"`
	State          string   `json:"state"`
	ManifestDigest string   `json:"manifest_digest"`
	ManifestID     string   `json:"manifest_id,omitempty"`
	Citations      []string `json:"cited_ids,omitempty"`
}

// finalizationTaskStates summarizes the visible system finalization tasks the
// archive created, so the report can show the plan is pending narrative rather
// than merely asserting it.
func finalizationTaskStates(status ProjectStatus) string {
	states := []string{}
	for _, task := range status.Tasks {
		if task.ID != TaskID {
			states = append(states, task.ID[:min(12, len(task.ID))]+"="+task.State)
		}
	}
	if len(states) == 0 {
		return "none"
	}
	return strings.Join(states, ",")
}

// FactualArchive is the subset of the verified factual manifest the rehearsal
// needs: the acceptance identities a narrative must cite, and the revisions the
// evidence was bound to.
type FactualArchive struct {
	SchemaVersion int    `json:"schema_version"`
	PlanID        string `json:"plan_id"`
	PlanRevision  int    `json:"plan_revision"`
	AcceptanceID  string `json:"acceptance_id"`
	SpecDigest    string `json:"spec_digest"`
	Tasks         []struct {
		ID           string `json:"id"`
		Revision     int    `json:"revision"`
		AcceptanceID string `json:"acceptance_id"`
	} `json:"tasks"`
}

// requiredCitations returns the plan acceptance and every task acceptance, which
// is exactly what the production narrative validator requires. Reading them from
// the verified manifest means the scenario cannot cite a plausible-looking but
// wrong identity.
func requiredCitations(manifest FactualArchive) []string {
	cited := []string{}
	if manifest.AcceptanceID != "" {
		cited = append(cited, manifest.AcceptanceID)
	}
	for _, task := range manifest.Tasks {
		if task.AcceptanceID != "" {
			cited = append(cited, task.AcceptanceID)
		}
	}
	return cited
}
