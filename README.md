# Vigil

A local control panel for development agents running through existing harnesses, combining frontier and local models.

Status: bounded Codex/Hermes adapters with Linux lifecycle evidence and a completed application-core specification. The dashboard remains a scaffold; scheduling, recovery, and product workflows are not implemented. Targets: Linux and macOS, on amd64 and arm64.

- [Technology proposal](docs/technology.md): Go versus Python and the proposed foundation.
- [Minimal architecture](docs/architecture.md): responsibilities and integration boundaries.
- [Functional requirements](docs/requirements.md): consolidated product baseline and open design questions.
- [First usable milestone](docs/mvp-acceptance.md): Codex/Hermes acceptance scenario.
- [Harness investigation](docs/harness-capabilities.md): recommended transports, verified probes, and remaining integration gates.
- [Next steps and resumption plan](docs/next-steps.md): ordered work, validation gates, and handoff for the next session.
- [Stage 1 adapter spike](docs/adapter-spike.md): adapter contract, isolated profile preparation, limits, and runtime prerequisites.
- [Stage 2 plan and runner](docs/stage-2-plan.md): transport implementation, controlled execution, verification, and limitations.
- [Stage 3 lifecycle/policy results](docs/research/stage-3-results.md): observed capabilities and strict execution limits.
- [Application core specification](docs/core-spec.md): state, policy, coordination, checkpoints and storage contracts.
- [Stage 5 backlog](docs/stage-5-plan.md): implementation slices and R01–R71 coverage.
- [Stage 3.5 macOS plan](docs/stage-3.5-macos.md): setup and runtime checks with the user.
- [Session continuity audit](docs/session-audit.md): decisions, alternatives, open questions, and documentation provenance.
- [Discovery history](docs/discovery-notes.md): brainstorming decisions and their evolution.

## Run

Install [Go 1.27.1 or newer](https://go.dev/dl/) and Make. This workspace also has a checksum-verified Go 1.27.1 installation in `.tools/go`; Make uses it automatically when present. No Python, Node, C compiler, SQLite executable, or tmux is required.

```sh
make build
./bin/vigil hello
./bin/vigil dashboard
```

The dashboard uses an interactive terminal. Press `q`, `Esc`, or `Ctrl+C` to exit. Use `hello` for noninteractive environments. `--help`, `--version`, and Cobra shell completion are available.

By default, both commands query a temporary in-memory SQLite database. To verify a file-backed connection:

```sh
mkdir -p .vigil
./bin/vigil hello --db .vigil/demo.sqlite
```

The parent directory must exist. The scaffold queries SQLite's version and does not create application tables or persist tasks. Connection failures return a nonzero exit code.

## Development

```sh
make fmt          # format Go source
make check        # vet and compile/test all packages
make check-race   # race detector for the transport/runner; requires a C compiler
make cross-build  # Linux/macOS, amd64/arm64; outputs in dist/
```

Synthetic tests exercise RPC correlation, native session events, request lifetimes, failure/shutdown paths, structured results, and independent fixture checks. Routine tests do not contact model providers. Cross-compilation does not establish runtime behavior on another OS.

Dependencies are pinned in `go.mod` and verified using `go.sum`. `make tidy` updates module metadata after changing imports. Make uses project-local caches and clears the inherited `GOROOT`, avoiding interference from an older system Go installation. Build outputs, caches, local databases, and the local toolchain are ignored by Git.

The module name `vigil` is local for now; replace it with the chosen repository import path when publishing.

## Structure

```text
cmd/vigil/          executable entry point
internal/cli/       Cobra commands
internal/harness/   bounded stdio transport and native session adapters
internal/spike/     isolated development runner and fixture validation
internal/tui/       Bubble Tea hello-world screen
internal/storage/   SQLite connection check
docs/               architecture and requirements discussion
```

## Controlled adapter spike

Both installed harnesses are required for preparation. Hermes uses its existing Python environment; Vigil remains a Go binary. Preparation checks pinned versions and creates private native homes plus a disposable Git fixture with no remotes:

```sh
python3 scripts/spike/prepare.py
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness hermes
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness hermes --live
```

Replace `stage1-EXAMPLE` with the printed directory. Use `--harness codex` for Codex. Prepare a **new fixture for every live attempt**, including when switching harnesses. Without `--live`, the runner checks metadata and starts no model turn. The live command edits the prepared fixture and verifies its result; it does not accept a product task.

Start llama.cpp before a Hermes run. The runner uses `VIGIL_LLAMA_API_KEY`, a private `--llama-key-file`, or inherited `OPENAI_API_KEY` when `OPENAI_BASE_URL` matches the prepared local endpoint. Keys never go in command arguments or manifests. Codex uses the referenced existing ChatGPT authentication, copied into its private home for the run and removed on normal exit. Global harness settings remain untouched.

Private activity and result evidence stays under the experiment's `evidence/` directory. Raw stderr and protocol payloads are omitted. Native histories remain private and separate. The runner enforces one attempt at a time per checkout, finite time/output limits, no prompt replay, and immediate denial/cancellation of native requests. A crash may leave `runner.lock` or a private Codex auth copy: inspect the recorded process before removing a stale lock, preserve experiment evidence, and prepare a new fixture. Cross-checkout endpoint coordination belongs to the later application core.

See [next steps](docs/next-steps.md) for current evidence and remaining lifecycle, resume, approval, policy, and macOS runtime gates.

Lifecycle experiments use the same fresh manifest with `--live --scenario resume|interrupt|child|loss|clarify|approval-allow|approval-deny`. The approval stimuli are Codex-specific. These scenarios may intentionally fail and retain partial fixture files; see the Stage 3 plan before running them. Draft schemas live in `docs/spec` and are checked by `make check`; the CLI does not install them.
