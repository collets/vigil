# AGENTS.md

Instructions for any agent working in this repository. Read this before changing code.

Vigil is a local control panel that runs existing agent harnesses (Codex, Hermes)
through a Go core with SQLite state. It is **pre-release**: the offline core through
Stage 5.5 is implemented and independently accepted, and production dispatch is
deliberately disabled. Do not enable it, and do not start Stage 5.6 without an
explicit instruction.

## Start here

1. `docs/next-steps.md` — current state, what is pending, and the next action.
2. `docs/README.md` — the document index: what each document is authoritative for.
3. `docs/STATUS` — machine-readable current state, read by the documentation check.
4. `docs/development-workflow.md` — mandatory branch, worktree, review, native
   validation and integration rules.
5. `make check` — baseline. Must pass before you report anything as working.

## Non-negotiable project rules

- **Avoid unplanned charges; do not ban useful local tools.** Standing-authorized
  development use includes the local llama route, verified included Codex/ChatGPT
  subscription usage, OpenCode while its selected route is free/included, read-only
  technical research, declared dependency downloads, workflow-compliant Git
  synchronization and contained local Docker/OrbStack. Follow the cost, credential,
  egress and container boundaries in
  [`docs/development-workflow.md`](docs/development-workflow.md). Never use a metered
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
- **Follow [`docs/development-workflow.md`](docs/development-workflow.md).** Develop
  on task branches, push checkpoints only to their matching branches, use separate
  worktrees for parallel agents, and push `main` only after exact-candidate validation
  and independent acceptance. Never force-push. Git synchronization never implies
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
| CLI command, flag, arg count, or output | `docs/stage-5-cli.md` |
| New/removed/renamed package | `README.md` structure block, `docs/README.md` index |
| New migration, or schema version change | the stage document, `docs/research/<stage>/results.md`, `docs/STATUS` |
| New/renamed document | `docs/README.md` index (no orphans allowed) |
| Requirement added, removed, or renumbered | `docs/stage-5-plan.md`, `docs/requirements.md` |
| Stage status flip (pending → accepted, finding opened/closed) | `docs/STATUS`, plus **every** document in `status_documents`, plus `docs/next-steps.md` status line and "Next concrete action" |
| A new finding from review | the review record, `docs/next-steps.md`, `docs/pending-decisions.md` |
| A design decision or accepted alternative | `docs/session-audit.md` decision index |
| Autonomy, tooling, network, Git or authority policy | `docs/development-workflow.md`, `docs/pending-decisions.md`, `docs/session-audit.md` |
| Validation evidence or a gate result | the stage's `docs/research/<stage>/results.md` validation section |

### The stage-status trap

The most common failure in this repository is a status flip applied to *some*
documents. When a stage is accepted, or findings are closed, the new state must
appear in **every** file listed in `status_documents` in `docs/STATUS`, and the
"Next concrete action" line in `docs/next-steps.md` must name the range you just
reviewed — not a superseded one. A stale "next action" range has already occurred
here twice; do not repeat it.

### Verify before you report

```sh
make docs-check   # documentation consistency gate
make check        # vet + full test suite
```

`make docs-check` fails on broken relative links, orphan documents, undocumented
CLI commands, package lists that do not match `internal/`, commit SHAs that do not
exist, migration-count drift, and status documents that disagree with `docs/STATUS`.
If you changed the CLI, migrations, packages, or a stage status, this gate is part
of your definition of done.

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
