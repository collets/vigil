# Stage 5 execution plan

<!-- vigil-tier: plan -->
<!-- vigil-status: stage=5.7; stage_accepted=true; implementation_commit=522cb967f732b578a66c051cf8938dd00584d238 -->

Prepared 2026-09-20 before implementation, from the [core specification](../../core/core-spec.md), [A–J backlog](stage-5-plan.md) and Stage 3/3.5 evidence. This plan defines the order, usable increments and validation; checkboxes represent delivered behavior only.

The remaining work is now organized into [seven standalone execution plans, Stage 5.1–5.7](README.md). Use that index to select the next bounded assignment, dependencies, user gates and fresh-context handoff. The A–J sections below preserve the original scope and requirement mapping; consult the dated checkpoints below for delivered state.

Current state, updated 2026-09-27: Stage 5.1–5.5 are independently accepted offline. Stage 5.5 checkpoints A–D and review remediation are implemented through `84c0275`, including stale-selection retirement, expiring recovery, persisted proposal/recovery/quality/clarification PTY paths, bounded planning, shared handlers/transport and native Hermes one-tool qualification. Narrow follow-up and exact-commit native macOS gates pass. Production dispatch, Codex/reviewer qualification, the Darwin observation and real human/manual gates remain disabled future gates.

## Outcome and boundaries

Deliver a foreground application that persists an approved plan, executes eligible tasks sequentially through qualified Codex/Hermes profiles, checks/reviews work, preserves recovery evidence, requests human decisions and can prepare explicitly authorized draft delivery. Acceptance remains the [first usable milestone](../../core/mvp-acceptance.md). Current adapter success is not containment qualification, and successful schema tests are not workflow implementation.

Implement one slice at a time. A–C, read-only views and proposal validation have no dependency on model availability. D gates every production model/check subprocess in E–J; no `--unsafe` escape hatch, inferred grant, native completion-as-acceptance, or fallback to the diagnostic spike. Existing experimental commands stay explicitly separate.

The first implementation checkpoint is a usable persisted planning/control foundation: initialize/reopen a project; configure explicit policy/profiles; import/revise a task graph; inspect readiness/inbox/history; exercise cooperative ownership/endpoint queues; preserve quarantine on owner death. It must report why execution is unavailable, without launching a model.

## 5A — durable commands and artifacts

- [ ] Promote reviewed SQL into embedded versioned migrations, leaving the historical design drafts distinct. Verify version, contiguous history and migration digest; reject newer/modified schemas, foreign databases and migration failures without destructive fallback.
- [ ] Open private file-backed SQLite with foreign keys on every connection, WAL, FULL synchronization, five-second busy timeout and one writer connection per handle. Serialize commands with `BEGIN IMMEDIATE`; rollback on every failure/cancellation.
- [ ] Add explicit state-directory selection and project identity lookup. Store project data outside managed roots; resolve filesystem identities before initialization; return an existing project for the same physical root and reject conflicting nested registration. Never silently initialize inside a parent project.
- [ ] Define typed command envelopes with caller identity, command ID and expected revision. Canonicalize JSON arguments, reserve immutable receipts and persist bounded audit events in the same transaction. Identical replay returns the stored result; changed actor/kind/arguments under the same ID fails.
- [ ] Add immutable config/profile/plan/task revision storage with digests, bounded inputs and no credential values. Revisions refer to explicit credential references only.
- [ ] Implement private content-addressed artifact publication: bounded stream, fsync, atomic publish, directory sync, transaction reference. Reading verifies size/digest and refuses symlinks/escape. Reconciliation reports unreferenced blobs and missing/corrupt referenced artifacts; automatic deletion is deferred until a retention policy authorizes it.

Acceptance: temporary real SQLite files, close/reopen, invalid/newer/digest-mismatched migrations, receipt replay/conflict across reopened handles, concurrent commands, rollback of partial changes/events, event cap, artifact corruption/symlink refusal, simulated crash after blob publication but before reference commit. First migration is additive; any later destructive migration needs the specified consistent backup/explicit recovery path before shipping.

## 5B — definitions, readiness and authority

- [ ] Define versioned closed JSON inputs for project policy, profiles, plans/tasks and permission decisions. Reject unknown fields, oversized documents, duplicate IDs/criteria, invalid scopes and missing explicit model policy.
- [ ] Validate task graphs transactionally: same-plan dependency IDs, no cycles, stable ranks, immutable criteria/revision references. Reordering changes order only; accepted/active tasks and criteria are protected. Human-authorized revisions invalidate affected downstream evidence and pending decisions.
- [ ] Resolve configuration with deny precedence, restriction intersection, required-check union and strictest limits. Ordinary scalar preferences may override; model policy and spending eligibility never get a hidden default.
- [ ] Persist profiles with explicit model/provider/role/endpoint identity and platform/version evidence. User declarations cannot manufacture production containment qualification. Readiness separates definition validity, unmet dependencies/manual prerequisites, permission/budget eligibility and runtime qualification.
- [ ] Persist operation intents and scoped grants bound to exact resource/argument digest, revision and policy epoch. Denial/revocation/expiry wins; once grants reserve atomically and remain consumed under uncertainty. Effect-start is a separate transaction and must recheck authority.
- [ ] Persist inbox decisions with expected context revision and a single resolution. Stale/duplicate decisions cannot authorize new operations. Human criteria edits are separate from model proposals; models cannot grant themselves permissions.
- [ ] Integrate explicitly global grants only through shared coordinator authorization records; until wired, reject that scope rather than treating it as project-local authority.

Acceptance: graph cycle/foreign dependency rejection; stale revision; policy deny versus grant; local-only versus cloud/auxiliary route; missing/manual/unsupported capability; once-grant reuse; revocation before start; expiry; wrong resource/argument; stale/duplicate inbox decision; model-supplied acceptance/criteria mutation rejected.

## 5C — cooperative ownership, capacity and crash recovery

- [ ] Implement filesystem identities and component-wise overlap using existing ancestor device/inode identities, resolved symlinks and common Git directory identity. Revalidate replacement; reject unsupported/ambiguous paths. Do not rely on lowercase strings or path-prefix comparison.
- [ ] Create private host coordinator DB and per-instance advisory locks. Persist PID/start/boot identity, unique instance ID and fencing generation. A free OS lock identifies a dead owner, not a safe workspace.
- [ ] Atomically claim all project/repository roots or none. Detect overlap across instances, including case/Unicode aliases, linked worktrees and symlinked ancestors. Release only with a scoped shutdown/reconciliation observation; close/crash quarantines unfinished ownership.
- [ ] Explicit physical endpoint IDs own capacity; canonical URLs are aliases, not separate slots. Normalize loopback spellings. Reject conflicting aliases and unsupported cross-host shared eligibility; the Windows/WSL/Mac route must not create two independent capacity authorities.
- [ ] Persist FIFO tickets and slots, acquire before native launch and retain quarantine after owner loss. Cancellation/release cannot affect another owner/ticket. Resource wait is not charged execution time. Reserve workspace first; do not hold an inference slot while waiting on workspace ownership.
- [ ] Persist recovery observations and stable reservation operation IDs. Reconcile crashes between project intent and coordinator reservation without claiming cross-database atomicity. Manual reconciliation requires explicit evidence/reason and current fencing identity.

Acceptance: independent DB connections/processes competing for parent/child/symlink/common-Git roots; similar-prefix siblings allowed; real process termination releases flock but preserves quarantine; two endpoint aliases share capacity; FIFO cancellation; stale ticket/owner cannot release; crash before/after reservation retains an inspectable state. Repeat filesystem/lock tests natively on macOS.

## 5D — execution boundary decision and qualification

Initial read-only inspection found cgroup v2 and bubblewrap 0.4.1 in WSL, but no delegated writable cgroup boundary/user service manager. After the user started Docker Desktop, its Linux engine became reachable (29.8.0). Docker is the implementation candidate. The Mac has no qualified VM/container runtime from Stage 3.5. Runtime availability does not establish production eligibility.

- [ ] Inventory an available runtime through a read-only doctor command. Record platform, runtime/version, required controls and unsupported reasons; readiness must consume this result.
- [ ] Select a concrete environment with read-only Git metadata/instructions, approved writable checkout, private native state, no broad host secrets/sockets, provider-only relay and descendant lifetime control. Runtime installation/host integration is a distinct user-visible setup step.
- [ ] Qualify the actual harness/profile, not just a shell: native authentication/model traffic must work through the boundary without exposing publishing credentials. Reuse existing Windows llama.cpp via an explicitly coordinated route; do not start another server implicitly.
- [ ] Test direct and alternate Git paths, protected-parent replacement, host credential reads, arbitrary network/localhost sockets, detached writers, signal loss and controller death. Verify boundary emptiness before releasing workspace and endpoint resources.
- [ ] Bind successful evidence to executable/runtime/profile/platform versions. Invalidate eligibility after relevant changes. Reproduce the macOS Codex/Hermes orphan-writer cases and prove both stop.

If runtime setup is unavailable, record D as blocked with the exact required setup and continue independent state/UI/tool validation. E's production dispatch remains closed. Installing a runtime is not itself a passing boundary test.

## 5E — one persisted execution

- [ ] Validate explicit repository/base/branch map and dirty-work choice; snapshot initial repository fingerprint. Prepare branch changes as journaled application effects.
- [ ] Persist run/config/limits/ownership before native process start. Bind all observed events to run, generation and native identity. Record `starting` before submit and distinguish proven-not-delivered from uncertain submission.
- [ ] Reserve the physical endpoint before create/resume because auxiliary inference can start there. Connect only qualified profile/boundary combinations.
- [ ] Ingest bounded events/artifacts and nullable usage. Native completion goes to checking only after result validation and writer proof; it never accepts the task.
- [ ] Add cumulative monotonic/persisted time segments. Human wait is excluded only with demonstrated native wait; unresolved crash time cannot enlarge the budget. No automatic replay after uncertainty.

Acceptance: one real scoped task through each qualified harness; injected crash at every journal/native boundary; reopen without duplicate submission; stream flood limits; failed persistence stops execution; inherited repair/time allowances. Use disposable repositories for live qualification.

## 5F–G — preservation, control and quality

- [ ] Pause disables future dispatch while the current run reaches its boundary. Stop retires pending native requests, interrupts once, enforces grace/termination and quarantines if writer or inference state is unknown.
- [ ] Resume requires an eligible recovery class, exact native identity/home/profile and reconciled workspace; unsupported interrupted/corrupt histories require an explicit fresh-context choice.
- [ ] Implement verified multi-repository content-addressed checkpoints with staged/unstaged/untracked distinctions. Verify every snapshot before any clear. Clear only provably owned differences with per-path compare-and-swap; never broad reset/clean/stash.
- [ ] Restore only an explicitly authorized set/destination fingerprint after a destination snapshot. Journal partial progress and preserve both recovery copies on conflicts.
- [ ] Execute approved checks inside the boundary and bind results to actual code/definition fingerprints. Accepted baseline failures remain visibly unhealthy; new failures block.
- [ ] Run a fresh read-only reviewer. Classify blocking findings versus suggestions; repairs consume cumulative allowance and invalidate affected evidence.
- [ ] Persist manual Pass/Fail/Cannot verify with evaluator, notes and fingerprint. Human acceptance waivers never imply manual Pass. Exhaustion invokes only a preauthorized bounded supervisor assessment.

Acceptance: mixed user/agent changes and binary/mode/symlink round-trip; one failed snapshot clears none; conflicting restore preserves both originals; paused queue stays stopped; stale native answers rejected; changes invalidate check/review/manual evidence; repair/time limits halt without accepting partial work.

## 5H–I — usable foreground UI and bounded proposals

- [ ] Introduce read-only project/status/plan/task/inbox/history views as soon as A–C persist them. Evolve the dashboard to read authoritative snapshots asynchronously and remain responsive during streaming.
- [ ] Add explicit plan queue/continue controls; select only eligible tasks with accepted dependencies. No automatic next-plan default and no within-project parallel agents.
- [ ] Expose the closed application handlers from the backlog before adding MCP transport. Enforce caller role, injected project scope, 64 KiB inputs, list/page/excerpt/proposal caps and expected revisions.
- [ ] Support read context, report-blocked observations and reorder/edit/split/merge proposals. Model claims cannot become acceptance evidence, permissions, raw SQL/shell or human criteria authorization.

Acceptance: UI restart shows persisted truth; output flood does not block stop/input; independent tasks can progress around blocked ones only after safe checkout preservation; malformed/oversized/foreign/stale proposals fail without partial mutations.

## 5J — delivery, archives and end-to-end milestone

- [ ] Journal application-owned exact-tree commits, approved ref/object push and draft GitHub/GitLab request creation as separate authorities. Expose no merge command and no force/mirror/checkpoint-ref push.
- [ ] Reconcile uncertain remote outcomes by stored operation/head/base identity before retry. Use local bare remotes/fake hosting in routine tests; a real draft request uses a concretely approved destination.
- [ ] Persist factual archive before narrative finalization. Reference validation and narrative failure cannot rerun accepted development. Preserve unfinished checkpoints/history under retention; transcript expiry never deletes durable decisions.
- [ ] Run the accepted milestone with both harnesses, a demonstrated review/repair cycle, checks, human/manual decisions and explicitly authorized draft delivery.

## Validation and completion protocol

Each slice receives focused failure tests, `make check`, race tests for concurrent code, a build and a reviewable local commit. Run cross-builds when platform code changes and Mac-native tests for identity/locking/process behavior. Reuse baseline harness evidence unless code/profile/boundary changes require new inference. Record actual model turns and never replay a failed prompt automatically.

Update [next steps](../../process/next-steps.md) and this checklist with implemented commands, limitations and the next action. Do not mark all Stage 5 complete while D or the end-to-end milestone is pending. Keep GitHub publication separate from local commits.

## Historical foundation checkpoint — 2026-09-20

The initial planning/control foundation is implemented and tested. [CLI guide](stage-5-cli.md) and [validation evidence](../../research/stage-5/foundation-results.md) describe the usable commands. The broad slice checkboxes above remain open where any named requirement is outstanding.

- **A implemented:** embedded version/digest-checked migrations; private WAL/FULL SQLite; atomic receipt/event commands; coherent readiness snapshots; immutable definition revisions; bounded content-addressed artifacts. Tests include reopened and concurrent command replay, rollback, modified/newer migrations, artifact corruption/symlinks and orphan publication.
- **B partial:** closed configuration/profile/plan inputs, graph/revision/criteria protection, deny/check/limit resolution, project/plan/task restriction intersection with provenance, explicit local-only eligibility, scoped permissions, bounded check definitions, manual setup prerequisites and internal effect-start transactions. At the time of this checkpoint, explicit repository enrollment/base maps (read-only nested discovery was implemented), downstream quality-evidence invalidation, profile capability qualification and shared global authorization remained pending. **All four have since been implemented** — enrollment in `internal/core/repositories.go`, staleness and invalidation in `internal/quality`, qualification in `internal/boundary`, global authorization in `coordination-002`/`internal/core/global_effect.go`. This block is a dated record, not current status.
- **C partial:** physical/ancestor/common-Git identity, advisory ownership, atomic root claims, FIFO endpoint slots, fencing and quarantine/manual reconciliation. Native Mac identity/locking tests passed. A project-side resource journal now covers intent, workspace claim, FIFO enqueue and slot acquisition with idempotent same-owner recovery. Native launch/submission/result journaling and coordinator global-grant start authorization have since been implemented; the full **live** crash-point matrix remains pending. Dated record, not current status.
- **D qualification foundation accepted:** runtime doctor, pinned Codex images, immutable trusted qualification records, exact eligibility, conservative checkout admission, guarded nested paths, effective-mount inspection contracts, lifecycle release evidence, disposable Docker probes, PID-1 guardian, scoped provider relay and worker Unix-socket bridge are implemented. Linux and macOS/OrbStack checks cover real Git bypasses, controller loss, quarantine and one contained Hermes turn per platform. See the [Stage 5.1 results](../../research/stage-5/5.1/results.md) and [boundary design](stage-5-boundary.md). Safe contained Codex subscription routing, provider-idle proof and the Stage 5.2 production launch/submission/result recovery matrix remain pending.
- **H initial reads:** project readiness, inbox, history and artifact/resource inspection are available through JSON CLI output. The TUI now displays consistent persisted overview/task/inbox/history snapshots with asynchronous refresh and bounded history/inbox views. Decision and dispatch controls remain pending.
- **E–G, I–J:** no production dispatch, tool-server or delivery flow is enabled. The **checkpoint and review flows are implemented and CLI-exposed** (`checkpoint-save`/`-clear`/`-restore`, `quality-review`); this line predates them. Two contained Hermes qualification turns have run, one per platform; no publishing actions occurred.

## Current checkpoint — 2026-09-26

- **5.1 / D:** the offline qualification/admission/lifecycle contract is implemented and independently accepted. Safe contained Codex routing, provider-idle proof, shared cross-host capacity authority and the real launch matrix remain pending, so production dispatch stays disabled.
- **5.2 / E:** repository enrollment, journaled fixture execution, exact results, ownership and recovery fencing are independently accepted offline through `d34f894`. Live qualified harness combinations remain pending.
- **5.3 / F:** durable pause/continue/stop, verified checkpoints, scoped clear/restore, exact/fresh recovery and cumulative retry/budget controls are independently accepted offline through `a182152`. Live interrupted-session qualification remains pending.
- **5.4 / G:** immutable quality scopes, contained fixture checks, fresh read-only review, bounded repair/assessment, manual/human evidence and atomic task/plan acceptance are independently accepted offline through `cba322b`. 5.4-R2, 5.4-R11, 5.4-R10 and 5.4-R1/5.4-R3/5.4-R4/5.4-R5/5.4-R6/5.4-R7/5.4-R8/5.4-R9 are closed. F1–F3 are remediated at `99cd6c0`: readiness is context/time bounded and fail-closed, signal exit evidence preserves the actual signal, and early cancellation is permanent coverage. Independent follow-up of that P3 commit is next. The Darwin fork-accounting observation remains separate and open.
- **Validation:** Linux full/race/build/boundary/cross-build gates, the retained Stage 5.4 review probe, permanent `TestIndependent`/`TestSupervisor` suites twice ordinarily and twice under race, and retained `TestStage54` regressions pass for `99cd6c0`. Native macOS validation is a separate evidence item and is not claimed by this checkpoint.
- **H–J:** Stage 5.5 is independently accepted offline at `84c0275`, with stale-selection retirement, expiring recovery, persisted proposal/recovery/quality/clarification interactions, bounded local planning, shared tool handlers/transport, native Hermes one-tool isolation and exact-commit native macOS validation. Stage 5.6 delivery/finalization is implemented and independently accepted for its autonomous scope, with all human-gated operations deferred to [Stage 8](../stage-8/stage-8.md); Stage 5.7 autonomous qualification is independently accepted at `522cb96` for its autonomous scope; its walkthrough found blocking delivery defect 5.7-F1, now fixed in the Stage 5.2/5.6 slice. [Stage 6](../stage-6/stage-6.md) terminal interface parity and [Stage 7](../stage-7/stage-7.md) the documentation website were added by user scope revision on 2026-09-29. Stage 6.1, its gap analysis, is implemented, was rejected by its first independent review, returned conditional by each of six further reviews, and all seven rounds of findings are remediated pending one more confirming review: it wrote no interface code, measured that the terminal interface fully expresses 4 of the 82 distinct capabilities the 85-row register classifies, and 6 of the 25 human decision classes, amended the unmeasurable R11/R45/R46/R47/R70 with P13–P18, and planned 6.2–6.9. [Analysis](../stage-6/6.1-parity-gap-analysis.md), [register and feature list](../../research/stage-6/results.md). No production activation, paid call, push or publication occurred during implementation; two explicitly authorized local Hermes planning turns and one successful bounded Hermes tool turn were used.

Next action: begin Stage 5.6 delivery/finalization from accepted Stage 5.5 implementation commit `84c0275` only after explicit user instruction, preserving every accepted safeguard. The example proposal remains deliberately unapproved; production Codex/reviewer qualification, the Darwin observation and all real live/user gates stay distinct and disabled.
