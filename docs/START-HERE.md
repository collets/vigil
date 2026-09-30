# Start here: reading Vigil's documentation

<!-- vigil-tier: entry -->
<!-- vigil-status: stage=5.7; stage_accepted=true; implementation_commit=522cb967f732b578a66c051cf8938dd00584d238 -->

Vigil is a local control panel that runs existing agent harnesses (Codex, Hermes)
through a Go core with SQLite state. It is **pre-release**: the offline core
through Stage 5.6 is implemented and independently accepted for its autonomous
scope, and production
check, model and reviewer dispatch are deliberately disabled. This page tells you
which documents to read, and — more importantly — which ones you can skip.

**Stage 6.1 is implemented, was rejected by its first
independent review, was returned conditional by each of ten further reviews, and all
eleven rounds of findings are remediated pending one more confirming review.** It is the terminal interface
parity gap analysis, so it wrote no interface code: it measured the interface
and the product surface from the code and the built binary, found that the
interface fully expresses 4 of the 82 distinct capabilities the 85-row register
classifies and 6 of the 25 human decision classes, found that
R11/R45/R46/R47/R70 were all unmeasurable as written, amended them with
conditions P13–P18, and planned sub-stages 6.2–6.9. Stage 6 is not complete.

[`docs/README.md`](README.md) is the full index: what every document is
authoritative for. This page is the *reading order*.

## The one rule: documentation is tiered by when you need it

The folder a document lives in **is** its tier. Every Markdown document carries a
`<!-- vigil-tier: ... -->` marker asserting the same thing, and `make docs-check`
fails if the two disagree or a document has no tier at all.

A tier tells you what a session *needs* and what it can usually skip. It is a
reading path, not a fence: reading outside your tier is always fine and often
useful, especially when you are checking how a related or earlier piece of work
was handled.

| Tier | Folder | When you need it |
| --- | --- | --- |
| Core | [`core/`](core/) | **Always**, in any session that changes code, plans work or answers a question about the product. |
| Process | [`process/`](process/) | **Always**, in any session that changes something or resumes work. Defines how work is done and what is currently pending. |
| Plan | [`plans/`](plans/) | **The task you are working on.** Other slices are usually unnecessary, but read the accepted ones when you want to know why a safeguard exists. |
| Evidence | [`research/`](research/) | When you need to support a claim about what was observed, or satisfy a validation gate. Supporting context, not a substitute for a spec. |
| History | [`history/`](history/) | When you want to know *why* something is the way it is, or to recover a superseded alternative. Superseded, so not a source for current behavior. |
| Record | [`spec/`](spec/) | Draft schemas kept as a design record. The installed migrations, not these, are the live schema. |

You are never expected to read all of `docs/`. The largest documents are plans
and evidence, and a session whose task is bounded can skip them without losing
anything it is accountable for.

## 1. Orientation (always, ~5 minutes)

| Step | Document | Why |
| --- | --- | --- |
| 1 | [`AGENTS.md`](../AGENTS.md) | Project rules, non-negotiables, and the documentation obligations every change must satisfy. |
| 2 | [`core/requirements.md`](core/requirements.md) | The product baseline R01–R71. What the product is supposed to do. |
| 3 | [`process/next-steps.md`](process/next-steps.md) | Current state, pending gates, and the exact next action. |
| 4 | [`core/architecture.md`](core/architecture.md) | Which package owns which responsibility, and the integration boundaries. |
| 5 | [`STATUS`](STATUS) | Machine-readable state. You rarely need to read it; `make docs-check` enforces it. |
| 6 | `make check` and `make docs-check` | The baseline. Do not report anything as working without it. |

## 2. Core: the durable product facts

Read these once per working session, in this order. They are short relative to
the plans, and they are what most questions actually turn on.

| Document | Read it when you need to know |
| --- | --- |
| [`core/requirements.md`](core/requirements.md) | What the product must do; the R01–R71 identifiers used by reviews and plans |
| [`core/architecture.md`](core/architecture.md) | Which component owns a responsibility, and where a change belongs |
| [`core/core-spec.md`](core/core-spec.md) | The normative state, policy, coordination, checkpoint and storage contracts |
| [`core/technology.md`](core/technology.md) | Why Go, SQLite, embedded migrations, no ORM; what the foundation stack is |
| [`core/harness-capabilities.md`](core/harness-capabilities.md) | Which harness transports exist, what was verified, and which runtime gates are still open |
| [`core/mvp-acceptance.md`](core/mvp-acceptance.md) | What the first genuinely usable milestone requires to demonstrate |
| [`core/checkpoints.md`](core/checkpoints.md) | Save/clear/restore behavior and the boundary that preserves user work |
| [`core/plan-finalization.md`](core/plan-finalization.md) | Finalization, local storage and retention defaults |

## 3. Process: how work is done here

| Document | Read it when you need to know |
| --- | --- |
| [`process/development-workflow.md`](process/development-workflow.md) | Branch, worktree, checkpoint, review, native validation and `main`-integration rules. **Mandatory before changing anything.** |
| [`process/pending-decisions.md`](process/pending-decisions.md) | Which questions are still the user's, and which have already been answered. Read before asking the user anything. |
| [`process/session-audit.md`](process/session-audit.md) | Why a decision was made, what alternatives were rejected, and the provenance limits of the record. |
| [`process/next-steps.md`](process/next-steps.md) | Current state, gates, resume plan, and the handoff for the next session. |

## 4. Plans: start from the slice you are working on

**Stage 5.7** (independently accepted for its autonomous scope) → [`plans/stage-5/5.7-end-to-end-qualification.md`](plans/stage-5/5.7-end-to-end-qualification.md), autonomous only.
**Start Stage 6.2+** → [`plans/stage-6/stage-6.md`](plans/stage-6/stage-6.md) for scope, then the sub-stage's own plan. 6.1 is **complete**: its measured inventories, requirement audit, gap table and the 6.2–6.9 decomposition are in [`plans/stage-6/6.1-parity-gap-analysis.md`](plans/stage-6/6.1-parity-gap-analysis.md), and the parity register and feature list are in [`research/stage-6/results.md`](research/stage-6/results.md). Do not re-derive the gap analysis.
**Start Stage 7 (or 7.1)** → [`plans/stage-7/stage-7.md`](plans/stage-7/stage-7.md); 7.1 owns the information architecture and the generator decision.
**Start Stage 8 (or 8.1)** → [`plans/stage-8/stage-8.md`](plans/stage-8/stage-8.md); this is the human stage, and 8.1 designs the walkthrough and the finding report.

The roadmap originally defined Stages 1–5 only. **Stages 6, 7 and 8 were added by
explicit user scope revision on 2026-09-29**, not earned by a completed stage.
Their documents are *scope* descriptions: outcome, boundaries and deliverable,
with the detailed sub-stage plans delegated to their `.1` sub-stage. Read the
scope document before the plan for whichever sub-stage you are running.

For Stage 5, start at [`plans/stage-5/README.md`](plans/stage-5/README.md). It
indexes Stage 5.1–5.7, their order, their dependencies, their user gates and
their completion criteria. The table below routes by task; it is the shortest
useful path, not a limit on what you may read.

| Your task | Start with |
| --- | --- |
| Any Stage 5 work | [`plans/stage-5/README.md`](plans/stage-5/README.md), then the slice you are changing |
| Stage 5.6 delivery and finalization (**implemented; acceptance scope narrowed to autonomous**) | [`5.6-delivery-and-finalization.md`](plans/stage-5/5.6-delivery-and-finalization.md) |
| Stage 5.7 autonomous qualification (implemented, under review) | [`5.7-end-to-end-qualification.md`](plans/stage-5/5.7-end-to-end-qualification.md) |
| Stage 6 terminal interface parity (**6.1 complete; 6.2–6.9 planned, not started**) | [`stage-6.md`](plans/stage-6/stage-6.md) for scope, then the sub-stage's own plan. Start at [`6.1-parity-gap-analysis.md`](plans/stage-6/6.1-parity-gap-analysis.md) for the measured gap |
| What the terminal interface can do today, and what it owes | [`research/stage-6/results.md`](research/stage-6/results.md) — the 85-entry parity register and the feature list |
| Stage 7 documentation website (**not started**) | [`stage-7.md`](plans/stage-7/stage-7.md), then 7.1 |
| Stage 8 human review (**not started; needs a human**) | [`stage-8.md`](plans/stage-8/stage-8.md), then 8.1 |
| Understanding why a Stage 5.1–5.5 safeguard exists | The accepted slice documents in [`plans/stage-5/`](plans/stage-5/), and the review record in [`research/stage-5/`](research/stage-5/) |
| Which requirements belong to which slice | [`plans/stage-5/stage-5-plan.md`](plans/stage-5/stage-5-plan.md) |
| Implementation order, prerequisites, failure tests | [`plans/stage-5/stage-5-execution.md`](plans/stage-5/stage-5-execution.md) |
| **A CLI command, flag, default or receipt** | [`plans/stage-5/stage-5-cli.md`](plans/stage-5/stage-5-cli.md) — the CLI reference is authoritative and complete |
| Container boundary, guardian, worker or relay | [`plans/stage-5/stage-5-boundary.md`](plans/stage-5/stage-5-boundary.md) |
| Earlier stages (1–4) | Superseded; see [`history/`](history/). The accepted baseline lives in `core/`. |

Do not start a stage without an explicit instruction naming it, and never
enable production dispatch: those are project rules, not defaults you may infer.
Stage 8 in particular cannot be completed by an agent alone. Stage 6.1's gap
analysis now exists, so 6.2–6.9 may be started — but only with an instruction
naming the sub-stage, one at a time, since they all edit the same source files.

## 5. Evidence and history, on demand

| Need | Go to |
| --- | --- |
| Proof that a runtime capability was observed | [`research/`](research/), by stage |
| An independent review record and its findings | [`research/stage-5/<slice>/astra-review.md`](research/stage-5/) |
| Why an option was rejected, or a superseded mechanism | [`history/`](history/) |
| The draft schemas superseded by the installed migrations | [`spec/`](spec/) — a historical design record, **not** the installed schema |

## 6. Schemas

[`internal/store/migrations/`](../internal/store/migrations) is the
authoritative installed schema: embedded, forward-only, and immutable once
applied. The SQL in [`spec/`](spec/) is a Stage 4 design record that
`internal/storage/spec_test.go` still validates; it is not what a running Vigil
uses.

## How this stays true

`make docs-check` (also part of `make check`) enforces the mechanics: every
relative link resolves, every document is listed in [`docs/README.md`](README.md)
and carries a tier marker matching its folder, every CLI command appears in the
CLI reference, the README structure block matches `internal/`, migration counts
match, and every live status document agrees with [`STATUS`](STATUS). The gate
cannot judge whether prose is *true*, so the obligations in
[`AGENTS.md`](../AGENTS.md) still require reviewing the prose a change touches.
