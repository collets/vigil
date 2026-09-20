# Stage 3 — lifecycle and policy validation plan

Prepared 2026-09-20 before implementation; Linux investigation completed the same day. Builds on [Stage 2](research/stage-2-results.md). Delivered native adapter improvements, reproducible bounded experiments, and an [evidence-based report](research/stage-3-results.md). No scheduler, production delivery, or global harness reconfiguration. Unsupported enforcement/recovery guarantees remain explicit qualification gates; completion does not mean those guarantees passed.

## Inputs and decisions before unattended work

- Existing Codex ChatGPT authentication and the configured `gpt-6-astra` remain selected. Hermes uses the inherited matching OpenAI-compatible localhost exports and `qwen3.8-27b-local`; metadata readiness is checked before execution. No new credential or paid provider is needed.
- Keep the external llama service and this machine running. No server lifecycle management is added. Linux work proceeds; macOS runtime evidence is explicitly pending unless a host becomes available.
- Experiments may create/commit disposable repositories and use a disposable **local bare repository** for push-path tests. They never push to a hosting service, create requests, or merge. Any approval allow case is restricted to a concrete harmless fixture operation; no permanent native grants.
- At most 16 explicitly dispatched model turns across the stage, sequentially, using Stage 2 per-turn limits (180s active, 300s wall, 16 MiB traffic). Metadata/native-callback tests need no model call. Do not retry an uncertain prompt. Fixes require a fresh explicit experiment. Preserve every failed/inconclusive attempt.
- Stage 4 may finish its design with unsupported capabilities represented as dispatch restrictions. A failed enforcement experiment is useful evidence, not permission to weaken R12/R41/R63.

## Ordered work and acceptance

1. **Baseline and pinned protocol audit.** Run existing tests; inspect installed schemas/source for resume, interrupt, input/approval, cancellation, and busy-session semantics. Fingerprint relevant source and record versions. Keep three evidence classes separate: model-backed live, installed-native integration with controlled stimuli, and synthetic transport.
2. **Exact resume contract.** Add resume to a fresh transport generation only. Require a controller-owned durable handle, original workspace/profile, and read-back identity validation. Restore neither a pending answer nor permission from the previous generation. Check history availability before any follow-up. Missing/corrupt/different identity or workspace is an explicit error; never fall back to creation. Test restart then resume with no spontaneous inference, followed by one explicit bounded continuation where possible.
3. **Interrupt and writer experiment.** Interrupt once during observed model output/inference and once while a fixture child periodically writes a heartbeat. Record acknowledgement, native terminal status, idle state, elapsed grace, and process cleanup separately. Compare heartbeat bytes before/after shutdown and check the owned process group. Finite observation is evidence about that child, not proof about arbitrary detached writers. Unknown writer state blocks clearing, checkpoint restore, and resource release in the core specification.
4. **Request lifecycle.** Exercise installed native approval/clarification paths with controlled harmless stimuli where a model cannot reliably trigger every branch. Cover one-time allow, deny, cancel, expiry, native cancellation, stale generation and duplicate answer. Also run a real-model clarification/approval case where supported. Assert no persistent allowlist change. Honor offered native choices; fail closed for unsupported methods. Distinguish protocol integration from model tool-selection coverage.
5. **Loss and failure.** Force a transport exit after partial progress and exercise provider/error/partial terminal evidence without replay. Confirm outcome unknown/failed, pending requests retired, partial files retained, no task acceptance. For provider failure, prefer a controlled local error fixture rather than breaking the user's llama server.
6. **Scheduling and local route audit.** Verify crash continuation disabled on resume, fresh submit rejects queued/redirected/steered acknowledgements, delegation disabled, and auxiliary configuration pinned locally. Inspect runtime inference request destinations/counts using a temporary loopback recording relay if feasible; retain metadata only. Distinguish configured-route evidence from complete OS egress enforcement. Do not claim cross-instance capacity enforcement from the checkout-local spike lock.
7. **Policy bypass probes.** In disposable repositories, exercise ordinary Git commands and alternate shell/Python paths through installed tool execution. Inspect commit/push/hosting credential boundaries, hooks and native sandbox coverage. An observed bypass yields an advisory/unsupported classification for that profile. Propose concrete credential/filesystem/network isolation options; do not install a global sandbox or change the user's profile.
8. **Consolidate and validate.** Add focused regression tests for discovered faults. Run normal/race checks, native controlled probes, relevant live fixtures, and four cross-builds. Record commands, IDs, stimulus, observation, guarantee level, platform and gaps. Update the capability matrix and handoff; commit the Stage 3 increment before Stage 4 design closure.

## Evidence matrix to fill

| Area | Required observable | Failure handling |
| --- | --- | --- |
| Resume | Same durable identity, workspace/profile/history; no inference before submit | Explicit recovery choice; preserve files and history |
| Inference interrupt | Native acknowledgement and terminal/idle observations | Bound shutdown; outcome unknown when unconfirmed |
| Child interrupt | Identified fixture writer stops changing after close | Quarantine ownership; no destructive cleanup |
| Requests | Native response maps narrowly; expiry/stale answers cannot authorize work | Deny/cancel/interrupt; request remains audit evidence |
| Lost transport | Partial evidence survives, no second submit | Unknown run; reconcile on reopening |
| Local inference | Selected routes/auxiliary calls observed or explicitly unproved | Unsupported profile guarantee remains visible |
| Gated actions | Representative alternate paths cannot bypass, or bypass recorded | Strict dispatch requires a stronger tested boundary |
| macOS | Essential runtime experiments on a real host | Record pending; cross-build is insufficient |

Completion means the available-platform experiments and adapter fixes are recorded with honest limits. It does not mean all profiles satisfy autonomous policy enforcement. Stage 4 must make every unresolved guarantee an explicit eligibility/recovery rule.

## Execution disposition

Sixteen actual model turns were used, with no automatic replay; one additional controlled provider-error submission was rejected before inference. Resume/recall, normal interruption, live Codex approval and Hermes clarification passed after recorded experiment fixes. Hermes abrupt-loss child cleanup failed. Both policy probes showed limitations in application push/commit enforcement. Native request timeout/cancel/late-answer paths and local provider-error routing were exercised without model inference. Full egress/auxiliary concurrency, corrupted real native-history recovery, interrupted-history resume, and live Codex clarification remain unqualified; synthetic/config evidence is not promoted to live proof. These are preserved in the Stage 4 dispatch and recovery gates.

The user explicitly scheduled Mac access/setup and runtime checks as [Stage 3.5](stage-3.5-macos.md), after the current Stage 3 and Stage 4 work.
