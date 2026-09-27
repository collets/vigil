# Supervision and composition options

<!-- vigil-tier: history -->

> **Superseded record.** Kept for provenance only. See
> [`README.md`](README.md) in this folder for what replaced it; do not
> implement from or cite it as current behavior.

Status: checkpoint supervision accepted; reconciled with the final requirements baseline on 2026-09-20. Detailed mechanisms remain under discussion; this document describes design, not implemented behavior.

Stage 4 follow-up: [core-spec.md](../core/core-spec.md) resolves the mechanism/default questions below, and [Stage 5](../plans/stage-5/stage-5-plan.md) defines implementation checks. Earlier proposed alternatives are retained for provenance; the core specification takes precedence for implementation. No production workflow or user configuration is installed by the specification.

## Main-agent role

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Long-lived main-agent conversation | Conversational continuity and immediate access to its history | Growing context, compaction, and potential dependence on stale assumptions |
| Fresh supervisor at every task boundary | Frequent checks of plan adherence | Repeated context loading, review cost, and possible overreaction to small changes |
| Event-driven supervisor with fresh sessions | Deliberate context reconstruction and selective use of frontier judgment | Requires useful context packets and explicit invocation triggers |

The third option is accepted. Candidate triggers: plan creation/revision, milestone completion, exhausted retries, scope/dependency surprises, and user requests. Exact trigger configuration remains open. The main agent becomes a supervisor role rather than an always-running conversation.

The application owns scheduling, state transitions, retry counts, validation, and approval enforcement. The supervisor assesses plan adherence, overall progress, cross-task coherence, and exceptional situations. It proposes changes instead of directly rewriting live orchestration state.

Fresh sessions reduce reliance on accumulated conversation history but do not eliminate mistakes or automatically save tokens. Context reconstruction and supervisor frequency must be measured.

## Context reconstruction

Proposed inputs: accepted specification and plan revision, relevant task/dependency state, project instructions or skills, decision history, recent results and review evidence, and unresolved questions. Include references to original artifacts so summaries are not the only available evidence. Summaries are aids, not authoritative substitutes for approved requirements.

The application owns authoritative state; the model accesses it through bounded tools. SQLite is the accepted structured store. Human-readable artifacts may accompany it, but external Markdown is not a competing task-state authority. Plan archives are local and Git-ignored by default; exporting or committing selected records is explicit. Exact export formats remain open.

## Simple execution process

The accepted direction keeps the process simple and initially sequential. Configured automated checks and fresh-session agent review are required; human review at task and plan acceptance can be waived through configuration. Review agents report findings and suggestions, and implementation agents apply repairs. Arbitrary agent-authored workflows are outside the current direction. Internal stages may still have typed inputs, outputs, and completion conditions; retry limits belong to application policy.

The supervisor may reorder remaining tasks and request approval to edit, split, or merge them. It cannot autonomously change acceptance criteria. The application should validate dependencies and policy for every change; exact validation rules remain to be specified. A task change must not implicitly remove a required gate.

Version the accepted plan and execution definition. A proposed change should identify affected pending or active work. Handling invalidated results, in-flight tasks, and prior approvals must be specified before live replanning is implemented.

## Policy questions

Separate scope (global/project/plan/task/agent), action category, and approval duration. Define how conflicting rules combine; broad agent permissions should not silently defeat a task restriction. Model selection and spending permission are distinct, and any automatic escalation still requires budget eligibility.

Approval should identify the concrete action and relevant revision. Determine when changed inputs invalidate an earlier approval. Application approvals do not replace the harness's tool permissions.

Autonomy removes selected human gates but must preserve configured eligibility and resource boundaries. Model policy and approval mode are separate. GitHub and GitLab draft requests are the accepted delivery endpoint; automatic merging is excluded. Project restrictions dominate task/agent grants, and permanent grants default to the project. Independent eligible tasks may proceed around blocked work after checkout safety is established. Exact profile coverage, permission matching, and blocked-state transitions remain design work.

## Workspace and evidence

Managed worktrees are deferred. Current-checkout execution needs explicit ownership and protection for pre-existing edits. A recovery agent may replace prior implementation once a recoverable snapshot exists; the application must not treat this as permission to destroy unrelated user work.

Quality checks may use an explicitly accepted baseline of pre-existing failures. Baseline comparison and freshness of test/review evidence require further definition. Proposed rule: evidence attaches to a particular code revision, and later repairs invalidate affected evidence rather than inheriting an earlier green result.

## Capability-aware integrations

Track launching, structured progress/results, cancellation, native resume, interactive access, and steering independently. The [initial investigation](../core/harness-capabilities.md) recommends Codex app-server and Hermes TUI gateway; live lifecycle validation remains open. If a required operation is unavailable, expose that limitation and define a fallback; do not simulate capabilities by assuming all harnesses share an interface.

Opening a native interactive session may require handing control over rather than attaching a second controller. Ensure the application and the user cannot issue competing actions to the same active session. This needs validation per harness.
