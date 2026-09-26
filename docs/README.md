# Vigil documentation index

Vigil is a local control panel that runs existing agent harnesses through a Go core
with SQLite state. It is pre-release: the offline core through Stage 5.4 is
implemented and independently accepted, and production dispatch is deliberately
disabled.

This index states what each document is authoritative for. Read
[`next-steps.md`](next-steps.md) first for current state and the next action, and
[`STATUS`](STATUS) for the machine-readable values used by `make docs-check`.

## Current state and decisions

| Document | Authoritative for | Status |
| --- | --- | --- |
| [`next-steps.md`](next-steps.md) | Current state, pending gates, resume plan, next action | Current |
| [`STATUS`](STATUS) | Machine-readable current state; single source for `make docs-check` | Current |
| [`pending-decisions.md`](pending-decisions.md) | Decisions awaiting the user; what is still a user gate | Current |
| [`session-audit.md`](session-audit.md) | Decision index, alternatives, unresolved questions, provenance | Current, append-only |
| [`checkpoints.md`](checkpoints.md) | Checkpoint product behavior and preservation boundaries | Current |
| [`README.md`](../README.md) | Build/run instructions and repository structure | Current |

## Product baseline (design records)

These describe intended behavior. They are authoritative for *what the product
should do*, not for what is implemented. Implementation status lives in
`next-steps.md`.

| Document | Authoritative for |
| --- | --- |
| [`requirements.md`](requirements.md) | Consolidated product baseline R01–R71 and open design questions |
| [`mvp-acceptance.md`](mvp-acceptance.md) | The first usable milestone demonstration scenario |
| [`architecture.md`](architecture.md) | Responsibilities and integration boundaries |
| [`core-spec.md`](core-spec.md) | State, policy, coordination, checkpoint and storage contracts |
| [`technology.md`](technology.md) | Go-versus-Python decision and the foundation stack |
| [`autonomy-presets.md`](autonomy-presets.md) | Proposed two-axis autonomy presets |
| [`supervision-options.md`](supervision-options.md) | Supervisor and workflow-composition options |
| [`plan-finalization.md`](plan-finalization.md) | Finalization, local storage and retention defaults |
| [`discovery-notes.md`](discovery-notes.md) | Chronological discovery history and superseded options |
| [`harness-capabilities.md`](harness-capabilities.md) | Harness transport recommendation and capability evidence |
| [`adapter-spike.md`](adapter-spike.md) | Stage 1 adapter contract and reproducible profiles |

## Stage plans and specifications

| Document | Authoritative for |
| --- | --- |
| [`stage-2-plan.md`](stage-2-plan.md) | Stage 2 transport and controlled execution plan |
| [`stage-3-plan.md`](stage-3-plan.md) | Stage 3 lifecycle and policy validation plan |
| [`stage-3.5-macos.md`](stage-3.5-macos.md) | Stage 3.5 macOS environment and runtime qualification |
| [`stage-4-plan.md`](stage-4-plan.md) | Stage 4 core specification plan |
| [`stage-5-plan.md`](stage-5-plan.md) | Stage 5 backlog and R01–R71 stage mapping |
| [`stage-5-execution.md`](stage-5-execution.md) | Stage 5 implementation order and checkpoints |
| [`stage-5-boundary.md`](stage-5-boundary.md) | Execution boundary and qualification contract |
| [`stage-5-cli.md`](stage-5-cli.md) | **CLI reference.** Every command, flag and command receipt |
| [`stage-5/README.md`](stage-5/README.md) | Stage 5.1–5.7 index, ordering and completion gates |
| [`stage-5/5.1-execution-qualification.md`](stage-5/5.1-execution-qualification.md) | Stage 5.1 execution qualification |
| [`stage-5/5.2-repositories-and-execution.md`](stage-5/5.2-repositories-and-execution.md) | Stage 5.2 repositories, execution and supervision |
| [`stage-5/5.3-recovery-and-controls.md`](stage-5/5.3-recovery-and-controls.md) | Stage 5.3 recovery, pause/stop and budgets |
| [`stage-5/5.4-quality-and-acceptance.md`](stage-5/5.4-quality-and-acceptance.md) | Stage 5.4 checks, review, repair and acceptance |
| [`stage-5/5.5-workflow-and-planning.md`](stage-5/5.5-workflow-and-planning.md) | Stage 5.5 workflow and planning (**implemented offline, partial and unaccepted**) |
| [`stage-5/5.6-delivery-and-finalization.md`](stage-5/5.6-delivery-and-finalization.md) | Stage 5.6 delivery and finalization (**not started**) |
| [`stage-5/5.7-end-to-end-qualification.md`](stage-5/5.7-end-to-end-qualification.md) | Stage 5.7 end-to-end qualification (**not started**) |

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
section instead.

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
| [`research/stage-5/5.2/review-probes/`](research/stage-5/5.2/review-probes/) | Retained inert Stage 5.2 review probes (`.go.txt`) |
| [`research/stage-5/5.3/review-probes/`](research/stage-5/5.3/review-probes/) | Retained inert Stage 5.3 review probes (`.go.txt`) |
| [`research/stage-5/5.4/review-probes/`](research/stage-5/5.4/review-probes/) | Retained inert Stage 5.4 review probes (`.go.txt`) |

Review probes are stored as `.go.txt` so they cannot compile. To run one, copy it
into the package it targets under a `_test.go` name, run it, then delete the copy.

## Identifier conventions

Two numbering schemes are in use. Qualify review findings with their stage whenever a
sentence spans more than one stage.

| Form | Meaning | Defined in |
| --- | --- | --- |
| `R01`-`R71` (two digits) | Functional requirements | [`requirements.md`](requirements.md), mapped in [`stage-5-plan.md`](stage-5-plan.md) |
| `5.4-R2`, `5.2-R6` (stage prefix) | A finding of one stage's independent review | That stage's `research/stage-5/5.N/astra-review.md` |
| `F1`-`F3` | P3 findings from the Stage 5.4 follow-up review | [`research/stage-5/5.4/astra-review.md`](research/stage-5/5.4/astra-review.md#independent-follow-up-of-cba322b) |

The same bare label denotes different findings in different reviews, so an unqualified
`R1` in a shared status document is ambiguous. Per-stage documents are self-scoping.

## Maintenance

`AGENTS.md` in the repository root defines the documentation obligations for agents
making changes. `make docs-check` enforces the mechanically checkable parts: broken
relative links, orphan documents, undocumented CLI commands, a structure block that
does not match `internal/`, commit ranges that do not resolve, superseded ranges in
"next action" text, migration-count drift, and status documents that disagree with
`STATUS`.
