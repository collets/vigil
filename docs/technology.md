# Technology proposal

Status: Go and the foundation stack accepted, 2026-09-19; status reconciled through 2026-09-27. Linux and macOS are the initial targets. The decision has held: the application is still Go, still uses SQLite with embedded forward-only migrations, and still has no ORM. What has changed is scale, not stack — the original hello-world scaffold has grown into the persisted core implemented and independently accepted through Stage 5.5 at `84c0275` (see [next steps](next-steps.md)). Stage 5.6 delivery/finalization is next; production execution/model qualification and delivery authority remain gated.

## Recommendation

Use Go for the CLI, terminal dashboard, session supervision, and orchestration core. Keep harnesses and model inference in separate processes or services. Use one implementation language initially.

This is principally a process-management and I/O application. Model latency, context size, inference hardware, and harness behavior are likely to dominate total task duration. Language benchmarks alone cannot predict its performance. TypeScript would be viable for this workload, but is excluded from the proposal following the user's preference.

## Go versus Python

| Consideration | Go | Python |
| --- | --- | --- |
| Runtime overhead | Strong default for CPU work and a lightweight long-running supervisor; measure actual memory use | Adequate for orchestration; CPU-heavy Python code needs more care |
| Concurrent sessions | Goroutines, contexts, channels, and subprocess APIs | asyncio and asynchronous subprocess APIs are sufficient for concurrent I/O |
| Distribution | Compiled executable; single-binary delivery depends on avoiding external native dependencies | Typically requires a managed interpreter/environment or a bundling step |
| Terminal dashboard | Bubble Tea, Bubbles, Lip Gloss | Textual and Rich |
| AI experimentation | HTTP and process interfaces are sufficient for this product | Broader ecosystem for in-process ML, evaluation, and model experimentation |
| Development ease | Static types and a small language; more explicit plumbing | Concise prototypes and flexible data handling; typing and packaging need discipline |
| Main tradeoff | More work for exploratory AI scripts | More runtime/distribution complexity for an installed CLI |

Go is preferred because the product manages existing agents. Python becomes more attractive if in-process ML experimentation becomes central, or if the developer is substantially more productive in Python. Do not add a Python sidecar speculatively.

## Proposed foundation

| Layer | Choice | Rationale |
| --- | --- | --- |
| Language | Go 1.27.1 | Concurrent supervision and executable distribution |
| CLI | Cobra | Subcommands, flags, help, and completion |
| Terminal UI | Bubble Tea with Bubbles and Lip Gloss | Stateful terminal dashboard and reusable widgets |
| Persistence | SQLite | Local transactions and relational task/session history without a database service |
| Configuration | Versioned JSON initially | Standard-library support and explicit validation; revisit TOML if usability warrants it |
| Harness integration | Native RPC, structured subprocess output, or documented HTTP interfaces | Preserve each harness's execution and permission semantics |
| Interactive access | Optional terminal handoff or tmux integration | Inspect native sessions where supported |
| Testing | Go standard testing tools, fake harness processes, recorded protocol fixtures | Verify lifecycle and adapter behavior without model charges |

SQLite uses modernc.org/sqlite, a pure-Go driver, with CGO disabled for portable builds. No ORM or application schema is included in the starter.

tmux is an optional presentation/session-access tool. Database state must remain authoritative even when panes close. A web dashboard, message broker, remote database, container platform, and ML framework are unnecessary for the initial foundation.

## Local development environment

Go 1.27.1 is installed in the ignored .tools/go directory for local development. Its archive was checked against the SHA-256 published by go.dev. Make targets prefer this installation and keep caches inside the ignored .cache directory. On another machine, install Go 1.27.1 or newer. Dependency versions and checksums are recorded in go.mod and go.sum.

The user explicitly permits installing the latest necessary development software rather than limiting the project to preinstalled tools, and can assist when access or environment setup requires it. Record chosen versions and revalidate upgrades; this is not a requirement to update dependencies on every run.

## Sources

- [Go compilation and installation](https://go.dev/doc/tutorial/compile-install)
- [Go concurrency primitives](https://go.dev/doc/effective_go#concurrency)
- [Python asynchronous subprocesses](https://docs.python.org/3/library/asyncio-subprocess.html)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [Cobra](https://github.com/spf13/cobra)
- [Textual workers](https://textual.textualize.io/guide/workers/)
- [Appropriate uses for SQLite](https://www.sqlite.org/whentouse.html)

Performance comparisons above are engineering judgments, not measured benchmarks of this application.
