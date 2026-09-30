# Initial harness capability investigation

<!-- vigil-tier: core -->
<!-- vigil-status: stage=5.6; stage_accepted=true; implementation_commit=b0a085b5551bf58ab8412e649405eef65a91f61d -->

Investigated 2026-09-20. Recommendation: Codex app-server over stdio and Hermes TUI gateway over stdio for the first adapters. Both retain the original harness. The Go application owns task state, scheduling, approval policy, and acceptance.

Verdict: the transport/execution foundation passes a controlled model-backed fixture through both harnesses on Linux. Stage 3 runtime validation is complete (see [Stage 3 results](../research/stage-3/results.md)) and offline recovery, policy, workflow, bounded-planning and delivery/finalization boundaries are independently accepted through Stage 5.6 at `b0a085b` (see [next steps](../process/next-steps.md)). Stage 5.7 autonomous qualification is implemented and under independent review. Do not advertise complete approval enforcement, live recovery qualification, or production containment: the native approval and abrupt-loss gaps found in Stage 3 remain open, and production dispatch is still disabled.

Stage 1 follow-up (2026-09-20): [contract and reproducible isolated profiles](../history/adapter-spike.md), with [effective-setting evidence](../research/stage-1/results.json). Versions are unchanged. Native settings checks passed without inference. Hermes background self-review is also disabled; every auxiliary task is explicitly pinned locally. Codex's native config loader rejects `untrusted` despite its presence in the generated schema; the spike uses `on-request` and user approval routing. At the Stage 1 checkpoint, the local llama.cpp endpoint was stopped; those settings checks alone did not establish live capabilities.

Stage 2 follow-up (2026-09-20): [bounded execution and validation](../research/stage-2/results.md), with [sanitized run evidence](../research/stage-2/results.json). Both harnesses passed profile/workspace checks, observable streaming/tool use, native completion, strict JSON, and independent fixture verification. Hermes used the working inherited OpenAI-compatible localhost credentials. One initial Hermes prose-prefixed result was rejected. Native input/approvals, interruption under load, resume, policy enforcement, and macOS runtime remain unverified live.

Stage 3 follow-up (2026-09-20): [Linux lifecycle/policy evidence](../research/stage-3/results.md). Exact completed-session resume, normal output/child interruption, Codex one-time approval/denial and Hermes clarification were exercised. Abrupt Hermes transport loss left a bounded child writing. Native policy probes did not establish full commit/push enforcement. Strict production eligibility therefore remains gated.

Stage 3.5 follow-up (2026-09-20): [macOS arm64 results](../research/stage-3.5/results.md) qualify the same pinned harness versions for bounded editing, completed-history resume and normal interruption. Both Codex and Hermes left writers running after abrupt transport loss on macOS. Native commit/push gaps reproduced; live Codex clarification was not demonstrated. Case/Unicode aliases require filesystem identity comparisons. Inference remained on native Windows through WSL and an SSH tunnel; no Mac-local model server was installed.

## Evidence and versions

- Codex CLI **0.155.1**: version/help inspected; generated 312 JSON Schema files; initialize handshake passed. Its CLI labels app-server experimental, so pin and validate its protocol version rather than assuming compatibility with every update.
- Hermes **0.21.3**, source commit `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`: ACP dependency check, ACP initialize, gateway-ready event, gateway capabilities, and client request-capability registration passed. Relevant inspected files had no local modifications.
- Initial tests ran on Linux amd64; Stage 3.5 subsequently tested macOS 26.6.2 arm64 with separately recorded limits. Neither platform's observed native profile qualifies strict production containment.
- The first Codex handshake failed because this research sandbox could not write Codex's normal SQLite state. An approved retry with normal state access passed. This was an environment restriction, not a protocol failure.
- The initial investigation started no model turns. Stage 2 subsequently verified live streaming execution and tool use; interruption under load, native resume, and approvals remain source-backed/synthetic-tested until exercised live. No publication actions were started by a fixture.

Sanitized results, source hashes, and schema field summaries are in [probe results](../research/stage-1/harness-probe-results.json). Full generated schemas and temporary probe files remain in ignored `.cache/research/`.

## Transport selection

### Codex: app-server

The documented app-server protocol provides threads, turns, streaming events, user-input requests, and approval responses. Use the stdio handshake (`initialize`, then `initialized`) and explicit IDs. Prefer it over `codex exec --json` for an interactive dashboard; exec remains suitable for one-shot jobs. Protocol schemas can be generated from the installed CLI. [App-server documentation](https://learn.chatgpt.com/docs/app-server), [non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode).

The installed schemas confirm `thread/start`, resume parameters, `turn/start` output schemas, steering with `expectedTurnId`, interruption identifiers, and approval/user-input structures. Keep the raw wire types behind the adapter; do not expose them as the application's task model. [Probe schema summaries](../research/stage-1/harness-probe-results.json).

### Hermes: TUI gateway, with ACP as an alternative

Hermes documents three programmatic interfaces: ACP, its TUI gateway, and an HTTP API. The gateway is the best fit for the required clarification inbox and steering: it has `session.create`, `prompt.submit`, `session.interrupt`, `session.steer`, resume/history, and server-originated `approval` and `clarify` requests. These requests require correlated JSON-RPC responses; they are not passive progress events. Register `client.capabilities` with `server_requests: true`. [Programmatic integration](https://hermes-agent.nousresearch.com/docs/developer-guide/programmatic-integration).

The installed gateway contracts and handshake confirm this surface. The investigated stdio entry is `python -m tui_gateway.entry`, run using the Hermes installation's own interpreter. This is an installation/version-specific entry, so discovery and version checks must precede launch. It does not require installing Python as a Vigil runtime: Python is already part of Hermes.

ACP (`hermes acp`) is a viable standard-protocol alternative for simpler jobs. Its narrower interaction and outcome semantics make it less suitable as the default here. Do not implement both transports in the first increment. The HTTP interface is not selected for the local foreground lifecycle. [ACP host integration](https://hermes-agent.nousresearch.com/docs/user-guide/features/acp).

## Capability matrix

“Source-backed” means present in the installed schema/source; it does not mean a model-backed scenario passed.

| Requirement | Codex app-server | Hermes TUI gateway | Application consequence |
| --- | --- | --- | --- |
| Launch / workspace / profile | `thread/start` effective cwd/model/provider/policy verified live | `session.create` and non-lazy info snapshot verified live | Pin the resolved profile and explicit cwd per attempt |
| Streaming / terminal outcome | Output/item activity and completion verified live; errors tested synthetically | Output/tool activity and completion verified live; missing/error status tested synthetically | Normalize events; submission acknowledgement is not completion |
| Pause scheduling | Application-owned | Application-owned | Stop dispatch after the active attempt |
| Stop active work | Native interruption passed for output/heartbeat; abrupt-loss heartbeat stopped in fixture | Native interruption passed; heartbeat survived abrupt transport death | Await quiescence; bound shutdown and preserve uncertain state |
| Resume | Exact completed-thread resume + recall passed | Exact completed-session resume + recall passed; new runtime ID | Store both handles; verify identity and workspace before continuing |
| Steering | `turn/steer` identifies expected active turn | `session.steer`; busy submits have separate redirect/queue behavior | Do not accidentally submit another scheduled task as steering |
| Human questions | Schema/synthetic mapping; live path unqualified | Model-triggered batched `clarify` passed | Track request lifetime and cancellation in the inbox |
| Native approvals | One-time command allow and deny observed live; unoffered decline requires cancellation | Native module once/deny/timeout/cancel/late-answer probes passed; ordinary Git paths bypassed callback | Bridge prompts; retain product policy independently |
| App-owned planning tools | MCP; dynamic tools are an experimental alternative | MCP/native extension interfaces | Prefer a small typed MCP tool surface; validate every mutation |
| Structured result | `turn/start.outputSchema` passed strict live validation | Explicit JSON-only prompt passed strict live validation; initial prose-prefixed output rejected | Require validated app result records; reject malformed results |
| Fresh review session | New thread with review context/profile | New session with review context/profile | Do not equate a forked history with an independent fresh reviewer |
| Usage | Cumulative token counters observed live; cost unavailable | Cumulative token counters observed live; cached input/cost unavailable | Preserve unavailable values; maintain time limits ourselves |
| Native UI handoff | Not demonstrated for a live app-server-owned turn | Gateway supports host interactions; native CLI takeover not demonstrated | Dashboard input first; stop/transfer ownership before native resume |

Codex rows are supported by the generated installed schemas and official protocol reference. Hermes rows are supported by the pinned gateway contracts listed in the evidence record. The current Hermes source is browsable at [the inspected commit](https://github.com/NousResearch/hermes-agent/tree/6a627e6eb38e28ac421d5ad8df3f676e49d0c287/tui_gateway).

## Findings that affect the design

### 1. Native approvals are not the complete product policy

Product decisions such as accepting plans, changing task scope, model eligibility, and budgets should be enforced before the app dispatches work or accepts a tool request. Native approval callbacks cover only operations the harness submits to them.

Codex command rules concern execution outside its sandbox. Its tool hooks can block supported calls, but the documentation explicitly identifies coverage exceptions and unsupported decisions; they are not a complete security boundary. [Rules](https://learn.chatgpt.com/docs/agent-configuration/rules), [hooks](https://learn.chatgpt.com/docs/hooks).

Hermes' installed `tools/approval.py::check_all_command_guards` returns approval directly when no flagged findings remain. “Manual” therefore does not mean every shell operation is routed to our application. Existing allowlists also affect prompting. Hermes offers pre-tool hooks, but their coverage, installation, and error behavior must be tested in the selected transport. [Hermes security](https://hermes-agent.nousresearch.com/docs/user-guide/security), [hooks](https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks).

Recommendation: application-owned commit/push/PR/MR operations, scoped task tools, and explicit restricted worker profiles. This reduces accidental bypass; it does not itself prevent an unrestricted worker shell or SDK from doing the same actions. If the product promises hard enforcement, we need an effective execution/credential/filesystem boundary. Until demonstrated, classify guarantees as app-enforced, native-enforced, advisory, or unsupported. Do not quietly weaken R12/R41/R63 to fit an adapter.

Never map a project-scoped permanent product grant directly onto a harness-wide permanent allowlist. Resolve product policy per request and prefer narrow native grants where available.

### 2. Harness scheduling can bypass our scheduler

The inspected Hermes gateway enables crash auto-continuation by default through `desktop.auto_continue`. Set `enabled: false` in the app-managed profile and verify the effective value before resume. Otherwise, loading a session may start inference before our approvals, time allowance, and local slot have been acquired. Source: `tui_gateway/session_auto_continue.py::_auto_continue_config` and resume paths.

Both harnesses can support their own delegation. Hermes' ACP coding toolset explicitly includes delegation; its gateway also exposes subagent controls. Constrain autonomous subagent spawning and audit background/auxiliary model calls. One foreground harness process is not proof of one inference request or one worker. Application pause also must not be defeated by a harness-owned queued follow-up.

### 3. Local-only requires a complete effective profile

Hermes supports llama.cpp as a custom model endpoint. Tool-calling template compatibility and sufficient context must be verified against the actual server/model. [Provider documentation](https://hermes-agent.nousresearch.com/docs/integrations/providers).

The inspected configuration supports auxiliary models and fallback chains. Pin the endpoint/provider for main work, review, compression, titles, and other enabled model calls; disable or constrain cloud fallbacks and unrelated tools. Merely selecting a local main model is insufficient. [Configuration](https://hermes-agent.nousresearch.com/docs/user-guide/configuration).

Use an app-specific Hermes profile/home with deliberate resource references rather than modifying the user's global default. Explicitly select the intended provider: a metadata auth advertisement is not validation of our llama.cpp profile. Stage 2 contacted the authenticated localhost server and verified model metadata plus real tool use; a complete destination/auxiliary audit remains Stage 3.

The shared slot covers application-managed local agent activity, including auxiliary work that outlives the visible reply. Release it only when the adapter establishes quiescence. The mechanism cannot control external programs using the same service.

### 4. ACP has concrete fallback hazards

Inspection of the pinned Hermes source found:

- `toolsets.py`: `hermes-acp` excludes the interactive `clarify` tool.
- `acp_adapter/server.py`: unknown-session resume can create a fresh session. An application must not report that as a successful exact resume.
- The same file can return `stop_reason=end_turn` from exception paths; end-turn alone does not prove execution success.
- `acp_adapter/permissions.py`: callback default timeout is 60 seconds; the server wires it without passing a configured timeout. Current host documentation describes the configured approval timeout instead. Treat this as a version-specific discrepancy and test before offering long-lived approval cards.
- `SessionManager` restores ACP-source sessions from its DB; empty session probes are not necessarily persisted. Do not rely on session creation alone as a durability guarantee.

These are reasons to keep ACP as an alternative rather than the first Hermes adapter. They also demonstrate why protocol presence is insufficient for a capability promise. [Pinned ACP source](https://github.com/NousResearch/hermes-agent/tree/6a627e6eb38e28ac421d5ad8df3f676e49d0c287/acp_adapter).

### 5. Cancellation, resume, and retention need explicit ownership

A cancellation response is not proof that every shell descendant, remote call, or detached process has stopped. Preserve separate states for interrupt requested, interrupted, failed, and outcome unknown. Do not stash/clear files or release workspace ownership while a writer may remain active. Test bounded process-group cleanup on Linux and macOS without treating it as rollback of external effects.

Persist app run IDs separately from native durable/runtime session IDs. On restart, reconcile the checkout, incomplete result, profile, and pending requests before allowing continuation. Never replay an uncertain prompt solely because a transport disconnected.

Our 30-day transcript retention applies to application-owned records. Harnesses may retain their own histories and memories. Native resume and deletion need a separate ownership-aware policy; do not delete global harness state to satisfy app retention.

## Proposed first adapter contract

Keep the minimum shared surface small: probe capabilities/version, create, submit, observe events, answer requests, interrupt, inspect outcome, and close. Resume, steer, and native handoff are separately advertised capabilities.

Normalize identifiers and events while retaining bounded, sanitized diagnostic metadata; do not copy raw credential-bearing protocol records into application evidence. Use distinct events for message output, tool activity, user input, native approval, usage, turn outcome, and adapter failure. All state-changing app tools must validate task/plan revision and permission. The transport must handle server-originated requests concurrently with reading output and cancellation.

Use one managed harness process per active execution context initially. This helps isolate config and lifecycle, but durable resume must be demonstrated before relying on process replacement between attempts.

## Validation gates before calling the adapters ready

1. **Passed in Stage 2 on Linux:** controlled single-turn task on each real provider, streamed output, tool event, valid result, correct cwd/profile, and durable IDs.
2. Required input and native approval: allow, deny, timeout, cancel, and stale response; verify task/plan grants do not become global native grants.
3. Stop during inference and a long-running child command; verify file writers terminate before checkpointing and local slot release.
4. Restart and resume a known session; unknown/corrupt session must produce an explicit fresh-start choice. Hermes auto-continuation stays disabled.
5. Local-only audit: all inference destinations, fallback routes, auxiliary requests, and nested delegation obey the profile and single-slot policy.
6. Approval enforcement tests through representative shell wrappers, alternate Git/API paths, and allowed tools. State the guarantee level rather than inferring it from prompt text.
7. Malformed result, provider error, duplicate/out-of-order events, transport loss, and partial completion must not mark a task accepted or silently dispatch a duplicate attempt.
8. Repeat essential lifecycle checks on macOS; record supported version ranges only after those checks.

These are finite integration gates, not a requirement to implement a second harness or a general-purpose workflow engine.
