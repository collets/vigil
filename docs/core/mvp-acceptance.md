# First usable milestone

<!-- vigil-tier: core -->
<!-- vigil-status: stage=5.7; stage_accepted=true; implementation_commit=522cb967f732b578a66c051cf8938dd00584d238 -->

Status: scenario accepted on 2026-09-20. The Go application now persists project planning, permissions, resource coordination and the bounded interactive workflow; the offline core is independently accepted through Stage 5.6 at `b0a085b` (see [next steps](../process/next-steps.md)), for Stage 5.6's autonomous scope. **The human steps of this milestone were moved to [Stage 8](../plans/stage-8/stage-8.md) on 2026-09-29**, and Stage 6 (terminal interface parity) now precedes it, since the interface is the primary interface (R11) and is far from ready. Stage 6.1 has now measured how far: of 85 classified product capabilities the terminal interface fully expresses 4, and of 20 human decision classes it fully expresses 3 ([analysis](../plans/stage-6/6.1-parity-gap-analysis.md)). Steps 1–3 and 8 of the demonstration below are therefore not reachable from the interface today, which is why Stage 6 precedes this milestone rather than following it. Contained Hermes probes passed on Linux and macOS. **The milestone itself has still not been demonstrated**: the acceptance below requires real execution through both harnesses and real recovery/boundary checks, and production dispatch, live reviewer qualification and real user/manual decisions are still open.

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
