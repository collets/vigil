# Stage 5 implementation backlog and requirement coverage

<!-- vigil-tier: plan -->
<!-- vigil-status: stage=5.7; stage_accepted=true; implementation_commit=522cb967f732b578a66c051cf8938dd00584d238 -->

Stage 4 output, updated after Stage 3.5 on 2026-09-20. Implement [core-spec.md](../../core/core-spec.md) in these increments after reviewing [Linux](../../research/stage-3/results.md) and [macOS](../../research/stage-3.5/results.md) evidence. Both platforms retain explicit containment/recovery gates. This is an implementation backlog, not a completion claim.

The [expanded execution plan](stage-5-execution.md) now supplies commit-sized steps, command/storage boundaries, prerequisite checks and concrete failure tests. Implementation begins with A–C; D remains a hard gate on production editing.

For the remaining implementation after the foundation, use the [Stage 5.1–5.7 standalone plans](README.md). Their index maps each assignment back to A–J and states dependencies and user decisions. This document remains the complete requirement coverage reference.

Current status, updated 2026-09-27: slices 5.1–5.5 are independently accepted offline. H and I plus independent-review remediation are implemented through `84c0275`: ranked scheduling with stale-selection retirement, expiring recovery, persisted PTY decisions, Markdown intake, bounded planning, typed handlers, shared MCP transport and native Hermes one-tool isolation. Narrow follow-up and exact-`84c0275` native macOS gates pass. Native Codex/production qualification and real decisions remain disabled production gates. Production dispatch remains disabled.

## Increment order and executable acceptance

| Slice | Work | Acceptance / failure test |
| --- | --- | --- |
| A — persisted commands | Project identity, migration runner, immutable config/revisions, command receipts, append-only events and artifact publication | Fresh/open/newer-schema rejection; rollback; repeated command ID gives same result, different arguments rejected; crash between file rename and DB commit reconciles orphan |
| B — readiness and policy | Explicit model policy/profile selection, capability evidence, task definition/dependency validation, grants/inbox, restriction precedence | Missing profile/manual prerequisites block; cycle rejected; project deny dominates global/task grant; stale revision and revoked/consumed once grant cannot authorize effect |
| C — coordination/recovery | Host coordination DB, canonical overlapping-root/common-Git claims, endpoint FIFO, process-start identity, quarantine | Two processes cannot own overlapping roots or exceed slot capacity; symlink/case/Unicode aliases collide; similar-prefix siblings remain distinct; tunnel aliases cannot bypass physical-endpoint capacity; cloud project progresses while local queue waits; crashed owner retains quarantine until reconciled |
| D — execution boundary qualification | Implement selected Linux/macOS containment, credential/egress boundary and supervisor cleanup using Stage 3/3.5 evidence | Shell/Python/Git alternate paths cannot write protected Git metadata, publish, access host secrets/sockets or escape containment; kill empties all descendants, including the reproduced macOS Codex/Hermes writers; no strict dispatch until passed |
| E — one persisted task | Plan branch/baseline setup, one owned harness run, state/event ingestion, exact result/evidence, no retry after uncertain submit | Execute one scoped edit through each qualified profile; kill at each start/submit/result persistence boundary; reopened task never double-submits or becomes accepted solely from native completion |
| F — preservation and control | Pause/stop, interrupted/unknown recovery, exact native resume, content-addressed checkpoints, approved restore | Stop heartbeat writers before clear; round-trip staged/unstaged/binary/mode/symlink/untracked state; preserve user edits; one failed repo snapshot clears none; partial restore retains both originals and blocks dispatch |
| G — checks/review/acceptance | Required check union/baselines, fresh read-only reviewer, repair ledger, manual checks, task/plan acceptance | Later edits invalidate evidence; suggestions do not force repair; accepted baseline remains unhealthy; Cannot verify fails manual gate; repair exhaustion invokes bounded supervisor with no implicit paid authorization |
| H — sequential progression/UI | Task selection around blocked work, explicit plan queue, resource waits, responsive dashboard/inbox and detail screens | No dispatch after pause; stop interrupts active run; output flood cannot block input; cancelled/stale requests cannot authorize; UI reads authoritative persisted state after restart |
| I — bounded model tools | Read-only context/tools, reorder proposal, approved edit/split/merge, criteria protection, manual intake/clarification | Tool session/role/revision/resource bounds enforced; cycle/oversize/unknown path rejected; stale proposal not applied; model cannot mutate criteria or mark its own task accepted |
| J — delivery/archive | Application-owned task commits, explicit ref push/draft GitHub/GitLab creation, factual manifest/narrative, retention | Crash after remote success reconciles without duplication; checkpoint refs never pushed; no merge API; summary retry does not rerun implementation; unfinished recovery survives transcript expiry |

A–C and read-only UI work can proceed while containment/platform work is pending. D gates production editing in E; the diagnostic spike is not a bypass. Each increment gets focused tests and a reviewable local commit. Do not start all packages at once.

## Model-facing command contracts

All tools use a versioned closed JSON schema, bounded strings/lists and `additionalProperties:false`. The server injects project/role/session authority; model arguments cannot select another project, grant permissions or submit arbitrary SQL/shell/Git. Default payload cap 64 KiB, list limit 100 with opaque cursors, artifact excerpts 32 KiB, proposal maximum 50 affected tasks. Validate every referenced ID and path against the owned project/repository map.

| Tool / result | Required fields and action | Guard |
| --- | --- | --- |
| `project.read`, `plan.read`, `task.read` | IDs, known revision or cursor; returns snapshot/provenance/next cursor | Role-scoped read capability, bounded context, secrets omitted |
| `artifact.read` | Artifact ID, offset/length; returns verified excerpt or explicit expired/corrupt | Stored owned path/digest; no arbitrary filesystem path |
| `plan.reorder` | Command ID, expected plan revision, ordered remaining task IDs, reason | Same set, acyclic dependencies, active/accepted task unchanged; allowed supervisor role |
| `plan.propose_change` | Command ID, expected revisions, operation edit/split/merge, affected IDs, proposed definitions and rationale | Creates proposal/inbox only; criteria edits require separately identified human authorization; approved application command applies atomically |
| `task.report_blocked` | Run/turn identity injected, structured reason, missing facts, evidence IDs | Observation only; core determines scheduling/state |
| `execution.result` | Summary, repository-relative changed paths, claimed check observations, artifact IDs, unresolved concerns | Claims validated against actual diff/checks; cannot set acceptance/state/commit permission |
| `review.result` | Evaluated fingerprint, finding IDs/severity/locations/evidence, suggestions, criteria assessment | Fresh reviewer session and read-only role; failed/partial output is not approval |
| `supervisor.result` | Assessed task/plan revision, findings, bounded proposals and requested clarifications | No implicit profile escalation, paid call, scope mutation or criteria relaxation |
| `finalization.result` | Narrative artifact and cited manifest revision/IDs | Reference completeness and factual consistency; no recursive task creation |

Implement the bounded tool boundary as application handlers first; expose through a small MCP server only after native integration/role isolation is validated. Do not adopt dynamic model-authored workflows. Steering/native UI takeover remain separately gated adapter capabilities; pause/reconcile ownership before handing control away.

## Readiness, quality and event schema details

Task definition v1 requires objective; immutable acceptance criteria IDs/text/manual flags; dependency IDs; relevant context/artifact references; verification/check IDs; implementation/reviewer profiles; difficulty label/rationale; scope repository/path patterns; required human gates/waivers; retry/time ceilings. Empty criteria, foreign dependencies, absent required manual checks, unsupported profile capabilities, or unresolved clarification prevent `ready`.

Check result v1 includes check-definition digest, run ID, argv/cwd/env-reference snapshot, start/end/exit/timeout/interrupt, repository-set fingerprint, bounded output artifact, normalized failure identities, and optional explicit baseline authorization. Review v1 binds the same fingerprint plus criteria revision. No model-supplied “tests passed” becomes authoritative check evidence.

Event v1 envelope: sequence, schema_version, occurred_at UTC, command_id/run_id, source generation/sequence, kind, sanitized payload. Kinds include command_applied/rejected, definition_revised, policy_changed, request_opened/resolved/retired, resource_queued/acquired/quarantined/released, run_prepared/submitted/outcome_observed, writer_observed, time_segment_closed, artifact_published/corrupt/expired, check_recorded, review_recorded, acceptance_recorded/invalidated, checkpoint_progress, delivery_observed and finalization_status. Avoid raw prompts, full tool arguments, secrets and binary data; those belong only in bounded private artifacts where explicitly retained.

The draft SQL encodes key integrity invariants; application transactions additionally enforce state-transition guards, dependency-cycle rejection, cross-record role/context matching, required evidence completeness, revision freshness, ancestor overlap, endpoint alias correctness and operation authorization. A valid SQL row alone is never authorization. Add command-level scenario tests for these rules during A–C/G rather than pretending foreign keys prove the entire workflow.

## Complete requirement map

| Requirements | Contract location | Implementation / qualification |
| --- | --- | --- |
| R01, R06, R07, R11, R45, R46, R47, R51, R70 | Core §§1–3,10; authoritative views/inbox; **Stage 6 interface contract** — parity register and feature list in [`research/stage-6/results.md`](../../research/stage-6/results.md), gap table and sub-stage plan in [`6.1-parity-gap-analysis.md`](../stage-6/6.1-parity-gap-analysis.md) | H; terminal only, external IDE for detailed diffs. **P13–P18 added by 6.1** |
| R02, R03, R04, R08, R10, R42, R58, R59, R69 | Core §§1,2,4,8 | B,D,E; Codex/Hermes first, existing server, other harnesses deferred |
| R05, R21, R23, R33, R34, R35 | Core §§2–4; typed tools above | B,I; explicit criteria changes remain human-only |
| R09 | Core §4 | **Jev** optional/deferred; no dependency or implicit remote routing |
| R12, R17, R27, R36, R63 | Core §§4,5,8 | B,D,G; approvals separate from model policy; strict boundary gated |
| R13, R19, R39 | Core §§7,9 | A,C,F; parent warning and explicit repository map |
| R14, R24, R26, R29, R30, R31, R48, R52, R53, R54 | Core §§3,10 | G; fresh review, exact baseline, manual outcomes and rejection actions |
| R15, R16, R25, R56 | Core §§3,6,7,9 | C,E,F; unknown/quarantine, bounded foreground shutdown, explicit resume |
| R18, R20, R55, R61 | Core §§4,6 | B,E,G,I; cumulative ledgers, bounded fresh supervisor and no implicit escalation grant |
| R22, R37, R57, R60, R62, R71 | Core §§3,6,7 | C,H; one plan/project, nonoverlap, FIFO capacity, no daemon |
| R28, R41, R44, R50 | Core §§5,8,10 | D,J; application-owned authorized effects, draft requests, merge unavailable |
| R32, R38, R40, R43, R49 | Core §9 | F; preserve mixed work, plan branches, verified checkpoint sets, approved restore |
| R64, R65, R66, R67, R68 | Core §11 | A,J; factual archives, independent finalization, explicit export, unfinished retention |

R38/R43 intentionally refer to the same branch rule. Every R01–R71 ID appears above; this map records design coverage, not implementation completion.

The first row's interface contract is Stage 6's. [Stage 6.1](../stage-6/6.1-parity-gap-analysis.md) audited R11, R45, R46, R47 and R70 on 2026-09-30, found all five unmeasurable as written, and amended them in [requirements.md](../../core/requirements.md) with the P13–P18 acceptance conditions above. The 6.1 gap table names the owning sub-stage for every interface gap, and [`research/stage-6/results.md`](../../research/stage-6/results.md) holds the 85-entry parity register and the feature list. The audit is a statement about the **interface**, not about the mapped product contracts: R01, R06, R07 and R51 keep their Stage 5 owners, and their Stage 6 rows are the views the interface must provide, not new product behavior. Proposed verification conditions P01–P12 are exercised across B/E/H (profiles/state/overrides), A/F (history/recovery), C/D (resources/control), G (acceptance), and A/G/J (honest usage/evidence).

## Remaining setup choices and explicit deferrals

User setup chooses real profiles/model policy/spending ceilings, project repository/base maps, required checks and manual requirements. Draft numerical defaults are proposals persisted only after readiness confirmation. Self-hosted hosting, issue-URL import, guided onboarding, within-project parallelism, managed worktrees, browser UI, external-session adoption and routing advisors remain deferred. Native handoff, interrupted/corrupt/long-history resume and live Codex clarification require dedicated evidence before enabling. Stage 3.5 established bounded macOS runtime behavior; complete containment remains D work. No accepted requirement is silently removed to fit current native behavior.
