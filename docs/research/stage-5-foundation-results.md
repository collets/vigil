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

These checks qualify primitives only. Production readiness remains false. At this initial checkpoint, remaining work included the provider relay, pinned native worker images and private native state, complete nested Git/instruction/parent-replacement bypass tests, actual controller-kill/restart integration with coordinator quarantine, exact runtime/profile evidence, and macOS container runtime qualification. No model turn, commit/push by a worker, or remote publishing action ran in these probes.

Continue B/C integration and D before connecting production E–J. In particular, the existing native spike is not a fallback for missing containment. Readiness currently includes explicit unconditional runtime gates; user-authored capability records cannot remove them.

## Provider relay increment

The [concrete boundary design](../stage-5-boundary.md) now includes an implemented model/route-scoped provider relay and an unprivileged worker-side Unix-socket bridge. Synthetic HTTP tests passed credential substitution, wrong-token/model denial, duplicate/oversized/deep inputs, unsupported hosted tools and routes, redirect/error-header suppression, concurrency limits, revocation/cancellation, and output-flood transport abort. Tests use loopback listeners and therefore need socket permission in restricted development sandboxes. An initial cancellation fixture failed to consume the HTTP request body and waited indefinitely in server cleanup; the corrected fixture consumes the body and has its own deadline, and the full suite passed.

A real Docker worker with `network=none`, no capabilities, no new privileges and a read-only root filesystem successfully reached a host synthetic provider solely through its mounted Unix socket. It received only a fixture run token; the distinct provider credential stayed on the host. One selected-model request reached upstream; the unauthorized-model request was rejected before upstream. This is WSL socket-topology evidence, not a real native harness/model turn.

Linux and native macOS `make check check-race build` passed after the relay increment; all four application cross-builds passed. Docker is absent on the Mac, so its container boundary remains pending. All primitive probe containers were removed. No actual model turns or publishing actions were performed in the relay increment.


## Contained Hermes increment

Pinned Hermes source `6a627e6eb38e28ac421d5ad8df3f676e49d0c287` now builds with `scripts/boundary/build-hermes.py`. The build uses only a Git archive, immutable base image, frozen Python dependency lock, checksum-verified SQLite 3.53.4 and the guardian/worker binaries. It records image and input digests in a private manifest. Package installation is disabled during native startup. Runtime versions were verified as Python 3.13.13 and SQLite 3.53.4 with FTS5.

Qualified experimental image: `sha256:863305d7a6fe32172f86ce512e3656560777ec09d92b472cf3eec1b30ae9f199` on WSL/Docker amd64. Metadata-only tests started the actual Hermes gateway, verified effective configuration, and proved detached-writer termination on normal close and stopped lease renewal. Stopping renewal is not an actual controller-process kill test. The fixture has an empty protected `.git` directory; full Git bypass qualification is still outstanding.

One explicitly enabled live native turn used the existing Windows llama.cpp endpoint and model `qwen3.8-27b-local`. Hermes changed only the requested fixture contents to `contained-hermes-ok\n` and returned the required JSON result. Its three provider requests completed through the scoped relay, with no aborted or active request at the final observation. Usage was 17,503 input and 284 output tokens. The worker received only the run token; the real provider credential remained on the host. Container inspection and a stable heartbeat separately proved cleanup; the native completion event alone did not prove writer quiescence.

Private evidence is `.cache/boundary/live-hermes-3548303052/report.json`; build manifest is `.cache/boundary/hermes-e4z3hyw9/image.json`. The live test is separately gated by `VIGIL_TEST_LIVE_HERMES=1` and is not part of routine tests. No publishing occurred, and production eligibility remains false. Startup failures exposed a native lazy-initialization error that previously waited for the deadline; the adapter now returns that error immediately, with a regression test. Coordinator shutdown is synchronized with mutations, and repeated owned endpoint reservations recover the original generation.

Linux checks, race tests and all four cross-builds passed after this increment. OrbStack has since become available on the Mac; its native containment results will be recorded separately.


## macOS OrbStack prerequisite results

The user installed and started OrbStack. SSH reached Docker context `orbstack`, engine **29.4.0**, Linux arm64. Its CLI is `~/.orbstack/bin/docker`, outside the noninteractive SSH default PATH; tests supply that path without changing shell configuration.

The pinned Alpine image was pulled for arm64. On the actual Mac, protected filesystem/network probes and the guardian's lease expiry, hard deadline and native-exit cleanup all passed. Native `make check check-race build` also passed with the latest coordinator and Hermes adapter changes. All labeled probe containers were removed.

The direct host Unix-socket relay test failed with HTTP 502. A separate credential-free diagnostic confirmed `connect: connection refused` when a Linux container dialed the mounted, listening macOS socket. Therefore the WSL host-socket topology is **not qualified on this OrbStack setup**. The ordinary suite passes because Docker experiments require explicit opt-in; this failed integration result must not be hidden by that default. A relay inside the Linux VM, sharing a socket volume with the isolated worker, remains the planned alternative. Its upstream provider connection and credential handoff still need implementation and qualification. No native model turn ran on the Mac during these tests.


## OrbStack relay and live Hermes follow-up

The VM-side relay is implemented as `vigil-relay`. Its bootstrap accepts only four nonempty string fields, rejects duplicate/unknown/trailing/oversized input, and emits no credentials in diagnostics. Startup has a ten-second deadline; execution has a nonrenewable limit and is wrapped by the existing guardian. Synthetic three-container tests passed on WSL and OrbStack: private stdin auth, read-only socket volume, worker network isolation, selected-model forwarding, wrong-model denial, absent credential metadata, and stopped relay namespace after lease expiry. A Linux-only initial fixture failure was corrected by allowing the capability-restricted root guardian to traverse the credential-free lease directory.

The complete OrbStack host route then passed with a synthetic provider: isolated worker → VM relay → loopback Mac relay → selected provider. The host relay retains the actual provider credential; the VM sidecar receives only the run token. No published network listener or broad host directory is mounted into the worker.

Hermes arm64 image `sha256:51a4c7a804d86ee15d6b1fd8e13f2af4f865cfc9f8ae84e95c575fd52d6b05c3` was built from the same pinned source/base/SQLite inputs. Manifest: `/tmp/vigil-stage5.yd7DDE/.cache/boundary/hermes-c9n18wob/image.json` on the Mac. Actual Hermes metadata, normal close and lease-loss detached-writer cleanup passed there.

Exactly one live Mac qualification turn then used `qwen3.8-27b-local` through the temporary reverse SSH tunnel to Windows llama.cpp. The requested fixture edit and JSON result passed, and container/heartbeat checks proved cleanup. The host relay recorded three completed requests, zero aborted requests and no active request. Native usage was 17,260 input and 313 output tokens. Evidence: `/tmp/vigil-stage5.yd7DDE/.cache/boundary/live-hermes-1582491851/report.json`. The credential was supplied through SSH stdin, not a command argument or Docker environment. The tunnel was closed after the test. This adds one model turn to the earlier WSL turn; none was replayed or published. Production eligibility remains false.


## Checkout admission and Git bypass evidence

The new conservative planner and alias/replacement tests passed natively on Linux and macOS. The pinned Hermes images supplied Git for inference-free Docker probes on both architectures. Allowed fixture edits succeeded; protected staging/commit/config/ref/raw writes, mount replacement, hard-link creation and a local push into the original Git directory were rejected. Native regression/race suites, builds and the four cross-builds passed. Unsupported nested/alias layouts remain explicit refusals; this does not qualify arbitrary project layouts or hostile concurrent host mutation. No additional inference or publishing occurred in this increment.

The user's away-time spending/safety constraints and deferred decisions are tracked in [pending decisions](../pending-decisions.md). Stage 6 remains conditional on finishing the accepted Stage 5 milestone.


## Enclosing policy restrictions

Closed exact-value restrictions now cover profile IDs and operation categories at project, plan and task scope. Offline tests prove that narrower scopes cannot widen the intersection, empty lists deny all values, invalid dimensions/values fail, readiness reports excluded profiles, and operation admission denies categories excluded by an enclosing plan. Plan replacement invalidates old authority by advancing the policy epoch; tests prove a later widening cannot revive the old operation/grant pair. These handlers still perform no external effect. Repository/path/check/manual definitions, global grants and runtime policy integration remain incomplete.


## Persisted dashboard

`vigil dashboard PROJECT_ID` now displays overview/readiness, task blockers/checks, pending decisions and recent history from one read-only SQLite transaction. Refresh runs asynchronously with a deadline; failures preserve the previous snapshot with a warning. Reads cannot authorize execution. Inbox and history are capped at 100 records, and terminal control sequences are removed from persisted text. Focused tests cover reopened truth, latest-event order/cap, no mutation, cancellation while loading, retained snapshots and terminal bounds.

Linux and native Mac check/race/build suites passed, as did four platform cross-builds. A real Linux PTY smoke test displayed overview, inbox and named history events and quit cleanly. The first smoke assertion expected a command name while history displayed only the generic event kind; the view now includes the command name and actor from the sanitized event payload. An initial synthetic event fixture omitted its required schema version; fixing the fixture restored the full test run. Decisions and execution controls remain read-only/pending.
