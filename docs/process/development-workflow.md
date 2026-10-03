# Development, review and integration workflow

<!-- vigil-tier: process -->

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
4. Every push is a normal non-force push. Force-pushing, rewriting shared history,
   publishing releases, opening/merging requests and changing repository settings
   require separate explicit user authority. A fully integrated task branch may be
   deleted only under the verified cleanup rule below.
5. A Git push is repository synchronization, not Vigil product delivery authority.
   It cannot approve a plan/task, grant spending, enable production dispatch or
   authorize publication through Vigil.
6. Preserve user work. Never use reset, destructive checkout, clean, stash deletion
   or worktree removal against a checkout or worktree that the agent did not create
   and positively identify.

## Cost-controlled tools and network access

The objective is to prevent surprise charges and unsafe external effects, not to
block ordinary development. The following are standing-authorized when relevant to
the assigned task:

- the prepared loopback llama service;
- Codex through the verified ChatGPT subscription while usage is included and no
  extra-credit charge or automatic paid fallback is enabled;
- OpenCode while the selected provider/model is visibly free or included;
- read-only web searches and retrieval of technical documentation;
- downloads declared by the repository's dependency manifests and lock files from
  their normal registries;
- normal fetch/push operations allowed by this workflow against the verified Git
  origin; and
- local Docker or OrbStack for development, tests and disposable fixtures.

If a model/provider route is ambiguous, metered, or changes from the verified
free/included route, stop before the call. Never infer spending authority from an
API key, environment variable, installed client or prior login. Without a new
explicit user authorization, do not use paid APIs or paid fallback, buy credits,
change subscriptions, provision hosted services, deploy workloads, create billable
cloud resources or accept a trial that can become chargeable.

Credentials may be used only by their intended client and endpoint. Never print,
copy, persist, document or send credential values elsewhere. Read-only internet
research does not authorize uploading repository content, prompts containing private
source, telemetry dumps or user data to arbitrary services. Mutating third-party
actions—issues, comments, messages, releases, package publication and service
configuration—remain separately authorized except for the Git branch operations
explicitly allowed here.

Docker use must remain local and scoped. Prefer pinned images and loopback-bound
ports. Do not use privileged containers, host networking, the Docker socket, broad
home/root mounts or public port exposure unless the user explicitly approves that
exact need. Name or label agent-created containers, networks and volumes; inspect
targets before cleanup and never remove resources the agent does not own. Docker may
support optional validation, but the default test suite must continue to pass without
Docker, network access, models or credentials.

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

### Integrated branch cleanup

After the accepted candidate is pushed to `main`, the integrator may remove its
agent-owned worktree and delete the corresponding local and remote task/integration
branches without another approval only when all of these checks pass:

- `origin/main` has been refreshed and either the exact branch tip is its ancestor,
  or a recorded squash/rebase integration maps the source base/range/tip to an
  independently accepted commit reachable from `origin/main` and confirms that no
  source change was omitted;
- the branch has no unintegrated change, open finding, active worktree or unresolved
  handoff;
- review/evidence records retain the accepted commit or range; and
- the branch is not protected, user-owned, shared by another task or otherwise
  marked for retention.

Use an ordinary branch deletion, never a force push or history rewrite. A squash or
rebase may make the original commits unreachable from `origin/main`, so its durable
integration record must retain the source SHA/range, accepted destination SHA and
content-equivalence evidence before the ref is removed. If mapping, completeness or
ownership is unclear, retain the branch and ask.

## Agent scratch space and disk discipline

Agents need somewhere to work: a tree to test in, a build cache, a fixture. This is
normal and not restricted — the rule is only **where** that space lives, and that you
**give it back**. An unbounded scratch habit is the one way a careful agent can still
fill a maintainer's disk.

**Put every byte you create under one scratch root you can delete in a single
command.** For example:

```text
/tmp/vigil-scratch/<task-id>/          # your own scratch
/tmp/vigil-scratch/shared-go-cache/     # optionally shared build/module cache
```

Never scatter scratch across `/tmp`, your home directory or the repository tree.
If everything you made is under one root, cleanup is `rm -rf` on that root, and you
never have to decide file by file.

### Prefer a worktree over a clone

`git worktree add` shares the repository's object store; `git clone` does not. The
measured difference on this repository is roughly **20×**: the review clones left in
`/tmp` by Stage 6.3 were ~7.6 GB each, and the review worktrees were ~370 MB. A
dozen clones filled a maintainer's disk. Reach for a clone only when a genuinely
independent object store is required, and then reuse it by resetting rather than
recreating.

When a reviewer must mutate code to prove a test bites, give it a detached worktree
created once and reused across findings — not a fresh clone per round. Say so
explicitly when dispatching a reviewer; a reviewer left to improvise will clone.

**A fresh worktree has no `.tools/`.** That directory is gitignored, so it exists
only in the checkout that installed it, and `make` then falls back to whatever `go`
is on `PATH`. That may be far too old to parse `go.mod` at all, which looks like a
broken change rather than a missing toolchain. In a worktree, invoke the pinned
toolchain by absolute path:

```sh
env -u GOROOT /path/to/vigil/.tools/go/bin/go test ./... -count=1
```

Or symlink the primary checkout's `.tools` into the worktree if you want `make` to
work unchanged.

### Keep build caches inside the scratch root

Go writes to `GOCACHE` and `GOMODCACHE`. Point both into your scratch root rather
than letting them land in a shared default that no single `rm -rf` will reclaim:

```sh
GOCACHE=/tmp/vigil-scratch/<task-id>/gocache \
GOMODCACHE=/tmp/vigil-scratch/<task-id>/gomodcache \
  make check
```

A module cache may be shared read-only between tasks that need identical versions;
a build cache may not be shared writable. When in doubt, give each task its own and
delete them together.

### Two deletion traps

- **Go module cache files are mode `0444`.** A plain `rm -rf` fails on them and
  leaves the directory silently in place, which reads as success. This was observed,
  not anticipated: the first deletion attempt on the 6.3 scratch reported success and
  removed nothing. `chmod -R u+w` the scratch root first.
- **A registered worktree is not an ordinary directory.** `git worktree list` shows
  them. Remove one with `git worktree remove <path>` followed by `git worktree
  prune`; never `rm -rf` it, which leaves stale metadata in `.git/worktrees`.

### Clear your scratch when you no longer need it

Do this as soon as the work is done — before handoff, and before starting unrelated
work. Do not wait for a clean session to tidy up, and do not assume a later session
will.

```sh
git worktree remove --force /tmp/vigil-scratch/<task-id>/wt   # if you made one
git worktree prune
chmod -R u+w /tmp/vigil-scratch/<task-id> && rm -rf /tmp/vigil-scratch/<task-id>
```

Check before you finish, and say what you removed:

```sh
du -sh /tmp/vigil-scratch 2>/dev/null
```

### What not to delete

Delete only what you created and can positively identify. Leave user files, other
agents' worktrees, and anything you did not create — a reviewer is not permitted to
clean up another agent's scratch, and this repository's rule against disturbing work
it did not create applies to `/tmp` as much as to a checkout. If cleanup requires
removing something of ambiguous ownership, leave it and say so.

## Integration to `main`

The **designated integrator** is the single agent/session assigned to construct,
validate and publish the current integration candidate. Implementers and parallel
agents do not become integrators implicitly, and only one integrator may own a given
`main` update.

The unit approved for `main` is the exact integration candidate:

- If `main` has not moved and the accepted task tip is a direct descendant, the
  accepted tip plus its permitted review-evidence tail is the candidate.
- If `main` moved, create `integration/<task-name>` from current `origin/main` and
  combine the accepted task there. Resolve no conflict casually: conflict resolution
  is a material change and requires affected validation plus narrow independent
  review of the integration delta.
- Merge commits are the simplest way to retain exact task ancestry. Squash or rebase
  integration is permitted only on a new integration branch, never by rewriting a
  published task branch. Record the source base/range/tip and resulting candidate;
  independently review the resulting integration commit because its SHA and history
  differ from the task candidate. Before deleting the source branch, retain evidence
  that every accepted source change is represented in the destination.
- If multiple accepted tasks are combined, validate and independently review their
  interaction before updating `main`.

Before pushing `main`, verify all of the following:

- the configured remote and target branch are the intended repository and `main`;
- the candidate contains no unresolved or unaccepted blocking finding;
- required Linux, race, build, documentation, boundary, cross-build and native gates
  for the affected scope pass at the recorded commits;
- documentation and `docs/STATUS` describe the same accepted state;
- the worktree is clean and the push is a fast-forward normal push.

When these conditions hold, the candidate is ready to propose. Meeting them makes
the candidate acceptable, not pre-authorized: this workflow grants no standing
authorization to commit or push to `main`. A normal non-force push of the accepted
integration candidate to `main` requires explicit user authorization for that
operation, which the user may give for a session or a named candidate. Checkpoint
pushes to a task branch are separately standing-authorized; `main` is not. If any
condition is ambiguous, stop before the main push and request direction. Never use
force to make the remote accept a candidate.

## Explicit user exceptions

The user may override a rule in this document for a specific operation. A valid
override must clearly identify the operation that conflicts with policy and its
target/scope; a general request to continue, be autonomous or finish quickly is not
enough. Confirm the narrow interpretation in the work record, preserve unaffected
rules and do not treat a one-time exception as a permanent policy amendment. Higher-
priority platform and system safety constraints still apply.

This includes a clear instruction to work, commit or push directly on `main`: the
user may authorize that exact exception even though the default workflow requires a
task branch and independent acceptance. Such an authorization is the only way the
agent commits or pushes to `main`, it is bound to the named operation and scope, and
it never becomes a standing default for later work. Record it before acting; do not
infer permission to force-push, publish, merge a request or perform product delivery.

Server-side branch protection is deliberately not configured yet. Do not assume
`main` is protected against force-push or deletion, and do not add repository-setting
recommendations to this document in anticipation of a later change; the process rules
above are the only enforcement until the user configures protection deliberately.
