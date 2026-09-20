# Stage 5 Docker boundary implementation

Docker Desktop's Linux engine is the selected WSL implementation candidate. Runtime controls and relay primitives are experimental until the qualification matrix passes with each actual harness/profile. Production readiness is still false.

## Process and filesystem layout

The trusted foreground controller owns project/coordinator databases, artifacts, Git effects and provider credentials outside the worker. The worker receives approved checkout paths, private native state, read-only protected Git/instruction mounts, an immutable executable image, a read-only controller lease, and a read-only mount of one provider socket. It receives no Docker/SSH agents, host credential homes or arbitrary host paths.

The container starts `vigil-guardian` as root PID 1, with capabilities dropped except SETUID/SETGID for launching a different non-root worker identity. `no-new-privileges` prevents the child gaining those privileges. The guardian observes a read-only controller lease (maximum ten-second horizon, checked every 100 ms) and an independent monotonic hard deadline (currently maximum 90 minutes). A failed/expired lease, deadline, signal or native-process exit terminates PID 1. Linux PID-namespace teardown stops detached descendants. Production cleanup must still confirm the engine reports a stopped, empty boundary and reconcile remote inference before releasing resource claims.

`vigil-worker` starts an ephemeral loopback-only HTTP listener inside a `network=none` namespace, then launches the native process with `OPENAI_BASE_URL` pointing there. Its transport can dial only the selected Unix socket. Other native provider routes must be disabled or explicitly rejected during profile qualification. The wrapper is not itself a sandbox and is not an alternate production entrypoint.

The first probes protect top-level `.git` and instructions. They do not establish complete protection for nested repositories/instructions, replaced ancestors, symlink/hardlink aliases or worktrees. Those layouts must be qualified or explicitly rejected by the production mount planner before launch. A read-only mount alone is not a blanket claim about every reachable alias.

## Provider relay

`internal/boundary.Relay` holds the actual API key. The worker gets a unique run-scoped token. The relay accepts only authenticated `GET /v1/models` (synthetic selected-model metadata) and `POST /v1/chat/completions` or `/v1/responses` for the exact selected model. It rejects noncanonical routes/query strings, redirects, unsupported request fields, duplicate JSON keys, hosted tools, background requests, multiple completions and provider-side storage requests. Only function tool definitions are accepted. Remote media is unsupported.

Limits: 8 MiB request body, 64 nesting levels, 16 MiB provider output, one in-flight request per relay, 30-minute transport ceiling. Output overflow or broken upstream streaming aborts the HTTP transport rather than reporting a clean end. Provider error bodies/cookies/auth headers are not forwarded. Proxy environment variables are ignored. Revoking the relay cancels its active transport and refuses subsequent calls, but does not prove remote inference has stopped.

The Unix-socket bridge passed a real WSL/Docker test using a network-isolated worker and a host synthetic provider. The actual provider key stayed outside the container; the worker's unauthorized model request never reached upstream. This topology is verified only on this WSL engine. On macOS OrbStack 29.4.0, the mounted host socket failed with connection refused, while filesystem and guardian cleanup primitives passed. The replacement VM relay now passes: a separate non-root relay process shares a socket volume read-only with the network-isolated worker. Its bounded, closed bootstrap arrives only on stdin. A root guardian controls its lease and hard deadline. Synthetic tests passed on both engines, including wrong-model denial and lease-loss revocation. Do not assume a Mac host Unix socket traverses the VM boundary.

The real Hermes gateway and one local model turn per platform now pass through the WSL and OrbStack boundaries with pinned architecture-specific images and private native homes. Normal close and lease loss stop detached writers. This is partial harness evidence: full Git layouts, actual controller crashes, persisted dispatch and Codex qualification remain outstanding. See the results record for exact image and model identity.

## Qualification still required

1. Complete pinned native images and version/source manifests (Hermes now has a tested WSL image; Codex remains pending); minimal controlled native homes and config; no host publishing credentials. Verify every auxiliary inference route uses the scoped relay. Existing ChatGPT authentication may require a separate provider-auth path; never assume the API-key relay qualifies it.
2. Qualify mount planning against actual Git commands, alternative git-dir/work-tree paths, parent replacement, nested instructions/repos, symlinks and instruction aliases. Preserve user changes and private recovery state outside writable mounts.
3. Wire stable container/run/generation identities and controller lease renewal into persisted start/stop journals. Kill the actual controller at each launch/attach/result point; prove no replay, bounded guardian termination and retained quarantine.
4. Verify both harnesses through the boundary against the existing Windows llama service or explicitly authorized frontier profile. Record model turns, request shapes, auxiliary calls, cancellation/idle behavior, native completion and exact filesystem effects.
5. Repeat cleanup, mounts, socket routing and ownership tests with the macOS container runtime. Cross-builds and native core tests do not qualify this VM boundary.
6. Bind evidence to runtime/image/guardian/profile/harness versions and digests. Only the trusted core may consume successful evidence to enable dispatch; profile declarations cannot manufacture it.

See [foundation results](research/stage-5-foundation-results.md) for executed primitive tests and [expanded execution plan](stage-5-execution.md) for the remaining application work.


## OrbStack host-provider route

On OrbStack, the VM relay forwards to a loopback host relay using `host.docker.internal`, the [documented Mac-host route](https://docs.orbstack.dev/docker/network). Only the VM relay has a bridge network; the worker retains `network=none`. No TCP ports are published. The host relay holds the real provider key. The VM relay receives only the per-run token on stdin, and its shared volume contains only the Unix socket. Synthetic tests exercised this full route, rather than assuming VM-to-host reachability from the shared-volume test.

The Mac live probe temporarily forwarded Mac loopback port 8080 over SSH to the existing Windows llama.cpp service reached from WSL. The credential was handed to the Mac test process through SSH stdin and stayed outside both containers. The tunnel closed after the one-turn experiment. This remains test orchestration: production cross-host capacity ownership, persisted identities, admission checks and crash reconciliation are not yet wired.
