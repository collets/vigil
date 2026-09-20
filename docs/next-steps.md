# Next steps and resumption plan

Updated: 2026-09-20. Stage 3 Linux investigation complete; Stage 4 specification in progress.

Status: Stage 3 Linux experiments and adapter improvements are complete, with explicit unresolved qualification gates. See [Stage 3 results](research/stage-3-results.md). Both Stage 3 and Stage 4 plans were prepared before implementation. Stage 4 is in progress. The user scheduled [Stage 3.5 macOS setup/runtime checks](stage-3.5-macos.md) after this work, when they return.

## Resume here

1. Work in `/home/scoletta/development/scdeveloper/vigil`.
2. Read this document, [session continuity audit](session-audit.md), [requirements](requirements.md), [architecture](architecture.md), and [harness investigation](harness-capabilities.md).
3. Inspect current Git status and applicable repository instructions. Preserve existing files and uncommitted work; do not reset the checkout.
4. Run `make check` to establish the starting baseline. Check installed harness versions against the evidence below; refresh affected protocol research if versions differ.
5. Read the [Stage 1 contract and profiles](adapter-spike.md) and [Stage 2 results](research/stage-2-results.md), then follow the [Stage 4 plan](stage-4-plan.md). Do not start implementing the full scheduler or dashboard before resolving the critical adapter feasibility questions.

Suggested handoff prompt:

> Resume Vigil using docs/next-steps.md, docs/research/stage-3-results.md and docs/stage-4-plan.md. Stage 3 Linux investigation is complete with documented enforcement/recovery gaps. Finish the Stage 4 specification and requirement map, without implementing the production scheduler. Stage 3.5 macOS setup/runtime work is reserved for the returning user.

## Current state

- Go hello-world scaffold with Cobra, Bubble Tea, Bubbles, Lip Gloss, and SQLite. Commands: `hello`, `dashboard`, and the development-only `spike`. Bounded stdio transports and session adapters are implemented in `internal/harness`; isolated execution/verification lives in `internal/spike`.
- Scaffold previously passed CLI/SQLite/interactive smoke checks, Go checks, and Linux/macOS amd64/arm64 cross-compilation. Cross-compilation is not macOS runtime verification.
- Functional baseline consolidated as R01–R71. See [first usable milestone](mvp-acceptance.md) for the eventual demonstration.
- Recommended transports: Codex app-server over stdio; Hermes TUI gateway over stdio. Hermes ACP is an alternative, not another adapter to build now.
- Metadata handshakes passed on Linux with Codex 0.155.1 and Hermes 0.21.3, source commit `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`.
- Model-backed execution, streamed output/tool activity, effective workspace/profile checks, identities, exact file/diff verification, and strict structured results passed through both harnesses on Linux. Synthetic/race tests cover transport failures, requests, and bounded shutdown. Stage 3 subsequently exercised normal interruption and exact completed-session resume; complete enforcement and abrupt-loss cleanup remain gated by observed limitations. No production scheduler exists.
- Stage 1 contract, versioned experiment profiles, fixture and preparation/verification scripts are complete. Both native configuration checks passed. Codex uses existing ChatGPT auth and `gpt-6-astra`; Hermes resolves `custom` / `qwen3.8-27b-local` at `http://127.0.0.1:8080/v1`.
- llama.cpp was healthy during Stage 2: authenticated metadata and tool use passed with 131072 context tokens and one slot. The inherited `OPENAI_BASE_URL` and `OPENAI_API_KEY` work; do not print or commit the key. Codex advertised and executed `gpt-6-astra` using existing ChatGPT authentication. Recheck service health before future experiments.
- Stage 3 added exact resume, lifecycle scenarios, native request/policy probes and provider-error injection. Resume, normal interruption, Codex approval and Hermes clarification passed; abrupt Hermes child cleanup failed. Native commit/push coverage is not a complete application gate. [Full evidence and limits](research/stage-3-results.md).
- The user has a Mac and explicitly deferred access/setup to Stage 3.5 after the current Stage 3/4 work. No credentials or access are needed now.
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
- [ ] macOS runtime qualification is scheduled with the user as Stage 3.5.

Investigation deliverable complete: native adapters and evidence support the core specification, with strict production dispatch gated on the unresolved boundaries.

## Stage 3.5 — macOS setup and runtime qualification

The user requested this follow-up after Stages 3 and 4, when they return. Follow the [macOS plan](stage-3.5-macos.md). Prefer running locally on the Mac; choose SSH only if useful. Reprepare fixtures there, do not copy Linux credentials/native homes, and qualify platform capabilities independently.

## Stage 4 — Specify the application core

Proceed after the spike establishes the usable adapter boundaries.

- [ ] Define project/plan/task/run state transitions, pause versus stop, crash reconciliation, and separate finalization/delivery states.
- [ ] Define SQLite schema and migrations, event/artifact boundaries, configuration precedence, and profile capability validation.
- [ ] Specify approval resolution and revocation for once/task/plan/permanent scopes; project restrictions dominate narrower grants.
- [ ] Specify retry and execution-time accounting, including review/supervisor/check time and excluded approval/resource waits.
- [ ] Design shared folder-tree ownership and endpoint capacity coordination, canonical identities, stale-owner recovery, and fair waiting.
- [ ] Design branch preparation and checkpoint recovery, including mixed user/agent edits, restoration conflicts, and multiple repositories.
- [ ] Define bounded model-facing tools and validated result records. Plan edits must respect revision checks and acceptance-criteria restrictions.

Deliverable: concrete design decisions and implementation tasks mapped to the existing requirements. Revisit only decisions affected by evidence or unresolved product questions.

## Stage 5 — Deliver a narrow functional slice

Implement in this order, expanding only after each increment works:

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

Next concrete action: finish the Stage 4 architecture/schema/tool contracts and requirements traceability, then hand off to Stage 3.5 macOS setup with the returning user. Preserve all Stage 3 limitations as dispatch/recovery gates.
