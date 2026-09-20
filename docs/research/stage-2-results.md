# Stage 2 validation — bounded harness execution

Completed 2026-09-20 on Linux amd64. Both selected transports executed the editing fixture and passed independent result/file/Git verification. The [expanded plan](../stage-2-plan.md) is complete; [sanitized machine-readable evidence](stage-2-results.json) retains successful and failed experiments. This proves the development spike's execution path, not production workflow readiness.

## Implementation

- Shared Go JSONL transport: explicit child environment/cwd, Unix process group, continuously drained pipes, bounded frames/traffic/queues, concurrent directional RPC correlation, deadlines, and bounded shutdown/reaping.
- Native session adapters: handshake, profile checks, creation, single submission, streamed activity, nullable usage, terminal normalization, request answer/expiry/cancellation, native interrupt, inspect, and close. Runtime/durable/application/turn identities remain separate even when native values happen to match.
- `vigil spike`: metadata-only default; explicit `--live`; private evidence; prepared-config hashes; clean disposable fixture baseline; one checkout-wide runner lock; persistent attempt marker preventing replay. Native requests default to deny/cancel. No acceptance, retry, scheduler, resume, or steering workflow is implemented.
- Credential handling: inherited matching `OPENAI_BASE_URL` / `OPENAI_API_KEY` worked for llama. No key file was needed. The isolated Codex auth copy was removed after execution. Global profiles were not edited.

## Actual experiments

Versions: Codex **0.155.1**, Hermes **0.21.3** at `6a627e6eb38e28ac421d5ad8df3f676e49d0c287`. llama advertised `qwen3.8-27b-local`, 131072 context tokens, one slot, and tool/tool-call template support. Codex advertised and executed `gpt-6-astra` under ChatGPT authentication.

| Experiment | Duration | Native outcome | Independent result |
| --- | ---: | --- | --- |
| Hermes initial fixture | 24.14s | complete | Rejected: prose before final JSON |
| Codex fixture | 14.37s | completed | Passed |
| Hermes clarified fixture | 18.64s | complete | Passed |

Each experiment used a fresh private workspace with its own initial Git commit and no remotes. The initial instructions said “Finish with JSON,” which allowed the model to interpret preceding prose as acceptable. The fixture now explicitly requires the entire final response to be JSON, without prose or fences. The validator remained strict; the rejected attempt was preserved and never automatically replayed.

For both passing runs, `message.txt` contained exactly `adapter spike ready\n`, `sh check.sh` passed, README/check files and HEAD were unchanged, no remotes or extra paths were added, and the full Git diff contained only the intended edit. Final `summary`/`files` JSON passed strict validation. Both reports explicitly retain `task_accepted: false`.

Codex emitted streamed output and command activity, plus native thread/session/turn IDs. Hermes emitted streamed output and read/write/terminal events, plus distinct runtime and durable session IDs; it does not supply a native turn ID in this gateway contract. Both reported native idle and zero pending requests. Owned process close completed, the runner lock was removed, and the copied Codex auth file was removed. Detached writer quiescence remains **unconfirmed**.

Native cumulative usage: Codex 38973 input / 135 output / 25472 cached input tokens; passing Hermes 24519 input / 261 output tokens, cached input unavailable. These are native counters, not billing estimates; cost remains null. Private stderr was drained and counted (Codex 0 bytes, Hermes 39 bytes), never persisted as text.

The original Codex report includes two user-message item events counted as tool events. The final mapping excludes these; actual command activity was present. Additional post-run synthetic checks cover malformed final frames during close and held output pipes. No additional inference was needed for those transport fixes.

## Verification and reproduction

Passed: `make fmt`, `make check`, `make check-race`, `make build`, and `make cross-build` for Linux/macOS amd64/arm64. The race target requires a C compiler; normal builds remain `CGO_ENABLED=0`.

Synthetic cases include concurrent/out-of-order responses, numeric/string and same-ID opposite-direction requests, server requests during a pending call, late replies, cancelled requests, malformed/oversize frames, queue/traffic pressure, early exit, inherited open pipes, blocked stdin, shutdown, malformed final output during close, early native events, duplicate/conflicting completions, rejected queued submits, stale/expired/duplicate answers, narrow approval mappings, and invalid JSON/file/diff/check/remote/extra-path results. Routine checks start no model inference.

Preparation and execution commands:

```sh
make build
python3 scripts/spike/prepare.py
./bin/vigil spike --manifest <printed-directory>/launch.json --harness codex
./bin/vigil spike --manifest <printed-directory>/launch.json --harness codex --live
# Prepare another fresh directory before the Hermes live run.
python3 scripts/spike/prepare.py
./bin/vigil spike --manifest <new-directory>/launch.json --harness hermes --live
```

See [README](../../README.md#controlled-adapter-spike) for credentials, reports, and stale-lock handling. The shared lock is per checkout, not a production cross-instance endpoint lease. Metadata checks contact the configured services; `--live` alone authorizes a model turn.

Protocol finding: Hermes `session.info` is an event, not an RPC. Creation may initially be lazy. The adapter polls the owned session's `session.activate` snapshot until its effective profile is available, then verifies the idle snapshot after completion. Submit acknowledgement `streaming` is distinct from terminal `message.complete`; queued/redirected/steered acknowledgements fail this fresh-turn contract.

## Stage 3 boundaries

Still unproved live: interruption during inference/child writes, stale request handling against real harnesses, allow/deny/input flows, exact resume after restart, auxiliary inference/capacity audit, alternative-path policy enforcement, detached writers, and macOS runtime behavior. Hermes manual approvals are not an OS sandbox. Native completion, native idle, process-group cleanup, fixture verification, and product acceptance remain separate facts. None of these limits is relaxed by the passing fixture.

No additional user credential setup is needed on this host. Recheck the external llama service before the next experiments. A macOS host will be needed for platform runtime evidence.
