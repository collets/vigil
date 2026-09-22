# Stage 5.4 independent security/correctness review

Latest verdict: [follow-up of 253efd2](#independent-follow-up-of-253efd2) leaves R2 (P1) and R10 (P2) open. R1/R3/R4/R5/R6/R7/R8/R9 are closed for the reported offline defects. Earlier sections are historical.

Date: 2026-09-22. Reviewed `4b48737..405b8c9`; implementation head `03f65e8`.

**Verdict: changes requested. Seven P1 and two P2 findings. Stage 5.4 is not independently accepted.** IDs in this report are local to Stage 5.4; the accepted historical Stage 5.3 R1–R10 remain separate. Do not begin Stage 5.5 on the assumption these quality contracts are accepted. Production dispatch must remain disabled; R1 is a concrete hole in the claimed check-dispatch gate, not authorization to use it.

All findings below have disposable-fixture reproductions in `stage-5.4-review/review_test.go.txt`. The tests assert the intended safe behavior and therefore fail on the submitted code. They reuse the permanent `quality_test` helpers, without models, providers, Docker or real-project mutations.

## R1 — P1: check execution bypasses fixture and live qualification gates

Locations: `internal/checks/runner.go:119–122,405–406,462–468`; CLI entry at `internal/cli/project.go:666–700`.

Check preparation accepts both `fixture` and `qualified_runtime`, but verifies neither the disposable marker nor a trusted runtime qualification. Both actors use the same direct host `exec.CommandContext` path. The CLI's `--synthetic-fixture` switch forwards `fixture` without checking the promised marker either. Live owner claims prevent conflicting Vigil owners; they do not provide execution containment or qualify an arbitrary host command. An isolated working directory is not a process security boundary.

`TestStage54ReviewUnqualifiedCheck` removes the marker from an otherwise valid disposable fixture and invokes each actor. Both launch and persist `pass`, with no qualification evidence. No real production repository was touched.

Correction: fail closed in the core runner for unsupported production execution and verify every fixture root before any external effect. Production checks must require the exact trusted boundary/driver binding and applicable qualification rather than a caller-provided actor string. Keep the CLI consistent with that core policy. Add unmarked-root, substituted-actor and unqualified-runtime negative tests that prove no child was started.

## R2 — P1: a check can pass while its descendant remains alive

Locations: `internal/checks/runner.go:371–375,405,451–468`.

`exec.CommandContext` owns the direct child only. The runner has no descendant containment/join protocol, yet assigns `contained_stopped` before entering `runHeld` and uses that proof on release even for errors. A command can spawn a background child with redirected output and exit successfully; the check returns `pass` with the child still running. Descendants retaining output pipes can also keep `Run` waiting beyond the intended direct-child timeout. This breaks bounded termination and safe ownership release independently of R1's qualification admission bug.

`TestStage54ReviewCheckLeavesDescendant` runs a disposable shell that launches `/bin/sleep 30`, redirects the child's streams, records its PID and exits. The runner returns `pass`; a PID liveness check succeeds. The probe immediately kills only that test-owned sleep child.

Correction: use a containment mechanism capable of stopping and proving retirement of the whole authorized process tree; retain/quarantine claims on uncertainty. Derive release proof after independent containment, including success, timeout, cancellation and storage failures. Bound output-drain/termination waits and test both detached-output and inherited-output descendants. Do not equate parent exit or context cancellation with writer containment.

## R3 — P1: modified source in the evaluated copy still approves the original fingerprint

Locations: `internal/checks/runner.go:178–236,436–440,495–505`.

The runner copies the checkout and allows the check to write the entire copy, then re-observes only the original checkout. A check can modify its copied source before testing and return success; the resulting pass is bound to the unchanged original source fingerprint, even though those are not the bytes evaluated. There is no verification that the initial copy exactly matches the bound source set or a post-check distinction between approved build outputs and source mutation.

`TestStage54ReviewMutatedEvaluatedCopy` uses an approved fixture command that overwrites `src/input.txt` in the isolated copy. It receives `pass` for the original repository digest.

Correction: construct and verify the exact evaluated repository set, protect source from writes or detect changes in the evaluated source tree, and isolate explicitly allowed generated/build output. A check that evaluated changed source must not approve the earlier tree. Cover copied-source mutation separately from the existing original-checkout mutation hook, including all participating roots.

## R4 — P1: late review completion overwrites an explicit human stop

Locations: `internal/review/review.go:348,419–426`; human stop at `internal/quality/actions.go:143–144`.

Review completion updates task state by ID without rechecking the task's current state, authority epoch or control decision. Recording `stop` during the reviewer callback sets the task to `stopped`, but the later successful review overwrites it with `awaiting_human`. Failure branches similarly write `needs_repair` unconditionally. The full-duration owner lock does not serialize typed human commands, which do not take that lock.

`TestStage54ReviewLateReviewOverwritesStop` records a fixture-human stop from inside a running review and then returns a pass. The final task state is `awaiting_human`.

Correction: fence review/assessment dispatch and terminal progression against durable stop/revision authority, retaining terminal evidence without undoing human decisions. Recheck allowed state and authority transactionally at persistence. Test stop/request-changes/criteria changes during both passing and failing review, and ensure no late result revives stopped work.

## R5 — P1: interrupted quality effects consume no budget and do not block fresh dispatch

Locations: `internal/quality/recovery.go:14–40`; `internal/checks/runner.go:440,554–576`; `internal/quality/budget.go:10–25`.

Quality time is charged only in the successful terminal-result transaction. There is no durable active segment or conservative crash-gap charge for a check/review/assessment that ran but failed before persistence. `RecoverUnfinished` marks effects uncertain without charging their elapsed/unknown time or establishing writer retirement. A fresh command ID can start another check while the prior effect remains uncertain. Copy/observation time is also outside the check's measured subprocess interval. Repeated failure gaps can therefore evade the cumulative allowance rather than preserving Stage 5.3's conservative accounting.

`TestStage54ReviewCrashBudgetAndRedispatch` runs an actual check, fails at `before_source_recheck` after an additional delay, invokes recovery and observes unchanged charged+unknown budget. A second command immediately executes and returns `pass` while the first effect is unresolved. This is a controlled post-process persistence failure, not a literal OS crash.

Correction: durably reserve/start quality budget segments before effects, account all active work and conservative restart gaps, and reconcile uncertain effects/writers before new execution across every scope for the target/resource set. Charge errors, cancellation, malformed output and failed persistence too. Recheck allowance before granting terminal success. Test repeated controller loss, new command IDs, scope changes and assessor/reviewer failures without resetting cumulative ledgers.

## R6 — P1: acceptance does not atomically recheck manual/evidence gates

Locations: `internal/quality/actions.go:449–458,491–535`.

The final `gates` call executes before `DB.Command` obtains its write transaction. Inside that transaction only task/plan state and its revision are checked; current manual/human results, check/review records, configuration/profile authority, unresolved effects and budget revisions are not revalidated. Those can change without changing task revision/state. The artifact committed with acceptance is also built from the earlier gate manifest, while the final gate manifest is discarded.

`TestStage54ReviewAcceptanceTransactionRace` uses a second WAL connection to insert a legitimate pending manual-result row under a held write transaction. The acceptor's read phase sees the earlier pass; after the writer commits, acceptance acquires its transaction and still accepts. The manual gate is already pending at acceptance commit. The probe uses an explicit delayed competing transaction, not a mutated production hook or forged acceptance row.

Correction: perform a transaction-local authority/version check of every selected gate and decision, or use a comprehensive quality revision/epoch that every relevant mutator advances and acceptance compares atomically. Bind the committed manifest to those exact selected records. Keep filesystem/artifact effects outside the transaction while binding their observations to the transaction's authority. Add a race at the actual gate-read/write-transaction boundary; the existing `before_final_observation` mutation tests alone are insufficient.

## R7 — P1: plan acceptance trusts task evidence already known to be stale

Locations: `internal/quality/actions.go:393–424`; `internal/quality/scope.go:340–401`.

Plan acceptance checks task state, a non-invalidated acceptance row, repository digest and the top-level manifest artifact. It does not compare each accepted task's current criteria/configuration/profile/instruction scope or its underlying evidence. Central staleness recording only appends diagnostics; it does not retire those acceptance rows or make this plan gate consult the staleness. Thus fresh plan evidence can carry a stale task acceptance into `finalizing`.

`TestStage54ReviewPlanAcceptsStaleTask` accepts a task, changes its reviewer profile/instructions, verifies the task scope is stale and explicitly calls central staleness recording. It then obtains fresh plan-only checks/review/manual/human decisions. Plan acceptance still succeeds without requalifying the task evidence.

Correction: make stale task acceptance non-authoritative for plan gates, with a coherent workflow to refresh affected task evidence and reaccept. Verify all current child scope bindings and required artifact dependencies before plan acceptance, and recheck those bindings transactionally. Do not require unrelated evidence to be discarded for harmless plan reorder. Cover changed profiles/configuration, stale manual results, corrupt child evidence and multi-task plans.

## R8 — P2: exhaustion source uniqueness is enforced after the external assessment

Locations: `internal/quality/assessment.go:107–117,135–168`; `internal/store/migrations/project-012.sql:15`.

An assessment gets an effect ID derived only from the new command ID. The source-bound uniqueness constraint exists on the terminal assessment row, so a second request for the same already-assessed repair exhaustion invokes the assessor again before the INSERT fails. Its elapsed time is then rolled back with that failing transaction. Timed-out or malformed first attempts leave even less terminal evidence to prevent another invocation.

`TestStage54ReviewAssessmentInvokedTwice` supplies two distinct command IDs for the same persisted fixture repair exhaustion. The assessor callback executes twice; only terminal uniqueness rejects the second result.

Correction: reserve the exact exhaustion-source authority before external dispatch, independently of command IDs, and distinguish receipt replay from uncertain prior effects. Reject already observed or unresolved assessments before calling the driver. Preserve budget charges on failure and bind any explicitly authorized reassessment to new authority rather than silently retrying the same source.

## R9 — P2: an authorized baseline failure cannot progress to review

Locations: `internal/checks/runner.go:577–578`; `internal/quality/gates.go:196–240`; `internal/review/review.go:173–179`.

A failing check immediately moves its task to `needs_repair`. Baseline authorization can then make `EvaluateChecks` satisfied as `accepted_baseline`, but does not reconcile the task state. Review admits only `checking` or `reviewing`, so the explicitly approved baseline still requires an unnecessary repair or out-of-band state change. If other required checks remain, their checking-state guard blocks them too.

`TestStage54ReviewAuthorizedBaselineCannotAdvance` records a real failing fixture check, explicitly authorizes its exact failure identities, confirms all check gates are satisfied, then fails to launch review with `task is not ready for review`.

Correction: evaluate the authoritative set of classified check gates when advancing the quality workflow. An exact human-approved baseline may remain visibly unhealthy while allowing remaining checks/review; any new/unclassified failure must still block. Add a complete baseline-authorization → remaining checks → review cycle and ensure exception authority remains narrowly fingerprint/definition/failure-bound.

## Independent validation and limits

- Submitted Linux `make check`, `make check-race` and `make build` passed. Boundary loopback tests required normal permissions outside the restricted sandbox.
- `make build-boundary` and all four `make cross-build` targets passed.
- All nine retained review probes (ten cases counting both actor variants) failed at their intended behavioral assertions in two focused `-race` repetitions, including the competing-WAL acceptance race. No race-detector data-race diagnostics were reported. Ordinary focused runs also reproduced R1–R8; the final baseline-progress probe was exercised in both race repetitions. These are offline defects, not live qualification evidence.
- Tests used disposable repositories and synthetic review/assessment callbacks. The only additional background process was a test-owned sleep child, killed immediately after its liveness observation. No models/providers, Docker, hosting, purchases, real-project recovery, pushes or production activation occurred.
- Native macOS validation remains the implementer's submitted exact-commit/bundle evidence; this review did not rerun native macOS. Historical installed migration files 001–009 are unchanged in the submitted diff. No new independent populated-009 migration probe was added in this review; retain that validation obligation in remediation.
- Temporary executable probes are removed after validation and retained as inert source. To rerun, copy `docs/research/stage-5.4-review/review_test.go.txt` to `internal/quality/stage54_review_test.go` and run the pinned Go tool with `go test ./internal/quality -run TestStage54Review -count=1 -v` (optionally `-race`). The existing permanent fixtures provide setup helpers.

## Implementing-agent handoff

Fix R1–R9 in reviewable checkpoints. Prioritize admission/containment and evidence/acceptance authority before expanding live routes. Preserve the earlier Stage 5.1–5.3 safeguards; do not weaken fail-closed checks to make fixtures pass. Convert these reproductions into permanent regression tests and extend them to the failure paths named above. Use forward migrations and validate populated upgrades, digest safety and immutable evidence. Update results and request independent follow-up; do not self-accept Stage 5.4, start Stage 5.5, push, incur additional spend or enable production dispatch.

## Independent follow-up of 0993a24

Date: 2026-09-22. Reviewed remediation `c224826..0993a24`, documentation HEAD `959818e`.

**Verdict: changes requested. R2 and R5 remain open (P1); R8 remains open for populated upgrades (P2); new R10 is P2.** R1, R3, R4, R6, R7 and R9 are closed for the originally reported offline defects. Stage 5.4 is not accepted; production dispatch remains disabled.

The original permanent reproductions pass, including the competing-WAL manual-gate race, both stop outcomes, source mutation, baseline progression and stale child acceptance. The acceptance epoch and transaction-local state fences close the demonstrated R6 race; the committed manifest now uses the final gate selection. Fixture-only check admission closes R1. The new defects below are not reasons to remove those protections.

### R2 remains open — P1: process-group absence is not descendant containment

Locations: `internal/checks/runner.go:387–405,578–590,678–680`.

The new runner kills and observes only the original Unix process group. A child that creates a new session leaves that group and survives while the group becomes absent; the check is recorded as a pass. Separately, even when `runContained` reports `contained=false`, `runHeld` converts that to a status string and can return a successfully persisted result with nil error. `Run` then sets `releaseProof=contained_stopped` and permits release. A terminal error result is not proof that writers stopped.

`TestStage54FollowupEscapedSession` launches a test helper that starts only a disposable `/bin/sleep 30` child with `Setsid:true`. The parent exits, the runner returns `pass`, and the detached child is still alive. The probe kills that exact child immediately. This demonstrates an escaped session, beyond the fixed original same-group child case.

Required correction: use a lifecycle boundary that actually covers the supported descendants, or leave unsupported containment explicitly unavailable/uncertain. Never issue a containment proof from process-group absence alone or from successful error-result persistence. Propagate containment uncertainty independently of check status, retain/quarantine ownership and block downstream work. Include detached sessions, timeout/failure, denied/ambiguous observations and the explicit `contained=false` return path in regression coverage. Production qualification remains separate; these fixtures do not qualify host execution.

### R5 remains open — P1: post-process observation and persistence escape charging

Locations: `internal/checks/runner.go:660–712,730–736,762`; corresponding terminal timestamp handling in `internal/review/review.go` and `internal/quality/assessment.go`.

Durable segments now charge crash gaps and prevent the original fresh-command redispatch, but a normal check fixes `ended` immediately after subprocess retirement. Source-copy verification, required-output reads, original-source observation, artifact persistence and any intervening delay occur later. Both exhaustion evaluation and segment closure use the earlier timestamp. Thus a check can exceed the remaining allowance, still return pass and leave most of that allowance available. The comment claiming the whole preparation/copy/observation interval is charged is inaccurate.

`TestStage54FollowupPostprocessBudget` gives the task 500 ms remaining, runs `/bin/true`, and delays the existing `before_source_recheck` hook by 800 ms. The check returns `pass`, charges only a few milliseconds, and leaves approximately 490 ms available. The hook models slow observation/persistence; it does not bypass a driver boundary.

Required correction: bound and account the complete active quality interval through authoritative terminal persistence, using a current terminal time and transactionally enforced remaining allowance. Apply the same rule to checks, reviews and assessments, including artifact/storage delay. Preserve terminal evidence without granting success after exhaustion. Add delayed observation and delayed final-transaction tests alongside the now-passing crash-gap tests.

### R8 remains open on upgrade — P2: migration 013 forgets already consumed assessment sources

Locations: `internal/store/migrations/project-013.sql:22–34`; `internal/quality/assessment.go:125–138,157–161,198–201`.

Migration 013 creates empty source-reservation and budget-segment tables. Existing v12 assessment records and executing effects are not imported into these authorities. For an already observed repair-exhaustion assessment, a new command after upgrade reserves the apparently unused source and invokes the assessor again. Only the old terminal uniqueness constraint rejects its result afterward. The new pre-effect reservation works on fresh databases, but does not close the reported duplicate-invocation defect for populated installations. The new recovery query also joins only effects with new segments, so pre-013 executing effects need an explicit conservative migration/reconciliation policy.

`TestStage54FollowupPopulatedV12Assessment` reconstructs a populated v12 fixture by removing only migration-013 additions/metadata, retaining an observed assessment and its other records. Reopening applies the real migration 013. A new command for the same source invokes the callback a second time before failing with the existing `supervisor_assessments_v2` uniqueness constraint.

Required correction: add a forward migration/reconciliation path that preserves consumed and uncertain source authority from historical effects/results, including multiple old scopes for one task/source. Conservatively account or block legacy running effects without segments; never infer non-occurrence from an empty new table. Keep historical migration digests unchanged and add populated-v12 observed, failed and executing upgrade regressions.

### R10 — P2: permission normalization makes ordinary files fail copy equivalence

Locations: `internal/checks/runner.go:258,316,619–633`.

`copyProject` masks file modes with `0700`, whereas `treeDigest` hashes the original full permission bits. A normal `0644` source file is copied as `0600`; the new equivalence check then declares source mutation before executing the check. The fixtures mostly use `0600`, hiding this common fresh-checkout case. Ordinary `0755` executables have the analogous mismatch.

`TestStage54FollowupRegularPermissions` changes only a fixture source file to `0644`, then runs its unchanged passing check. The result is `source_mutated` with no execution error.

Required correction: choose a consistent, documented source-mode equivalence policy. Either preserve required modes in the private isolated tree or compare the same approved normalization on both sides while preserving executable semantics. Do not remove byte/symlink/source-mutation validation. Cover `0644`, `0755` and existing private-mode fixtures on both platforms.

### Follow-up validation and implementing-agent notes

- Independent submitted-suite Linux `make check`, `make check-race`, `make build`, `make build-boundary` and all four cross-builds passed before adding follow-up probes.
- All four new cases reproduced in ordinary focused runs and twice under `-race`. Every permanent `TestStage54Review` regression passed in both focused repetitions. The combined focused command failed only at the four new behavioral assertions; no race-detector data-race diagnostics were reported.
- Retained probes: `stage-5.4-review/followup_test.go.txt`. Copy to `internal/quality/stage54_followup_test.go` and run `go test ./internal/quality -run TestStage54Followup -count=1 -v` using the pinned Go environment. The helper test is a subprocess fixture, not a separate finding. Executable review copies are removed after validation.
- Historical migration files are unchanged in the remediation diff; only migration 013 was added. The independent populated-v12 probe above exposes a semantic preservation gap despite the passing submitted migration tests.
- Native macOS results remain the implementer's submitted evidence for `0993a24776404dac00df39ca8d25d2c2c1c2f68f`, bundle `5e0cb97c4b9ea9a6c5a209af502075b648e010aecf6bee8704a55eaf8d3bb874`; native tests were not independently rerun here.
- Remediate R2/R5/R8 and R10, retain the six closed findings' safeguards, and request independent follow-up. Do not begin Stage 5.5, enable production dispatch, incur extra spending or push. No models, paid/provider calls, Docker, hosting, real-checkout recovery or production activation occurred during this review.

## Independent follow-up of 253efd2

Date: 2026-09-22. Reviewed implementation `ff0d9c0..253efd2` and documentation `e9056ce`.

**Verdict: changes requested. R2 remains P1 and R10 remains P2. R5 and R8 are now closed for the reported defects; R1/R3/R4/R6/R7/R9 remain closed.** Stage 5.4 is not accepted. Production dispatch remains disabled.

R5 now charges through post-processing/artifact work and checks exhaustion with current ledger values inside the terminal transaction for checks, reviews and assessments. R8's forward migration 014 restores observed assessment-source reservations and conservatively reconstructs missing legacy timing/uncertainty; the populated-v12 regression suite covers observed and unfinished sources. These fixes should be retained.

### R2 remains open — P1: a clean-environment descendant can evade polling

Locations: `internal/checks/process_tracker_linux.go:14–19,32–57`; `internal/checks/process_tracker.go:44–64,100–104`; `internal/checks/runner.go:437–439`.

The Linux containment verdict treats successful `/proc` enumeration as reliable tracking. It discovers children either through sampled ancestry or an inherited environment marker. The marker is not immutable: a child can start with an empty environment, and after its parent exits the ancestry relation disappears. A detached child created and orphaned between samples is therefore absent from both discovery mechanisms. `unresolvedProcessFork` always returns false on Linux, so the incomplete tracked set can still yield `contained=true` and a passing check.

`TestStage54FinalCleanEnvironmentEscape` starts a disposable helper which creates only `/bin/sleep 20` with `Setsid:true` and an explicitly empty environment, writes its PID, and exits. In the ordinary focused run, attempt zero returned `pass` with the child still alive. The probe kills only that exact test-owned child. This is the previously reported detached-child scenario with environment inheritance removed; it does not require guessing a token or changing host configuration.

The false-proof propagation on explicit `contained=false` is fixed in this submission, and the inherited-marker case is covered. The remaining issue is generating a false positive containment proof. Darwin's `groupHadDescendants || tracker.hasDescendants()` also cannot by itself establish that every fork was accounted for; observing one descendant does not resolve an unknown additional fork. That Darwin observation is source analysis, not a native reproduction from this review.

Required correction: do not promote sampling plus mutable environment data into authoritative absence. Use an inherited lifecycle boundary whose membership cannot escape this way, or explicitly fail closed for the unsupported host execution route. Preserve uncertainty/ownership rather than declaring success from a partial tracked set. Do not merely increase polling frequency. Cover empty/filtered environments, rapid reparenting and multiple forks, and bind any process handles used for signalling to process identity rather than assuming a PID can never be reused.

### R10 remains open — P2: file creation still applies the caller's umask

Location: `internal/checks/runner.go:267–276`.

Passing the source mode to `os.OpenFile` fixes the earlier unconditional `&0700` mask only when the process umask permits those bits. Under a common restrictive umask `077`, a `0644` source is still created as `0600`. Directories receive a later chmod but regular files do not, so copy equivalence again rejects ordinary unchanged source.

`TestStage54FinalRestrictiveUmask` sets a disposable source to `0644`, temporarily changes only the test process umask to `077`, invokes the passing fixture check, then restores the prior umask. The result is `source_mutated`. No host-wide setting or user checkout is changed.

Required correction: apply the chosen source permission policy explicitly to the opened destination file after creation, checking errors, rather than relying on creation mode alone. Preserve private temporary-directory isolation and source-integrity validation. Test `0600`, `0644` and `0755` under both permissive and restrictive masks.

### Evidence and next handoff

The retained source is `stage-5.4-review/final_followup_test.go.txt`; copy it to `internal/quality/stage54_final_review_test.go` and run `go test ./internal/quality -run TestStage54Final -count=1 -v` with the pinned environment. The escape probe is scheduling-sensitive and bounded to twelve attempts; a pass under slower instrumentation does not prove absence tracking sound. Its helper only exists to launch an owned disposable sleep process. Temporary executable copies are removed after validation.

Native macOS evidence remains the implementer's submission for `253efd2`, bundle `d7f606d316d05e56c22e9a5e84badeb6088c54497cafe164daedec73e86dcc7a`; this follow-up did not independently execute on the Mac. No models/providers, purchases, Docker qualification, hosting, real checkout recovery, pushes, production activation or Stage 5.5 work occurred.

Next agent: fix R2 and R10, retain all closed safeguards and forward migrations, add the two edge cases to permanent coverage, and request another independent follow-up. Do not treat process polling or passing synthetic suites as live containment qualification.


Final validation for this follow-up: independent submitted-suite `make check`, `make check-race`, `make build`, `make build-boundary` and four cross-builds passed. All permanent `TestStage54Followup` cases passed twice under `-race`. The restrictive-umask probe failed in ordinary and both race repetitions. The clean-environment escape failed on attempt zero in the initial ordinary run, then on attempts zero and four in subsequent ordinary repetitions; both race-instrumented repetitions exhausted twelve attempts without reproducing it. The scheduling-sensitive limitation is retained explicitly, rather than treating race-test passage as proof. No data-race diagnostics were reported. Executable probe copies were removed; only review documentation and inert reproduction source remain changed.
