# First usable milestone

Status: scenario accepted on 2026-09-20; Stage 1 adapter preparation is complete. The Go application now persists project planning, permissions and resource coordination. Contained Hermes probes passed on Linux and macOS; the full production milestone has not been demonstrated.

## Accepted demonstration

1. Configure harness/model profiles manually for Codex and Hermes.
2. Load a small local Markdown specification.
3. Produce a plan with dispatch-ready tasks and approve it.
4. Execute tasks sequentially through the configured profiles.
5. Demonstrate a fresh-session review finding a problem and a subsequent implementation attempt repairing it within configured limits.
6. Run configured quality checks and show evidence, review findings, and implementation summaries.
7. Complete task and plan human review, including manual checks where applicable.
8. Create a draft PR/MR on the configured GitHub or GitLab host. Never merge automatically.

At least one execution through each initial harness must be demonstrated; merely listing a second harness in configuration does not demonstrate integration. The exact roles assigned to each remain configurable. Local-model serving remains external.

The local profile uses an existing llama.cpp service. Plans also require a visible finalization task producing a durable local summary and references. Summary failure leaves finalization pending, preserves the factual record, and permits retrying finalization alone; draft delivery remains available explicitly. Detailed ordering remains to be designed.

## Required recovery and boundary checks

Before the full demonstration, complete the [adapter validation gates](harness-capabilities.md#validation-gates-before-calling-the-adapters-ready). Metadata handshakes alone do not establish runtime readiness.

The following are proposed verification cases derived from accepted requirements:

- Pause stops new dispatch after the current attempt; stop-now interrupts and preserves work.
- Restart offers supported native resume or a fresh agent with reconstructed context.
- Repair/time limits halt unbounded execution; infrastructure retries are counted separately.
- Saving/clearing agent work preserves user-owned changes, and reapplying a saved attempt requests approval.
- A second project with an overlapping folder tree cannot start execution concurrently.
- Nonoverlapping projects may run concurrently, subject to any configured shared-model capacity.
- Requests sharing a local endpoint queue behind its single-agent capacity; approval/queue waits do not consume execution-time allowance.
- Required checks or blocking review findings prevent acceptance; process exit alone does not complete a task.
- Publishing, model use, and other gated actions respect resolved permissions. Unsupported enforcement is visible, not silently treated as supported.
- Completed-plan transcripts expire under the configurable 30-day default without removing durable evidence or unfinished recovery context.

## Later scope

Claude Code, pi, and OpenCode adapters; guided onboarding; generated project skills; managed Git worktrees; within-project parallelism; and optional model-based routing evaluation.

The broader requirements remain in [requirements.md](requirements.md). This milestone is an acceptance scenario, not a claim that all wider product requirements fit into the first implementation increment.
