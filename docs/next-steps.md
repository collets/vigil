# Next steps and resumption plan

Updated: 2026-09-20. Stages 1 and 2 complete.

Status: Stage 2 complete. Both real harnesses passed the bounded editing fixture on Linux. See the [Stage 2 plan](stage-2-plan.md) and [validation report](research/stage-2-results.md). Next: Stage 3 lifecycle and policy boundaries.

## Resume here

1. Work in `/home/scoletta/development/scdeveloper/vigil`.
2. Read this document, [session continuity audit](session-audit.md), [requirements](requirements.md), [architecture](architecture.md), and [harness investigation](harness-capabilities.md).
3. Inspect current Git status and applicable repository instructions. Preserve existing files and uncommitted work; do not reset the checkout.
4. Run `make check` to establish the starting baseline. Check installed harness versions against the evidence below; refresh affected protocol research if versions differ.
5. Read the [Stage 1 contract and profiles](adapter-spike.md) and [Stage 2 results](research/stage-2-results.md), then expand Stage 3. Do not start implementing the full scheduler or dashboard before resolving the critical adapter feasibility questions.

Suggested handoff prompt:

> Resume Vigil using docs/next-steps.md and docs/research/stage-2-results.md. Stage 2 is complete: both harnesses passed live fixture execution. Expand Stage 3 into concrete lifecycle/policy experiments and proceed within those boundaries. Recheck llama health and inherited OPENAI_BASE_URL/OPENAI_API_KEY without exposing credentials. Preserve existing work and failed-attempt evidence; do not build the scheduler/dashboard yet.

## Current state

- Go hello-world scaffold with Cobra, Bubble Tea, Bubbles, Lip Gloss, and SQLite. Commands: `hello`, `dashboard`, and the development-only `spike`. Bounded stdio transports and session adapters are implemented in `internal/harness`; isolated execution/verification lives in `internal/spike`.
- Scaffold previously passed CLI/SQLite/interactive smoke checks, Go checks, and Linux/macOS amd64/arm64 cross-compilation. Cross-compilation is not macOS runtime verification.
- Functional baseline consolidated as R01–R71. See [first usable milestone](mvp-acceptance.md) for the eventual demonstration.
- Recommended transports: Codex app-server over stdio; Hermes TUI gateway over stdio. Hermes ACP is an alternative, not another adapter to build now.
- Metadata handshakes passed on Linux with Codex 0.155.1 and Hermes 0.21.3, source commit `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`.
- Model-backed execution, streamed output/tool activity, effective workspace/profile checks, identities, exact file/diff verification, and strict structured results passed through both harnesses on Linux. Synthetic/race tests cover transport failures, requests, and bounded shutdown. Live cancellation, native resume, and end-to-end permission enforcement remain Stage 3. No production scheduler exists.
- Stage 1 contract, versioned experiment profiles, fixture and preparation/verification scripts are complete. Both native configuration checks passed. Codex uses existing ChatGPT auth and `gpt-6-astra`; Hermes resolves `custom` / `qwen3.8-27b-local` at `http://127.0.0.1:8080/v1`.
- llama.cpp was healthy during Stage 2: authenticated metadata and tool use passed with 131072 context tokens and one slot. The inherited `OPENAI_BASE_URL` and `OPENAI_API_KEY` work; do not print or commit the key. Codex advertised and executed `gpt-6-astra` using existing ChatGPT authentication. Recheck service health before future experiments.
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

- [ ] Interrupt during inference and during a long-running child command. Establish when filesystem writers have stopped; record unknown outcomes instead of claiming clean termination.
- [ ] Restart and resume a known session with verified identity/workspace. Missing or corrupt sessions require an explicit fresh-start choice, not silent replacement.
- [ ] Exercise human input and native approvals: allow, deny, timeout, cancellation, and late response. Do not translate project grants into global native allowlists.
- [ ] Verify Hermes cannot restart or queue new work outside application scheduling. Confirm nested/auxiliary inference respects local-only and capacity policy.
- [ ] Investigate enforcement of gated commits, pushes, and publishing through alternative tool/shell paths. Application-owned operations alone do not prevent bypass by unrestricted workers.
- [ ] Record each guarantee as application-enforced, native-enforced, advisory, or unsupported. If hard enforcement needs an execution/credential/filesystem boundary, propose concrete options before promising autonomous readiness.
- [ ] Exercise provider errors, partial output, transport loss, and unexpected process exit without automatically replaying uncertain work.
- [ ] Run essential lifecycle checks on macOS when a host is available. If unavailable, record the gap; continue independent Linux work without claiming cross-platform runtime validation.

Deliverable: a runtime validation report containing versions, commands, outcomes, limitations, and retained sanitized evidence. Update the capability matrix from actual results.

Exit gate: both transports demonstrate usable execution and recovery; unresolved enforcement/platform limitations are explicit. Do not quietly relax accepted requirements to make the spike pass. If a transport fails, evaluate a bounded alternative and record the tradeoff.

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

Next concrete action: expand Stage 3 into bounded live interruption, request/approval, exact-resume, and policy-boundary experiments using the Stage 2 runner/adapter foundation. No new credential input is needed on this machine. A macOS host will eventually be needed for runtime validation; its absence does not block independent Linux work.
