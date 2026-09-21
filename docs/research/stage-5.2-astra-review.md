# Stage 5.2 independent security/correctness review

Latest independent follow-up, 2026-09-21, of `d34f894`: **R6 and R8 resolved; all R1–R9 findings are closed. The Stage 5.2 offline implementation review is accepted.** Production dispatch remains disabled pending the shared live qualification gates. Earlier verdicts and findings below are historical evidence; see the independent follow-up acceptance at the end.

Date: 2026-09-21. Baseline: `b167e10`. Reviewed implementation and evidence through `02f11bd`, including `cdb8f75` and `88cd2be`.

**Verdict: changes requested. Stage 5.2 is not accepted.** The following findings concern the implementation, independently of the explicitly deferred live qualification work. Production dispatch must remain disabled.

## Findings

### R1 — P1: interrupted effects and delivered prompts can be replayed

Locations: `internal/supervisor/runner.go:138–151` and `:407–431`.

`ensureDriverEffect` leaves the durable state `prepared` during the external call. After a crash between the call and its observation, failed inspection does not block another call: only `uncertain` is rejected. The driver can therefore create another runtime/native session when the original outcome is unknown.

Separately, `submit` rejects durable `uncertain` and unresolved `writing`, but not durable `delivered`. If a delivered turn leaves the checkout baseline unchanged and the process crashes before result persistence, a later explicit start with unavailable inspection calls `Submit` again. A user retry of start must not replay the generation's native prompt.

Reproductions: `TestReviewReplayAfterCrash` observed two create calls after the injected `after_runtime_create` crash and failed inspection. `TestReviewDeliveredSubmissionReplay` uses the public `Run` path twice, reconciles the open budget segment between calls, and observes two submissions after a crash at `after_terminal_observe`. Its first edit deliberately preserves the original bytes, so the baseline check legitimately passes.

Required correction: persist the in-flight transition before calling a driver, distinguish proven absence from failed inspection, and reconcile interrupted effects without replay unless non-occurrence is established. Treat durable delivered submission as terminal for submission purposes regardless of current inspection availability.

### R2 — P1: stale reservations authorize dispatch after quarantine

Location: `internal/supervisor/runner.go:173–175`.

The launch gate trusts the supplied reservation's run ID, phase string and claim count. It never checks the live coordinator owner, workspace fences, root membership, ticket state, endpoint or slot generation. `Runner` does not retain an owner/coordinator capability with which to validate them. A previously valid in-memory reservation remains sufficient after its owner closes and the coordinator quarantines its resources.

Reproduction: `TestReviewQuarantinedOwnerDispatch` calls `owner.Close()` after reservation and then calls `Run` with that reservation. Submission occurs and execution completes successfully despite quarantine.

Required correction: validate the persisted reservation against current coordinator ownership, exact roots/fences and endpoint capacity, and preserve that authority through effect start. Fail closed on owner loss, quarantine, release, replacement or inspection failure. Claim count is not ownership proof.

### R3 — P1: branch recovery executes the latest enrollment under an older intent

Locations: `internal/core/repositories.go:303–313` and `:361`.

An unfinished command receipt identifies a specific repository revision and branch in `repository_branch_operations`, but recovery loads `e.Repository`, which returns the latest enrollment. `DB.Command` returns the existing receipt without running its revision-checking closure again. `workspace.PrepareBranch` consequently receives the new enrollment while the old operation is marked observed.

Reproduction: `TestReviewBranchIntentRevision` crashes after intent for `vigil/original`, switches the disposable checkout to another same-commit branch, enrolls revision 2 selecting `vigil/replacement`, and retries the original command. It creates/switches to `vigil/replacement`, while the observed operation still names revision 1 and `refs/heads/vigil/original`.

Required correction: recover from the exact journaled revision, identity, base and branch; reject superseded authority or reconcile the original effect. Never substitute current enrollment data into an existing intent.

### R4 — P1: production qualification is not bound to the prepared execution

Locations: `internal/supervisor/runner.go:209–235`; compare `internal/core/core.go:647–700` and `internal/supervisor/preparation.go:254–272`.

The production gate checks required claim/recovery names, then invokes `boundary.QueryEligibility` directly. It bypasses `Engine.ExecutionEligibility`, which exists specifically to enforce the latest persisted profile revision and matching harness/model/provider/endpoint. Preparation also accepts the qualification request separately from the selected task profile without binding them. A supported record for a different or obsolete profile can thus satisfy this part of the gate.

There is also no equality check between the request's role, layout, mount-plan digest or endpoint authority and the actual prepared execution/checkout/routes. Validating a checkout and checking its observed mounts against itself does not bind it to the checkout that was qualified.

Required correction: derive qualification inputs from the immutable execution and actual runtime/checkout configuration, require implementation-role eligibility, bind routes/capacity and the actual mount digest, and invoke the core's current-profile gate immediately before effects. Do not accept a caller-supplied supported combination as evidence for a different execution.

This is a source-level finding in the future production path. No live qualification was manufactured and no production launch was attempted; the CLI still explicitly refuses non-synthetic execution.

### R5 — P1: fixture writes can escape the disposable repository

Location: `internal/supervisor/fixture_driver.go:149–154` (validation at `:42–66`).

Canonicalizing the root in `88cd2be` fixes the `/tmp` alias mismatch, but `os.WriteFile` still follows target symlinks and truncates existing hardlinked files. Validation does not enforce protected paths/task scope before the write or confine the target's filesystem traversal. Result validation happens after the damage.

Reproductions:

- `TestReviewFixtureSymlinkEscape`: an excluded `.git/review-escape` symlink targets an outside disposable sentinel. Writing this relative path overwrites the sentinel; only afterward does result validation reject the scope.
- `TestReviewFixtureHardlinkEscape`: replace tracked `src/.keep` with an identical-byte, identical-mode hardlink to an outside disposable sentinel. The baseline remains clean. Writing this allowed `src/**` path overwrites the outside file and the run completes successfully.

Required correction: enforce scope and protected-path exclusions before effects, use confined no-follow traversal, and safely handle/reject existing hardlinks. A regular fixture marker and canonical root alone do not establish confinement. Both victims in this review were agent-owned temporary files.

### R6 — P1: active budgets do not bound submission or gate completion

Locations: `internal/supervisor/runner.go:264`, `:431`, and `:487–506`.

Only the wall limit bounds the execution context. Active-budget checkpoints run before runtime phases and on the await ticker; the synchronous native submission has neither an active deadline nor concurrent budget enforcement. An immediate terminal response after a slow submission reaches result persistence without a final budget check. Final segment accounting records the excess but does not reject completion or contain execution when the limit expires.

Reproduction: `TestReviewActiveBudgetSubmit` uses the fixture's actual persisted 30,000 ms active allowance and 60,000 ms wall allowance, with a driver that waits 30,100 ms in `Submit` and then returns terminal completion. `Run` succeeds and reports `completed`. No persisted limit was changed for this final reproduction.

Required correction: enforce remaining active allowance across startup, native creation and submission as well as await, contain at expiry, and check budget state before committing successful completion. Drivers must receive cancellation/deadline semantics appropriate to the remaining allowance.

### R7 — P2: containment stop is journaled after the external stop

Location: `internal/supervisor/runner.go:275–279`.

The containment closure calls `Driver.Stop` before `recordContainment` creates the `containment_stop` effect. A crash in or immediately after stop leaves no durable intent for that external action, contrary to the required effect ordering.

Reproduction: `TestReviewContainmentIntent` injects a start failure and queries the effect journal from inside `Stop`; no containment intent exists.

Required correction: persist the stop intent before calling the driver whenever storage is available, then persist its observation. Keep an explicit emergency containment policy for storage failure; fixing journaling must not prevent stopping an unsafe writer.

### R8 — P2: budget checkpoints charge proven human-wait time

Location: `internal/supervisor/budget.go:63–75`.

`BeginHumanWait` creates a `human_wait` segment, but `checkpointSegment` never reads the category and treats its elapsed time as active execution. The normal await ticker can therefore exhaust the budget during a proven human wait, although `closeSegment` later excludes that same time.

Reproduction: `TestReviewHumanWaitCheckpoint` opens a proven human wait, waits 70 ms with a 50 ms test allowance, and observes `active execution budget exhausted` at checkpoint.

Required correction: make checkpoint and close accounting agree about excluded categories, transition wait/active segments atomically with respect to checkpointing, and retain cumulative attempt accounting across segments.

### R9 — P2: unchanged repository revisions collide on fingerprint primary keys

Locations: `internal/core/repositories.go:189–190`, `:209–210`; `internal/store/migrations/project-003.sql:22–35`.

The fingerprint primary key hashes only baseline content, but each fingerprint row belongs to a specific repository revision. Enrolling a new revision with unchanged checkout bytes/HEAD/index attempts to insert the same globally unique ID. Routine policy changes such as selecting a different plan branch fail with a uniqueness constraint instead of recording the new enrollment.

Reproduction: `TestReviewUnchangedReenrollment` enrolls a clean repository, then enrolls it again with a different plan branch and unchanged baseline. The second command fails with `UNIQUE constraint failed: repository_fingerprints.id`.

Required correction: separate content digest from observation identity, or explicitly normalize shared content and revision ownership. Preserve published migration digests; use a forward migration if schema changes are needed.

## Validation and scope

- Independent Linux `make check`, `make check-race` and `make build` passed on the original implementation.
- Ten focused disposable reproduction tests were run and failed their safety/correctness assertions as described above: eight supervisor tests and two core tests. R4 is based on direct source tracing, not a production execution.
- Reproduction sources are retained as inert text in [supervisor_test.go.txt](stage-5.2-review/supervisor_test.go.txt) and [core_test.go.txt](stage-5.2-review/core_test.go.txt). On the reviewed `02f11bd` baseline, copy them into the respective packages and run `go test ./internal/supervisor ./internal/core -run TestReview -count=1 -v`; the active-budget test intentionally takes about 30 seconds. Remediation intentionally changed the runner authority/effect APIs, so these historical sources need mechanical owner/context adaptation before compiling against `b23ee5e`. The equivalent current-API cases are permanent in `internal/supervisor/review_regression_test.go` and `internal/core/repositories_test.go`. No failing test file was left in the implementation packages.
- Reviewed Git observation/parsing and branch preparation, identity/canonicalization, migrations and their tests, multi-root/coordinator paths, cross-database grants, effect/recovery sequencing, receipts and artifact authority, result/event checks, budgets and CLI gates. Findings above are not a claim that all other interleavings have been exhaustively proven safe.
- `cdb8f75` retains argument/actor binding in receipt lookup and uses core authority for generated result artifacts. Result persistence still stops at `checking`; no acceptance write was found in that path.
- Historical migration files were not modified by this stage. Existing migration/digest/rollback tests passed; this review did not independently recreate every historical production database.
- The reported native macOS results were read as existing evidence, not independently rerun in this review. No Docker, provider, model, credentials or network qualification was exercised. Synthetic tests are not live production qualification.
- No implementation fixes, commits, pushes or production-enablement changes were made. Only this report and inert reproduction sources were added.

The previously deferred safe subscription route, independent provider-idle proof, complete live recovery matrix and shared Mac/WSL capacity authority remain pending independently of these findings.

## Remediation submitted for re-review

The original verdict above is unchanged until independent re-review. The implementation subsequently addressed R1–R9 in local commits `0d02a9f` and `b23ee5e`:

| Finding | Submitted correction |
| --- | --- |
| R1 | Durable `executing` fences every driver phase; retry requires trusted proof of absence. Durable delivered generations never resubmit. |
| R2 | Start requires the live owner and exact persisted roots/claims/fences/ticket/slot; exclusive owner authority is held through dispatch. |
| R3 | Branch recovery loads the exact journaled repository revision. |
| R4 | Preparation and start bind profile/revision/digest, role, endpoint, checkout layout/roots/mounts, routes and all actual driver qualification inputs; start calls `Engine.ExecutionEligibility`. Drivers without an independent binding fail closed. |
| R5 | Fixture targets are prevalidated against scope/protected/excluded paths and written through descriptor-relative no-follow traversal with atomic inode replacement. |
| R6 | Remaining active allowance bounds driver calls and is checked before result persistence. |
| R7 | Normal containment persists `executing` intent before `Stop`; storage failure uses an explicit bounded emergency-stop policy. |
| R8 | Proven human/resource waits are excluded at checkpoint/reconcile and wait transitions are atomic. |
| R9 | Fingerprint observation identity includes repository revision, allowing unchanged re-enrollment. |

Permanent regression tests cover failed recovery inspection, delivered-prompt restart, quarantined and concurrently closing owner authority, branch-revision substitution, unchanged re-enrollment, symlink/hardlink escape, slow submission, containment ordering, human-wait accounting and qualification mismatch. Linux focused/full/race/build/cross-build checks passed. The exact `b23ee5e` bundle also passed native macOS 26.6.2 arm64 full/race/build checks and explicit confinement/reservation/budget reruns. No live qualification or production dispatch was performed; the live gates in this report remain open.

## Independent re-review of 0d02a9f and b23ee5e

Reviewed at documentation HEAD `528480e`; implementation is the submitted `b23ee5e`. Verdict: **changes requested**.

| Finding | Independent disposition |
| --- | --- |
| R1 | Resolved: executing state precedes driver effects; failed inspection cannot authorize replay; durable delivered submission is checked before inspection. Permanent regression tests pass. |
| R2 | Resolved: live owner capability, persisted reservation equality and coordinator root/claim/fence/ticket/slot checks precede dispatch. Exclusive owner locking prevents concurrent close/release during the authorized operation. |
| R3 | Resolved: unfinished branch operations load the journaled repository revision. The branch-substitution regression passes. |
| R4 | Resolved at the contract/source level: profile, role, endpoint/capacity, checkout roots/layout/mount digest and independently derived driver inputs/routes are checked, then core ExecutionEligibility is called. This does not qualify an actual production driver or live route. |
| R5 | Resolved for the reported escapes: protected/scope/exclusion validation precedes descriptor-relative traversal; no-follow parent opens and atomic inode replacement preserve outside symlink/hardlink targets. Native macOS results are submitted evidence, not independently repeated here. |
| R6 | Partially fixed, still P1: synchronous submission overrun is detected, but successful result persistence/reconciliation can bypass budget exhaustion. See below. |
| R7 | Resolved: normal containment records executing intent before Stop, with an explicit emergency-stop exception for storage failure. |
| R8 | Partially fixed, still P2: segment accounting and transitions exclude waits correctly, but the await deadline still counts excluded time. See below. |
| R9 | Resolved: fingerprint observation IDs now include repository identity and enrollment revision, without rewriting historical migration files. |

### R6 still open — P1: outcome persistence can convert budget exhaustion into success

Exact locations: `internal/supervisor/reconcile.go:147–149` and `internal/supervisor/runner.go:910–923`. The normal path's last checkpoint is at `runner.go:560`, before fingerprinting/artifact publication/outcome persistence.

The added checks correctly reject a native submission that returns after its active allowance, but this does not survive reconciliation. Reconcile passes terminal delivered observations directly to persistResult, whose successful outcome transaction checks neither the remaining allowance nor an existing budget-exhaustion outcome. It can change an interrupted, over-budget run into completed/checking.

Independent disposable reproductions, both using the actual persisted 30,000 ms active and 60,000 ms wall limits:

1. `TestRereviewReconcileAfterBudgetExhaustion`: a synthetic driver returns delivered/terminal after 30,100 ms. Run correctly fails and contains the writer. Reconcile then succeeds with `run=completed task=checking`.
2. `TestRereviewBudgetAtOutcomeCommit`: delay at the existing `before_outcome_commit` hook by 30,100 ms, simulating latency after the last active checkpoint. The successful outcome still commits. This confirms that the final transaction does not enforce the allowance after intervening persistence work.

Required correction: successful completion must consult authoritative budget/outcome state at the final outcome boundary on both normal and recovery paths. Preserve terminal evidence from an over-budget run without converting it into successful progression. A late observation must not erase an exhaustion outcome; if timely completion can be proven independently, encode that proof and policy explicitly. Reconciliation must work with closed segments and conservative crash-gap accounting rather than assuming an open segment.

The submitted `TestActiveBudgetBoundsSubmissionAndCompletion` at `internal/supervisor/review_regression_test.go:179–188` does not currently exercise submission: it changes only the in-memory active limit to 50 ms. The new immutable-snapshot check rejects it before Create/Submit. `TestRereviewSubmissionRegressionReachesDriver` confirmed zero Submit calls and the error `prepared execution identity differs from its immutable run snapshot`. Configure the small allowance through normal preparation and assert the driver was reached, along with the expected budget failure and reconciliation behavior.

### R8 still open — P2: the fixed await deadline counts proven wait time

Exact locations: `internal/supervisor/budget.go:121–130` and `internal/supervisor/runner.go:748–755,775–780`.

Although checkpointSegment now excludes human/resource waits, activeCallContext always converts the remaining active allowance to a wall-clock timeout. Await retains that single deadline until it returns. Beginning a proven wait cannot suspend it; even entering await while already in a proven wait creates an active timeout. When it expires, checkpointSegment correctly says the excluded interval did not exhaust the allowance, but await still returns context deadline exceeded and the caller contains the execution.

`TestRereviewHumanWaitAwaitDeadline` starts a proven human-wait segment and calls the await helper with a 100 ms active allowance and a 300 ms parent timeout. Await aborts after approximately 103 ms while the parent context is still live. This test targets the timer/accounting integration; it does not modify a persisted run or claim a production execution.

Required correction: separate the absolute wall timeout from active-time enforcement. Suspend/recompute the active deadline across proven waiting transitions, including waits beginning during an existing Await call; continue enforcing the wall limit and containment lease. Add an await-level wait/resume regression, not only a direct checkpoint test.

### Re-review validation and retained evidence

- Independent Linux `make check`, `make check-race`, `make build` and all four `make cross-build` targets passed before adding the temporary probes.
- Four independent probes exposed the two remaining findings and the vacuous budget regression. Sources are retained as inert text in [rereview_test.go.txt](stage-5.2-review/rereview_test.go.txt). Copy into `internal/supervisor/rereview_test.go` in a disposable source checkout and run `go test ./internal/supervisor -run TestRereview -count=1 -v` with the repository Go environment. Two tests intentionally take about 30 seconds each. Their assertions are expected to fail on `b23ee5e`.
- Temporary executable test files were removed. Implementation source is unchanged. No commits, pushes, model/provider calls, production activation or live qualification were performed.
- Native macOS was not independently rerun in this review. Remaining live gates are unchanged.

## R6/R8 correction submitted for re-review

Commit `d34f894` addresses the two findings that remained open. This is an implementation response, not an independent acceptance; the latest independent verdict at the top of this report remains authoritative until Astra re-reviews it.

- **R6:** the outcome path checkpoints again after the `before_outcome_commit` boundary, then the same database transaction that would insert the result and advance the task recomputes authoritative consumption from the persisted task ledger, persisted run limit and any live active segment. Closed segments and conservative crash-gap `unknown_ms` are included. Reconciliation can retain delivered/terminal evidence, but an exhausted ledger cannot create an execution result or change the task to `checking`. Reconciliation also no longer regresses an interrupted/contained run merely while recording delivered submission evidence.
- **R8:** await now inherits only the absolute parent/wall context. A separate active-budget timer is scheduled from the current persisted segment, disabled while its category is proven `human_wait`/`resource_wait`, and recomputed immediately on wait transitions. Ending the wait restores the timer with cumulative consumption intact; the wall deadline and lease renewal continue throughout the wait.
- The formerly vacuous submission regression now creates its small active allowance through normal configuration/task preparation, asserts exactly one `Submit` call, asserts the initial budget failure, runs reconciliation, and proves no `completed` run or `checking` task. A separate late-outcome test proves the fault hook was reached before sleeping past the persisted allowance.
- Await regressions cover entering await while already waiting, beginning a wait during an existing await, and restoring enforcement after the wait ends.

Validation of exact commit `d34f894`: Linux focused tests, full `make check`, `make check-race`, `make build`, all four `make cross-build` targets, five race-detector repetitions of the wait-transition test and three repetitions of both outcome tests passed. A Git bundle with SHA-256 `0d2308ad5c86111809a3346ad2143743ec1b8e79f5266dc13c1715ff77e6d427` was tested in an isolated macOS 26.6.2 arm64 checkout with Go 1.27.1: focused R6/R8 tests, full check (`internal/supervisor` 30.705 seconds), race check (`internal/supervisor` 45.395 seconds) and build passed. The temporary Mac checkout and bundle were removed; the normal Mac checkout was not accessed. No live qualification or production dispatch was performed.

## Independent follow-up acceptance of d34f894

Date: 2026-09-21. Reviewed implementation `d34f894` at documentation HEAD `10bad93`. **R6 and R8 are resolved; no remaining findings from R1–R9.** This accepts the offline implementation review, not live production qualification or the still-open shared 5.1/5.2 live gate.

Confirmed independently:

- **R6 / normal completion:** persistResult checkpoints after the `before_outcome_commit` hook and calls enforceCompletionBudget inside the same write transaction that inserts the execution result and advances the task. The guard uses persisted ledger/run limits, charged and unknown consumption, and live active time; it does not trust a mutable caller limit. Delaying at the hook beyond the actual persisted 30-second allowance now rejects success.
- **R6 / reconciliation:** reconciliation reaches that same guarded outcome transaction after closing/reconciling any open segment. A delivered terminal result from a submission exceeding the actual persisted 30-second allowance cannot become completed/checking. Recording delivery no longer regresses interrupted/contained state.
- **Evidence preservation:** an additional disposable probe confirmed delivered submission, a reconciled terminal effect and one retained execution-result artifact, with zero execution_results rows, an interrupted run and no checking transition after budget failure and reconciliation.
- **Meaningful submission regression:** the permanent test now configures its allowance through normal preparation and asserts exactly one Submit call. The adapted retained reproduction likewise reaches Submit exactly once before budget rejection.
- **R8 / wall versus active time:** Await inherits the parent/wall context. Its separate active timer is disabled for excluded segments and re-evaluates segment state on transitions, timer expiry and periodic checks. Already-waiting and mid-Await wait cases pass without consuming active allowance, while the parent wall timeout remains effective.
- **R8 / cumulative resume:** checkpoint enforcement includes prior ledger consumption. An independent probe spent approximately 200 ms active, then 350 ms in proven wait, with a 300 ms active allowance. After EndHumanWait it exhausted the remaining allowance within 200 ms rather than receiving a fresh 300 ms. This probe and the permanent wait-transition tests passed three repetitions under the race detector.

Validation:

- Independent Linux `make check`, `make check-race`, `make build` and all four `make cross-build` targets passed.
- The four retained follow-up reproductions passed with the required adaptation of the submission regression to the new delayed-driver field and normal persisted limit configuration. The original 30-second limits/delays were retained for late-outcome and reconciliation tests. The additional evidence-retention probe passed; that invocation completed in 64.367 seconds.
- Three race-detector repetitions of the independent cumulative-resume probe plus permanent await-transition tests passed in 10.634 seconds.
- Adapted probes are retained as inert text in [followup_test.go.txt](stage-5.2-review/followup_test.go.txt). Copy into `internal/supervisor/followup_review_test.go` in a disposable checkout and run `go test ./internal/supervisor -run 'Test(Rereview|Followup)' -count=1 -v` using the repository's Go environment. The two original long cases take about 30 seconds each. The temporary executable copy was removed after validation.
- Native macOS evidence for exact commit `d34f894` was inspected as submitted evidence; macOS was not independently rerun in this follow-up. No live harness/provider qualification, credential use, production activation, implementation edits, commits or pushes occurred.

Next: Stage 5.3 recovery/control work may use these reviewed contracts in disposable fixtures. The scoped no-extra-charge Codex route, independent provider-idle proof, shared Mac/WSL capacity authority and complete advertised live recovery matrix remain pending. Production dispatch must stay disabled until those gates are independently satisfied.
