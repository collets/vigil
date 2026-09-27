# Stage 5.3 independent security/correctness review

<!-- vigil-tier: evidence -->

Date: 2026-09-21. Baseline `bf09f4d`; implementation range through `5617679`; documentation HEAD `2adba66`.

**Verdict: changes requested. Five P1 and two P2 findings; Stage 5.3 is not independently accepted.** Production dispatch remains disabled. These defects concern the implemented offline contracts, independently of the deferred live qualification gates. Finding IDs below are local to Stage 5.3 and do not reopen the historical Stage 5.2 findings.

## R1 — P1: stop can return contained before the active dispatcher starts more work

Locations: `internal/supervisor/control.go:58–74,144–147`; `internal/supervisor/runner.go:508–553`.

Stop pauses persisted dispatch and invokes interrupt/stop, but it does not cancel or synchronize with an already authorized Run. That Run checks revision/authority initially and then continues through later effects without consulting the new stop state. An in-flight create can finish after Stop observes containment, after which Run starts the runtime, creates a native session, submits a prompt and persists completion.

`TestStage53ReviewStopDuringCreate` blocks Create before it changes fixture state, invokes Stop concurrently, waits for its `observed` receipt, then releases Create. The stopped generation subsequently records one native_create and one submit; Run returns success. All actions are synthetic in disposable fixtures.

Correction: revoke/cancel the active generation's execution authority, fence every subsequent effect start against stop, and join or safely contain in-flight creation before reporting stopped. Keep stop responsive without acquiring the whole-run owner lock in a way that deadlocks. Completion must not override explicit stop merely because an older Run returned later.

## R2 — P1: destructive recovery lacks current writer and workspace authority

Locations: `internal/checkpoint/recovery.go:306–334,794–823`; compare `internal/checkpoint/manager.go:56–74,156`.

Clear and Restore verify historical checkpoint content and path state but do not require live coordinator claims/fences or independently contained current writers. The manager has no owner capability. writerSafe is consulted only for a new Save; a previously verified checkpoint is not evidence that the workspace remains quiescent. Another run can be active when an old set is used for mutation, and command-receipt replay does not re-establish current ownership either.

`TestStage53ReviewClearRejectsLiveWriter` creates verified baseline/captured sets, seeds the disposable run as active/unconfirmed, and calls Clear without any coordinator claim. Clear succeeds and reports saved. The test models durable live-writer state; it does not launch a real competing process. Restore has the same missing authorization boundary by source inspection.

Correction: acquire and validate the entire current workspace resource set, enforce stopped/quarantined-writer policy across all participating roots, and retain exclusive recovery authority through capture/apply/replay. Check current authority rather than trusting the run state at the earlier save. Ambiguity must block mutation, including mutation resumed after a crash.

## R3 — P1: core clear treats caller-supplied paths as proof of agent ownership

Locations: `internal/checkpoint/recovery.go:311,246–290`; CLI construction at `internal/cli/project.go:504–522`.

Clear accepts OwnedPaths after syntactic validation and uses that list to decide which differences it may discard. It never verifies a corresponding validated result, the recorded pre-attempt baseline, or provenance of the captured bytes. The CLI fetches a result's path names, but path membership alone cannot establish that later captured changes on those paths are still exclusively agent-owned. A manual edit after execution and before checkpoint capture matches the captured-state CAS and can therefore be cleared too.

`TestStage53ReviewClearNeedsOwnershipEvidence` saves a baseline, writes a user-only edit without any execution/result, saves again and supplies that path to Clear. It reports saved and replaces the user edit with baseline bytes. This demonstrates the core authorization gap directly, independently of CLI validation.

Correction: derive ownership inside the core from persisted attempt/baseline/result evidence and bind it to exact byte/index states. Reject substituted or unrelated baselines and mixed/user changes. Human authorization to invoke agent-scoped clear is not a general destructive reset authorization. Retaining a backup does not satisfy the requirement to leave user-owned changes untouched.

## R4 — P1: recovery writes follow replaced parent paths

Locations: `internal/checkpoint/recovery.go:498–553,593–607`; index replacement at `:575`.

Contrary to the results document, recovery apply is not descriptor-relative. validateParent walks names using Lstat and returns an absolute path; atomicWrite subsequently uses pathname-based OpenFile, Rename and directory sync, while deletion uses os.Remove. O_NOFOLLOW protects only the temporary file's final component. Replacing a validated parent with a symlink between validation and use redirects the write outside the enrolled root. The preceding content comparison also does not make the later replacement an atomic CAS.

`TestStage53ReviewPathnameTraversalGap` deterministically exercises the two helper calls used by applyAction: validate a parent, replace it with a symlink to an outside disposable directory, then call atomicWrite on the returned pathname. An outside sentinel becomes replacement and the call returns nil. This models the real gap between helper calls; it is not a stress-timed full CLI reproduction.

Correction: perform traversal and apply relative to validated, held directory descriptors, prevent symlink/root substitution at each boundary, and protect compare/apply against intervening edits. Apply equivalent protection to deletion and Git index changes, including Git's index-lock protocol. Correct the documentation only after the actual implementation and adversarial tests establish those properties.

## R5 — P1: index restore does not restore its staged Git objects

Location: `internal/checkpoint/recovery.go:560–575`; capture at `internal/checkpoint/store.go:358–376,430–440`.

Capture saves staged blob bytes privately, but the Git bundle includes HEAD history only. Index restore writes the saved raw index and never reinstalls the staged objects it references. Once a cleared, uncommitted staged blob has been pruned from the repository, restore can report success with a dangling index entry. Private blob digest verification is not proof that the restored Git index is usable.

`TestStage53ReviewRestoreStagedObjectAfterPrune` stages unique bytes, saves them, clears to baseline, runs Git prune only inside that disposable fixture, saves the destination, and restores the target. Restore reports restored, but git cat-file -e for the staged OID fails. The captured staged bytes still exist privately; they were simply never restored into Git's object store.

Correction: reconstruct and verify every required staged object with the repository's object format before exposing the restored index; journal those effects and verify the resulting index's referential integrity. Tests should prune non-HEAD staged objects between save and restore, including supported SHA-1/SHA-256 repositories and conflict stages where applicable.

## R6 — P2: infrastructure retry preparation ignores unresolved source writers

Location: `internal/supervisor/followup.go:73–87`.

Infrastructure eligibility checks attempt count, submission state and normalized event count, but not independently established source-writer containment. A native failure or proven prompt non-delivery does not prove the native runtime or auxiliary writer has stopped. PrepareFollowup can create new retry authority and return the project/task to ready/running while source writer state remains unconfirmed.

`TestStage53ReviewInfrastructureNeedsContainedWriter` seeds a failed/unconfirmed source with not_attempted submission and no normalized events. PrepareFollowup successfully creates an infrastructure attempt. This proves incorrect preparation/progression; the test does not claim to have bypassed coordinator fencing to run two actual writers.

Correction: require terminal/contained source state and reconcile outstanding effects and budget segments before creating a follow-up. Recheck safety in the preparation transaction and at dispatch, and preserve uncertainty instead of treating an empty normalized event table as complete proof of no tool effects.

## R7 — P2: exact resume cannot continue checkpointed partial work

Locations: `internal/supervisor/recovery.go:406–408`; `internal/supervisor/runner.go:303–305`.

PrepareExactResume copies the original run's repository baseline and initial attempt kind. validateStart then demands that same original baseline and explicitly rejects dirty initial attempts. Consequently a normal interrupted coding run with preserved edits can be classified exact-resume eligible and receive a new generation, but cannot actually dispatch. The permanent successful exact-resume test keeps the interrupted checkout clean.

`TestStage53ReviewExactResumeWithPreservedEdits` creates partial src/result.txt bytes, records contained interruption and a verified checkpoint, supplies matching synthetic history and obtains an eligible exact-resume choice. Preparation succeeds, but Run rejects it with repository repo baseline no longer matches before native resume.

Correction: bind a resume generation to explicitly verified recovery workspace state, preserving the original run baseline separately for provenance/clear. Revalidate that recovery state and native-history identity immediately before resume. Do not fix this by dropping baseline checks or clearing partial work automatically. If a layout/history cannot resume safely, report that at eligibility selection rather than preparing unusable authority.

## Validation, evidence and limits

- Independent Linux `make check`, `make check-race` and `make build` passed on the submitted implementation before adding probes.
- The Makefile race target omits the new checkpoint package. An additional independent `go test -race ./internal/checkpoint` passed in 7.342 seconds. Add that package to normal race coverage; passing race detection does not establish filesystem CAS safety.
- Seven focused disposable probes failed the intended safety/correctness assertions described above. Retained inert sources: [checkpoint_test.go.txt](review-probes/checkpoint_test.go.txt) and [supervisor_test.go.txt](review-probes/supervisor_test.go.txt). Copy them into the respective internal packages as stage53_review_test.go in a disposable checkout, and run `go test ./internal/checkpoint ./internal/supervisor -run TestStage53Review -count=1 -v` with the repository Go environment. These assertions are expected to fail on `5617679`.
- Writer-state probes explicitly seed disposable database state, as do existing recovery tests; they are not live process qualification. The pathname probe exercises the actual helper boundary with a deterministic replacement. No real user data was used as a victim.
- Reviewed the control, checkpoint codec/apply, recovery-choice/follow-up, migration and CLI changes and their tests. This is not a claim to have exhaustively proven every other interleaving. Historical migration files were unchanged in the submitted diff; existing store tests pass.
- Submitted native macOS results were read, not independently rerun. No live harness, provider, Docker qualification, credential use or production activation occurred. No implementation fixes, commits or pushes were made.

Fix and independently re-review these contracts before relying on them as accepted Stage 5.4 recovery prerequisites. Unaffected quality/check design can proceed independently; the shared live qualification gates remain unchanged.


## Independent follow-up of 9d095fd and a0afb05

Reviewed 2026-09-21 at documentation HEAD `1bd141e`. **Changes requested: three new P2 regressions (R8–R10). Stage 5.3 is not accepted. Production dispatch remains disabled.** Original findings and reproduction files above are retained as historical evidence.

### Original finding disposition

The specific original R1–R7 defects are addressed by the submitted code and passing permanent regressions: durable effect/outcome stop fencing and dispatcher retirement (R1); live full-set recovery ownership and terminal writer containment (R2); result-derived ownership and immutable baseline verification (R3); descriptor-relative no-follow worktree/index/object apply with index locking (R4); staged-object reconstruction before index exposure (R5); contained-source checks outside and inside follow-up preparation (R6); generation-specific checkpoint baselines and dispatch-time native-history validation (R7).

These closures apply to the reported offline defects, not blanket guarantees of filesystem concurrency safety or live harness recovery. The remediation introduces the following independently reproduced failures in the R3/R4/R7 paths. They block slice acceptance despite closure of the original examples.

### R8 — P2: clear rejects its own interrupted progress and completed receipt

Locations: `internal/checkpoint/recovery.go:274–283` and `:495–510`; misleading interruption coverage at `internal/checkpoint/manager_test.go:199–213`.

`clearOwnership` demands that the entire current repository still matches the completed execution fingerprint before looking up the command receipt or consulting per-path progress. After any effective clear mutation, that fingerprint necessarily changes. Retrying a crash after apply is rejected with `repository repo changed after validated execution result`; even replaying a fully completed clear fails with that error. The interrupted operation remains partially applied rather than reconciling its durable journal. No loss of the saved recovery copies was observed.

`TestStage53FollowupClearRecoveryAfterMutation` reproduces both cases using the authorized permanent fixture and persisted result evidence. The interruption specifically fires at `after_apply:`, before progress persistence. The existing permanent round-trip test fires on the first hook; adding `before_apply:` in this remediation moved that test's failure before mutation, so it no longer proves its stated post-apply recovery behavior.

Correction: establish and durably bind result-derived ownership when authorizing a new clear, then recover an existing exactly-bound command against its original authority and per-path expected/desired states. Retain live owner/writer checks and reject unrelated edits, changed arguments, substituted checkpoints and mixed bytes. Do not simply remove ownership checks. Test post-apply/pre-journal and post-journal crashes, completed receipt replay, and unrelated edits during recovery, including multiple roots. Make the permanent fault selector name the intended boundary explicitly.

### R9 — P2: missing parent directories are mistaken for conflicting edits

Locations: `internal/checkpoint/recovery.go:308–312`, `:434–438`, and `:699–716`.

`currentPath` now delegates to `confinedParent(..., false)`, which conflates an absent intermediate directory with symlink/replacement errors. Deleting the directory containing a tracked file is a normal captured deletion. When clear attempts to undo that result-proven deletion, its preflight rejects `dir/item.txt` as a concurrent/mixed edit instead of recognizing the absent path and recreating the parent safely. The create-capable apply traversal is never reached. Restore uses the same path inspection helper and should receive equivalent coverage.

`TestStage53FollowupClearDeletedDirectory` saves the baseline, deletes only the disposable fixture's tracked `dir`, saves the resulting state, records validated result evidence, and invokes authorized clear. It fails with `clear compare-and-swap rejected concurrent or mixed edit at dir/item.txt`.

Correction: distinguish an ENOENT intermediate component beneath a verified root from an invalid root, symlink, non-directory or identity replacement. Model the former as an absent leaf during comparison; create missing parents through confined descriptors only during authorized apply. Test whole tracked-directory deletion, restoration into absent nested parents, and repeat the existing symlink/root-replacement negative cases. Preserve conflict handling for real divergence.

### R10 — P2: globally unique snapshot digest prevents repeated exact resume

Locations: `internal/store/migrations/project-008.sql:6` and `internal/supervisor/recovery.go:460–462`.

The recovery snapshot digest hashes repository JSON alone, but migration 008 makes it globally unique. Two resume generations over the same unchanged workspace legitimately have identical snapshots. After a second contained interruption without file changes, classification again returns exact-resume eligible, but preparation fails with `UNIQUE constraint failed: generation_recovery_snapshots.digest (2067)`. Generation identity already supplies the required primary-key uniqueness; content equality is not duplicate execution authority.

`TestStage53FollowupRepeatedExactResumeUnchangedWorkspace` prepares the first resume, models another contained interruption using durable synthetic run/generation/session state, obtains a second eligible choice, and reaches the failed second preparation. It uses no live native resume and does not claim to qualify native session behavior.

Correction: permit identical content digests across distinct generation rows while preserving per-generation immutability and checkpoint binding. Since 008 is already committed/applied in submitted environments, use a forward migration preserving populated rows and historical migration digests. Test two unchanged-workspace resumes, populated upgrade preservation, and rejection of mutation to existing generation authority.

### Independent validation and implementing-agent handoff

- Submitted HEAD passed independent Linux `make check`, `make check-race` (now including checkpoint), and `make build` before review probes were added.
- All four new failing cases reproduced normally, then twice under `go test -race`; failures were behavioral assertions, with no reported data races.
- Retained exact probes: `review-probes/followup_checkpoint_test.go.txt` and `review-probes/followup_supervisor_test.go.txt`. Copy each to its corresponding package as `stage53_followup_review_test.go`, then run `go test ./internal/checkpoint ./internal/supervisor -run TestStage53Followup -count=1 -v` using the repository's pinned Go environment. They reuse the permanent test fixtures and expect the corrected behavior, so they currently fail. Executable temporary copies were removed after review.
- Historical migrations 001–007 are unchanged in the remediation diff; new 008 is the schema implicated in R10.
- Native macOS/cross-build evidence remains the implementer's submitted evidence; this follow-up did not rerun those checks. No models/providers, Docker qualification, real recovery mutation, purchases, push or production activation were used.
- Fix R8–R10, retain the R1–R7 safeguards, add permanent regressions reaching these precise boundaries, then request independent follow-up. Stage 5.4 must not treat Stage 5.3 recovery as accepted. Live interrupted-session evidence and all existing production gates remain separate and open.


## Independent follow-up acceptance of a182152

Date: 2026-09-22. Reviewed implementation `a182152` and documentation HEAD `ef3243b` against R8–R10, with the R1–R7 safeguards retained. **No new blocking findings. R1–R10 are closed at the reviewed offline scope; Stage 5.3 offline implementation is independently accepted.** Historical findings above describe earlier revisions and remain preserved for traceability. This is not live interrupted-session qualification and does not authorize production dispatch.

### Closure evidence

- **R8 closed.** Clear resolves its argument/actor-bound command receipt before choosing initial authorization versus replay. Initial calls still require result-fingerprint ownership. Replays reconcile the current captured/already-applied path and index states, check the persisted action count and each action's expected/desired digests against immutable checkpoints, and reject unrelated edits. The permanent interruption test now explicitly reaches `after_apply`, and coverage includes post-progress interruption, completed receipt replay, unrelated edits and multi-root recovery. Live reservation and contained-writer checks continue to wrap every call.
- **R9 closed.** Only an ENOENT intermediate component beneath an opened root becomes absent-path evidence. Invalid root opens, symlinks and non-directory parents remain errors. Apply recreates missing parents through held no-follow descriptors and checks the enrolled filesystem identity. Permanent clear and restore tests cover deleted tracked parents and absent nested destination parents; the existing parent-replacement regression remains in the passing suite.
- **R10 closed.** Forward migration 009 copies populated generation recovery rows, replaces global digest uniqueness with a nonunique index, retains generation primary-key and checkpoint foreign-key authority, and recreates both immutability triggers. Migrations 001–008 are unchanged in this correction. Populated-v8 upgrade and repeated unchanged-workspace resume regressions pass.
- **R1–R7 remain closed.** This correction does not alter supervisor stop/outcome fencing, follow-up source containment, native-history revalidation or staged-object reconstruction. Inspection of the changed recovery paths confirms that coordinator authority, immutable baseline/result ownership and descriptor-relative apply guards remain in place. Their permanent regressions are included in the full passing suites. This statement does not extend those offline tests to unqualified native providers/runtimes.

### Independent validation

- Restored the exact retained `followup_checkpoint_test.go.txt` and `followup_supervisor_test.go.txt` probes temporarily into their respective packages. All four previously failing cases now pass.
- Linux `make check`, `make check-race`, and `make build` passed with those probes present. The full check ran outside the restricted sandbox for the boundary test's loopback listener.
- Two additional focused race repetitions passed for the retained probes, post-apply/post-progress/completed clear replay, unrelated edits, multi-root clear, absent-parent clear/restore and populated-v8 migration.
- `make build-boundary` and `make cross-build` passed for Linux/macOS amd64/arm64.
- Removed only this review's temporary executable copies after verifying that their bytes still matched the retained evidence. Earlier reports/reproduction files remain preserved.
- Native macOS evidence is the implementer's submitted exact bundle `799d46aa24427b4f557cbe5de0265dfa15afad233668599652b891983d8fe50b`; this follow-up did not independently rerun native macOS checks. Cross-compilation is not native runtime validation.

### Next-agent handoff

Stage 5.4 may now consume the accepted offline recovery/checkpoint contracts. Read `docs/stage-5/5.4-quality-and-acceptance.md`, the Stage 5.3 results, and this acceptance section before proceeding. Keep synthetic coverage, independent offline acceptance and live qualification distinct. Live interrupted Codex/Hermes history, safe contained Codex subscription routing, independent provider-idle proof, shared Mac/WSL capacity authority, and the advertised live recovery matrix remain open gates. Production dispatch remains disabled. No model/provider calls, purchases, real-checkout recovery mutations, pushes or production activation were performed during this review.
