# Stage 5.4 implementation results

Date: 2026-09-22; updated 2026-09-26

Scope: offline implementation and fixture validation only

Status: R2/R11 remediation is submitted at `cba322b` after the independent follow-up of `49b9fbb`. R10 and R1/R3/R4/R5/R6/R7/R8/R9 remain closed for their reported offline defects. Stage 5.4 remains unaccepted pending independent follow-up. See the [latest findings and retained probes](stage-5.4-astra-review.md#independent-follow-up-of-49b9fbb). Implementation claims below are not self-acceptance.

Production dispatch: disabled

## Baseline and commit range

The checkout began clean at `4b48737` (`docs(stage5): record independent acceptance`). That commit already isolated the uncommitted Stage 5.3 follow-up report and reproduction sources from the implementation work; the probe copies remain documentation-only `.go.txt` files. The independently accepted Stage 5.3 implementation baseline is `a182152`, with R1–R10 closed offline. `ef3243b` is its documentation handoff.

Stage 5.4 was implemented as these local commits:

| Commit | Purpose |
| --- | --- |
| `7333891` | Expand the Stage 5.4 implementation plan before coding |
| `63726f4` | Add immutable quality scopes, actual checks, fresh review, human/manual records and task/plan acceptance |
| `15b8c66` | Add bounded, owner-fenced exhaustion assessments and migration 012 |
| `e992685` | Add explicit instruction-freshness and source/criteria/check-definition acceptance-race fixtures |
| `03f65e8` | Hold live owner fences through the full check effect and reject ambient executable lookup |
| `405b8c9` | Initial implementation handoff |
| `c224826` | Preserve the independent R1–R9 report and inert reproduction source separately |
| `872be1a` | Close R1–R9 with migration 013, permanent probes and extended failure-path coverage |
| `9558b3e` | Add project-state authority and transaction-local project/plan acceptance fences |
| `0993a24` | Persist rejected/raced acceptance attempts for concurrent state changes |
| `959818e` | First follow-up implementation handoff |
| `ff0d9c0` | Preserve the independent R2/R5/R8/R10 follow-up and inert retained probe separately |
| `f3057aa` | Add terminal budget accounting, migration 014, permission fidelity and descendant tracking |
| `9c31eaa` | Add pre-exec Darwin fork observation |
| `253efd2` | Fail closed when a Darwin fork cannot be tied to a contained group or observed PID |
| `49b9fbb` | Add the Linux subreaper supervisor and umask-independent copied-file modes |
| `cba322b` | Require private supervisor readiness/cleanup proof and retire configuration resources |

Initial review range: `4b48737..03f65e8`. First remediation range: `c224826..0993a24`. Second follow-up remediation range: `ff0d9c0..253efd2`. Final R2/R10 submission: `253efd2..49b9fbb`. Current R2/R11 submission: `49b9fbb..cba322b`. No commit was pushed and no publication or production activation occurred.

## Independent-review remediation

| Finding | Implemented correction |
| --- | --- |
| R1 | Core check admission now accepts only marked disposable fixtures. `qualified_runtime` fails closed until a real qualified execution boundary exists; actor strings cannot substitute for qualification. |
| R2 | Linux tracks the process group, observed ancestry and a per-effect inherited process identity. Darwin stops a fixed wrapper before approved code executes and registers fork observation. Same-group/observed descendants are retired; an unaccounted detached fork is explicit uncertainty, never completion proof. Uncertain paths charge unknown time, retain claims and block downstream gates. |
| R3 | The isolated source tree is digested before and after copying and after execution, with only declared required-output paths excluded. A copied-source mutation records `source_mutated`; the original enrolled set is still re-observed. |
| R4 | Review and assessment terminal state changes are conditional on the exact task revision and allowed in-progress state. Late pass/failure results persist without overwriting stop, request-changes or criteria authority. |
| R5 | Durable per-effect budget segments now close from current time inside the terminal transaction after source re-observation and artifact persistence. Check, review and assessment pass/action enforcement uses that same transaction-local ledger state; uncertainty charges through its own terminal write. |
| R6 | Migration 013 adds a comprehensive quality-authority epoch advanced by every relevant mutator. Acceptance binds the final gate manifest, compares the epoch before/after reads and inside its immediate write transaction, and records a raced attempt on concurrent evidence changes. |
| R7 | Staleness explicitly invalidates current acceptances. Plan gates re-observe every child scope, require a compatible current acceptance, recursively revalidate child gates/artifacts, and refuse stale children. A later task check can reopen an invalidated accepted task without discarding harmless evidence. |
| R8 | Forward migration 014 backfills task-wide source consumption from populated v12 assessments and reserves legacy executing/uncertain sources. It creates conservative unknown segments and marks legacy executing effects uncertain, so empty v13 tables cannot imply no prior invocation. |
| R9 | Exact baseline authorization reconciles `needs_repair` back to `checking` only when no already-observed current check is blocking. Missing required checks still have to run and any new failure remains blocking. |
| R10 | Isolated copies preserve the exact regular-file permission bits (including 0600, 0644 and 0755); private temporary parents still provide isolation. Source equality no longer fails solely because the copy narrowed ordinary modes. |

Migrations 013 and 014 are forward-only. Migration 014 reconciles populated legacy authority without changing migrations 001–013 or their recorded digests.

## Implemented authority model

### Immutable evidence and freshness

Migration 010 installs the authoritative v2 quality tables; the similarly named migration-001 draft tables remain untouched and unused. A `quality_scopes_v2` row binds:

- task or plan identity and applicable revisions;
- the ordered enrolled repository manifest and repository-set digest;
- criteria and task/plan definition digests;
- current resolved project configuration and required check-set digests;
- reviewer profile identity, revision, snapshot digest and instruction digest.

Check, baseline-exception, review, finding, manual-result, human-decision, acceptance-attempt and acceptance records are immutable by trigger. An acceptance may only receive a single explicit invalidation annotation; its original evidence is never rewritten. Central scope comparison records append-only reasons: `repository_content`, `plan_revision`, `task_revision`, `criteria`, `definition`, `configuration`, `check_set`, `reviewer_profile`, `reviewer_instructions`, `artifact_missing_or_corrupt`, `superseded` and `criteria_retired`. Task evidence intentionally survives a bare plan reorder, while enclosing plan restrictions remain part of its definition digest; plan evidence requires the exact plan revision.

Migration 011 replaces the inherited global uniqueness of `execution_results.result_digest` with a non-unique lookup index. Distinct Stage 5.3 repair runs may therefore produce equal normalized content without confusing content equality with run authority. Migration 012 adds immutable, uniquely source-bound supervisor assessments.

### Checks and effect recovery

`internal/checks` executes an approved argv without user-supplied shell interpretation in an agent-owned isolated source copy. Darwin uses a fixed internal stop/exec wrapper solely to register fork observation before approved code runs. The runner uses only its owned home/temp/locale plus sorted approved variables, the approved relative cwd, a bounded timeout/output ceiling and required output paths. Bare executable lookup requires an explicitly approved absolute canonical `PATH`; ambient lookup is rejected. The live owner capability and exact repository fences remain held from effect start through copy, subprocess containment, source re-observation and result persistence. The managed source is re-fingerprinted after execution. Result/output artifacts and required-output digests are durable evidence; missing, corrupt or truncated evidence cannot satisfy a gate.

Terminal check statuses are `pass`, `fail`, `timeout`, `interrupted`, `error`, `source_mutated`, `missing_output` and `output_overflow`. A baseline exception is explicit immutable fixture-human/human authority bound to the check definition, base fingerprint and exact normalized failure identities. It classifies only those known identities as `accepted_baseline`; an additional identity still blocks.

Check, review and assessment intent is durable before its external effect. Process/reviewer/assessor execution occurs outside database transactions while a live owner holds exact repository claims/fences. Budget timing begins durably with the executing transition. An `executing` effect found after restart becomes `uncertain`, charges its conservative unknown interval and is never reset or automatically replayed; any unresolved effect for the target blocks fresh dispatch across scopes. A paused project cannot dispatch a new check or review. Task effects charge the existing cumulative task ledger; plan-wide effects charge the separate plan-services ledger.

### Fresh read-only review, repair and exhaustion

`internal/review` accepts only a new reviewer session/native identity, distinct from implementation and every prior review. Its manifest binds the current scope, exact criteria/definition, repository digest and current check gates. The versioned closed result schema rejects unknown or malformed data. The application—not the reviewer—derives blocking findings from the configured severity threshold. Suggestions remain visible and nonblocking.

Repository mutation during review produces `write_denied`. The reviewer has no code-writing, acceptance, publishing or delivery authority. Rejection routes to the existing Stage 5.3 repair preparation path; repair count and task ledger are cumulative. Source or definition changes require fresh checks and a distinct fresh review.

A fixture-only bounded assessment may inspect a persisted repair or budget exhaustion. It runs under current owner fences, charges the same task ledger, reserves the exact task/source before invocation and may return only `clarify`, `revise_or_split`, `eligible_reassignment` or `remain_blocked`. It leaves the task blocked and cannot change criteria, increase budgets, accept partial work, create gate evidence or authorize further spending. No assessment starts once the ledger is exhausted; failure leaves a charged uncertain source that cannot be replayed.

### Manual/human and atomic acceptance

Manual outcomes are `pending`, `pass`, `fail` and `cannot_verify`; only an explicit current `pass` satisfies a manual criterion. Human decisions are `accept`, `request_changes`, `clarify` and `stop`. Both bind the exact scope. A human-acceptance setting cannot manufacture a manual pass. `task.criteria.revise` requires explicit human revision authority, advances task and plan revisions, retires any current acceptance and invalidates the prior evidence scope.

Task acceptance holds current claims and re-observes repository bytes, evaluates the exact final manifest under a comprehensive quality-authority epoch, then compares that epoch inside the immediate acceptance transaction. This atomically covers revisions, checks, review/findings, manual results, configured human decision, budgets, artifacts and unresolved effects. A mismatch records a rejected or raced attempt rather than accepting. Only this path moves a task to `accepted`. Once all tasks are accepted the plan enters `verifying`.

Plan-wide checks/review/manual/human evidence uses an exact plan scope and the plan-services ledger. Atomic plan acceptance re-observes and revalidates every child task scope, gate, artifact and non-invalidated acceptance under the same epoch, then moves only to `finalizing`. No acceptance path creates a delivery, commit, push or publication authority.

## Permanent fixture coverage

The default suite requires no model, Docker, hosting service or credential. It covers:

- passing, nonzero, timed-out and caller-interrupted subprocesses, plus approved-path, ambient-path-denial and missing-executable cases;
- source mutation, missing required output, output overflow and output artifact tampering;
- exact known baseline failure plus a new unapproved failure;
- closed-schema malformed review, reviewer write denial and suggestion-only findings;
- configuration/check-set, criteria, definition, reviewer profile and instruction freshness;
- uncertain-effect restart with no automatic replay;
- repair reuse of the cumulative ledger and bounded exhaustion assessment with no acceptance/overspend;
- stale manual/human decisions, acceptance repository races and persisted `raced` attempts;
- pause denial for new check and reviewer dispatch, and live-owner release blocked for the complete check interval;
- a database restart inside a complete failure → Stage 5.3 repair → fresh passing check → distinct fresh review → fixture manual/human task acceptance → plan-wide gates → `finalizing` cycle;
- an assertion that the completed acceptance cycle creates no delivery.
- all ten independent R1–R10 reproductions as permanent tests, including detached-session fail-closed behavior, post-process budget exhaustion, ordinary file modes and populated-v12 observed/executing/uncertain assessment upgrades.

The fixture actors `fixture`, `fixture_human` and `fixture_core` are deliberately closed and accepted only for repositories carrying the disposable-fixture marker. They are not real check qualification, review or human acceptance.

## Validation evidence

### Linux/WSL

The pre-remediation suites were reported passing at `0993a24`. At final R2/R10 remediation commit `49b9fbb`, the following gates passed:

```text
make check
make check-race
make build
make build-boundary
make cross-build
git diff --check
```

`make check-race` includes `internal/checks`, `internal/review` and `internal/quality`. The permanent Stage 5.4 review probes also passed twice under `-race`. Cross-build completed `linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64`. Full tests used normal loopback/process permissions for the existing boundary cases. No model, Docker or hosting call ran.

### Native macOS

The previously authorized host was available. An isolated temporary checkout was cloned from the final bundle; the normal checkout was not touched. `make check`, `make check-race` and `make build` completed successfully on Darwin arm64. The checkout and bundle were removed afterward; only resources created for this validation were cleaned up.

| Item | Exact value |
| --- | --- |
| Commit | `49b9fbb67d31dd604da673bd0b92cc9bbb15a27c` |
| Bundle SHA-256 | `322b3facdb75a3056c346dc770043e9a97127373cb0328db6dc70059cb21b238` |
| Host | Darwin 25.6.0 arm64 |
| Go | `go1.27.1 darwin/arm64` |
| Commands | `make check` (including permanent R1–R10 probes); `make check-race`; `make build` |
| Result | all passed |

The temporary Mac directory `/tmp/vigil-stage54-f3057aa.VvWBtx`, all remote bundles/checkouts and all three local diagnostic/final bundles were removed and verified absent. Only disposable module caches were made writable to permit their removal. The normal Mac checkout was not accessed or changed.

## Final follow-up remediation

Commit `49b9fbb` adds a Linux-only re-exec supervisor that enables `PR_SET_CHILD_SUBREAPER`, starts the approved command from a canonical serialized specification, handles cancellation, recursively signals descendants, and reaps adopted children before returning. A reserved supervisor exit status makes cleanup uncertainty fail closed. The copy path now calls `Chmod` after regular-file creation, eliminating umask-dependent source evidence. `internal/quality/stage54_final_followup_test.go` permanently covers both the empty-environment detached child and `umask(077)` cases; the retained inert probe remains at `docs/research/stage-5.4-review/final_followup_test.go.txt`.

Independent follow-up closed R10 but retained R2 because supervisor signal death could still be interpreted through non-authoritative polling, and added R11 for configuration descriptors/writers left to garbage collection.

Commit `cba322b` addresses those findings. Linux containment now uses private readiness and cleanup-proof pipes: cancellation is sent only after the subreaper and its signal handler are ready, and only an exact completion token written after authoritative cleanup can create containment authority. Signal death, missing/malformed proof, supervisor status 125, process-observation failure and shutdown escalation remain uncertain and retain claims. Descendant signals use pidfds after `/proc` start-time verification rather than reusable bare PIDs. Normal success bypasses the former polling authority; polling after supervisor loss is cleanup-only and cannot authorize release.

The containment resource object closes parent copies of every inherited descriptor immediately after start, closes all ends on preparation/start failure, and synchronously joins the bounded configuration writer. Permanent Linux tests cover explicit supervisor death, repeated successful descriptor use with GC disabled, cleanup-time cancellation with an empty-environment detached child, and a failed start with a configuration larger than pipe capacity.

Linux validation at `cba322b` passed:

- `go test ./internal/checks -run 'TestIndependent|TestSupervisor' -count=2 -v`;
- the same focused suite twice under `-race`;
- all existing `TestStage54` and interrupted-check regressions;
- `make check`, `make check-race`, `make build`, `make build-boundary`, `make cross-build`, and `git diff --check`.

The exact `git archive` for `cba322b` had SHA-256 `f2951b2628abb2a61752f16d7fc7ab8de1cdfce12b85a159155d4d31b7257833`. Native macOS transfer/execution was not authorized by the execution environment in this session, so native validation remains pending. The local temporary archive was removed. The implementing agent does not self-accept R2/R11.

## Deferred gates and limitations

- Stage 5.4 remediation requires independent follow-up acceptance; this implementation report is not self-acceptance and does not close R2/R11 by assertion.
- Darwin 25 rejects kernel `NOTE_TRACK`. Vigil registers `NOTE_FORK` before approved code executes; an otherwise unaccounted fork makes the effect uncertain instead of passing. This is intentionally fail-closed and can reject a legitimate forking check until a qualified production containment boundary exists.
- Production check/model/reviewer dispatch remains disabled. No real fresh model review was attempted.
- Safe contained Codex subscription routing, effective Luna/low selection, provider-idle proof, shared Mac/WSL capacity authority and live recovery across advertised harness/platform combinations remain pending. No paid fallback or larger-model fallback is authorized.
- Real project repository/base/branch/check choices, baseline exceptions, criteria changes, manual functional verification and task/plan human acceptance remain explicit user gates. No such evidence was fabricated.
- Unsupported repository layouts from earlier stages remain unsupported. The check runner intentionally rejects special files and unsupported nested boundaries instead of creating a production fallback.
- Acceptance ends at task `accepted` or plan `finalizing`; delivery/finalization implementation belongs to later slices. Stage 5.5 was not started.

## Independent review instructions

Review `49b9fbb..cba322b` against R2/R11 in `docs/research/stage-5.4-astra-review.md`. The earlier remediation ranges remain historical context. Begin from the accepted Stage 5.3 contracts and explicitly preserve every previously closed finding.

1. Verify migrations 010–014 on both a fresh database and populated older databases. Confirm historical migration bytes/digests are unchanged and migration 014 preserves observed source consumption while conservatively reconciling failed/executing legacy assessment effects and their ledger charge.
2. Audit `internal/quality/scope.go` field by field. Reproduce repository, task/plan revision, criteria/definition, configuration/check-set, reviewer profile/instruction and artifact invalidation. Confirm a harmless plan reorder does not stale task evidence but an enclosing restriction change does.
3. Audit `internal/checks/runner.go` and platform trackers for fixture-only admission before effects, argv/cwd/environment authority, exact copied modes, post-check source integrity, detached-session handling, bounded inherited-output draining, required outputs, owner-release proof and restart uncertainty. On Darwin, verify an unaccounted fork produces uncertainty and cannot issue a result/release proof. Verify unsupported `qualified_runtime` dispatch starts no child.
4. Audit `internal/review/review.go` for distinct identities, exact manifests, closed decoding, deterministic thresholds, owner fences and write denial. Confirm suggestions cannot block and reviewer output cannot grant acceptance or publishing authority.
5. Trace rejection into the existing Stage 5.3 repair path. Confirm repair counters and ledgers never reset, changed source requires fresh checks/review, and migration 011 does not weaken run identity.
6. Audit `internal/quality/assessment.go`. Confirm exact source reservation precedes invocation, second/failed/uncertain requests cannot invoke again, every failure is charged, the task remains blocked and no action can weaken criteria or spending limits.
7. Audit manual/human commands and `Acceptor`. Re-run the competing-WAL pending-manual probe at the gate-read/write-transaction boundary. Exercise code, criteria, definition/config/profile, stale child acceptance and artifact races. Confirm the committed manifest names the final selected gates, only current explicit manual `pass` satisfies a manual gate, and plan acceptance creates no delivery.
8. Run `make check`, `make check-race`, `make build`, `make build-boundary`, `make cross-build` and `git diff --check` without models, Docker or hosting. Re-run native process/filesystem/locking validation if review changes those paths.
9. Report findings independently. If accepted offline, update the review record and shared status while leaving live qualification and real human/manual gates pending. Do not enable production dispatch, push, publish, self-authorize recovery, or begin Stage 5.5 as part of the review.


## Independent review of final remediation

The independent follow-up of `49b9fbb` closes R10 but leaves R2 (P1) open: signal death of the subreaper can still produce `contained_stopped` without completed cleanup. New R11 (P2) covers configuration-pipe descriptors left to garbage collection. See [the exact findings and reproduction instructions](stage-5.4-astra-review.md#independent-follow-up-of-49b9fbb). Stage 5.4 remains unaccepted and production dispatch remains disabled; submitted passing suites do not supersede these findings.
