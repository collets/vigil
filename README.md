# Vigil

<!-- vigil-status: stage=5.7; stage_accepted=true; implementation_commit=522cb967f732b578a66c051cf8938dd00584d238 -->

A local control panel for development agents running through existing harnesses, combining frontier and local models.

Status: **pre-release.** The persisted core is independently accepted offline through **Stage 5.7 at `522cb96`**, for each stage's **autonomous** scope. **Stage 6.1**, the terminal interface parity gap analysis, is implemented, was **rejected** by its first independent review, returned **conditional** by each of four further reviews, and all five rounds of findings are remediated pending one more confirming review; it wrote no interface code and measured that the interface fully expresses 4 of the 82 distinct capabilities the 85-row register classifies, and 6 of the 25 human decision classes, then planned sub-stages 6.2–6.9. Stage 5.7's autonomous qualification found blocking finding 5.7-F1, which made the delivery path unreachable from the documented workflow; that defect was fixed in the Stage 5.2/5.6 slice under explicit authorization, and the walkthrough now reaches commit, push and draft delivery end to end. Every human-gated operation is deferred to Stage 8, and no delivery path has been exercised against a real remote. Stages 6, 7 and 8 were added by user scope revision on 2026-09-29. Stage 5.5 checkpoints A–D and independent-review remediation are implemented through `84c0275`, including stale-dispatch retirement, expiring recovery choices, persisted terminal decisions, bounded planning, shared MCP handlers and native Hermes one-tool isolation; exact-commit Linux/WSL and native macOS gates pass. Automatic plan advancement remains disabled. Production check, model and reviewer dispatch remain deliberately disabled pending live qualification, and nothing in planning or acceptance can commit, push, publish or deliver.

Targets: Linux and macOS, on amd64 and arm64. See [`docs/process/next-steps.md`](docs/process/next-steps.md) for current state.

## Documentation

Documentation is tiered by **when you need it**. Start at
[`docs/START-HERE.md`](docs/START-HERE.md) for the reading order; the full
authoritative-for index is [`docs/README.md`](docs/README.md).

| Read | Folder | What it settles |
| --- | --- | --- |
| [Agent instructions](AGENTS.md) | repository root | Project rules and the documentation obligations every change must satisfy |
| [Core product facts](docs/core/requirements.md) | `docs/core/` | Requirements, architecture, core specification, technology, harness capabilities, checkpoints, milestone |
| [Process and current state](docs/process/next-steps.md) | `docs/process/` | Development workflow, next steps, pending decisions, decision provenance |
| [Stage 5 index](docs/plans/stage-5/README.md) | `docs/plans/` | Stage 5.1–5.7 ordering, completion gates and the CLI reference — start from the slice you are working on |
| [Research evidence](docs/research/README.md) | `docs/research/` | Dated observations and independent reviews |
| [Superseded records](docs/history/README.md) | `docs/history/` | Rejected alternatives and completed-stage plans, kept for provenance only |

## Run

Install [Go 1.27.1 or newer](https://go.dev/dl/) and Make. This workspace also has a checksum-verified Go 1.27.1 installation in `.tools/go`; Make uses it automatically when present. No Python, Node, C compiler, SQLite executable, or tmux is required.

```sh
make build
./bin/vigil hello
./bin/vigil project init /absolute/path/to/project
./bin/vigil dashboard PROJECT_ID
```

Use the project ID returned by initialization. The dashboard is interactive, refreshes every two seconds, and exits on `q`, `esc` or `ctrl+c`. Use `hello` for noninteractive environments; it only queries `sqlite_version()`. `--help`, `--version`, and Cobra shell completion are available. The full command surface, including every flag and its default, is in the [CLI guide](docs/plans/stage-5/stage-5-cli.md).

The dashboard and all `project`/`resources` commands read private persisted application state, which defaults to `$XDG_STATE_HOME/vigil` or `~/.local/state/vigil` and is overridden with `--state-dir`. `hello` instead uses a temporary in-memory SQLite database unless given `--db`:

```sh
mkdir -p .vigil
./bin/vigil hello --db .vigil/demo.sqlite
```

The parent directory must exist. Connection failures return a nonzero exit code. Note that `--db` is inert for every command other than `hello`; persisted state is resolved only through `--state-dir`.

## Development

```sh
make fmt          # format Go source
make check        # vet and compile/test all packages, including the documentation gate
make check-race   # race detector for transport, runner and persisted core; requires a C compiler
make docs-check   # documentation consistency gate on its own, verbose
make cross-build  # Linux/macOS, amd64/arm64; outputs in dist/
```

`make check` and `make docs-check` enforce the documentation contract described in [`AGENTS.md`](AGENTS.md): after changing the CLI, schema, packages, plans or stage status, the affected documents must be updated in the same change. The gate fails on broken relative links, orphan documents, undocumented CLI commands, a structure block that does not match `internal/`, commit ranges that do not resolve, superseded ranges in next-action text, migration-count drift, and status documents that disagree with `docs/STATUS`.

Synthetic tests exercise RPC correlation, native session events, request lifetimes, failure/shutdown paths, structured results, and independent fixture checks. Routine tests do not contact model providers, Docker, or any network service. Cross-compilation does not establish runtime behavior on another OS.

### Autonomous qualification walkthrough

`make scenario` runs `cmd/vigil-scenario`, a **development qualification tool**, not a product path. It drives the built `bin/vigil` as real subprocesses against a disposable Git fixture in `/tmp/vigil-stage-5.7-scenario`, and writes a JSON report with a milestone table, a recovery/boundary matrix and the R01–R71 classification. It contacts no provider, no hosting service and no real remote; it makes one bounded metadata GET against the loopback route when one is configured on the host, purely to record that route's identity, and never issues a prompt, follows a redirect or leaves the loopback interface. It exits zero when it completes: the gaps it records are the deliverable, not a tool failure. A nonzero exit means a stage aborted.

Four capabilities are separate opt-ins, all off by default. `codex_live` and `remote_delivery` are refused in this build and recorded as Stage 8 gates with their exact blockers. `make scenario-live` additionally permits the bounded local inference capability, which probes the existing loopback route; it does not yet drive a live model turn, and the report says so. `make scenario-clean` removes only that disposable root, and every target refuses a `SCENARIO_ROOT` that does not resolve to the documented `/tmp/vigil-stage-5.7-scenario` directory. `make scenario-guard-check` exercises that refusal automatically, creating and removing only its own sentinel. See [Stage 5.7 results](docs/research/stage-5/5.7/results.md).

Dependencies are pinned in `go.mod` and verified using `go.sum`. `make tidy` updates module metadata after changing imports. Make uses project-local caches and clears the inherited `GOROOT`, avoiding interference from an older system Go installation. Build outputs, caches, local databases, and the local toolchain are ignored by Git.

For the npm-installed Codex harness, `.nvmrc` selects Node 24 (`nvm use`, or a configured shell auto-switch hook). Node is not a Vigil binary/build dependency.

The module name `vigil` is local for now; replace it with the chosen repository import path when publishing.

## Structure

```text
cmd/vigil/            main executable entry point
cmd/vigil-guardian/   container boundary guardian
cmd/vigil-worker/     container boundary worker
cmd/vigil-relay/      container boundary relay
internal/cli/         Cobra commands
internal/harness/     bounded stdio transport and native session adapters
internal/spike/       isolated development runner and fixture validation
internal/tui/         Bubble Tea persisted project views (Stage 6.2 onward splits this into a screen stack)
internal/tools/       bounded role/session-scoped model application handlers
internal/mcp/         small JSON-RPC transport reusing the model handlers
internal/storage/     SQLite connection check
internal/store/       private application databases, embedded migrations, durable commands
internal/core/        project definitions, readiness, human authority, dashboard reads
internal/policy/      closed definitions and policy evaluation
internal/quality/     quality scopes, freshness, effects, budgets, manual/human records, acceptance
internal/checks/      contained check execution, supervisors and process trackers
internal/review/      fresh read-only review
internal/supervisor/  execution lifecycle, recovery choice, fixture driver
internal/scenario/    Stage 5.7 disposable qualification walkthrough and evidence matrix
internal/checkpoint/  verified checkpoint sets, clear and restore journals
internal/coordinator/ cooperative claims, endpoint queues and crash quarantine
internal/boundary/    runtime doctor and experimental container guardian/probes
internal/workspace/   physical directory, discovery and common Git identities
internal/artifacts/   bounded content-addressed evidence
internal/doccheck/    documentation consistency gate
docs/                 architecture, plans, specifications, evidence and reviews
```

## Controlled adapter spike

`spike` is a bounded Stage 1–3 experiment runner, not a production path. Both harnesses are required for preparation. Hermes uses its existing Python environment; Vigil remains a Go binary. Preparation checks pinned versions and creates private native homes plus a disposable Git fixture with no remotes:

```sh
python3 scripts/spike/prepare.py
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness hermes
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness hermes --live
```

Replace `stage1-EXAMPLE` with the printed directory. Use `--harness codex` for Codex. Prepare a **new fixture for every live attempt**, including when switching harnesses. Without `--live`, the runner checks metadata and starts no model turn. The live command edits the prepared fixture and verifies its result; it does not accept a product task, and `--live` starts a real model turn that costs money.

Start llama.cpp before a Hermes run. The runner uses `VIGIL_LLAMA_API_KEY`, a private `--llama-key-file`, or inherited `OPENAI_API_KEY` when `OPENAI_BASE_URL` matches the prepared local endpoint. Keys never go in command arguments or manifests. Codex uses the referenced existing ChatGPT authentication, copied into its private home for the run and removed on normal exit. Global harness settings remain untouched.

Private activity and result evidence stays under the experiment's `evidence/` directory. Raw stderr and protocol payloads are omitted. Native histories remain private and separate. The runner enforces one attempt at a time per checkout, finite time/output limits, no prompt replay, and immediate denial/cancellation of native requests. A crash may leave `runner.lock` or a private Codex auth copy: inspect the recorded process before removing a stale lock, preserve experiment evidence, and prepare a new fixture. Cross-checkout endpoint coordination belongs to the persisted core, not to the spike.

See [next steps](docs/process/next-steps.md) for current evidence and the remaining lifecycle, resume, approval, policy, and macOS runtime gates.

Lifecycle experiments use the same fresh manifest with `--live --scenario resume|interrupt|child|loss|clarify|approval-allow|approval-deny`. The approval stimuli are Codex-specific. These scenarios may intentionally fail and retain partial fixture files; read the historical [Stage 3 plan](docs/history/stage-3-plan.md) before running them. Historical draft schemas live in `docs/spec` and are validated by `internal/storage/spec_test.go` under `make check`; they are a design record, not the installed schema. `project init` installs the separately versioned embedded migrations from `internal/store/migrations`, which are the authoritative schema. See the [CLI guide](docs/plans/stage-5/stage-5-cli.md).
