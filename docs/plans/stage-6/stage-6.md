# Stage 6 — Terminal interface parity and completeness

<!-- vigil-tier: plan -->

Status: **6.1 and 6.2 complete, 6.2 accepted; 6.3 implemented on its task
branch, its candidate review rejected, and twenty-one remediations applied with only
the twenty-first awaiting its follow-up review; 6.4–6.9 planned, not started.**
Established 2026-09-29
by explicit user scope revision. Stage 6 does not exist in the original roadmap
(which defines Stages 1–5 only); the user directed that it follow Stage 5.7.

This document is a **scope** description. It states the outcome, the boundaries
and the deliverable. It deliberately contains **no work checkpoints** — those are
[6.1's](#61-parity-gap-analysis) job, because the sub-stage structure cannot be
known before the gap is measured. That job is now done: see
[6.1's analysis](6.1-parity-gap-analysis.md), the
[results document](../../research/stage-6/results.md) and the
[independent review](../../research/stage-6/6.1-review.md).

## Why this stage exists

R11 **required**, in its pre-amendment text: *"Make the dashboard the primary
application interface, with ways to intervene in individual agents."* Stage 6.1's
audit found that clause **not measurable as written** and narrowed it — the
current text is the amended one in
[`requirements.md`](../../core/requirements.md), not the sentence quoted here,
which is quoted as the text the audit examined. R70 scopes the product to a terminal-only
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
| [6.1](6.1-parity-gap-analysis.md) | Parity gap analysis; amend the requirements it finds untestable — **complete** | yes |
| [6.2](6.2-interface-architecture-and-parity-register.md) | Interface architecture, key safety, and the mechanical parity register — **complete, accepted at `582e2d1`** | yes |
| [6.3](6.3-main-dashboard.md) | Main dashboard, live run state and navigable history | yes |
| [6.4](6.4-actionable-inbox.md) | The complete human decision surface | yes |
| [6.5](6.5-quality-review-and-evidence-views.md) | Quality, review, evidence and human acceptance views | yes |
| [6.6](6.6-execution-recovery-and-checkpoints.md) | Execution, recovery and checkpoint screens | yes |
| [6.7](6.7-delivery-archive-and-finalization.md) | Delivery, archive, finalization and retention screens | yes |
| [6.8](6.8-setup-definitions-and-resources.md) | Setup, definitions and resource coordination screens | yes |
| [6.9](6.9-parity-closure-and-feature-list.md) | Exclusion register, complete feature list, Stage 6 closure | yes |

6.3–6.8 are serialized rather than parallel: they all edit `internal/tui` and
the same read model, and the [development workflow](../../process/development-workflow.md)
forbids two agents owning one set of source files. 6.2 is first because every
later sub-stage navigates through the model it establishes and is checked by the
parity test it installs; 6.9 is last because the feature list is accumulated by
6.2–6.8 and can only be audited once they have landed.

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
   compromise*, or *not expressible, with the reason*. (6.1 renamed the third
   class to **excluded, with reason**, after establishing that none of its rows
   is a technical impossibility and that conflating a scope decision with one
   would be misleading. The class is the same; only the label changed, because
   the label was doing work the content did not support. See
   [6.1 §2.2](6.1-parity-gap-analysis.md#22-classification-rule).)
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

**6.1 delivered all six items** on 2026-09-30 from `27182de`, and wrote no
interface code. It measured the gap from the code and the built binary rather
than from prose, and found that of the 85 register rows — 82 distinct
capabilities — the interface fully expresses **4**, partially expresses **9**
and cannot reach **66** by any action; that 6 of the 25 human decision classes
are fully expressed, 6 are partial and 13 are absent; and that all five audited
requirements were unmeasurable as written, which is why the interface could sit
at four expressed capabilities through two accepted stages unnoticed. It
recorded 21 measured defects, 12 documented compromises, 6 excluded register rows (the first six of the 8-entry closed list in `requirements.md`) with
reasons, and 8 sub-stages (6.2–6.9), and it wrote the **closed** exclusion register that
stops Stage 6 from excusing itself out of R11. The evidence is in
[`research/stage-6/results.md`](../../research/stage-6/results.md) and the
derivation of every number is in
[§4.9](../../research/stage-6/results.md#49-register-totals) so a reviewer can
re-derive rather than believe it.

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
