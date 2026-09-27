# Stage 1: adapter contract and reproducible profiles

<!-- vigil-tier: history -->

> **Superseded record.** Kept for provenance. See
> [`README.md`](README.md) in this folder for what replaced it, and prefer
> the `core/` documents it points to for current behavior.

Prepared 2026-09-20. This is the implementation contract for the bounded Stage 2/3 spike, not a production readiness claim. Requirements R10, R15–16, R24–25, R42, R55–61, R63 and R69–71 remain in force. No scheduler, database schema or dashboard changes are included.

## Contract

Implement in Go behind `internal/harness` in Stage 2. Native wire types stay inside each adapter. One launched transport owns one session and at most one active turn in this spike. The application supplies the resolved profile, canonical absolute workspace, run identity and limits before starting the process.

| Operation | Required behavior |
| --- | --- |
| `Probe(ctx, profile)` | Inspect executable/version, configuration and capability evidence without inference. Report executable availability, authentication metadata, endpoint reachability and model readiness separately. No session resume or prompt. |
| `Create(ctx, run)` | Initialize transport, register client request capabilities, create a fresh native session with explicit workspace/profile. Return observed native identities and effective settings. Reject mismatches. Creation/agent initialization may have side effects; it must already hold execution ownership. |
| `Submit(ctx, session, appTurnID, prompt)` | Only from idle with no unresolved requests; send once. Return an acknowledgement and native turn ID when present. A timed-out write/acknowledgement is ambiguous, not safe to replay. |
| `Observe(session)` | Ordered normalized event stream independent of UI rendering or human response latency. Drain stdout and stderr concurrently. Bounded queues; overflow fails visibly and interrupts rather than silently losing lifecycle/input events. |
| `Answer(ctx, requestRef, answer)` | Validate kind, choices, ownership, generation, deadline and unresolved status. Resolve at most once. Use the narrow native response; never translate a project grant into a global allowlist. A response write lost in transit becomes uncertain, not automatically resent. |
| `Interrupt(ctx, turnRef)` | Request immediate stop, retire pending requests, await terminal evidence and separately establish quiescence. An interrupt acknowledgement alone does not prove stopped writers. Escalate shutdown within limits; preserve unknown state if necessary. |
| `Inspect(ctx, session)` | Return adapter outcome, evidence, pending requests and writer/auxiliary activity status. Reconcile native read/status data when available. Does not submit, resume, repair or infer success from exit status. |
| `Close(ctx, session)` | Idempotent bounded cleanup of owned resources. Interrupt active work first, close native session if supported, close stdin, terminate then kill owned process group as necessary, reap process and drain diagnostics. Never delete histories, fixture edits or evidence. |
| Optional `Resume(ctx, handle)` | New transport generation; verify durable ID, canonical workspace and profile before further work. Missing/corrupt/mismatched session returns an explicit error requiring a fresh-start choice. No silent replacement or prompt replay. |
| Optional `Steer(ctx, turnRef, text)` | Require the expected active turn. Codex uses `expectedTurnId`; Hermes requires an application-side active-turn check because its native method is session-scoped. Queued corrections do not authorize another task. |

Contexts bound method waits; cancelling a Go RPC context alone does not cancel native inference. The run supervisor must invoke `Interrupt`/`Close`. Only the transport reader correlates RPC messages; it must never wait for a human. Serialization and outcome inspection must be race-safe. Capability states are `unsupported`, `source-backed`, or `runtime-verified`, with version/platform/evidence references. Do not upgrade a capability merely because an adapter implements its method.

## Identity, requests and events

Use distinct opaque string types for `RunID` (application attempt), `AppTurnID` (one submission), `NativeRuntimeSessionID`, `NativeDurableSessionID`, `NativeTurnID`, and `TransportGeneration`. Keep the native JSON-RPC ID as a typed string or number: `1` and `"1"` are different. Outstanding request identity is `(generation, direction, rpcID)` plus run/turn ownership; server and client can reuse the same ID independently.

Codex's thread handle drives thread operations; preserve `thread.sessionId` separately if returned instead of deriving it. Hermes `session.create` returns runtime `session_id` and durable `stored_session_id`; resume accepts the durable ID and can return a new runtime ID. Hermes prompt acknowledgement need not contain a native turn ID: leave it absent and correlate events to the sole active application turn. Never label the submission RPC ID a native turn ID.

Every normalized event has schema version, run ID, transport generation, application sequence number, UTC receive time, kind, optional session/turn/request/item IDs, payload and sanitized wire reference. Native timestamps are optional separate values. Record:

| Kind | Payload and interpretation |
| --- | --- |
| `output` | Stream/item, text delta, channel and optional native sequence. Final text is not a terminal signal. |
| `tool` | Item ID, name, start/progress/end, sanitized arguments/output, optional exit status. Tool success is not task acceptance. |
| `input_requested` / `approval_requested` | Request reference, exact supported choices/schema, prompt/action, blocking flag, native deadline if present and application deadline. |
| `request_resolved` | Answered/denied/expired/cancelled/native-resolved, reason, request reference. Late answers are rejected locally. |
| `usage` | Nullable input/output/cache/reasoning/cost fields, units, source, delta versus cumulative scope, observed versus estimated. Absence stays unknown, never zero; do not sum cumulative snapshots. |
| `outcome` | Exactly one terminal adapter outcome per application turn, native status/error and evidence. Identical duplicates are diagnostic only; conflicting terminal evidence is a protocol fault and blocks acceptance. |
| `transport_failure` | Phase, bounded sanitized error, process exit/signal if observed, last valid sequence and delivery certainty. It can follow a native outcome without erasing it. |

Requests expire at the earlier native/application deadline. Deny approvals on expiry when the protocol supports it; cancel/interrupt the turn when required input has no valid cancellation response. Never invent a human answer. Turn termination, transport loss, or generation change invalidates outstanding requests. Unexpected request kinds fail closed and are recorded as unsupported; Stage 2 only enables the interactions it can answer.

Execution states: `created → submitting → active ↔ awaiting_request → terminal`, with `interrupt_requested` possible from submitting/active/waiting. The terminal outcome is `completed`, `failed`, `interrupted`, or `unknown`; quiescence is a separate `confirmed`/`unconfirmed` field. Codex uses the native terminal turn status and error. Hermes uses `message.complete` status/error/failure fields, not `prompt.submit`'s `streaming` acknowledgement; missing or contradictory terminal information is `unknown` until reconciled. A rejected submission is a failure; a lost acknowledgement is unknown.

`completed` means the harness reports finishing. Application acceptance still requires a valid result, verified artifacts/checks, fresh review and applicable human gates. An unknown execution, unconfirmed writers or conflicting evidence blocks acceptance and workspace reuse. Never release a local inference slot while known auxiliary work continues. The spike does not implement cross-instance locks yet: run one experiment at a time.

## Fixture, limits and diagnostics

[Fixture](../../testdata/spike/README.md): edit one text file, run `sh check.sh`, return a tiny JSON result. The application independently checks exact bytes and the complete diff against the fixture baseline; do not trust a worker-edited check script. Expected changed paths: only `message.txt`. Require valid JSON with a string `summary` and exactly `files: ["message.txt"]`. Malformed results are not accepted.

Preparation creates a new private `.cache/spike/stage1-*/fixture` Git repository with a local baseline commit and no remotes. It never resets an existing experiment or edits the real project through a harness. Use a fresh prepared fixture for each harness's live run. No live commits, push, PR/MR or publishing tests. Publication boundary tests in Stage 3 use mocks or disposable local destinations.

[Limits](../../config/spike/profiles.json) are spike defaults, not new product defaults: one managed worker and one submitted turn per harness; zero application repair/replay attempts; 30s startup, 15s ordinary RPC wait, 180s active attempt/cumulative task, 60s maximum request wait, 300s absolute wall cap, 5s interrupt grace and 5s termination grace. Approval waits are excluded from active time but not the wall cap. Maximum frame 1 MiB, evidence 16 MiB, event queue 256 and outstanding requests 8. Preserve room for a terminal error record; stop on a limit instead of unbounded buffering. Native Hermes has eight loop iterations and a 20s default command timeout; its SDK can still retry internally. These do not substitute for the Stage 2 wall-clock supervisor.

Preparation enforces subprocess timeouts, fresh directories, environment selection, version checks and effective config assertions. **The live time, event, frame and shutdown limits are a Stage 2 implementation obligation, not enforced by `launch.json` itself.** Do not run the manifest as an unattended worker before that runner exists.

Wire diagnostics are private, bounded, sanitized JSONL: direction, generation, sequence, method, typed RPC ID, size and allowlisted payload fields. Omit auth/token/key/cookie/secret fields, environment dumps, credential-bearing URLs, secret request bodies and unknown payloads by default. Output/tool text can echo secrets: apply known-value redaction and omit unsafe free text. Keep unknown method name and structural metadata, not arbitrary raw content. Never persist unsanitized wire data first and redact it later. Share only reviewed summaries in `docs/research`; native private histories have a separate retention boundary.

## Profiles and reproduction

[Profile manifest](../../config/spike/profiles.json), [Codex template](../../config/spike/codex.toml), [Hermes template](../../config/spike/hermes.json). The JSON manifest is versioned experiment input, not the eventual application configuration schema. Profiles do not contain credentials. Python is used only as a development inspection helper with the existing Hermes interpreter; the application and Stage 2 transports remain Go.

```sh
make check
~/.hermes/hermes-agent/venv/bin/python scripts/spike/prepare.py
```

Optional flags: `--hermes-root /path/to/hermes-agent --codex /path/to/codex --base-url http://127.0.0.1:8080/v1 --model qwen3.8-27b-local --harness both|hermes|codex --fixture-source /owned/repository --qualification-spec /owned/spec.md`. A fixture source is copied read-only from its committed `HEAD` via `git archive`; dirty and untracked work is never copied or changed. The optional bounded regular Markdown qualification spec is copied into that disposable fixture before its baseline commit. The helper rejects unpinned selected harness versions, non-loopback endpoints and a Hermes installation `.env` that would leak into the isolated process. It never deletes or alters source/global files. It prints the unique run directory containing `launch.json`, private homes, fixture, and effective-setting evidence. Re-running creates a separate fixture and preserves previous work. No gateway process or model turn remains running afterward.

After moving or renaming the checkout, run preparation again: generated launch manifests contain absolute paths. Earlier ignored experiment directories remain historical evidence; use the newly generated manifest for the current checkout.

**Codex:** CLI 0.155.1, `gpt-6-astra`, high reasoning, existing ChatGPT login. Login status was verified; model entitlement remains a live gate. Fresh config disables web search and multi-agent tools, chooses user approvals and workspace-write sandbox with tool network disabled. The isolated home has no inherited MCP definitions. `launch.json` references the existing auth file but preparation neither copies nor dumps it. At Stage 2 launch, copy that explicitly selected auth file into the private Codex home with mode 0600 (no symlink); never overwrite the original when refreshing. Validate `account/read`, `model/list` and effective configuration before starting a turn. If subscription auth is unavailable, stop: do not silently choose paid API credentials or a different model. No dollar-cost guarantee is possible from login status alone.

**Hermes:** 0.21.3 at commit `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`, `custom` Chat Completions at `http://127.0.0.1:8080/v1`, model `qwen3.8-27b-local`. Fresh `HERMES_HOME`, separate child home and explicit environment replace inherited provider credentials/profiles. Templates keep `${...}` references; preparation uses a public `local-no-auth` placeholder. If the server requires authentication, supply `VIGIL_LLAMA_API_KEY` only to the live child environment through the Stage 2 runner; never put it in `launch.json` or send it in chat.

Hermes toolsets are pinned to file/terminal/clarify using `HERMES_TUI_TOOLSETS`, so coding-mode defaults cannot broaden them. Resolved tools include `process_manage`: detached writers remain a Stage 3 concern. Automatic crash continuation, compression (including idle), titles/model upgrades, background self-review, memory and voice are disabled. Main fallback chains are empty. Every auxiliary task in the pinned installed defaults is explicitly routed to the same local endpoint/model with an empty task fallback chain; the native selection policy refuses implicit provider discovery for an explicitly selected `custom` main provider. Native approval mode is manual, disabling its automatic model-based approval classifier. No agent is instantiated by the settings check.

Isolation is configuration/native-source evidence, **not an OS sandbox for Hermes**. Its shell can still read accessible files, use network access, spawn commands or bypass a product operation gate. No production claim of local-only enforcement, publication prevention or child-process cancellation follows from these settings. Those remain Stage 3 boundary tests. The settings-inspection subprocess disables socket connections only for its own metadata imports; it is not the live harness execution boundary.

Before every Stage 2 launch, compare installed versions/source fingerprints, revalidate effective settings and environment, and retain the resolved configuration digest. Auth readiness, endpoint health, supported model ID, context capacity and compatible tool-calling template are distinct preflight results. Do not accept a model alias mismatch or missing tools by quietly changing the profile.

## Runtime inputs and handoff

- llama.cpp health at `127.0.0.1:8080` returned connection refused on 2026-09-20, including outside the restricted tool sandbox. Nothing needs starting for Stage 1.
- Before the Hermes Stage 2 turn, the user starts their existing llama.cpp service exposing `qwen3.8-27b-local`. Check `GET /health` and `GET /v1/models` with short timeouts. No server installation/model lifecycle work is part of this project.
- The previous Hermes profile lists 131072 context tokens, but the running server's actual context size and chat/tool template are unverified. Validate metadata and the bounded live tool call in Stage 2; ask for launch details only if metadata or execution cannot establish compatibility.
- No missing endpoint/model or Codex spending decision blocks Stage 1. A changed endpoint/model, authentication requirement or unavailable Codex entitlement needs resolution before the affected live turn.
- Next: implement the minimum Go transport and a development runner using this contract, synthetic transport tests first. Explicit live runs then use a newly prepared fixture per harness. macOS runtime testing remains unperformed.

References: [installed probe evidence](../research/stage-1/harness-probe-results.json), [Stage 1 evidence](../research/stage-1/results.json), [official Codex app-server documentation](https://learn.chatgpt.com/docs/app-server), and [pinned Hermes source](https://github.com/NousResearch/hermes-agent/tree/6a627e6eb38e28ac421d5ad8df3f676e49d0c287). Installed schemas/source govern the pinned experiment; current documentation can describe a different protocol revision.
