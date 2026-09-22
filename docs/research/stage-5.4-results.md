# Stage 5.4 implementation results

Date: 2026-09-22

Scope: offline implementation and fixture validation only

Status: implementation through `03f65e8` independently reviewed on 2026-09-22; **changes requested: seven P1 and two P2 findings (R1–R9)**. Stage 5.4 is unaccepted. See the [independent review and retained probes](stage-5.4-astra-review.md). The implementation claims below are superseded where that review demonstrates a defect.

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

Review range: `4b48737..03f65e8`. No commit was pushed and no publication or production activation occurred.

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

Check, review and assessment intent is durable before its external effect. Process/reviewer/assessor execution occurs outside database transactions while a live owner holds exact repository claims/fences. An `executing` effect found after restart becomes `uncertain`; it is never reset or automatically replayed. A paused project cannot dispatch a new check or review. Task effects charge the existing cumulative task ledger; plan-wide effects charge the separate plan-services ledger.

### Fresh read-only review, repair and exhaustion

`internal/review` accepts only a new reviewer session/native identity, distinct from implementation and every prior review. Its manifest binds the current scope, exact criteria/definition, repository digest and current check gates. The versioned closed result schema rejects unknown or malformed data. The application—not the reviewer—derives blocking findings from the configured severity threshold. Suggestions remain visible and nonblocking.

Repository mutation during review produces `write_denied`. The reviewer has no code-writing, acceptance, publishing or delivery authority. Rejection routes to the existing Stage 5.3 repair preparation path; repair count and task ledger are cumulative. Source or definition changes require fresh checks and a distinct fresh review.

A fixture-only bounded assessment may inspect a persisted repair or budget exhaustion. It runs under current owner fences, charges the same task ledger, is unique for the exact scope/source and may return only `clarify`, `revise_or_split`, `eligible_reassignment` or `remain_blocked`. It leaves the task blocked and cannot change criteria, increase budgets, accept partial work, create gate evidence or authorize further spending. No assessment starts once the ledger is exhausted.

### Manual/human and atomic acceptance

Manual outcomes are `pending`, `pass`, `fail` and `cannot_verify`; only an explicit current `pass` satisfies a manual criterion. Human decisions are `accept`, `request_changes`, `clarify` and `stop`. Both bind the exact scope. A human-acceptance setting cannot manufacture a manual pass. `task.criteria.revise` requires explicit human revision authority, advances task and plan revisions, retires any current acceptance and invalidates the prior evidence scope.

Task acceptance holds current claims and re-observes repository bytes, then atomically rechecks all revisions, checks, review/findings, manual results, configured human decision, budgets, artifacts and unresolved effects. A mismatch records a rejected or raced attempt rather than accepting. Only this path moves a task to `accepted`. Once all tasks are accepted the plan enters `verifying`.

Plan-wide checks/review/manual/human evidence uses an exact plan scope and the plan-services ledger. Atomic plan acceptance rechecks every current task acceptance and plan gate, then moves only to `finalizing`. No acceptance path creates a delivery, commit, push or publication authority.

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

The fixture actors `fixture`, `fixture_human` and `fixture_core` are deliberately closed and accepted only for repositories carrying the disposable-fixture marker. They are not real check qualification, review or human acceptance.

## Validation evidence

### Linux/WSL

The pre-implementation baseline `make check` passed at `4b48737`. After the final implementation commit, the following combined gate passed:

```text
make check
make check-race
make build
make build-boundary
make cross-build
git diff --check
```

`make check-race` includes the new `internal/checks`, `internal/review` and `internal/quality` packages. Cross-build completed `linux/amd64`, `linux/arm64`, `darwin/amd64` and `darwin/arm64`. The first restricted-sandbox rerun could not open the existing boundary test's loopback listener; the same full command was then run with its normal loopback/process permissions and passed. No model, Docker or hosting call ran.

### Native macOS

The previously authorized host was available. An isolated temporary checkout was cloned from a complete Git bundle; the normal checkout was not touched.

| Item | Exact value |
| --- | --- |
| Commit | `03f65e8bc2d1e7ec9234df8e81377dd53c950671` |
| Bundle SHA-256 | `d2dfeb11e5cd4b004ca18cd95b4f06daa18edcce32bb7cafb1a5aba1ea6c509d` |
| Host | Darwin 25.6.0 arm64 |
| Go | `go1.27.1 darwin/arm64` |
| Commands | `make check`; `make check-race`; `make build` |
| Result | all passed |

The temporary Mac directory `/tmp/vigil-stage54.BFYvfH` and local bundle `/tmp/vigil-stage54-03f65e8.bundle` were removed and verified absent. Only resources created for this validation were cleaned up.

## Deferred gates and limitations

- Stage 5.4 requires remediation of independent review R1–R9 and follow-up acceptance; this implementation report is not self-acceptance.
- Production check/model/reviewer dispatch remains disabled. No real fresh model review was attempted.
- Safe contained Codex subscription routing, effective Luna/low selection, provider-idle proof, shared Mac/WSL capacity authority and live recovery across advertised harness/platform combinations remain pending. No paid fallback or larger-model fallback is authorized.
- Real project repository/base/branch/check choices, baseline exceptions, criteria changes, manual functional verification and task/plan human acceptance remain explicit user gates. No such evidence was fabricated.
- Unsupported repository layouts from earlier stages remain unsupported. The check runner intentionally rejects special files and unsupported nested boundaries instead of creating a production fallback.
- Acceptance ends at task `accepted` or plan `finalizing`; delivery/finalization implementation belongs to later slices. Stage 5.5 was not started.

## Independent review instructions

Review `4b48737..03f65e8`; treat `7333891` as the pre-code plan, `63726f4`/`15b8c66` as primary implementation, `e992685` as authority-race/freshness coverage and `03f65e8` as the final check-boundary remediation. Begin from the accepted Stage 5.3 contracts and explicitly preserve R1–R10.

1. Verify migrations 010–012 on both a fresh database and a populated migration-009 database. Confirm historical migration bytes/digests are unchanged, immutable triggers hold, duplicate result content across distinct runs is allowed, and supervisor assessments are uniquely scope/source bound.
2. Audit `internal/quality/scope.go` field by field. Reproduce repository, task/plan revision, criteria/definition, configuration/check-set, reviewer profile/instruction and artifact invalidation. Confirm a harmless plan reorder does not stale task evidence but an enclosing restriction change does.
3. Audit `internal/checks/runner.go` for argv/cwd/environment authority, isolated copy behavior, output bounding, required outputs, source re-observation, owner fences, effect ordering and restart uncertainty. Reproduce every terminal status and artifact tamper case. Verify an exact baseline exception cannot cover a new failure.
4. Audit `internal/review/review.go` for distinct identities, exact manifests, closed decoding, deterministic thresholds, owner fences and write denial. Confirm suggestions cannot block and reviewer output cannot grant acceptance or publishing authority.
5. Trace rejection into the existing Stage 5.3 repair path. Confirm repair counters and ledgers never reset, changed source requires fresh checks/review, and migration 011 does not weaken run identity.
6. Audit `internal/quality/assessment.go`. Confirm only persisted exhaustion is eligible, the same task ledger bounds it, the task remains blocked and none of its four actions can change criteria, accept partial work, increase budget or authorize spending.
7. Audit manual/human commands and `Acceptor`. Exercise code, criteria, definition/config/profile and artifact races immediately before acceptance. Verify only current explicit manual `pass` satisfies a manual gate and plan acceptance uses the plan-services ledger and creates no delivery.
8. Run `make check`, `make check-race`, `make build`, `make build-boundary`, `make cross-build` and `git diff --check` without models, Docker or hosting. Re-run native process/filesystem/locking validation if review changes those paths.
9. Report findings independently. If accepted offline, update the review record and shared status while leaving live qualification and real human/manual gates pending. Do not enable production dispatch, push, publish, self-authorize recovery, or begin Stage 5.5 as part of the review.
