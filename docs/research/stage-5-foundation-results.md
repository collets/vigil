# Stage 5 foundation validation

Date: 2026-09-20. Scope: persisted planning/control foundation and initial Docker containment primitives; Stage 5 is in progress.

## Core evidence

Linux `make check`, `make check-race`, build and Linux/macOS amd64/arm64 cross-builds passed. Tests cover durable command replay across reopened and independent concurrent DB handles, revision conflicts, transaction rollback, migration rejection, content-addressed artifact corruption/orphans, dependency cycles, criteria protection, local-only restrictions, grant revocation/once consumption, owner fencing, FIFO capacity and real controller-process termination retaining quarantine. CLI tests initialize, apply/replay configuration, reopen readiness/events and reject implicit capacity authority.

Native macOS arm64 `make check check-race build` passed in an isolated temporary source copy. Filesystem tests exercised the actual volume's case/Unicode alias behavior, symlinks, replaced roots and shared Git worktrees. The first transfer omitted the historical SQL fixtures, causing two fixture-loading failures; adding `docs/spec` corrected the transfer and the full run passed. The user's normal Mac checkout and native harness configuration were not changed.

## Docker evidence

Docker Desktop was initially switched off. Once started by the user, the WSL client reached Linux engine **29.8.0**. This corrected the initial suggestion that WSL integration needed enabling; no integration setting was changed by Vigil.

The explicit opt-in test suite uses `alpine@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0`, private disposable fixture paths, no host credentials/sockets and no model traffic. It removes its containers on completion.

```sh
VIGIL_TEST_DOCKER=1 make check
```

The image must already be available (`docker pull alpine:3.23` was used and its resolved digest pinned). Routine tests skip Docker. The test compiles the internal guardian for the engine's Linux architecture; no image build or package installation occurs inside the test.

| Probe | Observation |
| --- | --- |
| Writable fixture plus read-only root filesystem | Approved fixture write succeeded |
| Mounted `.git` and root instructions | Raw writes, deletion and mount-point rename blocked; original host bytes retained |
| Remount, Docker socket and host paths | Remount failed; checked host credential/socket paths absent |
| `network=none` | Only loopback interface; attempted public HTTP and host llama localhost connection failed |
| Detached writer after exec-client exit | Writer ran until whole-container termination, then stopped |
| Whole-container termination | Engine reported `Running=false`, PID 0, stable heartbeat file |
| Root PID-1 guardian / unprivileged worker | Worker could not signal-stop guardian or overwrite read-only controller lease |
| Lease expiry | Guardian exited and detached writer stopped |
| Nonrenewable hard deadline | Guardian exited before the lease deadline and writer stopped |
| Native foreground exit | Guardian exited successfully and detached writer stopped |

The first mount invocation omitted an explicit propagation option required by this Docker CLI. Adding `bind-propagation=rprivate` with `bind-recursive=readonly` fixed the invocation; the full suite then passed. Controls follow Docker's [container runtime options](https://docs.docker.com/engine/containers/run/) and [recursive bind mount behavior](https://docs.docker.com/engine/storage/bind-mounts/).

## Qualification limits and next implementation

The guardian is an internal Linux-container entrypoint and refuses host execution. Its child uses a different non-root UID/GID. The host must mount the executable/control directory read-only, drop capabilities except the guardian's credential-switch requirements, forbid new privileges, and inspect the container after exit. Lease renewal must be controller-owned and atomic; maximum lease horizon is ten seconds, checked every 100 ms. A monotonic hard deadline cannot be renewed. This alone does not prove inference idle at a remote provider.

These checks qualify primitives only. Production readiness remains false. Still required: credential-holding provider relay with bounded routes/model policy, pinned Codex/Hermes worker images and private native state, complete nested Git/instruction/parent-replacement bypass tests, actual controller-kill/restart integration with coordinator quarantine, exact runtime/profile evidence, and macOS container runtime qualification. No model turn, commit/push by a worker, or remote publishing action ran in these probes.

Continue B/C integration and D before connecting production E–J. In particular, the existing native spike is not a fallback for missing containment. Readiness currently includes explicit unconditional runtime gates; user-authored capability records cannot remove them.
