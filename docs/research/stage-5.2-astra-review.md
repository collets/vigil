# Stage 5.2 independent security/correctness review

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
