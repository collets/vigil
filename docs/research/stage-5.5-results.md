# Stage 5.5 implementation results

Status: offline implementation through checkpoint A commit `4dbb444` plus the B–D working implementation; Stage 5.5 is unaccepted.

This record is append-only evidence for the four checkpoints in
[the Stage 5.5 plan](../stage-5/5.5-workflow-and-planning.md). It distinguishes
offline fixture mechanics from real human approval and live model/runtime evidence.
The complete user-only gate list is maintained in the
[Stage 5.5 blocker log](stage-5.5-blockers.md).

## Baseline

On Linux/WSL at `6e8e986`, before edits on 2026-09-26:

```text
make check       PASS
make docs-check  PASS
```

The checkout was `main`, clean, 69 commits ahead of `origin/main`. Production
dispatch was disabled and no model, paid, Docker, hosting or network call was made.

## Checkpoint evidence

Passing fixture paths prove application mechanics only; they are never real spec,
criteria, task or plan acceptance.

### A — sequential scheduler and typed control API

Implemented from baseline `6e8e986`:

- forward-only project migration 015, including the permanently disabled
  automatic-advance control and persisted selection/consumption records;
- receipt-backed `queue` and `advance` handlers plus the bounded `queue-list`
  read view, all sharing expected project revisions with CLI and future TUI use;
- stable plan/task rank ordering, dependency/policy/profile/budget checks, one
  active plan and one selected/active execution context, and preservation of
  blocked reasons;
- fail-closed scheduling while blocked work still has uncontained writers, and
  while pause/recovery/quarantine prevents implementation dispatch;
- restart-safe truth: no open/start path automatically continues or advances.

Focused Linux tests cover multiple equal-rank plans, stable selection, pause,
receipt replay, safe progression around independent blocked work, selection
consumption, populated v14 upgrade, injected migration rollback, modified digest
rejection and newer-schema rejection. Shared endpoint and overlapping-root
serialization remain the coordinator authority and are rechecked during resource
acquisition rather than duplicated in the scheduler.

Checkpoint A validation on Linux/WSL with the pinned Go 1.27.1 toolchain:

```text
go test ./internal/store ./internal/core ./internal/supervisor
        ./internal/checkpoint ./internal/quality ./internal/cli  PASS
make check                                               PASS
make check-race                                          PASS
make build                                               PASS
make docs-check                                          PASS
git diff --check                                         PASS
```

The first sandboxed `make check` attempt could not create the existing
loopback-only `httptest` listener in `internal/boundary`; the same offline suite
passed with loopback permission. No external network, model, Docker or hosting
call occurred. Native macOS validation was not run (B4).

## Tool-role matrix

| Role | Read tools | Mutation/proposal tools | Explicitly unavailable |
| --- | --- | --- | --- |
| implementation | project, plan, task, artifact | `task.report_blocked` observation | reorder, proposal apply, acceptance, permission, spending, delivery |
| review | project, plan, task, artifact | none | writes, reorder, acceptance, delivery |
| supervisor | project, plan, task, artifact | reorder and change proposal | proposal apply, criteria authority, acceptance, spending, delivery |
| planning | project, plan, task, artifact | change proposal | reorder, proposal apply, acceptance, spending, delivery |
| finalization | project, plan, task, artifact | none | task mutation, acceptance, delivery |

The server injects project, role, run, native session and generation. The 64 KiB
input, 100-item page, opaque cursor, 32 KiB excerpt and 50-task proposal caps are
shared by direct calls and `internal/mcp`. Fixture tests reject duplicate/unknown
fields, foreign IDs, arbitrary paths, stale revisions, self-approval and a
role-forbidden tool. Run-bound sessions revalidate the active persisted generation
and native-session identity on every call, so a terminal generation immediately
loses tool authority. `report_blocked` retains task state and creates an inbox item.

## PTY and UI evidence

The Bubble Tea model keeps refreshes and mutations outside the input loop. Tests
cover a blocked slow mutation with immediate quit, a large control-sequence-bearing
event stream, refresh failure with retained stale truth, failed mutation feedback,
stale displayed revision rejection, and database reopen showing persisted truth.
On Linux, a real disposable `/dev/ptmx` pair also drives `RunProject` and proves a
quit key remains actionable through the terminal path.
Permission decisions are enabled only in Inbox, bind the visibly focused request,
and show the operation resource/arguments digests, policy revision and once scope.
The five views expose budgets, check artifact references, blocking findings versus
suggestions, baseline health, manual outcomes and recovery quarantine. Active-run
stop/recovery remains delegated to existing owner-aware commands.

## Markdown and proposal evidence

Offline fixtures import hostile Markdown containing terminal controls and text
requesting grant, scope, self-acceptance and tool changes. Bytes remain private
data. Outside/symlink/non-Markdown and oversized sources fail. Closed proposals
reject incomplete tasks, cycles, escaping scopes, oversize affected sets,
self-approval, a stale explicitly selected planning-profile revision and stale
application without partial plan mutation. Missing facts
create clarification requests. Exact human application reuses `plan.put` atomically
and creates no delivery record. The fixture cycle queues and continues the applied
plan, pauses it, closes and reopens the database, verifies persisted paused truth,
then explicitly continues and selects the proposed task.

## Read-only implementation review

The required adversarial reviewer inspected scheduler controls, planning,
model-tool authority and the TUI without modifying files. Its five findings were
disposed as follows:

- the reported multi-active-plan risk is already prevented by the immutable
  `one_active_plan` partial unique index, which includes paused and active states;
- the missing live plan-services budget gate is retained as the documented B2
  limitation and checkpoint C remains partial;
- proposal creation now requires and fences an explicit current profile revision;
- run-bound tool authority is revalidated against active run/generation/native
  session identity on open and every call;
- permission decisions now require the Inbox view and a visibly focused request,
  whose resource, argument, policy-revision and decision-scope fields are rendered.

The reviewer also confirmed the already documented B stop/recovery and distinct
non-permission-action gaps and the D native-integration blocker. This review is not
Stage acceptance.

## Combined B–D validation

Validation ran on Linux/WSL2 x86_64 (`6.18.33.2-microsoft-standard-WSL2`) with
the pinned `go1.27.1 linux/amd64` toolchain. The final working-tree gate matrix is:

```text
go test ./internal/core ./internal/tui ./internal/tools ./internal/mcp  PASS
go test ./internal/core -run TestMarkdownProposalApprovalCycleIsRevisionedAndFailClosed -count=1  PASS
go test ./internal/tui -run TestProjectDashboardAcceptsInputThroughPTY -count=1 -v             PASS
make check                                                                                     PASS
make check-race                                                                                PASS
make build                                                                                     PASS
make docs-check                                                                                PASS
make build-boundary                                                                            PASS
make cross-build (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)                        PASS
git diff --check                                                                               PASS
```

The first focused test invocation inside the restricted filesystem could not use
the existing Go build cache; the identical offline command passed with build-cache
access. `make check` used loopback access only for existing local `httptest`
fixtures. No external network, model, paid, Docker, hosting or publishing call was
made. Cross-build is compile coverage only; native macOS execution remains B4.

## Limitations and blockers

See the [blocker log](stage-5.5-blockers.md). Stage 5.5 remains unaccepted pending
independent review. Production dispatch, live model/reviewer qualification, real
human decisions and native macOS validation remain pending.
