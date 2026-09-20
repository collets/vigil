# Checkpoint proposal

Status: save/restore behavior accepted, implementation mechanism still proposed, 2026-09-20. Automatic saving and clearing of agent-owned changes is allowed while preserving pre-existing user work; bringing saved changes back requires approval. No Git mutations are implemented or authorized for this design step.

Stage 4 follow-up: [core-spec.md](core-spec.md) resolves the mechanism/default questions below, and [Stage 5](stage-5-plan.md) defines implementation checks. Earlier proposed alternatives are retained for provenance; the core specification takes precedence for implementation. No production workflow or user configuration is installed by the specification.

## Product behavior

Call this a checkpoint to distinguish it from Git's staging area. Associate every checkpoint with project, plan, task, attempt, repository, timestamp, and the base revision. The dashboard should expose saved attempts and recovery options.

Recommend identifiable application-managed stash/checkpoint records rather than failed-attempt commits in the plan branch. Separate local Git refs are one implementation option, not a settled requirement. Normal delivery must push only the intended plan branch and must not publish checkpoint refs. An internal checkpoint is not an accepted task or a deliverable commit.

Saving and clearing are distinct operations:

1. Stop writers and record the current repository state.
2. Persist and verify a recoverable checkpoint covering the intended changes.
3. Clear only the saved agent-owned changes to return affected paths to the pre-task baseline, preserving user work; stop if ownership or completeness is uncertain.
4. Mark the task blocked and reconsider eligible independent tasks.

A saved commit alone does not clear the checkout. Bringing a checkpoint back requires approval and may conflict with work completed since it was created; retain both states and handle conflicts explicitly.

## Preservation boundaries

- Preserve the distinction between pre-existing user edits and changes made during the attempt. Do not blindly restore the entire checkout to HEAD.
- Track staged/unstaged state and relevant untracked files; a single ordinary commit does not preserve the complete index/working-tree distinction.
- Leave ignored environment/configuration files outside automatic Git snapshots and cleanup by default. If an agent must modify an excluded file, define a separate preservation strategy or stop before replacement.
- Handle each nested repository independently, linked by a checkpoint-set record. Do not clear any participating checkout until all required snapshots are verified. Cross-repository restore is not one atomic Git operation; persist progress for crash recovery.
- Preserve existing user work and pause when ownership or concurrent edits are ambiguous. Snapshot existence alone does not resolve which edits may be removed from the active checkout.
- Keep checkpoint retention explicit. Never silently prune the only recovery copy of an unfinished attempt.

## Options and evidence

Git can create a commit object without moving the current branch, and refs can retain a reference to the object. This supports the proposed separate checkpoint history, but exact capture/restore logic needs a tested implementation design.

- [git-commit-tree](https://git-scm.com/docs/git-commit-tree)
- [git-update-ref](https://git-scm.com/docs/git-update-ref)
- [git-stash](https://git-scm.com/docs/git-stash) provides existing worktree/index capture concepts and highlights differences in tracked, untracked, and ignored-file handling.

Do not assume the user's stash stack is application-owned. Choice between custom checkpoint objects and stash-shaped snapshots remains open.

## Accepted decisions and remaining questions

- Local recovery checkpoints may be saved and agent-owned changes cleared automatically while preserving user work. This does not grant deliverable-commit permission.
- Bringing saved changes back requires approval. Whether task/plan-scoped advance grants can authorize later restorations remains open. Autonomous mode alone does not bypass the gate.
- Deliverable commits are per task, with multiple commits allowed for clarity, under the configured commit policy. Task acceptance remains separate from the existence of commits.
- How long must checkpoints remain available, and how is explicit cleanup presented?
