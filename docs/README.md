# Vigil documentation index

<!-- vigil-tier: index -->
<!-- vigil-status: stage=6.1; stage_accepted=true; implementation_commit=bad613950b11de53130487fdb24e0a10d40d1480 -->

Vigil is a local control panel that runs existing agent harnesses through a Go core
with SQLite state. It is pre-release: the offline core is
implemented and independently accepted through Stage 5.7 at implementation
commit `522cb96`, and production dispatch is deliberately disabled. Each stage's
acceptance covers its **autonomous** scope; every human-gated operation is
deferred to Stage 8, and no delivery path has been exercised against a real
remote. Stages 6 (terminal interface
parity), 7 (documentation website) and 8 (human review) were added by explicit
user scope revision on 2026-09-29.

**Stage 6.1 is independently accepted at `bad6139` for its autonomous scope.** It was rejected by its first independent review, returned conditional by each of thirteen further, and all fourteen rounds of findings are remediated. Acceptance rests on the measurements — fourteen independent reviewers re-derived every figure from source and found none wrong — and not on a clean final round; the bookkeeping findings of rounds five to fourteen are recorded in its review file and were judged process noise. It was the parity gap
analysis, so it wrote no interface code. It measured the interface and the
product surface from the code and the built binary, found that the interface
fully expresses 4 of the 82 distinct capabilities the 85-row register classifies, and 6 of the 25 human
decision classes, found that R11/R45/R46/R47/R70 were all unmeasurable as written,
amended them with conditions P13–P18, and planned sub-stages 6.2–6.9. Stage 6
itself is not complete.

This index states what each document is authoritative for. For the *reading
order* — what to read now and what you can skip — start at
[`START-HERE.md`](START-HERE.md). Read
[`process/next-steps.md`](process/next-steps.md) for current state and the next
action, and [`STATUS`](STATUS) for the machine-readable values used by
`make docs-check`.

## How the documentation is organized

Documents are grouped by **when you need them**, not by when they were written.
The folder is the tier, every Markdown document carries a `<!-- vigil-tier: ... -->`
marker agreeing with its folder, and `make docs-check` fails when a document is
unclassified or disagrees with its folder. A marker quoted inside a code block is
documentation *about* the convention and is not counted. A tier is a reading
path, not a boundary: reading across tiers is expected and useful.

| Tier | Folder | Authority |
| --- | --- | --- |
| Entry | [`START-HERE.md`](START-HERE.md) | Reading order and task-to-document routing |
| Core | [`core/`](core/) | Durable product facts: what the product is and must do |
| Process | [`process/`](process/) | How work is done, and what is currently pending |
| Plan | [`plans/`](plans/) | Task-scoped implementation plans |
| Evidence | [`research/`](research/) | Dated observations and independent reviews |
| History | [`history/`](history/) | Superseded records kept for provenance |
| Record | [`spec/`](spec/) | Draft schemas superseded by the installed migrations |
| Index | this file, [`STATUS`](STATUS) | What each document is authoritative for |

## Core: durable product facts

These describe intended behavior. They are authoritative for *what the product
should do*, not for what is implemented. Implementation status lives in
[`process/next-steps.md`](process/next-steps.md).

| Document | Authoritative for |
| --- | --- |
| [`core/requirements.md`](core/requirements.md) | Consolidated product baseline R01–R71 and open design questions |
| [`core/architecture.md`](core/architecture.md) | Responsibilities and integration boundaries |
| [`core/core-spec.md`](core/core-spec.md) | State, policy, coordination, checkpoint and storage contracts |
| [`core/technology.md`](core/technology.md) | Go-versus-Python decision and the foundation stack |
| [`core/harness-capabilities.md`](core/harness-capabilities.md) | Harness transport recommendation and capability evidence |
| [`core/mvp-acceptance.md`](core/mvp-acceptance.md) | The first usable milestone demonstration scenario |
| [`core/checkpoints.md`](core/checkpoints.md) | Checkpoint product behavior and preservation boundaries |
| [`core/plan-finalization.md`](core/plan-finalization.md) | Finalization, local storage and retention defaults |

## Process: how work is done, and what is pending

| Document | Authoritative for | Status |
| --- | --- | --- |
| [`process/development-workflow.md`](process/development-workflow.md) | Mandatory Git branches, worktrees, review, native validation and main-integration rules | Current |
| [`process/next-steps.md`](process/next-steps.md) | Current state, pending gates, resume plan, next action | Current |
| [`process/pending-decisions.md`](process/pending-decisions.md) | Decisions awaiting the user; what is still a user gate | Current |
| [`process/session-audit.md`](process/session-audit.md) | Decision index, alternatives, unresolved questions, provenance | Current, append-only |
| [`../AGENTS.md`](../AGENTS.md) | Project rules and the documentation obligations every change must satisfy | Current |
| [`../README.md`](../README.md) | Build/run instructions and repository structure | Current |

## Plans: task-scoped

| Document | Authoritative for |
| --- | --- |
| [`plans/stage-5/README.md`](plans/stage-5/README.md) | Stage 5.1–5.7 index, ordering and completion gates |
| [`plans/stage-5/5.1-execution-qualification.md`](plans/stage-5/5.1-execution-qualification.md) | Stage 5.1 execution qualification |
| [`plans/stage-5/5.2-repositories-and-execution.md`](plans/stage-5/5.2-repositories-and-execution.md) | Stage 5.2 repositories, execution and supervision |
| [`plans/stage-5/5.3-recovery-and-controls.md`](plans/stage-5/5.3-recovery-and-controls.md) | Stage 5.3 recovery, pause/stop and budgets |
| [`plans/stage-5/5.4-quality-and-acceptance.md`](plans/stage-5/5.4-quality-and-acceptance.md) | Stage 5.4 checks, review, repair and acceptance |
| [`plans/stage-5/5.5-workflow-and-planning.md`](plans/stage-5/5.5-workflow-and-planning.md) | Stage 5.5 workflow and planning (**independently accepted offline**) |
| [`plans/stage-5/5.6-delivery-and-finalization.md`](plans/stage-5/5.6-delivery-and-finalization.md) | Stage 5.6 delivery and finalization (**implemented; autonomous scope only; human gates deferred to Stage 8**) |
| [`plans/stage-5/5.7-end-to-end-qualification.md`](plans/stage-5/5.7-end-to-end-qualification.md) | Stage 5.7 autonomous end-to-end qualification (independently accepted for its autonomous scope; human steps moved to Stage 8) |
| [`plans/stage-5/stage-5-plan.md`](plans/stage-5/stage-5-plan.md) | Stage 5 backlog and R01–R71 stage mapping |
| [`plans/stage-5/stage-5-execution.md`](plans/stage-5/stage-5-execution.md) | Stage 5 implementation order and checkpoints |
| [`plans/stage-5/stage-5-boundary.md`](plans/stage-5/stage-5-boundary.md) | Execution boundary and qualification contract |
| [`plans/stage-5/stage-5-cli.md`](plans/stage-5/stage-5-cli.md) | **CLI reference.** Every command, flag and command receipt |
| [`plans/stage-6/stage-6.md`](plans/stage-6/stage-6.md) | **Stage 6 scope:** terminal interface parity and completeness, and its complete feature list (user scope revision 2026-09-29) |
| [`plans/stage-6/6.1-parity-gap-analysis.md`](plans/stage-6/6.1-parity-gap-analysis.md) | **Stage 6.1:** the measured interface and product-surface inventories, the R11/R45/R46/R47/R70 audit and its amendments, 21 measured defects, the gap table and the 6.2–6.9 decomposition (**complete**) |
| [`plans/stage-6/6.2-interface-architecture-and-parity-register.md`](plans/stage-6/6.2-interface-architecture-and-parity-register.md) | Stage 6.2 screen-stack architecture, key safety and the mechanical parity register |
| [`plans/stage-6/6.3-main-dashboard.md`](plans/stage-6/6.3-main-dashboard.md) | Stage 6.3 main dashboard, live run state and navigable history |
| [`plans/stage-6/6.4-actionable-inbox.md`](plans/stage-6/6.4-actionable-inbox.md) | Stage 6.4 the complete human decision surface |
| [`plans/stage-6/6.5-quality-review-and-evidence-views.md`](plans/stage-6/6.5-quality-review-and-evidence-views.md) | Stage 6.5 quality, review, evidence and human acceptance views |
| [`plans/stage-6/6.6-execution-recovery-and-checkpoints.md`](plans/stage-6/6.6-execution-recovery-and-checkpoints.md) | Stage 6.6 execution, recovery and checkpoint screens |
| [`plans/stage-6/6.7-delivery-archive-and-finalization.md`](plans/stage-6/6.7-delivery-archive-and-finalization.md) | Stage 6.7 delivery, archive, finalization and retention screens |
| [`plans/stage-6/6.8-setup-definitions-and-resources.md`](plans/stage-6/6.8-setup-definitions-and-resources.md) | Stage 6.8 setup, definitions and resource coordination screens |
| [`plans/stage-6/6.9-parity-closure-and-feature-list.md`](plans/stage-6/6.9-parity-closure-and-feature-list.md) | Stage 6.9 exclusion register, complete feature list and Stage 6 closure |
| [`plans/stage-7/stage-7.md`](plans/stage-7/stage-7.md) | **Stage 7 scope:** user-facing documentation website for the terminal interface (7.1 decides the information architecture and generator) |
| [`plans/stage-8/stage-8.md`](plans/stage-8/stage-8.md) | **Stage 8 scope:** the human review stage — the walkthrough guide, the bulk finding report format, and every human-gated step deferred out of 5.6/5.7 |

## History: superseded, kept for provenance

Read these when you want the reasoning behind a decision, or the alternative
that was rejected. They are superseded, so treat them as provenance rather than
as the authority for current behavior.

| Document | Authoritative for |
| --- | --- |
| [`history/README.md`](history/README.md) | What superseded each record, and the conventions for this folder |
| [`history/discovery-notes.md`](history/discovery-notes.md) | Chronological discovery history and superseded options |
| [`history/adapter-spike.md`](history/adapter-spike.md) | Stage 1 adapter contract and reproducible profiles |
| [`history/stage-2-plan.md`](history/stage-2-plan.md) | Stage 2 transport and controlled execution plan |
| [`history/stage-3-plan.md`](history/stage-3-plan.md) | Stage 3 lifecycle and policy validation plan |
| [`history/stage-3.5-macos.md`](history/stage-3.5-macos.md) | Stage 3.5 macOS environment and runtime qualification |
| [`history/stage-4-plan.md`](history/stage-4-plan.md) | Stage 4 core specification plan |
| [`history/autonomy-presets.md`](history/autonomy-presets.md) | Proposed two-axis autonomy presets |
| [`history/supervision-options.md`](history/supervision-options.md) | Supervisor and workflow-composition options |

## Schemas

| Path | Authoritative for |
| --- | --- |
| [`spec/project.sql`](spec/project.sql) | Draft project/plan/task schema (Stage 4 record) |
| [`spec/coordination.sql`](spec/coordination.sql) | Draft coordination schema (Stage 4 record) |
| `internal/store/migrations/` | **Authoritative installed schema.** Forward-only; historical files are immutable |

The files in `docs/spec/` are validated against `internal/storage/spec_test.go` and
run under `make check`. They are a historical design record, **not** the installed
schema; the installed schema is the embedded migrations.

Stage 5.5's implementation boundary is `internal/tools/` for authoritative bounded
handlers and `internal/mcp/` for the transport-only JSON-RPC adapter. The transport
does not own authorization, sessions, limits or workflow state.

## Evidence and reviews

Historical evidence. Do not rewrite these to reflect newer state; add a new dated
section instead. Paths, commits and line numbers quoted inside these records are
left as the reviewer saw them, so a quoted path may no longer exist — see
[`history/README.md`](history/README.md).

| Document | Authoritative for |
| --- | --- |
| [`research/README.md`](research/README.md) | Research tree layout, naming and historical-evidence conventions |
| [`research/stage-1/results.json`](research/stage-1/results.json) | Stage 1 harness settings, versions, metadata probes |
| [`research/stage-1/harness-probe-results.json`](research/stage-1/harness-probe-results.json) | Durable harness probe evidence |
| [`research/stage-2/results.md`](research/stage-2/results.md) / [`.json`](research/stage-2/results.json) | Stage 2 transport validation |
| [`research/stage-3/results.md`](research/stage-3/results.md) / [`.json`](research/stage-3/results.json) | Stage 3 Linux lifecycle and policy results |
| [`research/stage-3.5/results.md`](research/stage-3.5/results.md) / [`.json`](research/stage-3.5/results.json) | Stage 3.5 macOS runtime results |
| [`research/stage-4/results.md`](research/stage-4/results.md) | Stage 4 specification validation |
| [`research/stage-5/foundation-results.md`](research/stage-5/foundation-results.md) | Stage 5 planning/control foundation |
| [`research/stage-5/5.1/results.md`](research/stage-5/5.1/results.md) | Stage 5.1 results and live gates |
| [`research/stage-5/5.1/astra-review.md`](research/stage-5/5.1/astra-review.md) | Stage 5.1 independent review |
| [`research/stage-5/5.2/results.md`](research/stage-5/5.2/results.md) | Stage 5.2 results |
| [`research/stage-5/5.2/astra-review.md`](research/stage-5/5.2/astra-review.md) | Stage 5.2 independent review |
| [`research/stage-5/5.3/results.md`](research/stage-5/5.3/results.md) | Stage 5.3 results |
| [`research/stage-5/5.3/astra-review.md`](research/stage-5/5.3/astra-review.md) | Stage 5.3 independent review |
| [`research/stage-5/5.4/results.md`](research/stage-5/5.4/results.md) | Stage 5.4 results and validation evidence |
| [`research/stage-5/5.4/astra-review.md`](research/stage-5/5.4/astra-review.md) | **Stage 5.4 independent review record**, R1–R11 and F1–F3 |
| [`research/stage-5/5.5/results.md`](research/stage-5/5.5/results.md) | Stage 5.5 implementation and validation evidence |
| [`research/stage-5/5.5/blockers.md`](research/stage-5/5.5/blockers.md) | Append-only Stage 5.5 user/runtime blocker log |
| [`research/stage-5/5.5/user-decisions.md`](research/stage-5/5.5/user-decisions.md) | Review packet for remaining Stage 5.5 product, live-route and validation choices |
| [`research/stage-5/5.5/qualification-input.md`](research/stage-5/5.5/qualification-input.md) | Visible credential-free review copy of the immutable Stage 5.5 qualification Markdown input |
| [`research/stage-5/5.5/qualification-proposal.md`](research/stage-5/5.5/qualification-proposal.md) | Human-readable review copy of exact pending qualification proposal revision 1 |
| [`research/stage-5/5.6/results.md`](research/stage-5/5.6/results.md) | Stage 5.6 implementation, review and validation evidence (**accepted `b0a085b`, autonomous scope only; human gates in Stage 8**) |
| [`research/stage-5/5.7/results.md`](research/stage-5/5.7/results.md) | Stage 5.7 autonomous qualification evidence, the evidence matrix, and **finding 5.7-F1** (the delivery path was unreachable; fixed in the Stage 5.2/5.6 slice) |
| [`research/stage-6/results.md`](research/stage-6/results.md) | **Stage 6 results:** the measured interface inventory with screen captures, the 85-entry **parity register**, the closed **exclusion register**, and the **feature list** — Stage 6's required deliverable. Skeleton created by 6.1; completed by 6.9 |
| [`research/stage-6/6.1-review.md`](research/stage-6/6.1-review.md) | Stage 6.1 independent adversarial review record, findings and verdict |
| [`research/stage-6/6.2-review.md`](research/stage-6/6.2-review.md) | Stage 6.2 independent acceptance review record, condition and verdict |
| `research/stage-5/5.2/review-probes/`, `research/stage-5/5.3/review-probes/`, `research/stage-5/5.4/review-probes/` | Retained inert review probes (`.go.txt`) |

Review probes are stored as `.go.txt` so they cannot compile. To run one, copy it
into the package it targets under a `_test.go` name, run it, then delete the copy.

## Identifier conventions

Two numbering schemes are in use. Qualify review findings with their stage whenever a
sentence spans more than one stage.

| Form | Meaning | Defined in |
| --- | --- | --- |
| `R01`-`R71` (two digits) | Functional requirements | [`core/requirements.md`](core/requirements.md), mapped in [`plans/stage-5/stage-5-plan.md`](plans/stage-5/stage-5-plan.md) |
| `P01`-`P18` (two digits) | Proposed verification conditions; P13–P18 added by Stage 6.1 | [`core/requirements.md`](core/requirements.md) |
| `5.4-R2`, `5.2-R6` (stage prefix) | A finding of one stage's independent review | That stage's `research/stage-5/5.N/astra-review.md` |
| `F1`-`F3` | P3 findings from the Stage 5.4 follow-up review | [`research/stage-5/5.4/astra-review.md`](research/stage-5/5.4/astra-review.md#independent-follow-up-of-cba322b) |
| `6.1-F1`-`6.1-F21` (stage prefix) | A measured interface defect found by Stage 6.1 | [`plans/stage-6/6.1-parity-gap-analysis.md`](plans/stage-6/6.1-parity-gap-analysis.md#4-measured-defects) |

The same bare label denotes different findings in different reviews, so an unqualified
`R1` in a shared status document is ambiguous. Per-stage documents are self-scoping.

## Maintenance

`AGENTS.md` in the repository root defines the documentation obligations for agents
making changes, including the layout and tier rules above. `make docs-check` enforces
the mechanically checkable parts: broken relative links, orphan documents,
unclassified or mis-tiered documents, undocumented CLI commands, a structure block
that does not match `internal/`, commit ranges that do not resolve, superseded
ranges in "next action" text, migration-count drift, and status documents that
disagree with `STATUS`.
