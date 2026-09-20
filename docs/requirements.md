# Functional requirements baseline

Status: accepted product direction consolidated on 2026-09-20. The application remains a hello-world scaffold. This baseline defines intended behavior; it is not an implementation-completion claim.

The application owns reliable coordination and state; existing harnesses execute agent work. See [MVP acceptance](mvp-acceptance.md), [architecture](architecture.md), and [discovery history](discovery-notes.md). Detailed design questions are listed below separately.

## Confirmed needs

| ID | Need |
| --- | --- |
| R01 | A CLI application providing a dashboard-like control panel for development agents |
| R02 | Manage sessions across multiple existing harnesses rather than build another harness |
| R03 | Support a frontier model in the main-agent role |
| R04 | Delegate suitable work to local models through configured harnesses |
| R05 | Support specification analysis, plans, tasks, and task assignment |
| R06 | Show current, upcoming, and completed tasks plus task details |
| R07 | Represent multiple plans and project information |
| R08 | Mix paid frontier models and local models to reduce token use and monetary cost |
| R09 | Consider Jev as an optional aid to model/task selection |
| R10 | Implement Codex and Hermes first; retain Claude Code, pi, and OpenCode as subsequent integration targets |
| R11 | Make the dashboard the primary application interface, with ways to intervene in individual agents |
| R12 | Default to approval-first operation, with granular configuration of increased autonomy |
| R13 | Support starting application sessions in different folders, with folder/project discovery behavior to be defined |
| R14 | Support completing complex feature or component plans with human quality validation and high-risk decision gates |
| R15 | On normal exit, request immediate cessation of active work rather than waiting for task completion; define bounded shutdown and interrupted-state recovery |
| R16 | On restarting interrupted work, offer native session resume where supported or a fresh agent with reconstructed context |
| R17 | Include per-task and per-agent approval configuration |
| R18 | After a configured retry limit, escalate to the main model for a proposed resolution |
| R19 | Detect a possible parent project and warn before creating a project in a nested folder |
| R20 | Use fresh, checkpoint-triggered supervisor sessions rather than an always-on main-agent conversation |
| R21 | Keep application state authoritative; expose bounded tools through which the main model reads state and requests changes |
| R22 | Execute agents sequentially within a project initially; allow concurrent projects with nonoverlapping folder trees; defer within-project parallelism |
| R23 | Let the supervisor reorder remaining tasks; require approval for editing, splitting, or merging them; prohibit autonomous acceptance-criteria changes |
| R24 | Require configured automated quality gates and agent review before task acceptance, plus human review unless explicitly waived in task configuration |
| R25 | Never automatically discard partial work; preserve failed attempts for inspection and recovery |
| R26 | Support plan-level acceptance against the original requirements in addition to individual task acceptance |
| R27 | Allow task and plan human-review gates to be waived for explicitly configured autonomous execution, while retaining configured limits and quality gates |
| R28 | Support bounded autonomous user-story execution ending at draft GitHub PR or GitLab MR creation |
| R29 | During project setup, discover likely quality-check commands and propose them for approval; tasks may add checks but may not remove project-required checks |
| R30 | Permit explicitly accepted pre-existing check failures as a recorded baseline; continue reporting the project as unhealthy |
| R31 | Run review in a fresh agent session, select its model at task creation based on difficulty, and restrict its role to findings and suggestions rather than repairs |
| R32 | Allow a recovery agent to replace an implementation within scope after preserving a recoverable snapshot, without a separate start-over approval |
| R33 | Support interactive requirement clarification and direct intake from pasted requirements or local Markdown; direct intake can skip discussion but not readiness validation |
| R34 | Require task objective, acceptance criteria, dependencies, relevant context, verification steps, and implementation/reviewer profiles before dispatch |
| R35 | Route underspecified tasks to the supervisor for assessment and, when needed, questions for the user |
| R36 | Offer reusable autonomy/model-policy presets with visible overrides |
| R37 | When a task is blocked, continue independent eligible tasks where possible and retain the unresolved items for human attention |
| R38 | Start each plan on a dedicated branch in each affected repository; reuse those branches on resume |
| R39 | Discover nested repositories at project startup, track their boundaries, and include the repository map in agent context |
| R40 | Ask explicitly about pre-existing uncommitted changes and never delete them automatically; require pausing before manual edits during execution |
| R41 | End automated delivery at merge/pull request creation; automatic merging is excluded |
| R42 | Support manually configurable harness/model profiles initially; defer guided onboarding |
| R43 | Use one dedicated branch per plan per affected repository, reusing it when resuming the plan |
| R44 | Support GitHub and GitLab delivery with draft requests as the default |
| R45 | Prioritize task progression and actionable user requests on the main dashboard; show compact quality status and optionally live agent activity |
| R46 | Put detailed changes, quality results, and cost information in secondary navigable views |
| R47 | Show reviewer findings, implementation summaries, and check results in human review; use external IDE tools for code/diff inspection |
| R48 | Show human verification checklists only for task requirements that need manual functional verification |
| R49 | Automatically save and clear agent-owned changes while preserving pre-existing user work; request approval to bring a saved attempt back |
| R50 | Produce commits per task, allowing multiple commits where they improve clarity, subject to commit authorization |
| R51 | Provide one actionable inbox for approvals, agent questions, failed checks, and manual verification, linked to task context; nonblocking items need not halt eligible work |
| R52 | Support Pass / Fail / Cannot verify outcomes for manual checks, with notes and evidence references |
| R53 | Distinguish blocking review findings from suggestions using configurable severity thresholds; suggestions alone need not trigger repairs |
| R54 | Offer distinct human rejection actions: request changes, clarify requirements, and stop task |
| R55 | Require configurable repair-attempt and execution-time limits for autonomous execution; distinguish infrastructure retries from repair attempts |
| R56 | Provide pause-scheduling (finish the current attempt, then stop dispatch) and stop-now (interrupt and preserve) controls |
| R57 | Prevent simultaneous execution ownership of overlapping project folder trees |
| R58 | Let profiles reference existing harness skills/instructions, supplemented by project- and role-specific instructions; expose unsupported adapter combinations |
| R59 | Connect to an existing llama.cpp service; model-server installation and lifecycle management are outside the initial scope |
| R60 | Default each shared local inference endpoint to one active agent across projects, queuing other requests without blocking unrelated cloud execution |
| R61 | Enforce per-attempt and cumulative task execution-time limits; exclude approval/resource-queue waits, and do not reset the cumulative allowance on retries |
| R62 | Keep multiple plans per project in an explicit queue with one active plan; automatic advancement is optional |
| R63 | Enforce project restrictions over task/agent grants; permanent grants default to project scope, are visible/revocable, and require an explicit choice for global scope |
| R64 | Retain completed-plan summaries, decisions, check records, and commit references indefinitely by default; make raw-transcript retention configurable and require explicit cleanup of unfinished checkpoints |
| R65 | Create a visible end-of-plan finalization task that summarizes the work, references produced resources, and saves the durable record in the plan folder |
| R66 | Store plan archives in a local Git-ignored folder by default; export or commit selected records only explicitly |
| R67 | On summary failure, retain the factual archive and show finalization pending; retry finalization alone and allow draft delivery as an explicit action |
| R68 | Default raw transcript retention to 30 days after plan completion, configurable per project; preserve unfinished recovery context |
| R69 | Manage only harness sessions launched by this application in the first release |
| R70 | Provide a terminal-only interface initially; defer a browser dashboard |
| R71 | Coordinate workspace ownership and shared local inference capacity across application instances without requiring background agent execution |

The user selected the project name Vigil after Stage 1, approved Go, Cobra, Bubble Tea, and SQLite, and requested technology selection followed by functional analysis before workflow design.

## Proposed verification conditions

| ID | Requirement | Observable acceptance condition |
| --- | --- | --- |
| P01 | Configure named harness/model profiles | Two profiles can use different endpoints or models without changing application code |
| P02 | Inspect harness availability and capabilities | An unavailable executable or unsupported operation has an explicit explanation |
| P03 | Preserve task and run history | Reassignment retains previous attempts and their results |
| P04 | Display useful session state | Running, awaiting input, terminated, and unknown states are distinguishable where the adapter supports them |
| P05 | Allow manual assignment overrides | The selected profile and override reason are recorded |
| P06 | Expose task context and outputs | A task detail view shows requirements, dependencies, attempts, and artifacts |
| P07 | Report cost and usage honestly | Observed, estimated, and unavailable usage are distinguishable; unknown is never shown as zero |
| P08 | Support bounded resource use | Concurrency, local inference capacity, and spending constraints can be configured |
| P09 | Keep the dashboard responsive | Streaming output cannot block input; retention and buffering are bounded |
| P10 | Preserve native harness controls | Required permission/input requests are surfaced or the limitation is stated |
| P11 | Separate process completion from task acceptance | A successful process exit alone does not mark the task as accepted |
| P12 | Make recovery explicit | After interruption, stale runs are reconciled or shown as unknown rather than silently relaunched |

Local inference may have no per-token vendor charge, but still consumes hardware, energy, and time. Evaluate savings using total attempts and review effort, not only the worker model's token price. Exact billing may be unavailable for some subscription-based harnesses.

## Accepted scope and defaults

- Linux and macOS; Go, Cobra, Bubble Tea/Bubbles/Lip Gloss, and SQLite.
- Codex and Hermes first; existing llama.cpp service; manual profile configuration.
- Foreground execution with pause/stop/resume controls; terminal dashboard and actionable inbox.
- Sequential agents and one active plan per project; explicit plan queues; nonoverlapping projects can run concurrently.
- Local-only, hybrid, and cloud-allowed model policies separate from supervised/autonomous approval modes. Effective overrides must be visible. No default model policy has been selected.
- Approval-first by default. Categories include plan acceptance, model assignment, spending, scope changes, commits, pushes, request creation, and bringing saved work back. File editing, ordinary commands, and bounded retries need no separate application approval by default; they cannot bypass the gated actions.
- Approval scopes: once, task, plan, or permanent, with proposed shortcuts Y/T/P/A respectively. Permanent grants default to project scope; project restrictions dominate lower-level grants. Exact keyboard bindings and denial/deferral UI details remain design work.
- Required checks and agent review remain in autonomous mode. Human acceptance can be waived through configuration; a manual verification requirement is not silently satisfied by such a waiver.
- Fresh supervisor sessions at defined checkpoints; fresh reviewers report findings rather than modifying code. Task edits/splits/merges require applicable approval; acceptance criteria cannot be changed autonomously.
- Retry exhaustion invokes supervisor assessment, with possible proposals for a stronger eligible model, user clarification/confirmation, task revision, or splitting. Escalation does not itself grant spending or scope permissions. Acceptance-criteria changes require explicit human authorization rather than being a way to force a failing task to pass.
- Existing user work is preserved. Agent work may be saved/cleared automatically; bringing it back asks for approval.
- Draft GitHub PR/GitLab MR is the delivery endpoint. Automatic merging is excluded.
- Local plan archives and durable evidence; 30-day completed-plan transcript retention; explicit cleanup for unfinished checkpoints.

## Open design and feasibility questions

These are unresolved details, not reasons to reopen accepted product direction:

1. Complete live Codex/Hermes validation for launching, observing, cancelling, resuming, intervening, instruction resources, and enforcing gated actions. The [initial investigation](harness-capabilities.md) verified metadata handshakes and selected candidate transports; its runtime validation gates remain open. An unsupported operation must remain visible.
2. Specify task/run/plan transitions, typed model tools/results, retry accounting, numerical defaults, and attribution of review/check/supervisor time.
3. Define coordination storage, canonical folder and endpoint identity, fair queues, stale-owner recovery, and crash reconciliation. Initial scope is application instances on one machine; external clients or hosts cannot be assumed to honor these slots.
4. Define snapshot mechanics, ownership of mixed user/agent edits, accepted-task partial commits, reapplication conflicts, retention, and multi-repository recovery.
5. Define plan edits, dependency validation, review/check evidence invalidation, baseline-failure comparison, and in-flight permission revocation.
6. Specify profile/config schemas, instruction precedence, safe credential references, event/transcript boundaries, and archive manifest format.
7. Decide self-hosted GitHub/GitLab coverage, issue-URL import scope, repository base selection, submodule handling, and multi-repository request linkage.
8. Define exact interaction/navigation behavior for the inbox, native harness handoff, manual takeover, human rejection, and manual checks.
9. Specify finalization/delivery ordering and archive verification without recursive summary tasks or rerunning completed development.
10. Define the project relationship model: a project may span repositories; subprojects are a desired possibility, with independent projects sufficient initially. Warn about an existing parent instead of silently registering a child. Specify first-visit versus returning-folder behavior and optional initialization prompts.

Supporting decisions: [supervision](supervision-options.md), [presets](autonomy-presets.md), [checkpoints](checkpoints.md), [finalization](plan-finalization.md).

For a topic-by-topic audit of decisions, alternatives, and provenance limits, see the [session continuity audit](session-audit.md). R38 and R43 intentionally retain the same branch-lifetime decision under their historical IDs; they do not describe two different behaviors.

## Deferred or excluded

Deferred: Claude Code/pi/OpenCode adapters, guided onboarding, generated project skills, managed worktrees, within-project agent parallelism, browser UI, adoption of external sessions, and optional Jev routing evaluation.

Excluded from the initial direction: background agent execution, model-server lifecycle management, arbitrary agent-authored workflows, and automatic merging. Remote/distributed workers are not part of the agreed first milestone.
