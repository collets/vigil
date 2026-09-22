# Stage 5.4 independent security/correctness review

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
