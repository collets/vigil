# Vigil

A local control panel for development agents running through existing harnesses, combining frontier and local models.

Status: hello-world foundation. Go, Cobra, Bubble Tea, Bubbles, Lip Gloss, and SQLite are connected. Agent integration and workflows are not implemented. Targets: Linux and macOS, on amd64 and arm64.

- [Technology proposal](docs/technology.md): Go versus Python and the proposed foundation.
- [Minimal architecture](docs/architecture.md): responsibilities and integration boundaries.
- [Functional requirements](docs/requirements.md): consolidated product baseline and open design questions.
- [First usable milestone](docs/mvp-acceptance.md): Codex/Hermes acceptance scenario.
- [Harness investigation](docs/harness-capabilities.md): recommended transports, verified probes, and remaining integration gates.
- [Next steps and resumption plan](docs/next-steps.md): ordered work, validation gates, and handoff for the next session.
- [Stage 1 adapter spike](docs/adapter-spike.md): adapter contract, isolated profile preparation, limits, and runtime prerequisites.
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
make cross-build  # Linux/macOS, amd64/arm64; outputs in dist/
```

There are no application unit tests yet; this starter is verified with CLI, SQLite, and interactive terminal smoke checks. Cross-compilation does not establish runtime behavior on another OS.

Dependencies are pinned in `go.mod` and verified using `go.sum`. `make tidy` updates module metadata after changing imports. Make uses project-local caches and clears the inherited `GOROOT`, avoiding interference from an older system Go installation. Build outputs, caches, local databases, and the local toolchain are ignored by Git.

The module name `vigil` is local for now; replace it with the chosen repository import path when publishing.

## Structure

```text
cmd/vigil/          executable entry point
internal/cli/       Cobra commands
internal/tui/       Bubble Tea hello-world screen
internal/storage/   SQLite connection check
docs/               architecture and requirements discussion
```

Requirements discovery and Stage 1 adapter preparation are complete. Initial Codex/Hermes protocol probes passed; isolated profiles and the disposable fixture can be reproduced using `~/.hermes/hermes-agent/venv/bin/python scripts/spike/prepare.py`. This development helper uses the existing Hermes interpreter and starts no inference. See the Stage 1 document for paths and overrides. Next: Stage 2 Go transports and explicit live turns. Execution, cancellation, resume, and policy enforcement still need runtime validation. The application currently remains a hello-world scaffold.
