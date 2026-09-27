# Development, review and integration workflow

Status: authoritative repository workflow for agents and maintainers. This document
governs Git branches, checkpoint pushes, independent review, native validation,
parallel worktrees and integration to `main`. Product delivery remains governed by
the product's separate authority model.

## Core invariants

1. `main` contains only independently accepted work. Never develop directly on
   `main`, and never push an unreviewed task or remediation to it.
2. Every implementation, remediation and material documentation task starts on a
   dedicated branch from the intended integration base. Checkpoint commits may be
   pushed to that branch before acceptance.
3. A reviewer accepts an exact commit or explicit commit range, not a branch name.
   Any material change after review invalidates that acceptance.
4. Every push is a normal non-force push. Force-pushing, deleting remote branches,
   rewriting shared history, publishing releases, opening/merging requests and
   changing repository settings require separate explicit user authority.
5. A Git push is repository synchronization, not Vigil product delivery authority.
   It cannot approve a plan/task, grant spending, enable production dispatch or
   authorize publication through Vigil.
6. Preserve user work. Never use reset, destructive checkout, clean, stash deletion
   or worktree removal against a checkout or worktree that the agent did not create
   and positively identify.

## Branches and checkpoint pushes

Use one branch for one coherent task. Preferred names are:

```text
task/<stage-or-area>-<short-purpose>
fix/<stage-or-area>-<finding>
docs/<short-purpose>
integration/<task-name>
```

Before editing:

1. Verify the primary checkout is clean or identify and preserve existing user work.
2. Fetch the configured origin, verify its URL and establish the exact base commit.
3. Create the task branch and, for parallel work, its dedicated worktree.
4. Record the branch, base SHA, worktree and owned scope in the agent handoff or
   coordination message.

Commit coherent checkpoints using the repository's message style. Run focused tests
while developing and the required checkpoint gates before describing a checkpoint
as working. Ordinary non-force pushes of checkpoint commits to the matching remote
task branch are standing-authorized. Such commits are visibly provisional: they do
not belong on `main`, do not establish acceptance and grant no runtime or delivery
authority. Never commit credentials, local caches, generated secrets or private
fixture contents merely because a task branch is not yet accepted.

## Validation and independent review

The implementation agent must record the exact commands actually run and distinguish
local, cross-build and native-platform evidence. Before independent review, the task
branch should contain all implementation, tests, documentation and required native
validation evidence for the candidate SHA.

An independent reviewer must:

- use a fresh agent/session that did not implement the candidate;
- review an exact SHA or range and report findings only, without modifying files;
- inspect the applicable requirements and security/authority invariants;
- run proportionate adversarial probes and the prescribed validation gates;
- state what was not rerun and whether the verdict is accepted, conditional or
  rejected.

Self-review, fixture approval and a passing test suite are useful evidence but are
not independent acceptance. Blocking findings are remediated on the same task branch,
then revalidated and submitted for a narrow independent follow-up. A changed candidate
must never inherit an earlier verdict silently.

### Review-evidence tail

The independent verdict normally arrives after the reviewed implementation commit.
One later evidence-only commit may faithfully record that verdict, update stage status
and refresh resumption documentation without causing recursive review. This exception
is limited to Markdown/status records and mechanical documentation metadata. It may
not change source, tests, schemas, migrations, build configuration, executable scripts,
policy rules or runtime configuration. Run `make docs-check`, `make check` when the
documentation gate is part of it, and `git diff --check` on that tail. Any material
change requires another independent review.

## Native macOS validation from a task branch

Use the configured remote rather than copying ad hoc source bundles when the task
branch is available:

1. Push the exact candidate to its remote task branch.
2. On the Mac, fetch that branch in the existing Vigil repository without changing
   its current checkout.
3. Create an agent-owned detached worktree in a unique temporary path at the exact
   candidate SHA. Never run validation in the user's normal working tree.
4. Confirm the SHA, OS/build, architecture and pinned Go version; run the stage's
   prescribed native gates and record exact results.
5. Remove only the agent-owned temporary worktree and verify the normal checkout is
   unchanged. Go's module cache may contain read-only files; permission changes for
   cleanup must remain strictly inside the identified temporary worktree.

Cross-compilation is not native runtime validation. If the Mac or remote is
unavailable, log the evidence gap and continue independent work rather than claiming
coverage. A native failure is a candidate failure, not permission to patch directly
on the Mac.

## Parallel agents and worktrees

Every write-capable agent working concurrently uses its own Git branch and its own
Git worktree. Never point two write-capable agents at the same worktree, and never let
a background agent edit the primary checkout. A read-only reviewer that needs to run
tests also receives a separate detached review worktree.

Create worktrees outside the repository tree, for example:

```text
../vigil-worktrees/<task-id>
/tmp/vigil-worktrees/<task-id>
```

Before delegating, give the agent its absolute worktree path, branch, base SHA, exact
scope and forbidden areas. Agents must inspect `git status` in their own worktree
before editing and before handoff. Do not create a nested worktree inside Vigil.

Worktree isolation does not make overlapping changes safe. Parallel tasks must not
independently own the same source files, migration number, CLI surface, stage-status
flip or authoritative decision document. When overlap cannot be avoided, serialize
the work or designate one owner and make the other task read-only. Each agent commits
only its own changes and reports its exact tip SHA; it must not stage or commit another
agent's files.

Only the creating agent or the designated integrator removes a worktree, and only
after its commits are pushed or otherwise durably retained. Never prune or delete an
unknown worktree to resolve a collision.

## Integration to `main`

The unit approved for `main` is the exact integration candidate:

- If `main` has not moved and the accepted task tip is a direct descendant, the
  accepted tip plus its permitted review-evidence tail is the candidate.
- If `main` moved, create `integration/<task-name>` from current `origin/main` and
  combine the accepted task there. Resolve no conflict casually: conflict resolution
  is a material change and requires affected validation plus narrow independent
  review of the integration delta.
- If multiple accepted tasks are combined, validate and independently review their
  interaction before updating `main`.

Before pushing `main`, verify all of the following:

- the configured remote and target branch are the intended repository and `main`;
- the candidate contains no unresolved or unaccepted blocking finding;
- required Linux, race, build, documentation, boundary, cross-build and native gates
  for the affected scope pass at the recorded commits;
- documentation and `docs/STATUS` describe the same accepted state;
- the worktree is clean and the push is a fast-forward normal push.

When these conditions hold, this workflow gives standing authorization for a normal
non-force push of the accepted integration candidate to `main`. If any condition is
ambiguous, stop before the main push and request direction. Never use force to make
the remote accept a candidate.

## Recommended server-side enforcement

Process rules should also be backed by repository settings when available. Protect
`main` against force pushes and deletion, require pull requests or an equivalent
reviewed integration path, require the Linux/documentation checks, dismiss approvals
when material commits are added and restrict direct main updates to the integrator.
Changing those settings is an external administrative action and requires explicit
user authorization.
