# Stage 5.4 implementation results

Date: 2026-09-22

Scope: offline implementation and fixture validation only

Status: initial implementation through `03f65e8` received seven P1 and two P2 findings. All R1–R9 remediations and final acceptance-state fencing are implemented and validated through `0993a24`; Stage 5.4 remains **unaccepted pending independent follow-up review**. See the [independent review and retained probes](stage-5.4-astra-review.md).

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

Initial review range: `4b48737..03f65e8`. Follow-up remediation range: `c224826..0993a24`. No commit was pushed and no publication or production activation occurred.

## Independent-review remediation

| Finding | Implemented correction |
| --- | --- |
| R1 | Core check admission now accepts only marked disposable fixtures. `qualified_runtime` fails closed until a real qualified execution boundary exists; actor strings cannot substitute for qualification. |
| R2 | Checks run in a dedicated Unix process group with bounded pipe draining and TERM/KILL retirement proof. Claims are released only after terminal persistence and containment; uncertain paths retain them. |
| R3 | The isolated source tree is digested before and after copying and after execution, with only declared required-output paths excluded. A copied-source mutation records `source_mutated`; the original enrolled set is still re-observed. |
| R4 | Review and assessment terminal state changes are conditional on the exact task revision and allowed in-progress state. Late pass/failure results persist without overwriting stop, request-changes or criteria authority. |
| R5 | Migration 013 adds durable per-effect quality budget segments. Effect start and segment reservation are atomic; every terminal path charges the full interval, uncertain recovery charges unknown time, and unresolved effects block fresh target dispatch across scopes. |
| R6 | Migration 013 adds a comprehensive quality-authority epoch advanced by every relevant mutator. Acceptance binds the final gate manifest, compares the epoch before/after reads and inside its immediate write transaction, and records a raced attempt on concurrent evidence changes. |
| R7 | Staleness explicitly invalidates current acceptances. Plan gates re-observe every child scope, require a compatible current acceptance, recursively revalidate child gates/artifacts, and refuse stale children. A later task check can reopen an invalidated accepted task without discarding harmless evidence. |
| R8 | The exact task/exhaustion source is reserved before assessment dispatch, independently of command ID. Observed or uncertain sources cannot call the assessor again; failures are charged and non-replayable. |
| R9 | Exact baseline authorization reconciles `needs_repair` back to `checking` only when no already-observed current check is blocking. Missing required checks still have to run and any new failure remains blocking. |

Migration 013 is forward-only. It adds immutable single-close budget segments, single-resolution assessment-source reservations and the acceptance authority epoch without changing migrations 001–012 or their recorded digests.

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

`internal/checks` executes an approved argv directly, without a shell, in an agent-owned isolated source copy. It uses only the runner-owned home/temp/locale plus sorted approved variables, the approved relative cwd, a bounded timeout/output ceiling and required output paths. Bare executable lookup requires an explicitly approved absolute canonical `PATH`; ambient lookup is rejected. The live owner capability and exact repository fences remain held from effect start through copy, subprocess containment, source re-observation and result persistence. The managed source is re-fingerprinted after execution. Result/output artifacts and required-output digests are durable evidence; missing, corrupt or truncated evidence cannot satisfy a gate.

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
- all nine independent R1–R9 reproductions as permanent tests, plus inherited-output descendant retirement, late failing-review stop preservation and failed-assessment charge/non-replay coverage.

The fixture actors `fixture`, `fixture_human` and `fixture_core` are deliberately closed and accepted only for repositories carrying the disposable-fixture marker. They are not real check qualification, review or human acceptance.

## Validation evidence

### Linux/WSL

The pre-remediation baseline `make check` passed after the review-artifact commit `c224826`. At exact final remediation commit `0993a24776404dac00df39ca8d25d2c2c1c2f68f`, the following gates passed:

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

The previously authorized host was available. An isolated temporary checkout was cloned from a complete Git bundle; the normal checkout was not touched.

| Item | Exact value |
| --- | --- |
| Commit | `0993a24776404dac00df39ca8d25d2c2c1c2f68f` |
| Bundle SHA-256 | `5e0cb97c4b9ea9a6c5a209af502075b648e010aecf6bee8704a55eaf8d3bb874` |
| Host | Darwin 25.6.0 arm64 |
| Go | `go1.27.1 darwin/arm64` |
| Commands | `make check` (including permanent R1–R9 probes); `make check-race`; `make build` |
| Result | all passed |

The final temporary Mac directory `/tmp/vigil-stage54-0993a24.rnP5qK` and remote bundle were removed and verified absent; the local bundle was also removed. The earlier `872be1a` validation checkout/bundle was likewise removed after making only its disposable module cache writable. The normal Mac checkout was not accessed or changed.

## Deferred gates and limitations

- Stage 5.4 remediation requires independent follow-up acceptance; this implementation report is not self-acceptance and does not close R1–R9 by assertion.
- Production check/model/reviewer dispatch remains disabled. No real fresh model review was attempted.
- Safe contained Codex subscription routing, effective Luna/low selection, provider-idle proof, shared Mac/WSL capacity authority and live recovery across advertised harness/platform combinations remain pending. No paid fallback or larger-model fallback is authorized.
- Real project repository/base/branch/check choices, baseline exceptions, criteria changes, manual functional verification and task/plan human acceptance remain explicit user gates. No such evidence was fabricated.
- Unsupported repository layouts from earlier stages remain unsupported. The check runner intentionally rejects special files and unsupported nested boundaries instead of creating a production fallback.
- Acceptance ends at task `accepted` or plan `finalizing`; delivery/finalization implementation belongs to later slices. Stage 5.5 was not started.

## Independent review instructions

Review remediation range `c224826..0993a24` against the original findings in `docs/research/stage-5.4-astra-review.md`. The initial implementation range remains `4b48737..03f65e8`. Begin from the accepted Stage 5.3 contracts and explicitly preserve R1–R10.

1. Verify migrations 010–013 on both a fresh database and populated older databases. Confirm historical migration bytes/digests are unchanged, migration 013 rollback is atomic, its segment/reservation guards hold, duplicate result content remains allowed, and assessment sources are unique per exact task/exhaustion across scope changes.
2. Audit `internal/quality/scope.go` field by field. Reproduce repository, task/plan revision, criteria/definition, configuration/check-set, reviewer profile/instruction and artifact invalidation. Confirm a harmless plan reorder does not stale task evidence but an enclosing restriction change does.
3. Audit `internal/checks/runner.go` for fixture-only admission before effects, argv/cwd/environment authority, copy equivalence, post-check source integrity, process-group retirement, bounded inherited-output draining, required outputs, owner-release proof and restart uncertainty. Verify unsupported `qualified_runtime` dispatch starts no child and an exact baseline exception cannot cover a new failure.
4. Audit `internal/review/review.go` for distinct identities, exact manifests, closed decoding, deterministic thresholds, owner fences and write denial. Confirm suggestions cannot block and reviewer output cannot grant acceptance or publishing authority.
5. Trace rejection into the existing Stage 5.3 repair path. Confirm repair counters and ledgers never reset, changed source requires fresh checks/review, and migration 011 does not weaken run identity.
6. Audit `internal/quality/assessment.go`. Confirm exact source reservation precedes invocation, second/failed/uncertain requests cannot invoke again, every failure is charged, the task remains blocked and no action can weaken criteria or spending limits.
7. Audit manual/human commands and `Acceptor`. Re-run the competing-WAL pending-manual probe at the gate-read/write-transaction boundary. Exercise code, criteria, definition/config/profile, stale child acceptance and artifact races. Confirm the committed manifest names the final selected gates, only current explicit manual `pass` satisfies a manual gate, and plan acceptance creates no delivery.
8. Run `make check`, `make check-race`, `make build`, `make build-boundary`, `make cross-build` and `git diff --check` without models, Docker or hosting. Re-run native process/filesystem/locking validation if review changes those paths.
9. Report findings independently. If accepted offline, update the review record and shared status while leaving live qualification and real human/manual gates pending. Do not enable production dispatch, push, publish, self-authorize recovery, or begin Stage 5.5 as part of the review.
