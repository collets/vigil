# Stage 6 — Terminal interface parity and completeness

<!-- vigil-tier: plan -->

Status: **scope defined, not planned and not started.** Established 2026-09-29
by explicit user scope revision. Stage 6 does not exist in the original roadmap
(which defines Stages 1–5 only); the user directed that it follow Stage 5.7.

This document is a **scope** description. It states the outcome, the boundaries
and the deliverable. It deliberately contains **no work checkpoints** — those are
[6.1's](#61-parity-gap-analysis) job, because the sub-stage structure cannot be
known before the gap is measured.

## Why this stage exists

R11 requires: *"Make the dashboard the primary application interface, with ways
to intervene in individual agents."* R70 scopes the product to a terminal-only
interface initially.

The interface does not yet meet that. As of `f2d740c`, `internal/tui` is a
single 700-line file with one `View()` function, five tabs (Overview, Tasks,
Inbox, History, Detail) and no sub-views. It mentions **no** delivery,
finalization, archive, retention, reconciliation or attestation concept, and
exposes none of the Stage 5.6 or Stage 5.7 command surface. Meanwhile the CLI
has grown to roughly sixty commands, so the two surfaces have diverged badly.

So this stage is not "polish the TUI". It is **building the primary interface
out to parity with the product**, and then to a feature state good enough for a
human to use it as their main way of running Vigil.

## Outcome

At the end of Stage 6:

1. **Every Vigil capability reachable from the terminal interface is reachable
   through it.** Not "the common ones" — the full set, including recovery,
   quality, delivery, finalization, archive, retention and the human decision
   surfaces. Where a capability is genuinely impossible to express in a
   terminal interface, that exclusion is written down with its reason, in the
   stage's own results document, rather than left as a silent gap.
2. The interface satisfies R11, R45, R46, R47 and R70 as they are currently
   written.
3. **`docs/research/stage-6/results.md` contains a complete feature list of the
   terminal interface**: every screen, every action, every key binding, the
   command each action maps to, and its requirement coverage. This is a
   **required deliverable of Stage 6**, not a side artifact, because Stage 7
   documents from it and Stage 8 tests against it.
4. The stage's work is autonomous. No human decision, live credential or real
   hosted remote is required to complete it.

## Boundaries

- **In scope:** `internal/tui`, its interaction model, navigation, information
  architecture, and the production paths that feed it. Adding a read model or
  query in `internal/core` to support a view is in scope when the interface
  needs it.
- **In scope:** amending the requirements. If parity requires R11/R45/R46/R47 to
  be sharpened, widened or made testable, Stage 6.1 **may and must** do that,
  updating `docs/core/requirements.md` and the requirement map in the same
  change. A vague requirement that cannot be verified is a defect.
- **Out of scope:** the browser dashboard. R70 defers it and this stage does not
  revisit that; doing so is a scope revision in its own right.
- **Out of scope:** live qualification, real harness runs, real delivery, and any
  human review. Those are [Stage 8](../stage-8/stage-8.md).
- **Out of scope:** new product capability that the CLI does not already have.
  Stage 6 makes the existing product usable from the TUI; it does not add
  features. If the gap analysis finds a capability the *product* needs, it is
  recorded for a later scope decision, not built here.

## Sub-stages

| Sub-stage | Purpose | Autonomous |
| --- | --- | --- |
| [6.1](#61-parity-gap-analysis) | Parity gap analysis; amend the requirements it finds untestable | yes |
| 6.2 … | Implementation sub-stages, planned by 6.1 from the measured gap | yes |

### 6.1 Parity gap analysis

6.1 does analysis and planning work only. It does not implement interface code.

Its deliverables:

1. **Inventory** the terminal interface as it actually is today: every screen,
   every action, every key binding, and the command each one drives. Produced by
   reading `internal/tui` and by exercising the built binary — not from the
   source's self-description.
2. **Inventory** the reachable product surface: every CLI command group, the
   `apply` envelope commands, and the human decision points, each classified as
   *expressible in a terminal interface*, *expressible with a documented
   compromise*, or *not expressible, with the reason*.
3. **Requirement audit** against R11, R45, R46, R47 and R70. For each, state
   whether the requirement as written is (a) met, (b) partially met, or (c) not
   measurable as written — and for (b) and (c), **amend `requirements.md` and the
   requirement map** so the criterion is specific and testable. This amendment is
   authorized by this document and is required where the gap analysis shows a
   criterion is vague.
4. **The gap table**: capability → current interface state → required state →
   requirement touched → the sub-stage that will own it.
5. **The sub-stage decomposition**: a proposal of sub-stages 6.2…6.y, each with
   its own outcome, dependencies, validation method and completion criteria, plus
   a per-sub-stage plan written to the standard of the Stage 5 slice plans.
6. **The feature-list skeleton** for `results.md`, so the final document is
   accumulated during implementation rather than reconstructed at the end.

**6.1 must not** implement interface code, and **must not** silently absorb the
implementation into itself. Its output is decisions and plans.

## Dependencies

- [Stage 5.7](../stage-5/5.7-end-to-end-qualification.md) autonomous evidence, so
  the interface is built against a settled backend and a known scenario runner.
- [5.6 delivery and finalization](../stage-5/5.6-delivery-and-finalization.md)
  accepted for the autonomous part, since the interface must expose its command
  surface.
- No dependency on Stage 5.1's live Codex route. A live route is not required to
  build or verify an interface.

## What Stage 6 completion is not

Stage 6 completing does **not** mean the product works for a real user. It means
the interface is complete and coherent. No human has used it; nothing has been
run against a credentialed model or a real remote. That evidence arrives in
Stage 8, and until it does, any claim that Vigil is usable as a primary tool is
unsupported.

## Handoff

An interrupted 6.x must record its last completed sub-stage, the gap-table state
as written, the sub-stages planned so far, and the next safe action. A resumed
agent reads the 6.1 results and the current sub-stage plan; it does not
re-derive the gap analysis.

Fresh-context prompt: "Execute Vigil Stage 6 using this document and the
predecessor results. Start at 6.1 unless a later sub-stage plan is named.
Measure the gap from the code and the binary, not from prose."
