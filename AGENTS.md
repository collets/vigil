# AGENTS.md

<!-- vigil-status: stage=6.1; stage_accepted=true; implementation_commit=bad613950b11de53130487fdb24e0a10d40d1480 -->

Instructions for any agent working in this repository. Read this before changing code.

Vigil is a local control panel that runs existing agent harnesses (Codex, Hermes)
through a Go core with SQLite state. It is **pre-release**: the offline core through
Stage 5.7 is implemented and independently accepted at implementation commit
`522cb96`, and **Stage 6.1's parity gap analysis is independently accepted at
`bad6139`** for its autonomous scope. It was **rejected** by its first independent
review and returned **conditional** by each of thirteen further; all fourteen
rounds of findings are remediated. Acceptance rests on the measurements, which
fourteen independent reviewers re-derived from source without finding one wrong,
and not on a clean final round — the bookkeeping findings of rounds five to
fourteen are recorded in its review file and were judged process noise.
Production dispatch remains deliberately disabled.

**Each stage's acceptance covers the autonomous part only.** Stage 5.7's walkthrough
found that the Stage 5.2/5.6 delivery path was unreachable and fixed it under
explicit authorization; delivery is now reached end to end. No human has exercised
any delivery path against a real remote or a real hosting provider, and no such
evidence exists. Human-gated operations are Stage 8's. Do not enable production
dispatch.

**Do not start a stage without an explicit instruction naming it.** The stage
order is now Stage 6 (terminal interface
parity) → Stage 7 (documentation website) → Stage 8 (human review), and
`docs/START-HERE.md` routes to each. Stages 6–8 were added by explicit user
scope revision on 2026-09-29; the original roadmap defined Stages 1–5 only. Two
constraints are easy to get wrong:

- **Stage 6.1 was a gap analysis, not implementation, and is now complete.** It
  wrote no interface code. Its measured result is that the terminal interface
  fully expresses **4** of the 82 distinct capabilities an 85-row register
  classifies, and **6** of 25
  human decision classes, and that R11/R45/R46/R47/R70 were all unmeasurable as
  written — which is why that gap survived two accepted stages unnoticed. The
  sub-stage plans for 6.2–6.9 are in
  [`docs/plans/stage-6/`](docs/plans/stage-6/); the parity register and the
  feature list are in
  [`docs/research/stage-6/results.md`](docs/research/stage-6/results.md). Do not
  re-derive the analysis, and do not start a sub-stage without an instruction
  naming it.
- **Stage 8 cannot be completed by an agent.** It is the human stage, and
  re-running it is not a way to recover from a failure.

## Start here

1. [`docs/START-HERE.md`](docs/START-HERE.md) — the reading order: which documents
   you must read for this task, and which you can skip.
2. `docs/process/next-steps.md` — current state, what is pending, and the next action.
3. `docs/README.md` — the document index: what each document is authoritative for.
4. `docs/STATUS` — machine-readable current state, read by the documentation check.
5. `docs/process/development-workflow.md` — mandatory branch, worktree, review,
   native validation and integration rules.
6. `make check` — baseline. Must pass before you report anything as working.

## Documentation layout and tiers

Documentation is grouped by **when you need it**, so a new session can read a
short, sufficient set instead of the whole tree. The folder a document lives in
*is* its tier, and every Markdown document under `docs/` carries a
`<!-- vigil-tier: ... -->` marker asserting the same thing; `make docs-check`
fails when a document has no tier, has the wrong tier for its folder, or sits in
neither a tier directory nor the small set of documents allowed directly in
`docs/`. A marker quoted inside a code block or inline code is documentation
*about* the convention, not a declaration, and is ignored.

A tier is a reading path, not a fence: it says what a session needs and what it
can usually skip, and reading outside your tier is always fine — often useful,
when checking how a related or earlier piece of work was handled. The gate
enforces that each document is *classified*, not that a session only reads its
own tier.

| Tier | Folder | Typically needed for |
| --- | --- | --- |
| Entry | `docs/START-HERE.md` | Every session: reading order and task-to-document routing |
| Index | `docs/README.md`; `docs/STATUS` (not Markdown, so exempt from the marker) | What each document is authoritative for; machine-readable state |
| Core | `docs/core/` | Every session that changes code: requirements, architecture, core specification, technology, harness capabilities, milestone, checkpoints, finalization |
| Process | `docs/process/` | Every session that changes something or resumes work: development workflow, next steps, pending decisions, decision provenance |
| Plan | `docs/plans/` | The slice being worked on; `docs/plans/stage-5/README.md` routes to it, and the accepted slices answer "why is this safeguard here" |
| Evidence | `docs/research/` | Supporting a claim about what was observed, or satisfying a gate |
| History | `docs/history/` | The reasoning behind a decision, or a superseded alternative |
| Record | `docs/spec/` | The draft schemas the installed migrations superseded; not the live schema |

When you add a document, choose its tier first, then write the matching marker.
A new tier directory needs an entry in `tierByDirectory`, and a new document
placed directly in `docs/` needs an entry in `tierByRootDocument`, both in
`internal/doccheck/doccheck_test.go`.

## Non-negotiable project rules

- **Avoid unplanned charges; do not ban useful local tools.** Standing-authorized
  development use includes the local llama route, verified included Codex/ChatGPT
  subscription usage, OpenCode while its selected route is free/included, read-only
  technical research, declared dependency downloads, workflow-compliant Git
  synchronization and contained local Docker/OrbStack. Follow the cost, credential,
  egress and container boundaries in
  [`docs/development-workflow.md`](docs/process/development-workflow.md). Never use a metered
  API or paid fallback, purchase credits, change a subscription, provision hosting
  or create a chargeable cloud resource without a new explicit user authorization.
  The default test suite must remain offline, credential-free and Docker-free.
- The existing local llama route takes `OPENAI_API_KEY` from the inherited
  environment and requires `OPENAI_BASE_URL` to name the prepared loopback
  endpoint. Check presence and route identity only: never print, copy, persist or
  document the key value. Environment credential presence never authorizes a
  metered endpoint or fallback.
- **Never enable production dispatch.** `qualified_runtime` check dispatch and
  production model/reviewer drivers must fail closed.
- **Acceptance creates no delivery authority.** Nothing in the quality or
  acceptance path may commit, push, open a draft request, publish, or merge.
- **Fixture actors are labelled** (`fixture`, `fixture_human`, `fixture_core`) and
  are only accepted for repositories carrying the disposable-fixture marker. They
  are never real user acceptance.
- **Preserve user work.** Never reset the checkout, delete uncommitted changes, or
  run destructive recovery outside agent-owned disposable fixtures.
- **Historical migrations are immutable.** `internal/store/migrations/project-001..009`
  and their recorded digests must never change. Schema changes are forward-only.
- **Follow [`docs/development-workflow.md`](docs/process/development-workflow.md).** Develop
  on task branches, push checkpoints only to their matching branches, use separate
  worktrees for parallel agents, and propose `main` only after exact-candidate
  validation and independent acceptance. Never force-push. There is no standing
  authorization to commit or push to `main`; that requires explicit user
  authorization for the named operation or session. Git synchronization never implies
  release, request, merge or Vigil delivery authority.
- **The user may explicitly authorize an exception** to a repository policy for a
  concrete operation. The authorization must clearly identify the conflicting
  operation and its target/scope; vague encouragement is not an override. Record
  the exception and rationale, do not broaden it, and continue to obey platform and
  system safety constraints.
- Use the pinned toolchain: `make` prefers `.tools/go/bin/go` (Go 1.27.1) and
  clears `GOROOT`. If you invoke `go` directly, use `env -u GOROOT .tools/go/bin/go`.

## Documentation obligations — required, not optional

**Any change to the code, schema, CLI, plans, specs, gates, status, or
authoritative decisions must update the affected documentation in the same change.
A change is not finished until its documentation is updated.**

Documentation here is the project's durable memory. A correct implementation with a
stale document is a defect, because the next session will plan from the document.

### What to update, by change type

| Change | Must update |
| --- | --- |
| CLI command, flag, arg count, or output | `docs/plans/stage-5/stage-5-cli.md` |
| New/removed/renamed package | `README.md` structure block, `docs/README.md` index |
| New migration, or schema version change | the stage document, `docs/research/<stage>/results.md`, `docs/STATUS` |
| New/renamed/moved document | `docs/README.md` index (no orphans allowed) and the `<!-- vigil-tier -->` marker matching its folder |
| Requirement added, removed, or renumbered | `docs/plans/stage-5/stage-5-plan.md`, `docs/core/requirements.md` |
| Stage status flip (pending → accepted, finding opened/closed) | `docs/STATUS`, the canonical `vigil-status` marker and prose in **every** `status_documents` file, plus `docs/process/next-steps.md` status line and "Next concrete action" |
| A new finding from review | the review record, `docs/process/next-steps.md`, `docs/process/pending-decisions.md` |
| A design decision or accepted alternative | `docs/process/session-audit.md` decision index |
| Autonomy, tooling, network, Git or authority policy | `docs/process/development-workflow.md`, `docs/process/pending-decisions.md`, `docs/process/session-audit.md` |
| Validation evidence or a gate result | the stage's `docs/research/<stage>/results.md` validation section |
| A document superseded by a newer decision | move it to `docs/history/`, add the superseded banner, and record the replacement in `docs/history/README.md` |

### The stage-status trap

The most common failure in this repository is a status flip applied to *some*
documents. When a stage is accepted, or findings are closed, the new state must
appear in **every** file listed in `status_documents` in `docs/STATUS`, and the
"Next concrete action" line in `docs/process/next-steps.md` must name the range
you just reviewed — not a superseded one. Each listed document must contain
exactly one canonical `vigil-status` tuple matching `stage`, `stage_accepted` and
`implementation_commit`; `make docs-check` enforces that tuple, not the semantics
of arbitrary prose, which still require review. Only registered live-status documents
are checked; do not add a `vigil-status` marker to historical or unregistered records.
A stale "next action" range has already occurred here twice; do not repeat it.

### The unclassified-document trap

The second most common failure is adding a document that no session ever reads.
A new document must, in the same change: live in the tier folder matching when it
is needed, carry that tier's `<!-- vigil-tier -->` marker, and be listed in
`docs/README.md`. `make docs-check` fails on an orphan or an unclassified
document, but it cannot tell you the tier is *sensible* — a plan filed as core
documentation will be read by every session forever, and core documentation filed
as a plan will be skipped when it is needed. Choose deliberately.

### Verify before you report

```sh
make docs-check   # documentation consistency gate
make check        # vet + full test suite
```

`make docs-check` fails on broken relative links, orphan documents, documents with a
missing or mismatched `vigil-tier` marker, undocumented CLI commands, package
lists that do not match `internal/`, commit SHAs that do not exist,
migration-count drift, and status documents that disagree with `docs/STATUS`.
If you changed the CLI, migrations, packages, documentation layout, or a stage
status, this gate is part of your definition of done.

## Before you report a change as working

State plainly which of these you actually ran, and which you did not. Do not report
a passing submitted suite as verification of a change you did not re-run. If native
macOS validation matters for your change, say that it is unverified rather than
implying cross-build coverage is equivalent.

## Reviewing your own work

`make check` and `make docs-check` are necessary, not sufficient. For changes to
process containment, recovery, budgets, migrations, or acceptance authority, spawn a
read-only reviewer subagent with explicit instructions naming the files, the
invariants that must hold, and "report findings only, do not modify files". Prefer a
weaker model for this: the task is careful reading and adversarial probing, not
deep design. Do not treat that review as self-acceptance of your own change.
