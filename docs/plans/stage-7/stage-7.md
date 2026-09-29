# Stage 7 — User-facing documentation for the terminal interface

<!-- vigil-tier: plan -->

Status: **scope defined, not planned and not started.** Established 2026-09-29
by explicit user scope revision, deliberately sequenced *after* Stage 6.

This document is a **scope** description: outcome, boundaries and deliverable.
Work checkpoints are [7.1's](#71-documentation-plan) job.

## Why this stage exists

Vigil is a terminal application with roughly sixty commands, an approval model,
a recovery model and a delivery model. That is a lot to learn by reading `--help`.

At the end of Stage 6 there will be a complete, authoritative feature list of the
terminal interface in `docs/research/stage-6/results.md`. Stage 7 turns that
into documentation **a new user can actually follow** — which is the whole point.
This stage was originally proposed as "5.9" and was deferred here for two
reasons: the interface would still have moved after a human used it, and writing
user documentation before the thing is human-validated is rework.

**A documentation website, not a folder of Markdown files.** The deliverable is a
published, navigable site a user can read without a checkout.

## Outcome

At the end of Stage 7:

1. **A documentation website exists**, buildable and servable from this
   repository, presenting Vigil for a first-time user.
2. It is **complete against the Stage 6 feature list**: every screen, every
   command group, every human decision point, every key binding. Coverage is
   reconciled programmatically against the feature list, not asserted.
3. It includes, at minimum: an installation and first-run path; a guided
   walkthrough of a real workflow; what Vigil will and will not do on its own;
   the approval and permission model explained for a human; recovery and
   checkpoint behaviour; delivery and what "draft" means; and a reference
   section generated or checked against the CLI.
4. **Documentation content is versioned separately from the generator.** The
   prose lives as plain files in a documented structure; the site build is a
   thin, replaceable layer. A generator change must not be able to lose content.
5. The build is reproducible offline and adds no credentialed or network
   requirement to `make check`.

## Boundaries

- **In scope:** authoring the content, the information architecture, the
  navigation, the site build, and whatever the site needs to stay in sync with
  the product.
- **In scope:** choosing the generator. This is an architectural decision with
  real consequences — new dependencies, a new build target, and an ongoing
  maintenance surface — and it is made **here, in 7.1**, not before. The
  standing project rules require a pinned toolchain and an offline,
  credential-free `make check`; a generator that breaks either is disqualified.
  Whichever generator is chosen, adding it is a declared dependency change under
  the repository's dependency policy, and it must not quietly pull a toolchain
  that needs network access to build.
- **In scope:** a coverage check that fails when a feature in the Stage 6 list
  has no corresponding page, so the site cannot silently drift.
- **Out of scope:** changing product behaviour to make it easier to document. If
  documentation reveals a real defect, it is reported and routed to the owning
  slice, not patched here.
- **Out of scope:** screenshots and media assets beyond what a terminal
  application can honestly produce. This is a terminal product; a gallery of
  fabricated screenshots would be worse than none.
- **Out of scope:** localizing beyond English.

## Sub-stages

| Sub-stage | Purpose | Autonomous |
| --- | --- | --- |
| [7.1](#71-documentation-plan) | Information architecture, generator decision, per-sub-stage plans | yes |
| 7.2 … | Content, reference generation, site implementation, coverage gate | yes |

### 7.1 Documentation plan

7.1 decides and plans; it does not write the manual.

Its deliverables:

1. **Information architecture**: the section structure, the navigation model, and
   the two reading paths — a first-time guided path and a reference path — with
   the content of each section assigned.
2. **The generator decision**: a short comparison of the realistic options
   against this repository's actual constraints (pinned toolchain, offline
   build, no credentialed `make check`, Go-only core, existing documentation
   conventions), a recommendation, and the consequence of the choice recorded in
   `docs/process/session-audit.md`.
3. **The content/presentational split**: the exact on-disk structure of the
   prose, and the contract between that structure and whatever consumes it, so
   the generator stays replaceable.
4. **The sub-stage decomposition** 7.2 … 7.y, with outcomes, dependencies,
   validation and completion criteria, each with a plan to the Stage 5 slice
   standard.
5. **The coverage-check design**: how the site proves it covers the Stage 6
   feature list, and where that check runs.

### 7.1 must not** write the user-facing content itself; that is 7.2+.

## Dependencies

- **Stage 6 complete**, including the final feature list. Stage 7 must not run
  against an interface still in flux; a human has validated that interface
  first.
- The CLI reference (`docs/plans/stage-5/stage-5-cli.md`) remains authoritative
  for command syntax. Stage 7 documents it for users; it does not replace it.

## What Stage 7 completion is not

Published documentation is not evidence the product works. It can be complete,
correct and current while the product still has never been used by a human —
which is the case at the time of writing. Stage 7 inherits Stage 6's boundary.

## Handoff

An interrupted 7.x records the last completed sub-stage, the content inventory
written so far, the coverage status, and the next safe action.

Fresh-context prompt: "Execute Vigil Stage 7 using this document and the Stage 6
feature list. Start at 7.1 unless a later sub-stage plan is named. Content is
authored in plain files; the generator is replaceable."
