# Stage 5.5 implementation results

Status: implementation in progress; Stage 5.5 is unaccepted.

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

Pending checkpoint D implementation.

## PTY and UI evidence

Pending checkpoint B implementation.

## Limitations and blockers

See the [blocker log](stage-5.5-blockers.md). Stage 5.5 remains unaccepted pending
independent review. Production dispatch, live model/reviewer qualification, real
human decisions and native macOS validation remain pending.
