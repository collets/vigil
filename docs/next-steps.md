# Next steps and resumption plan

Updated: 2026-09-22. Stages 3/3.5 investigations and Stage 4 specification are complete. Stage 5.1 offline qualification/review fixes and the independently accepted Stage 5.2 offline implementation are complete at their stated scope. Stage 5.3 through `a182152` is independently accepted offline, with R1–R10 closed. Stage 5.4 is implemented offline through `03f65e8` and awaits independent review. Live production qualification remains pending.

Status: Stage 3 Linux experiments and adapter improvements are complete, with explicit unresolved qualification gates. See [Stage 3 results](research/stage-3-results.md). Both Stage 3 and Stage 4 plans were prepared before implementation. Stage 4 is complete: [core specification](core-spec.md), [validated draft schemas](spec/project.sql), and [Stage 5 backlog/requirements map](stage-5-plan.md). [Stage 3.5 macOS checks](research/stage-3.5-results.md) are also complete: basic runtime behavior passed, but both harnesses left writers after abrupt loss and strict production containment remains unsupported.

## Resume here

Follow-up [Stage 5.1 validation](research/stage-5.1-astra-review.md) confirms R1–R4 are resolved. The [Stage 5.2 follow-up review](research/stage-5.2-astra-review.md#independent-follow-up-acceptance-of-d34f894) closes R6/R8 at `d34f894`, with R1–R5, R7 and R9 remaining closed. The offline implementation review is accepted. Independent Linux full/race/build/cross-build and retained budget/wait reproductions pass. Production dispatch stays disabled pending live qualification.

The next action is an independent review of [Stage 5.4 — quality and acceptance](stage-5/5.4-quality-and-acceptance.md) using the exact instructions in [Stage 5.4 results](research/stage-5.4-results.md#independent-review-instructions). Do not begin Stage 5.5 from implementation evidence alone. The [Stage 5.3 independent acceptance](research/stage-5.3-astra-review.md#independent-follow-up-acceptance-of-a182152) closes R1–R10 at the offline scope. The joint live gate remains open: safe contained Codex and reviewer routes, independent provider-idle proof, shared Mac/WSL capacity authority and live recovery evidence for both harnesses/platforms. Production dispatch stays disabled.

The user has answered the setup questions for 5.1–5.3: existing Codex included subscription usage is authorized after verifying no additional charges (otherwise defer live Codex); use existing local llama; demonstrate repository execution only in disposable repositories; and perform destructive recovery tests only in agent-owned disposable fixtures. Preserve real checkout changes and defer ambiguous real recovery. See the confirmed decisions in each plan and [pending decisions](pending-decisions.md); do not ask for these permissions again.

1. Work in `/home/scoletta/development/scdeveloper/vigil`.
2. Read this document, [session continuity audit](session-audit.md), [requirements](requirements.md), [architecture](architecture.md), and [harness investigation](harness-capabilities.md).
3. Inspect current Git status and applicable repository instructions. Preserve existing files and uncommitted work; do not reset the checkout.
4. Run `make check` to establish the starting baseline. Check installed harness versions against the evidence below; refresh affected protocol research if versions differ.
5. Read the [Stage 1 contract and profiles](adapter-spike.md), [Stage 2 results](research/stage-2-results.md), completed [core specification](core-spec.md), and [macOS findings](research/stage-3.5-results.md). Implement Stage 5 in slices; containment qualification gates production editing/delivery.

Suggested independent-review handoff prompt:

> Independently review Stage 5.4 over `4b48737..03f65e8`, beginning with `docs/research/stage-5.4-results.md` and `docs/stage-5/5.4-quality-and-acceptance.md`. Reproduce the migration, check, freshness, review, repair/exhaustion, human/manual and atomic acceptance cases in disposable fixtures. Preserve Stage 5.3 R1–R10, keep production dispatch disabled, and report findings without self-extending authority or beginning Stage 5.5.

## Current state

- Stage 5.1 now provides immutable schema-v2 qualification records with retained artifact validation, collision-safe evidence-class lookup, exact core eligibility, pinned Codex amd64/arm64 images, explicit nested-repository admission, guarded nested ancestors, post-create mount inspection, read-only reviewer mounts, inference-start-aware lifecycle evidence and cross-host capacity rejection. R1–R4 are fixed with permanent negative tests and independently reviewed. [Results and remaining gates](research/stage-5.1-results.md). Production model dispatch remains disabled pending live Codex/provider-idle proof and Stage 5.2 recovery evidence through each real combination.
- Stage 5.2 adds immutable repository/run/generation/effect/result/budget state, explicit enrollment/fingerprints, crash-reconcilable branches, atomic all-root ownership and `internal/supervisor`. Review fixes now fence interrupted effects/delivered prompts, live reservation authority, exact branch revisions, production bindings, fixture writes and active budgets; containment and human-wait ordering/accounting are permanent tests. Linux and exact-commit native macOS check/race/build pass. Production drivers require an independently derived exact Stage 5.1 binding and remain disabled.
- Stage 5.3 adds durable pause/continue/stop, request retirement, private verified checkpoint sets, per-path CAS clear/restore journals, destination-bound three-way recovery, exact-resume versus fresh-attempt identity, retry-class limits and task-cumulative exhaustion evidence. Linux full/race/build/cross-build and exact-commit native macOS full/race/build plus focused destructive-fixture tests pass. The offline implementation is independently accepted at `a182152`; all live interrupted-session combinations remain pending and production dispatch is unchanged and disabled.
- Stage 5.4 adds exact immutable quality scopes, isolated real check processes, durable effect/result artifacts, exact baseline authorities, distinct read-only reviews, deterministic finding thresholds, cumulative bounded repairs/assessments, typed fixture-human evidence and atomic task/plan acceptance. Offline Linux and exact-checkpoint native macOS validation pass through `03f65e8`. Independent review, live reviewer/runtime qualification and real human/manual gates remain pending. Acceptance creates no commit, push, publication or delivery authority.
- Go hello-world scaffold with Cobra, Bubble Tea, Bubbles, Lip Gloss, and SQLite. Commands: `hello`, `dashboard`, and the development-only `spike`. Bounded stdio transports and session adapters are implemented in `internal/harness`; isolated execution/verification lives in `internal/spike`.
- Scaffold passed Linux CLI/SQLite/interactive smoke checks and Linux/macOS amd64/arm64 cross-compilation. Stage 3.5 separately passed native macOS arm64 Go/race/schema/build checks and CLI/dashboard smoke tests; macOS amd64 remains cross-build-only.
- Functional baseline consolidated as R01–R71. See [first usable milestone](mvp-acceptance.md) for the eventual demonstration.
- Recommended transports: Codex app-server over stdio; Hermes TUI gateway over stdio. Hermes ACP is an alternative, not another adapter to build now.
- Metadata handshakes passed on Linux with Codex 0.155.1 and Hermes 0.21.3, source commit `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`.
- Model-backed execution, streamed output/tool activity, effective workspace/profile checks, identities, exact file/diff verification, and strict structured results passed through both harnesses on Linux. Synthetic/race tests cover transport failures, requests, and bounded shutdown. Stage 3 subsequently exercised normal interruption and exact completed-session resume; complete enforcement and abrupt-loss cleanup remain gated by observed limitations. No production scheduler exists.
- Stage 1 contract, versioned experiment profiles, fixture and preparation/verification scripts are complete. Stage 5.1 test templates now select Codex `gpt-5.6-luna` at low effort; the existing ChatGPT account and advertised model passed a metadata-only probe. Historical Astra runs remain historical. Hermes resolves `custom` / `qwen3.8-27b-local` through the existing local route.
- llama.cpp was healthy during Stage 2: authenticated metadata and tool use passed with 131072 context tokens and one slot. The inherited `OPENAI_BASE_URL` and `OPENAI_API_KEY` work; do not print or commit the key. Codex advertised and executed `gpt-6-astra` using existing ChatGPT authentication. Recheck service health before future experiments.
- Stage 3 added exact resume, lifecycle scenarios, native request/policy probes and provider-error injection. Resume, normal interruption, Codex approval and Hermes clarification passed; abrupt Hermes child cleanup failed. Native commit/push coverage is not a complete application gate. [Full evidence and limits](research/stage-3-results.md).
- Stage 3.5 used user-authorized SSH, Go 1.27.1, nvm Node 24.21.0, Codex 0.155.1 and locked Hermes 0.21.3/Python 3.11.16 on macOS 26.6.2 arm64. Sixteen model turns across fourteen cases yielded eleven passes and three explicit failures: both abrupt-loss writer checks and Codex native clarification. The temporary tunnel reached native Windows llama.cpp through WSL. No model was installed on the Mac, and no cross-host inference capacity guarantee is established.
- Durable Stage 2 evidence: [validation report](research/stage-2-results.md) and [sanitized results](research/stage-2-results.json). One initial Hermes response included prose before JSON and was correctly rejected; a fresh fixture with explicit JSON-only instructions passed. No automatic replay occurred.
- Durable Stage 1 evidence: [settings, versions and validation results](research/stage-1-results.json). Repeat preparation with `~/.hermes/hermes-agent/venv/bin/python scripts/spike/prepare.py`; it creates a fresh private experiment and starts no inference.
- Durable evidence: [probe results](research/harness-probe-results.json). Temporary scripts and generated schemas are in ignored `.cache/research/`; do not rely on that directory being available in another checkout.

## Constraints to carry forward

- Linux and macOS targets. Go remains the application language; existing harnesses retain their agent loops and tools.
- Application owns state, transitions, approvals, retries, and model eligibility. Models propose changes through bounded, validated operations.
- Sequential execution per project; multiple projects only on nonoverlapping folder trees. Shared local endpoints default to one active agent across application instances.
- Existing llama.cpp service; no model-server lifecycle management. Manual profiles first.
- Fresh reviewer sessions; required checks and review before acceptance. Human gates are configurable, but explicit manual checks remain manual.
- Foreground execution, explicit pause/stop/resume, bounded repair/time allowances, and preserved recovery evidence.
- Dedicated plan branches, no managed worktrees initially. Never delete existing user changes. Saved-work restoration requires approval.
- Draft GitHub PR/GitLab MR is the delivery endpoint. No automatic merging.
- Latest necessary software may be installed. Preserve version evidence and avoid changing the user's global harness profiles as part of experiments.

## Stage 1 — Define the spike and runnable profiles

Scope: produce a reviewed contract and reproducible, inference-free preparation. Do not implement the Stage 2 Go transport, scheduler, or dashboard in this stage. A stopped llama server does not block preparation; live inference and tool compatibility remain Stage 2 gates.

1. **Establish evidence.** Preserve the existing checkout, run `make check`, compare installed versions and relevant source fingerprints with the earlier probe. Inspect only necessary configuration fields and credential presence/type; never copy credentials into evidence.
2. **Specify the adapter.** Define probe/create/submit/observe/answer/interrupt/inspect/close, optional resume/steer, method preconditions, ambiguous submission handling, capability evidence levels, and ownership of process versus session lifetimes.
3. **Specify identity and events.** Separate application run/turn IDs, native runtime/durable session IDs, native turn IDs, transport generations, and directional request IDs. Define normalized output/tool/input/approval/usage/outcome/failure records, request expiration, duplicate terminal handling, and task acceptance outside the adapter.
4. **Make the experiment bounded.** Define one small file-edit task with a deterministic check, a fresh temporary Git repository with no remotes, one active turn, finite time/output limits, no replay or automatic retries, and retained failure evidence. State which limits are implemented now versus required of the Stage 2 runner.
5. **Resolve explicit profiles.** Select existing Codex ChatGPT authentication and configured model without changing global settings. Resolve the llama.cpp URL/model from existing Hermes configuration; record metadata reachability separately from inference readiness. Treat any alternative paid route as a new spending choice.
6. **Prepare and verify isolation.** Create fresh private homes from credential-free templates. Pin Hermes toolsets, disable crash continuation/delegation/compression/memory and cloud fallbacks, constrain auxiliary routes, and inspect settings through the installed Hermes loader. Use an explicit environment allowlist; preserve the user's global profiles. Never start inference as part of preparation.
7. **Close the stage.** Save the contract, templates, preparation command and sanitized evidence in durable files. Verify repeatable preparation, fixture checks, template/config parsing and `make check`. List missing runtime inputs and the exact Stage 2 entry point.

Completion checklist:

- [x] Adapter contract, identity/event semantics, outcome/acceptance separation documented.
- [x] Disposable fixture and explicit execution/evidence limits reproducible (live supervisor enforcement belongs to Stage 2).
- [x] Codex and local Hermes profiles resolved, with credentials referenced separately.
- [x] Isolated Hermes effective settings verified against the pinned installation.
- [x] Validation evidence and any missing inputs recorded; handoff updated.

Deliverable: a short adapter design document and reproducible probe configuration, with missing inputs explicitly listed. Configuration must not contain committed credentials.

Completed artifacts: [contract and runbook](adapter-spike.md), [profile manifest](../config/spike/profiles.json), [preparation helper](../scripts/spike/prepare.py), [native Hermes verification](../scripts/spike/verify_hermes.py), and [disposable fixture](../testdata/spike/README.md). `make check`, native config checks, repeat preparation, fixture positive/negative checks and unsafe-profile rejection passed. No model turns were started. Codex's native loader rejects schema-listed `untrusted`; the runnable profile uses `on-request` with user approval routing. Hermes background self-review must be explicitly disabled alongside crash continuation and title generation.

## Stage 2 — Implement the minimum Go transport and adapters

Follow the [complete implementation plan](stage-2-plan.md): baseline/profile validation → shared process transport → session/event semantics → Codex → Hermes → bounded CLI runner → synthetic/race checks → explicit live fixture turns and durable evidence. The plan defines failure behavior, package boundaries, runtime limits and the Stage 3 exclusions.

- [x] Build process/stdin/stdout supervision with bounded buffering, stderr diagnostics, request correlation, deadlines, and orderly shutdown.
- [x] Keep protocol reading responsive while handling native approval/input requests. Support cancellation of outstanding requests and reject stale responses.
- [x] Implement Codex initialization and a single controlled turn through app-server.
- [x] Implement Hermes gateway capability registration, session creation, and a single controlled turn. Distinguish submit acknowledgement from terminal completion.
- [x] Expose a minimal development command or integration runner; defer dashboard integration.
- [x] Add focused transport tests for disconnects, malformed messages, concurrent requests, duplicate terminal events, and cancellation. Use recorded/synthetic fixtures for routine tests; live inference must be explicit.

Acceptance: each harness completes one bounded task in the fixture repository, streams observable activity, reports the selected workspace/profile, and returns identifiable results. An error or malformed result never becomes successful task acceptance.

## Stage 3 — Prove lifecycle and policy boundaries

The [expanded plan](stage-3-plan.md) was executed on Linux; [results](research/stage-3-results.md) distinguish passing observations, failed guarantees and unqualified cases. No requirement was relaxed to fit native behavior.

- [x] Normal interruption during model output and heartbeat writes tested through both harnesses; finite writer observations recorded.
- [x] Exact completed-session restart/resume and recall tested; missing identities rejected without fallback. Corrupt real native-history and interrupted-history recovery remain unqualified.
- [x] Live Codex one-time command approval/denial and Hermes clarification tested. Installed Hermes request expiry/cancel/late answers and synthetic stale-generation cases covered; live Codex clarification remains unqualified.
- [x] Profile/continuation/route audit performed, including one controlled local provider rejection. Full OS egress, auxiliary concurrency and cross-instance capacity guarantees remain unsupported until a stronger boundary exists.
- [x] Representative commit/ref-write/local-push paths probed. Bypasses/limitations recorded; no hosting actions performed.
- [x] Forced loss retained unknown outcomes/partial files without replay. Hermes detached-child cleanup failed and now explicitly requires quarantine in the core design.
- [x] Guarantee levels and Linux/macOS limits recorded; bounded alternative containment designs carried into Stage 4.
- [ ] Production execution/recovery qualification: corrupt/interrupted native history, live Codex input, full inference/permission boundary and arbitrary writer cleanup still require implementation evidence. These remain gates, not passed capabilities.
- [x] Bounded macOS runtime investigation completed in Stage 3.5; production containment/recovery remains gated.

Investigation deliverable complete: native adapters and evidence support the core specification, with strict production dispatch gated on the unresolved boundaries.

## Stage 3.5 — macOS setup and runtime qualification

Completed through user-authorized SSH after Stages 3 and 4. See the [plan](stage-3.5-macos.md), [results](research/stage-3.5-results.md) and [sanitized evidence](research/stage-3.5-results.json). Fresh Mac preparations passed editing, completed-history resume, normal interruption, Codex approvals and Hermes clarification. Forced transport loss left writers under both harnesses; Codex clarification was not demonstrated. Native Git gate gaps reproduced. Case/Unicode aliases, shared Git identity, advisory locks, persisted quarantine and narrow Seatbelt primitives were investigated. All recorded native processes and copied auth were cleaned up; private evidence remains in ignored caches. Strict production eligibility is not qualified.

## Stage 4 — Specify the application core

Specified from Stage 3's observed boundaries. See [core-spec.md](core-spec.md), [draft project/coordination schemas](spec/project.sql), [validation record](research/stage-4-results.md), and [Stage 5 requirement map](stage-5-plan.md). No application schema was installed.

- [x] Define project/plan/task/run state transitions, pause versus stop, crash reconciliation, and separate finalization/delivery states.
- [x] Define SQLite schema and migrations, event/artifact boundaries, configuration precedence, and profile capability validation.
- [x] Specify approval resolution and revocation for once/task/plan/permanent scopes; project restrictions dominate narrower grants.
- [x] Specify retry and execution-time accounting, including review/supervisor/check time and excluded approval/resource waits.
- [x] Design shared folder-tree ownership and endpoint capacity coordination, canonical identities, stale-owner recovery, and fair waiting.
- [x] Design branch preparation and checkpoint recovery, including mixed user/agent edits, restoration conflicts, and multiple repositories.
- [x] Define bounded model-facing tools and validated result records. Plan edits must respect revision checks and acceptance-criteria restrictions.

Deliverable: concrete design decisions and implementation tasks mapped to the existing requirements. Revisit only decisions affected by evidence or unresolved product questions.

## Stage 5 — Deliver a narrow functional slice

Follow the [expanded execution plan](stage-5-execution.md) and [Stage 5 backlog](stage-5-plan.md), including containment before strict production editing. The initial persisted planning/control slice and read-only project dashboard are implemented; broad B/C requirements, D profile qualification and E–J remain in progress/pending. The original functional order remains:

1. Manual project/profile setup, repository discovery, branch preparation, and one persisted task execution.
2. Sequential dispatch with approvals, pause/stop/recovery, and shared workspace/local-model coordination.
3. Fresh review, bounded repair, configured quality checks, and human acceptance.
4. Dashboard progression and actionable inbox, then detailed findings/check views.
5. Markdown specification to approved plan, with bounded supervisor tools for permitted plan changes.
6. Durable finalization records and explicit draft GitHub/GitLab delivery, followed by retention handling.

Acceptance: run the [agreed milestone](mvp-acceptance.md), including actual execution through both harnesses and recovery/boundary checks. Validate Linux and macOS runtime support separately.

## End-of-session handoff

- Update the checkboxes and current-state section with completed work.
- Record exact validation commands/results and remaining limitations.
- Keep durable findings in `docs/`, not only ignored caches or conversation history.
- Record the next concrete action and any input needed from the user.
- Preserve unfinished work and running-session identities; stop experimental processes before ending the session.

Next concrete action: independently review [Stage 5.4](stage-5/5.4-quality-and-acceptance.md) over `4b48737..03f65e8` using [the exact review checklist](research/stage-5.4-results.md#independent-review-instructions). Do not begin Stage 5.5 or close the shared live gate from synthetic evidence. Safe Codex/reviewer routes, provider-idle proof, shared capacity authority, the real runtime crash matrix and real human/manual decisions remain separate gates.

Away-time constraints and deferred user decisions: [pending decisions](pending-decisions.md).
