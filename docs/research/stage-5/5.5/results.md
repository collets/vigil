# Stage 5.5 implementation results

Status: implementation through checkpoint A commit `4dbb444`, initial B–D
commit `20ca4d0`, bounded planning/control commit `192c6ba`, native-tool
isolation fix `959eaaf` and revision-bound quality controls `669467e`; Stage 5.5
interaction completion is `bdd6e33`. Checkpoints A–D are implemented offline;
Stage 5.5 is unaccepted pending independent review.

This record is append-only evidence for the four checkpoints in
[the Stage 5.5 plan](../../../stage-5/5.5-workflow-and-planning.md). It distinguishes
offline fixture mechanics from real human approval and live model/runtime evidence.
The complete user-only gate list is maintained in the
[Stage 5.5 blocker log](blockers.md).

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

## 2026-09-26 bounded runtime and owner-control follow-up

Commit `192c6ba` adds the remaining application boundary needed to exercise the
selected local route without creating production dispatch:

- migration 016 stores a cumulative 30-minute pre-plan ledger, immutable planning
  attempts, terminal/unknown charges and one-time transfer into the ordinary
  plan-services ledger. Each turn is capped at five active minutes;
- `planning-run` selects the exact current eligible profile/specification revisions,
  labels Markdown as untrusted context, exposes no tools, accepts only a closed
  64 KiB proposal and requires native idle. `planning-reconcile` is the explicit
  receipt-backed crash path and charges the full reserved cap unknown;
- stop requests are revision-bound persisted intents. Only the dispatcher owning
  the native driver performs interruption/termination; an atomic conditional
  claim prevents concurrent owners from repeating the external effect;
- `tool-server` is a transport-only MCP adapter over the same injected handler
  session, caps and receipts. The Hermes qualifier exposes only that server,
  requires one audited `project.read`, waits for idle, retires the generation and
  verifies stale rejection.

The real Cardtracker checkout was inspected read-only at
`8edc269cb4654822c629de0f29b1068ae3b721fa`; its untracked `.agents/` and
`skills-lock.json` were neither copied nor modified. `git archive HEAD` created a
disposable fixture and added the 952-byte qualification specification (digest
`364ca28d…`) before the fixture baseline. No Cardtracker commit was created.
The ignored fixture path is hidden by many file browsers; an exact credential-free
[visible review copy](qualification-input.md) is indexed without
creating a new specification revision or approval.
The actual persisted proposal is separately rendered as an indexed
[human-readable proposal review](qualification-proposal.md), preserving
revision 1 and digest `d90d3e5b…64a6b`; the review copy does not approve or apply it.

Two explicitly authorized local Hermes planning turns used
`custom/qwen3.8-27b-local` through the existing loopback llama route. The final
observed turn created `qualification-proposal` revision 1, digest
`d90d3e5b47e30dd66d52e94702859c70e2ce7c9c144519ef787b2f08bfe64a6b`, with one
docs-only task, human acceptance and the requested local checks/profile/caps. It
charged 56,038 ms of the 1,800,000 ms pre-plan allowance and recorded
`provider_idle=1`; replay returned the receipt without another turn. No plan was
applied. This proves the bounded local planning integration, not the user's
approval or production model eligibility.

The first Hermes MCP qualification turn completed natively but exposed that the
prepared `HERMES_TUI_TOOLSETS=file,terminal,clarify` pin hid the configured MCP
server, so zero audited calls correctly failed qualification. The implementation
now replaces that pin with only `vigil`, fixes the audit query and makes retirement
fail-visible. A fresh rerun stopped before native launch because
`127.0.0.1:8080` returned connection refused. Thus offline transport/session tests
pass, while native Hermes MCP and Codex integration remain unproven.

The required read-only adversarial follow-up found three issues: planning crash
reconciliation was not reachable from CLI, concurrent stop claims ignored affected
row counts, and qualification cleanup could hide retirement failure. All three are
remediated in `192c6ba`; a second read-only pass found no remaining actionable
finding. It reiterated that checkpoint B still lacks TUI task acceptance, manual
outcome and native clarification actions. This review is not Stage acceptance.

Native macOS validation of exact implementation commit `192c6ba` was attempted
against the documented authorized host. SSH returned `No route to host` before a
temporary directory was created, so no Mac command ran and no Mac checkout was
changed. Darwin cross-build remains compile evidence only.

Final Linux/WSL2 validation for `192c6ba` used pinned `go1.27.1 linux/amd64`:

```text
go test ./internal/core ./internal/supervisor ./internal/spike
        ./internal/cli ./internal/mcp                         PASS
make check                                                    PASS
make check-race                                               PASS
make build                                                    PASS
make docs-check                                               PASS
make build-boundary                                           PASS
make cross-build (linux/amd64, linux/arm64,
                  darwin/amd64, darwin/arm64)                 PASS
git diff --check                                              PASS
```

The full/race suites required permission for their existing loopback-only
`httptest` listeners. They made no external network call. The final native Hermes
qualification attempt made no model call because endpoint health failed first.

## 2026-09-27 native Hermes and macOS follow-up

The user confirmed that the existing llama endpoint was listening and that
`OPENAI_API_KEY` was inherited. Vigil inspected only credential presence and the
exact loopback route; it did not print or persist the key. The installed Hermes
source had moved to canary commit `26780d55…`, so the old prepared manifest
correctly failed its source-version pin. Qualification used a disposable detached
clone of the locked Hermes `0.21.3` commit `6a627e6…`, reusing its environment
without changing the user's installed source.

Retained failed attempts exposed three integration boundaries before the passing
run: the MCP server was discovered before Vigil opened the injected session; the
SDK supplied reserved `_meta` transport metadata outside application arguments;
and a planning-role session initially exposed five allowed tools rather than the
one capability under test. Commit `959eaaf` now opens the injected session before
an explicit native `reload.mcp`, accepts only the bounded reserved `_meta` member
at the JSON-RPC transport layer, and lets server-injected capability subsets reduce
but never expand the role's authority. Application arguments remain recursively
closed, arbitrary authority fields are rejected, and the subset is receipt-bound.

Fresh Hermes qualification command `hermes-tool-qualification-009` passed with:

```text
harness                 hermes 0.21.3 (6a627e6…)
native_session_id       20260927_004604_5c0eec
generation              tool-qualification-1
audited calls           1 (mcp__vigil__project_read only)
native idle observed    true
post-retirement call    stale rejected
project observed        7daa3f7bf425433853c3f1bda099077e
```

The read-only adversarial reviewer first found that merely requiring
`project.read` did not reject additional exposed tools. After the capability
reduction and exact-one-tool assertion, its second pass reported no findings. This
is native Hermes tool integration evidence, not production dispatch qualification
or Stage acceptance. Native Codex remains pending on a contained supported
ChatGPT-subscription route.

Native macOS validation used a local Git bundle with SHA-256
`bf3c75cdd6ea0bed1be3382a5d9e40d3e91280445ec57719e19a71d6df3ce436`.
The bundle was reverified on `Simones-MBP.home`, cloned into a fresh temporary
directory, and detached at exact commit
`959eaafe8165e45e2805c3089f66bac2739c0617`. Darwin 25.6.0 arm64, macOS 26.6.2
(25G83), Go 1.27.1 passed:

```text
make check                                             PASS
make check-race                                        PASS
make build                                             PASS
make docs-check                                        PASS
make build-boundary                                    PASS
make cross-build (linux/amd64, linux/arm64,
                  darwin/amd64, darwin/arm64)          PASS
```

The first source-archive run reached every package but its documentation test
correctly rejected the absent Git object database; the history-bearing bundle
rerun passed. The fresh Mac checkout downloaded public Go modules before testing;
no model, paid API, Docker, hosting or publishing action ran there. The normal Mac
checkout and Cardtracker checkout were not changed. Native success does not close
the inherited Darwin fork-accounting observation at
`internal/checks/runner.go:459` or qualify the production crash matrix.
The remote temporary checkout/cache and both local transfer archives were removed
and verified absent after validation; Go module-cache read-only modes required
making only that temporary tree owner-writable before removal.

Linux focused validation for the fix passed before commit:

```text
go test ./internal/tools ./internal/mcp ./internal/spike  PASS
make check                                                PASS
```

## 2026-09-27 actionable quality-control follow-up

Commit `669467e` adds visibly focused task actions without adding a UI-owned state
machine. `h` records the existing human-acceptance decision, `m` records Pass for
the exact manual criterion rendered beside the task, and `t` invokes the existing
core acceptor through a registered coordinator owner. All three run outside the
Bubble Tea input loop and retain pending/success/failure feedback. The displayed
task revision is included in the receipt arguments; human/manual writes recheck it
inside their command transactions, while acceptance also preserves its final
scope, authority-epoch and task/plan revision fencing.

The first read-only review found a check/use race and an ambiguous first-manual-
criterion action. After the transactional expected-revision field and explicit
rendered criterion binding were added, the reviewer reported no findings. It also
confirmed that the Inbox now says native clarification is unavailable. This is an
intentional partial boundary: there is no production supervisor driver that can
persist a native request and let the owning dispatcher deliver the exact answer,
so Vigil does not offer a fake UI-only response.

Focused Linux tests passed:

```text
go test ./internal/tui ./internal/core ./internal/cli ./internal/quality  PASS
make check                                                            PASS
```

Because `669467e` changes native terminal and SQLite quality paths, the earlier
`959eaaf` Mac result was not reused as coverage. A second Git bundle (SHA-256
`698bb5fd349c26970affb049620684d8565738abf8672b3c8026c8205e9b39ea`)
was reverified on the same host and detached at exact commit
`669467e49b40b634e9104fda972854dae261b104`. Native `make check`,
`make check-race`, `make build`, `make docs-check`, `make build-boundary` and
`make cross-build` all passed. The fresh isolated cache downloaded public Go
modules; it made no model/provider call. The remote tree/cache and local bundle
were removed and verified absent, and the normal Mac checkout was untouched.

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

## 2026-09-27 interactive completion

Commit `bdd6e33` closes the remaining offline checkpoint-B interaction boundary:

- `planning.proposal.decide` records exact immutable proposal rejection or a
  replacement-revision request without applying a plan. Fixture tests separately
  exercise approve, reject and revision-request followed by revision-2 approval;
- Inbox renders proposal identity/revision/digest and bounded sanitized native
  prompt context. PTY keys keep proposal, permission, recovery, clarification and
  quality decisions distinct;
- exact/fresh/remain-blocked recovery binds the visibly displayed request, run and
  task revision and rejects expired, stale or foreign generations;
- native clarification persistence reloads the authoritative run/session/task,
  binds owner, generation and opaque request key, persists intent before delivery,
  and never automatically replays uncertain delivery;
- explicit reconciliation accepts only adapter-observed `delivered` or
  `proven_not_delivered`. A replacement owner must first fence the prior owner's
  OS lock and quarantine its authority, preventing inspection while an old delivery
  remains live. Proven non-delivery transfers the request binding but not its old
  answer;
- `dashboard --synthetic-interactions` is an explicit application path only for
  synthetic runs whose every repository marker and physical/common-Git identity
  revalidate. The Linux CLI/PTTY test runs the real Cobra command through a
  persisted request and `InteractiveOwner` to a durable
  `fixture_native_clarification_delivered` event. This is labelled fixture mechanics,
  not a real human or provider decision.

The required read-only adversarial review initially reported five P1 and three P2
findings, then four and one follow-up findings. Fixes added displayed recovery
binding, current-generation validation, authoritative prepared-run reloads,
prompt/proposal rendering, guarded outcome writes, exclusive owner control,
delivery-side reconciliation proof, prior-owner lock fencing, physical fixture
identity checks and the persisted CLI/PTTY test. The final follow-up reported no
remaining P1/P2 findings. This review is not independent Stage acceptance.

Linux/WSL2 x86_64 validation with pinned Go 1.27.1 after `bdd6e33`:

```text
go test ./internal/core ./internal/supervisor ./internal/tui ./internal/cli ./internal/coordinator  PASS
go test ./internal/cli -run 'TestDashboardSynthetic|TestSyntheticInteraction' -count=1 -v     PASS
make check                                                                                      PASS
make check-race                                                                                 PASS
make build                                                                                      PASS
make docs-check                                                                                 PASS
make build-boundary                                                                             PASS
make cross-build (linux/amd64, linux/arm64,
                  darwin/amd64, darwin/arm64)                                                   PASS
git diff --check                                                                                PASS
```

No model/provider, paid, Docker, hosting, publishing or Cardtracker mutation was
performed.

The exact implementation commit was then transferred as a complete-history Git
bundle with SHA-256
`c32a3b90a4873409130faaf079b37dcd5d5e00f45148e4bf25b429b974150516`.
The bundle digest was reverified on `Simones-MBP.home`, and exact commit
`bdd6e3341fcbd88e773b885ce71ec487bd6cf79f` was detached in an isolated
temporary checkout. Darwin 25.6.0 arm64, macOS 26.6.2 (25G83), Go 1.27.1 passed:

```text
make check                                             PASS
make check-race                                        PASS
make build                                             PASS
make docs-check                                        PASS
make build-boundary                                    PASS
make cross-build (linux/amd64, linux/arm64,
                  darwin/amd64, darwin/arm64)          PASS
git diff --check                                       PASS
```

The initial non-login-shell invocation omitted Homebrew's Go directory and failed
before executing tests; rerunning with the installed Go 1.27.1 path produced the
results above. The fresh isolated cache downloaded public Go modules. The remote
checkout/cache and bundle plus the local transfer bundle were removed and verified
absent. The normal Mac checkout and Cardtracker checkout were not accessed or
changed.

The first focused test invocation inside the restricted filesystem could not use
the existing Go build cache; the identical offline command passed with build-cache
access. The final restricted `make check` attempt was likewise denied an existing
local IPv6 `httptest` listener; the identical full gate matrix passed when rerun
with loopback access. No external network, model, paid, Docker, hosting or
publishing call was made. Cross-build is compile coverage only; the exact-commit
native macOS runtime result is recorded above and does not close the inherited
Darwin fork-accounting observation.

## Limitations and blockers

See the [blocker log](blockers.md). Stage 5.5 offline implementation is complete at
`bdd6e33` and remains unaccepted pending independent review. Production dispatch,
production model/reviewer and native Codex qualification, real project decisions,
the live crash matrix and the Darwin fork-accounting observation remain disabled
future gates. Native Hermes one-tool integration and the Stage 5.5 macOS suite are
demonstrated.
