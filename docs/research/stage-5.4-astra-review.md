# Stage 5.4 independent security/correctness review

Latest verdict: [follow-up of 49b9fbb](#independent-follow-up-of-49b9fbb) closes R10, leaves R2 (P1) open, and adds R11 (P2). R1/R3/R4/R5/R6/R7/R8/R9 remain closed for the reported offline defects. Stage 5.4 is not accepted. Earlier sections are historical.

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


## Independent follow-up of 49b9fbb

Date: 2026-09-22. Reviewed final remediation `49b9fbb67d31dd604da673bd0b92cc9bbb15a27c` and documentation through `e77a17c`, with the prior findings retained as context.

**Verdict: changes requested. R10 is closed; R2 remains P1. New R11 is P2.** Previously closed R1/R3/R4/R5/R6/R7/R8/R9 remain closed for their reported offline defects. Stage 5.4 is not independently accepted; production dispatch remains disabled.

The normal Linux subreaper path closes the demonstrated clean-environment orphan case: the orphan is adopted and retired before normal supervisor exit. The explicit post-creation chmod fixes R10 under the restrictive umask. These improvements should be retained. Successful normal execution does not establish containment after losing the supervisor.

### R2 remains open — P1: supervisor death is accepted as containment completion

Locations: `internal/checks/process_tracker_linux.go:56–57`; `internal/checks/runner.go:419–445,733–735,644–646`; `internal/checks/supervisor_linux.go:51–60,81`.

`processContainmentFailed` rejects only exit status 125. A supervisor terminated by a signal has exit code -1 and is therefore treated as having passed this check. When the polling tracker sees no remaining known processes, `runContained` returns `contained=true` even though the supervisor never finished descendant cleanup/reaping. `runHeld` can persist an ordinary failed check, and `Run` then issues `contained_stopped` and releases its claims. This is false containment authority, even though the check status itself is not pass.

`TestIndependentSupervisorDeathMustBeUncertain` launches only an owned helper which sends SIGKILL to its own supervisor parent and exits. Both ordinary repetitions and both race repetitions return `contained=true` alongside `signal: killed`. The probe deliberately creates no surviving writer; it directly demonstrates the invalid proof on supervisor loss. With descendants, the fallback is the same partial PID/environment polling whose blind spots motivated the subreaper.

This is also relevant to normal cancellation: the outer runner sends SIGKILL after 250 ms, while the supervisor's own termination routine sleeps 250 ms before its SIGKILL phase and can spend further time reaping. The outer deadline can kill the component needed to establish the cleanup proof. Signal handlers are installed only after the approved child starts, and are removed before cleanup, creating further supervisor-loss windows.

Required correction: require positive, supervisor-owned completion evidence after authoritative cleanup, and reject signal termination/missing or malformed evidence independently of command exit status. Keep claims and effects uncertain when the supervisor is lost. Coordinate outer cancellation with bounded supervisor shutdown; escalation that kills the supervisor must never infer successful containment from the old polling mechanism. Include early cancellation, cleanup-time cancellation, explicit supervisor death and detached clean-environment children in permanent tests.

Related source-level gaps remain within R2: `linuxDescendants` (`supervisor_linux.go:123–149`) converts enumeration/read/parse failures to empty or incomplete sets, and `cleanupDescendants` (`88–107`) treats an empty set as absence. Establish child exhaustion through an authoritative mechanism and propagate observation failure. PID-only signalling still lacks stable process identity. Darwin's unchanged `unresolvedProcessFork || observed-group/descendant` exception at `runner.go:444` still allows one observed child to excuse an unaccounted additional fork; there was no independent native reproduction in this follow-up. Do not describe these source observations as newly reproduced escapes.

### R11 — P2: configuration-pipe read descriptors have no explicit retirement

Locations: `internal/checks/process_tracker_linux.go:29–49`; `internal/checks/runner.go:385–395`.

`prepareProcessContainment` appends the read end of a newly created pipe to `command.ExtraFiles`. Neither the successful start path nor the failed start path closes the parent's read end. `os/exec` does not own/close caller-supplied ExtraFiles in the parent. Consequently every check retains a descriptor until nondeterministic garbage collection; a long-running scheduler can exhaust its descriptor budget independently of concurrent execution limits. Start failures can also leave a configuration writer blocked if its payload exceeds pipe capacity and the unclosed read end has no reader.

`TestIndependentSupervisorPipeDescriptors` runs twelve successful `/bin/true` checks with automatic garbage collection disabled only for the test and restored afterward. The open descriptor count grows by exactly twelve in both ordinary and race repetitions (for example 7 to 19). Disabling GC exposes the missing explicit close; the claim is not that these descriptors survive all future garbage collections.

Required correction: give the preparation resources an explicit cleanup lifetime. Close the parent's inherited read descriptor immediately after Start, close both ends on preparation/start failure, and bound or join the configuration writer so failed launches cannot leave blocked goroutines. Cover repeated successful checks and failed starts with payloads larger than pipe capacity. Preserve the private, fixed supervisor invocation and command binding.

### Validation and implementing-agent handoff

Retained new probes: `stage-5.4-review/subreaper_followup_test.go.txt`. Copy to `internal/checks/stage54_independent_test.go` and run `go test ./internal/checks -run TestIndependent -count=2 -v`, then the same command with `-race`, using the repository's pinned Go environment. The two behavioral assertions fail on the reviewed implementation; the helper is not an independent finding. Temporary executable review tests were removed. No production source was changed.

Independent native macOS execution was not repeated. The implementer's submitted evidence remains the exact `49b9fbb` bundle `322b3facdb75a3056c346dc770043e9a97127373cb0328db6dc70059cb21b238`; passing native suites are not live runtime qualification.

Next agent: remediate R2 and R11, preserve R10 and all other closed safeguards, add permanent failure-path regressions, and request independent follow-up. No Stage 5.5 implementation, production activation, paid/model calls, push or publication is authorized by this review. Review documentation and inert reproductions are intentionally left uncommitted for a separate review-artifact checkpoint.


## Independent follow-up of cba322b

Date: 2026-09-26. Reviewed implementation `49b9fbb..cba322b` and documentation checkpoint `558c063` (checkout HEAD `4008325`), with every prior finding retained as context.

**Verdict: R2 is closed. R11 is closed. R10 and R1/R3/R4/R5/R6/R7/R8/R9 remain closed. Three new P3 findings and one documentation defect are recorded below; none of them is a containment-escape or a false-proof defect. Stage 5.4 is accepted offline. Production dispatch remains disabled and live qualification remains pending.**

R2's substance is now real rather than asserted. On Linux, containment authority requires an exact, supervisor-written cleanup token produced only after authoritative subreaper cleanup, and the former sampling/environment polling path can no longer contribute to it. I verified the central claim adversarially rather than by reading the passing suite: an approved check that writes the proof token to every descriptor it can reach and then SIGKILLs its own supervisor receives `contained=false`, because the supervisor's private pipes are `FD_CLOEXEC` and the signalled supervisor is rejected independently of exit status. R11's descriptor and configuration-writer ownership is correct on all six lifecycle paths, including the two the submitted permanent tests do not cover.

### R2 — closed: only exact supervisor-owned proof establishes containment

`runContained` (`internal/checks/runner.go:426-484`) computes `contained = lifecycleStopped && reliable && completionErr == nil` at `runner.go:461`. On Linux `containment.authoritative()` is true (`process_tracker_linux.go:187`), so the fallback block at `runner.go:443` is entered only when `completionErr != nil`; polling can therefore never manufacture authority. The remaining work — signalling, drain bounding and reporting — is preserved.

`processContainment.completed` (`process_tracker_linux.go:137-155`) is the sole authority and requires all four conditions independently: a non-nil `ProcessState`; `status.Signaled() == false`; an exit code other than the reserved `125`; and byte-exact equality with `linuxContainmentCompletion` read through a `len+1` limit reader. Each rejection maps to containment uncertainty, `runHeld` returns an error (`runner.go:756-758`), `releaseAllowed` stays false (`runner.go:656`, `runner.go:667-668`) so repository claims are retained, and the effect is marked uncertain (`runner.go:663-666`).

Signal death, missing/malformed proof, observation failure and forced escalation all remain uncertain:

- **Signal death** — `process_tracker_linux.go:141-143`. Probe `TestReviewProbeForgedProofCannotEstablishContainment`: a check that writes the token everywhere and SIGKILLs its supervisor returns `contained=false`, `state=signal: killed`, `supervisor did not exit normally`. The submitted `TestIndependentSupervisorDeathMustBeUncertain` covers the same property and passes.
- **Missing/malformed proof** — `process_tracker_linux.go:151-153`. Verified by inspection; there is no external injection point, so this is not a runtime reproduction. The exact-equality check under a `len+1` limit also rejects a duplicated or truncated token, so a forged write concatenated with a real one fails closed rather than passing.
- **Observation error** — `discoverCheckProcesses` (`process_tracker_linux.go:193-255`) returns errors for `ReadDir`, missing `)` and short/invalid `stat` fields, and `processTracker.scanErr` is sticky (`process_tracker.go:31-44,104-108`), so `reliable()` is false and `contained` is false. Verified by inspection; not runtime-probed, because no external injection point exists. This closes the `linuxDescendants` gap recorded against `49b9fbb` (`supervisor_linux.go:205-251` now propagates `ReadDir` failure, distinguishes `ErrNotExist` from other read errors, and rejects short or unparsable `stat`).
- **Forced escalation** — `runner.go:433-438`. Escalation signals the supervisor's process group with `SIGKILL`, so `status.Signaled()` holds and `completionErr != nil`; the escalation is therefore incapable of yielding `contained=true`.

**The readiness handshake does close the early-cancellation races.** `signal.Notify` is now the first statement of `supervisorMain` (`supervisor_linux.go:27-28`) and `signal.Stop` is deferred to function return (`supervisor_linux.go:29`), so the handler is installed before the approved child starts and remains installed through descendant cleanup and the proof write. At `49b9fbb` it was installed after the child start and removed before cleanup, which is exactly the supervisor-loss window the prior review named. The parent additionally refuses to proceed until it has read the exact readiness token (`process_tracker_linux.go:107-115`), and a missing or malformed token fails the run.

**Cancellation does give the supervisor bounded time to terminate and reap detached descendants.** `tracker.signalRoot(SIGTERM)` targets the supervisor alone (`runner.go:432`, `process_tracker.go:85-87`) instead of signalling the group, and the grace is `processContainmentShutdownGrace()` = 4s (`process_tracker_linux.go:188`), which exceeds the supervisor's own 250ms `SIGTERM`→`SIGKILL` pause plus its cleanup loops (`supervisor_linux.go:123`, `139-181`). Measured: early cancellation completes in 261ms; `TestReviewProbeCancellationDuringCleanup`, which cancels inside the supervisor's own termination phase while a detached, empty-environment descendant ignores `SIGTERM`, is `contained=true` in 12/12 attempts between 469ms and 1.79s; `TestReviewProbeCancellationStartupRace` is bounded and never falsely contained across 30 attempts with `SIGTERM` landing at progressively earlier points of supervisor startup.

Early cancellation legitimately returns `contained=true` when the supervisor genuinely completes cleanup and writes a real proof. That is correct, not a false proof: the alternative — refusing to report containment for a cancellation that was fully cleaned up — would lose the ability to distinguish a clean stop from an unclean one. `status` is separately recorded as `interrupted` (`runner.go:748-749`).

**Descendant signalling uses stable pidfds with start-time identity checks.** `signalObservedProcess` (`supervisor_linux.go:253-282`) opens a pidfd, re-reads `/proc/<pid>/stat`, compares field 19 (`starttime`) against the value captured during enumeration, and only then calls `PidfdSendSignal` on the pidfd. `ESRCH` and `ErrNotExist` are treated as benign exit races; an actual identity mismatch is an error. All three signalling sites use it (`supervisor_linux.go:118-122,131-135,151-155,162-166`), so no signalling path relies on a bare reusable PID. The `49b9fbb` "PID-only signalling still lacks stable process identity" gap is closed.

**The subreaper is a genuinely authoritative descendant boundary, not an improved poll.** Because the supervisor sets `PR_SET_CHILD_SUBREAPER` before starting the approved child (`supervisor_linux.go:65-67`), any descendant orphaned by an intermediate exit is reparented to the supervisor, so the `linuxDescendants` ancestry closure is complete at every snapshot; a descendant cannot become a child of `init` while the subreaper lives. `cleanupDescendants` requires an empty post-reap closure before the proof is written (`supervisor_linux.go:89-97,139-181`). Confirmed empirically: `TestReviewProbeDetachedEmptyEnvChildIsReaped` orphans a `setsid`, empty-environment `/bin/sleep 25` and records its PID; 8/8 attempts find the child retired. Combined with the retained `TestStage54FinalCleanEnvironmentEscape` and `TestStage54FollowupEscapedSession`, the previously demonstrated escape class is closed on the reviewed implementation.

### R11 — closed: configuration, readiness and proof resources have an explicit lifetime

`processContainment` (`process_tracker_linux.go:24-32`) owns all three pipes and the writer channel, and `close()` (`:157-185`) retires every surviving end, is idempotent, and joins the writer. Audited on every path:

| Path | Behaviour | Evidence |
| --- | --- | --- |
| Preparation failure | Each already-created pipe pair is closed before returning (`:56-57`, `:62-65`); the writer goroutine is only started after all three pairs exist | inspection |
| Start failure | `defer containment.close()` (`runner.go:393`) runs; `configRead` is closed first, which makes a blocked oversized write fail with `EPIPE`, then `writeDone` is drained | `TestReviewProbeDescriptorRetirementAllPaths` |
| Normal execution | `started()` closes the parent's `configRead`, `proofWrite` and `readyWrite` immediately after start (`:98-103`) | probe, 10 runs |
| Cancellation | same `close()` via defer | probe, 10 runs |
| Supervisor loss | same `close()` via defer; supervisor death does not orphan the parent's ends | probe, 10 runs |
| Readiness failure / forced escalation | same `close()` via defer | probe, 10 runs |

The submitted permanent tests cover only repeated success and one failed start. `TestReviewProbeDescriptorRetirementAllPaths` extends this to all six: `/proc/self/fd` count after 60 runs across every path stays at the baseline (10 → 10), with the submitted `TestIndependentSupervisorPipeDescriptors` independently confirming no growth with GC disabled.

**Configuration writers are bounded and joined.** The writer goroutine always closes its end and reports through a capacity-1 buffered channel, so it can never block on a receiver (`process_tracker_linux.go:90-93`). The normal case is bounded by a 1s timer (`:119-135`). The timer path closes the *write* end and then blocks on `writeDone`; in practice that path is unreachable as a hang because `started()` has already closed `configRead` at `:98`, which unblocks any pending write with `EPIPE` first. Verified with a 256 KiB configuration — larger than the 64 KiB pipe capacity — on the start-failure path, where the writer retires well inside 2s. No blocked goroutine or descriptor survived any probe. `waitForConfigWriter` deliberately leaves its own bound unenforced for a still-open reader, which is sound only because of that ordering; a future refactor that moves the `configRead` close after the writer join would reintroduce an unbounded wait.

### Linux/Darwin build separation

`internal/checks/process_tracker_linux.go`, `internal/checks/supervisor_linux.go` and `internal/checks/stage54_independent_test.go` are each `//go:build linux`; `internal/checks/process_tracker_darwin.go` is `//go:build darwin` and supplies the full `processContainment` surface plus `activateProcessContainment`, `closeProcessContainment`, `unresolvedProcessFork` and `discoverCheckProcesses`. `GOOS=darwin GOARCH=arm64 go vet ./...` and `go test -c ./internal/checks` both succeed, and `make cross-build` builds `linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64`. The Darwin supervisor and readiness/proof protocol are confined to the Linux files; no Linux-only symbol leaks into `runner.go`.

**Darwin containment behaviour was not weakened.** `authoritative()` returns false on Darwin (`process_tracker_darwin.go:41`), so the fallback branch always runs and `contained` still requires the unchanged `lifecycleStopped && reliable` together with the unchanged `unresolvedProcessFork(pid) || groupHadDescendants || tracker.hasDescendants()` exception (`runner.go:459`). `processContainmentShutdownGrace()` is 250ms on Darwin (`process_tracker_darwin.go:42`) — the same value that was previously hardcoded — and the new root-only `SIGTERM` is still followed by the same `tracker.signal(SIGKILL)` group escalation. The one Darwin-observable difference is that the initial `SIGTERM` now targets the root rather than the whole group, so a `setsid`-escaped descendant gets 250ms of additional grace before the group `SIGKILL`. That is a gracefulness change, not a containment-authority change, and it cannot convert a surviving descendant into a passing result.

The previously recorded Darwin limitation stands unchanged and is **not** a regression: one observed descendant can still excuse an unaccounted additional fork at `runner.go:459`, and the Linux-only `internal/checks` tests do not run natively. It is a distinct, still-open source-level observation. I did not run Darwin natively; see the evidence section.

### New findings

**F1 — P3: the readiness handshake is the only phase of `runContained` that ignores the caller's context.** `internal/checks/process_tracker_linux.go:107` performs an untimed `io.ReadAll` on the readiness pipe, and `started()` runs at `runner.go:399` before any `ctx` is consulted. A supervisor that stalls between `exec` and its readiness write would block the check past its `checkCtx` timeout while `HoldClaims` retains the live owner fence. I could not reach this through any production path: the approved command is not started until after readiness is written, so check code cannot influence this window, and a supervisor that dies produces immediate `EOF` rather than a stall. I initially reproduced a block with `context.Background()`, then established that this was an artifact of the probe — `runHeld` always wraps with `context.WithTimeout` (`runner.go:723`), and 60 attempts against a production-shaped 1s context all returned bounded and never falsely contained. The defect is therefore a missing defence-in-depth bound, not a demonstrated hang. Recommended: bound the readiness read and honour `ctx`, mirroring `waitForConfigWriter`.

**F2 — P3: every signal-terminated check is recorded with exit code 129.** `internal/checks/supervisor_linux.go:98-100` maps a signal death with `128 + (-code)`, but `ProcessState.ExitCode()` returns `-1` for *any* signal, not `-signum`. `TestReviewProbeSignalExitCodeFidelity` shows `SIGTERM` recorded as `129` instead of `143` and `SIGKILL` as `129` instead of `137`; only `SIGHUP` matches by coincidence. This writes a wrong `exit_code` into durable check-result evidence (`runner.go:741-745,790`). It is not an authority defect — status, containment and the reserved-125 mapping are unaffected, and the `125` collision the code is guarding against is real and correctly handled — but recorded evidence should not misreport the observed status. Recommended: use `syscall.WaitStatus.Signal()` for the mapping.

**F3 — P3: the required early-cancellation permanent regression is missing.** The `49b9fbb` follow-up required permanent coverage of "early cancellation, cleanup-time cancellation, explicit supervisor death and detached clean-environment children". `internal/checks/stage54_independent_test.go` covers the latter three (`:92`, `:43`, and `internal/quality/stage54_final_followup_test.go:33`) but not early cancellation. The behaviour is correct — `TestReviewProbeEarlyCancellation` shows a bounded 261ms outcome with a genuine proof — so this is a coverage gap against an explicit requirement rather than a behavioural defect. Recommended: promote the early-cancellation case into the permanent suite.

**F4 — documentation: `docs/next-steps.md:149` is stale.** The "Next concrete action" line still names remediation range `ff0d9c0..253efd2`, which was already reviewed, while `docs/next-steps.md:11` correctly names the `49b9fbb` follow-up against `cba322b`. This has been unchanged since `49b9fbb` and survives at `4008325`, so a resuming agent would be sent to re-review a superseded range. `docs/pending-decisions.md` has the same staleness in the same commit range. Recommended: update both to `cba322b`.

### Residual observations, not findings

- `runner.go:394-398` returns `contained=true` when `command.Start()` fails, without consulting the proof. This is sound: a start failure means no supervisor and no approved child, and Go reaps a child that fails to exec before returning the error (`.tools/go/src/syscall/exec_unix.go:236`). Containment is vacuously established, not assumed.
- `supervisorMain` uses `exec.Command`, which performs a `LookPath` when `spec.Path` contains no separator, against the *supervisor's* inherited environment rather than the approved `spec.Env`. Not reachable today: `resolveExecutable` (`runner.go:516-545`) only returns absolute paths, and a missing absolute path fails before dispatch.
- A single descendant identity-verification failure aborts the entire cleanup rather than retrying (`supervisor_linux.go:274-276` into `:139-181`), so an extremely rare PID-reuse race converts a clean shutdown into uncertainty. This is the safe direction and is not a defect.
- The combined output pipe is outside R2/R11's scope. When containment fails and a surviving descendant still holds its write end, `runner.go:462-469` gives the drain 1s and then abandons the copying goroutine with the descriptor still open. I did not construct that case and am not reporting it as a finding.

### Validation

Pinned Go 1.27.1, Linux. All commands run with the Makefile's environment; `GOROOT` had to be cleared because the ambient value points at an unrelated Go 1.15.2 tree.

```text
go test ./internal/checks -run 'TestIndependent|TestSupervisor' -count=2 -v          pass
go test -race ./internal/checks -run 'TestIndependent|TestSupervisor' -count=2 -v  pass
go test ./internal/quality -run 'TestStage54' -count=1 -v                            pass (all 14)
go test ./internal/checks -count=1                                                  pass
make check                                                                             pass
make check-race                                                                         pass
make build                                                                             pass
make build-boundary                                                                     pass
make cross-build                                                                        pass (4 targets)
git diff --check / git diff --check HEAD                                              pass, no output
GOOS=darwin GOARCH=arm64 go vet ./...                                                  pass
GOOS=darwin GOARCH=arm64 go test -c ./internal/checks                                 pass
```

Every retained Stage 5.4 regression still passes, including the prior-round reproductions: `TestStage54FinalCleanEnvironmentEscape`, `TestStage54FinalRestrictiveUmask`, `TestStage54FollowupRegularPermissions` (`0600`/`0644`/`0755`), `TestStage54FollowupEscapedSession`, both populated-v12 assessment cases, the acceptance race, the baseline-advance case and the descendant/output cases. R10 and every previously closed safeguard are preserved. No data-race diagnostics were reported in any run.

Retained new probes: `stage-5.4-review/r2r11_followup_test.go.txt`. Copy to `internal/checks/zz_review_probe_test.go` and run `go test ./internal/checks -run TestReviewProbe -count=1 -v`. Every R2/R11 property assertion passes against `cba322b`; only `TestReviewProbeSignalExitCodeFidelity` fails, reproducing F2. Helpers act solely on processes they create. The executable copy was removed; no production source was changed.

### Evidence limits and next handoff

Native macOS validation was **not** independently repeated and remains a distinct evidence item: the implementer's submitted `git archive` of `cba322b`, SHA-256 `f2951b2628abb2a61752f16d7fc7ab8de1cdfce12b85a159155d4d31b7257833`, with `make check`, `make check-race`, `make build`, `make build-boundary` and `make cross-build` reported passing on Darwin 25.6.0 arm64. I verified only the build separation and type-correctness of the Darwin configuration; I make no claim about native runtime behaviour, and the Darwin containment exception at `runner.go:459` remains an unverified source-level observation rather than a reproduced escape.

This review establishes offline implementation acceptance only. Live contained Codex/reviewer qualification, provider-idle proof, shared capacity authority, the real runtime crash matrix, real project check/baseline/manual/human decisions and the Darwin fork-accounting limitation all remain visibly pending. No model or paid call, no production dispatch, no Stage 5.5 work, no push and no publication occurred.

Next agent: record F1–F4 and this acceptance in the Stage 5.4 status documents, correct the stale ranges in `docs/next-steps.md` and `docs/pending-decisions.md`, and add the early-cancellation permanent regression. Do not treat this acceptance as live containment qualification, and do not close the shared live gate from synthetic or offline evidence.

### Implementation response to F1–F3

Date: 2026-09-26. This is implementing-agent evidence, not an independent review amendment. Commit `99cd6c0` bounds and joins the readiness read under both caller cancellation and a four-second timeout while retaining exact-token comparison and `FD_CLOEXEC`; maps signal exits with `syscall.WaitStatus.Signal()` while retaining reserved status 125 and its 124 remap; and promotes early cancellation plus signal fidelity into the permanent Linux suite. No Darwin containment logic changed.

The retained `TestReviewProbe` suite now passes in full, including all six descriptor/configuration-writer lifecycle paths and TERM/KILL/HUP/INT exit values 143/137/129/130. The permanent `TestIndependent`/`TestSupervisor` suite passed twice ordinarily and twice under `-race`; retained `TestStage54`, `make check`, `make check-race`, `make build`, `make build-boundary`, `make cross-build` and `git diff --check` also passed on Linux/WSL2 amd64 with Go 1.27.1. Native macOS validation remains separate. Independent follow-up of `99cd6c0` is requested without reopening R2, R11, R10 or any other closed safeguard; the Darwin observation at `internal/checks/runner.go:459` remains distinct and open.
