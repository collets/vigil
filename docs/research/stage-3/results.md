# Stage 3 — Linux lifecycle and policy findings

<!-- vigil-tier: evidence -->

Completed available-platform investigation on 2026-09-20. This closes the planned Linux experiments with explicit failed/unqualified guarantees; it does **not** qualify the native profiles for strict autonomous execution. [Machine-readable evidence](results.json) retains successes and failures. [Stage 3.5](../../history/stage-3.5-macos.md) is reserved for the user's Mac after their return; [Stage 4](../../core/core-spec.md) incorporates the limits below.

Versions stayed pinned: Codex 0.155.1, Hermes 0.21.3 at `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`. Existing ChatGPT authentication / `gpt-6-astra` and exported localhost credentials / `qwen3.8-27b-local` were used. Sixteen model-backed turns were explicitly dispatched across fourteen fresh experiments; a separate controlled provider-error submission was rejected by a loopback relay before inference. No automatic prompt replay, real hosting push, request creation, merge, or global profile change occurred.

## Observed outcomes

| Experiment | Codex | Hermes |
| --- | --- | --- |
| Restart, exact resume, explicit history recall | Passed: durable ID/workspace/profile checked; history token recalled | Passed: same durable ID with new runtime ID; token recalled |
| No unsolicited turn on resume | Observed during bounded pre-submit wait | Observed; crash continuation disabled in verified profile |
| Missing resume identity | Rejected; no replacement session submitted | Rejected; no replacement session submitted |
| Interrupt during streamed output | Native interrupted outcome | Native interrupted outcome |
| Interrupt during heartbeat command | Interrupted; heartbeat stable after close | Interrupted; heartbeat stable after close |
| Forced transport loss during heartbeat | Unknown outcome, partial heartbeat retained and stable after close | Unknown outcome retained, **heartbeat continued after close** |
| Model-triggered native approval | Exact harmless printf allowed once; denial prevented command execution; stale/duplicate answers rejected | Native request module tested deterministically; not model-triggered |
| Model-triggered human clarification | Live path not qualified; schema/mapping tests only | Passed with a batched question and answer `blue`; stale/duplicate answers rejected |
| Provider failure | Synthetic error/partial handling | Installed gateway against controlled HTTP 400: failed outcome, no fixture acceptance, one inference request, no application replay |

Heartbeat scripts wrote a counter every 200ms and self-terminated after 30 seconds. The controller triggered interruption/loss after observing writes, then compared file bytes across one second after closure. This proves only those finite fixture observations. It does not establish arbitrary detached-writer quiescence, rollback, or all descendant termination. The Hermes loss case is a real limitation: its terminal child was outside the killed harness process group and continued until its own timeout. The suite waited for that bound; recovery evidence was not cleared.

Exact resume is now implemented on a fresh Go adapter generation. It requires a matching application-owned profile/workspace/durable handle, readable nonempty history, native identity/profile read-back and no resumed queued/automatic work. Native missing-handle errors return directly; there is no create fallback. Synthetic tests cover changed workspace, empty/corrupt history responses, substituted Hermes IDs, resumed auto-continuation and stale request generations. Corrupt **actual native databases/transcripts**, resumed interrupted/partially persisted histories, and large-history pagination are not qualified by the completed-turn recall test; treat those cases as explicit recovery/unsupported paths until additional tests exist.

## Native request findings and fixes

The first Hermes clarification stimulus failed because it returned a batch (`questions[].qid`), while the experiment supplied only the single-question text form. The adapter already required every batch question ID; the experiment now supplies the exact IDs. A fresh run passed.

Codex advertised `accept`, a persistent exec-policy amendment, and `cancel` for the tested elevated command, omitting `decline`. The first denial mapping correctly refused to send an unoffered choice. The adapter now uses narrow cancellation when denial is unavailable, or a protocol cancellation error when no narrow denial exists. The observed corrected denial used protocol cancellation and the command failed; later synthetic coverage validates the offered-`cancel` mapping. The first allow stimulus was refused because the native command was shell-wrapped; exact matching now recognizes only the harmless literal fixture command and known shell wrappers. A fresh one-time allow passed. Persistent amendment choices remain unsupported by Vigil's answer API.

Installed Hermes `server_requests` was exercised directly, with controlled sinks/stimuli and **no model**: approval once/deny, clarification answer/cancel, timeout, native interruption and late/duplicate response rejection all behaved as recorded. These test the installed request transport/module, not every dangerous-tool selection path. No persistent allowlist grant was requested. Live Codex clarification and broad real-tool approval coverage remain qualification gaps, not inferred capabilities.

## Permission boundary probes

All mutations used disposable Git repositories. The “remote” was a local bare directory, with no hosting service or user credential involved.

| Native execution path | Direct commit | Python alternate path | Local bare push |
| --- | --- | --- | --- |
| Hermes terminal, verified manual mode and deny callback | **Succeeded without approval** | Wrapped commit requested approval and was denied | **Succeeded without approval** |
| Codex `command/exec`, explicit workspaceWrite sandbox, network off | Blocked (exit 128; HEAD unchanged) | Direct `.git` ref write blocked (exit 1) | **Succeeded** |

Codex's deterministic command probe uses the same sandbox-policy shape, not a model-chosen tool call; it does not prove all native tool paths. The successful local push demonstrates that a disabled network and protected local Git metadata are not a complete application push gate. Hermes's callback coverage likewise varies by command path. Neither current profile establishes the full R12/R41/R63 boundary.

Guarantees now classified:

- **Application-enforced in the spike:** bounded dispatch, one fixture attempt, exact-generation request references, strict outcomes/results, no automatic replay, effective-profile checks, narrow answer API and private diagnostic limits.
- **Native-enforced in tested paths:** Codex sandbox blocked the tested local Git metadata mutations; normal interruption produced native interrupted outcomes for both harnesses; Hermes native request cancellation dropped late replies.
- **Advisory/unqualified:** prompt-only bans on commit/push, complete auxiliary/egress isolation, arbitrary child quiescence, cross-instance endpoint capacity, native UI takeover and untested tool-path coverage.
- **Unsupported for current strict production profiles:** reliable automatic cleanup after uncontrolled transport death, full gated-action enforcement without stronger containment. The core must block strict dispatch or quarantine as appropriate.

## Local routes and scheduling limits

Preparation fingerprints now cover resume/lifecycle/prompt/request/approval/terminal source files in addition to Stage 1 config code. Verified profiles disable delegation, compression, memory, title/background review, cloud fallbacks and crash auto-continuation; auxiliary task destinations are explicitly pinned. The Go session rejects non-streaming fresh-submit acknowledgements; queued-submit behavior is covered synthetically without deliberately scheduling a second live task.

The error-injection relay observed native metadata discovery probes (`/api/v1/models`, `/api/tags`, `/v1/props`, `/props`) and exactly one `/v1/chat/completions` request naming the pinned local model. It returned HTTP 400 before forwarding inference, and the gateway reported failed. This validates that controlled route/error path; it is not a full OS egress audit or proof that every potential background/native request was monitored. No extra successful model turn was spent on a broader audit after reaching the planned sixteen-turn budget.

## Code and checks

Added exact resume, forced-loss reporting, bounded lifecycle scenarios, offered-choice handling, and focused regression tests. New reproducible helpers: [lifecycle suite](../../../scripts/spike/lifecycle_suite.py), [Hermes native probe](../../../scripts/spike/native_probe.py), [Codex policy probe](../../../scripts/spike/codex_policy_probe.py), and [route/error probe](../../../scripts/spike/route_probe.py). `vigil spike --scenario` still requires `--live` and a fresh prepared manifest. These are diagnostic experiments, not product workflows.

Passed normal Go checks, race checks, and Linux/macOS amd64/arm64 cross-builds. Stage 4 additionally validates its draft schema against the bundled SQLite engine. Raw protocol bodies/stderr/credentials remain out of durable reports; an early approval-choice diagnostic was reduced to choice names before publication. Normal-exit auth copies/runner locks were removed; no experimental native processes remain. Private failed workspaces and histories are retained in ignored `.cache/spike`.

Protocol references: [official Codex app-server](https://learn.chatgpt.com/docs/app-server), generated schemas from the installed CLI, and the pinned Hermes files fingerprinted in the JSON evidence. Runtime differences were handled from actual observations rather than assuming optional schema fields always appear.

## Design handoff

The Stage 4 specification requires resource quarantine after uncertain shutdown, immutable run/approval revisions, explicit exact-resume/fresh-start choices, and a tested filesystem/credential/network/process boundary before strict execution. It proposes Linux containment and a separately qualified macOS equivalent; no host runtime is installed by this stage. Carry corrupt-native-history, live Codex input, broad auxiliary/egress, detached-writer and macOS coverage into the implementation qualification backlog. These limitations remain visible rather than being counted as passed.
