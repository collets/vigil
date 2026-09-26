# Stage 5.1–5.7 execution plans

Prepared 2026-09-20 against foundation commit `930ed37`. These seven plans organize the **remaining** Stage 5 work; they do not mark it implemented or move unfinished requirements into Stage 6. Recheck the checkout and newer evidence before starting.

## Why these seven slices

Keep all seven. Each has a distinct result and acceptance boundary. The larger slices have internal checkpoints suitable for separate commits and context resets. The original [A–J backlog](../stage-5-plan.md) remains the requirement coverage map; [core-spec.md](../core-spec.md) remains normative. These plans supply the next execution order and handoffs. If implementation evidence conflicts with the design, record the discrepancy instead of silently dropping requirements.

| Plan | Result | Original work | Dependencies for completion |
| --- | --- | --- | --- |
| [5.1 Execution qualification](5.1-execution-qualification.md) | Trusted evidence for an exact harness/profile/platform boundary | B, C, D | Existing foundation; consumes final launch integration from 5.2 |
| [5.2 Repository setup and execution](5.2-repositories-and-execution.md) | One durably journaled task, with safe repository preparation | B, C, E | 5.1 for live dispatch; 5.3 for save/restore dirty-work choices |
| [5.3 Recovery and controls](5.3-recovery-and-controls.md) | Safe stop/restart, checkpoints, restoration and cumulative limits | C, E, F | 5.2 identities/journals; 5.1 for real process qualification |
| [5.4 Quality and acceptance](5.4-quality-and-acceptance.md) | Actual checks, fresh review, bounded repairs and human acceptance | B, G | 5.1–5.3 for live execution and safe repair |
| [5.5 Interactive workflow and planning](5.5-workflow-and-planning.md) | Usable foreground controls and Markdown-to-approved-plan flow | H, I | 5.2–5.4 commands; 5.1 for model-backed planning |
| [5.6 Delivery and finalization](5.6-delivery-and-finalization.md) | Authorized delivery, factual archive and independent finalization | J | 5.2–5.4 evidence/authority; 5.5 workflow integration |
| [5.7 End-to-end qualification](5.7-end-to-end-qualification.md) | Demonstrated accepted milestone on supported platforms | A–J integration | 5.1–5.6; real human/model/delivery gates |

The numbers express a working order, not permission to bypass a dependency. Build 5.1's boundary/evidence contract first; build 5.2's journal against synthetic workers next; then close their shared real-launch qualification gate. This resolves their integration dependency without pretending either alone proves production safety. Likewise, 5.2 can initially support a clean disposable checkout while 5.3 adds saved-work handling. Keep those paths visibly unavailable until implemented.

Current implementation status: 5.1's offline evidence ledger, eligibility API, Codex images, admission and lifecycle contracts are implemented; R1–R4 are closed. Stage 5.2 checkpoints A–D and R1–R9 fixes are independently accepted offline at `d34f894`. Stage 5.3 is independently accepted offline through `a182152`, with R1–R10 closed. Stage 5.4 is independently accepted offline at `cba322b`: R2 and R11 are closed, and R10 with R1/R3/R4/R5/R6/R7/R8/R9 remain closed. P3 findings F1–F3 are remediated at `99cd6c0` and await independent follow-up; the Darwin fork-accounting limitation remains open. See [Stage 5.4 results](../research/stage-5.4-results.md). Live contained Codex/reviewer qualification, provider-idle proof, shared capacity authority, native macOS validation and the live recovery matrix remain pending. Production dispatch remains disabled.

Suggested sequence: 5.1 offline work → 5.2 offline work → 5.3 offline recovery → 5.4 → 5.5 → 5.6 → 5.7, with joint live qualification kept as a separate gate. If a live gate needs the user, continue the next plan's explicitly independent work. Do not mark a live or review gate complete from local implementation evidence.

## Shared operating contract

Each plan repeats its critical limits and links here for the full handoff protocol. A fresh agent should:

1. Work in the Vigil repository (current Linux path `/home/scoletta/development/scdeveloper/vigil`). Inspect `git status`, recent commits and applicable `AGENTS.md` instructions. Preserve existing uncommitted work.
2. Read the selected plan, its listed references, [pending decisions](../pending-decisions.md), [current CLI](../stage-5-cli.md) and latest [results](../research/stage-5-foundation-results.md). Existing docs contain chronological evidence; later observations can supersede earlier pending notes.
3. Read the named implementation files before proposing new packages. Treat `docs/spec/*.sql` as design drafts, and `internal/store/migrations/` as installed schema history. Never edit an applied migration to add new behavior. Before a second schema version, extend the current migration runner and test upgrade from a populated v1 database, digest/version rejection and rollback. Destructive migration needs the core's consistent-backup/recovery procedure.
4. Establish `make check` before implementation. Mutating handlers use command receipts, expected revisions, closed bounded inputs and atomic events. External effects happen outside SQLite transactions, with durable intent and uncertainty reconciliation. Update race coverage when adding concurrent packages.
5. Implement one internal checkpoint at a time. Run focused tests, then `make check`, applicable `make check-race` and `make build`. Run `make cross-build` for platform changes and native Mac checks for filesystem/process/locking behavior. Default tests must not launch models, contact hosting or require Docker. Explicitly report skipped opt-in suites.
6. Record commands, platform/runtime/harness identities, actual results and limitations in a durable result document under `docs/research/`; link it from the selected plan. Do not rely solely on `.cache`, a temporary Mac copy or chat. Leave a reviewable diff/local commit under existing authorization; never push by inference.
7. Update the selected plan's checkboxes, parent status and `docs/next-steps.md`. Handoff must state last completed checkpoint, next exact action, unresolved gates, test evidence and any owned running resources/recovery records. Do not mark completion from code presence alone.

### Machine and spending constraints

- No additional spending, paid API calls, purchases, subscription changes, destructive host configuration or unapproved publication. Local implementation, fixture tests and existing local llama inference are authorized. Unknown provider cost stays unknown; do not silently substitute a paid route.
- Confirmed Stage 5.1 choice: bounded existing Codex subscription usage is authorized after verifying no additional charges; otherwise defer live Codex and continue independent work. Do not ask again for included-usage permission.
- GPT qualification tests use `gpt-5.6-luna` with low reasoning effort to reduce usage. Repository spike templates now select it; future Stage 5 test profiles must do likewise. Verify effective selection and do not automatically fall back to a larger model. This is a test-call setting, not an instruction to change the implementing agent's model.
- Confirmed Stage 5.2 choice: demonstrate repository setup/execution only in disposable repositories, with agent-selected fixture branches, checks and conservative budgets. Defer real-project setup choices.
- Confirmed Stage 5.3 choice: destructive crash/checkpoint/clear/restore tests are authorized only in agent-owned disposable fixtures. Preserve real checkout changes and defer ambiguous real recovery. No further fixture-test permission is needed.
- llama.cpp runs on **native Windows**, accessed from WSL through localhost. `OPENAI_BASE_URL` and `OPENAI_API_KEY` are existing credential sources; inspect presence, never print values or store them in docs/events. Do not start a new model server.
- Mac access was authorized at `simonecoletta@192.168.0.203`; verify availability before use. OrbStack supplies Docker. Use an isolated temporary source/fixture directory, preserving the user's checkout. Mac model access previously used a temporary SSH tunnel, now closed. Recreate only when needed and clean up only resources created for the test.
- WSL Docker and Mac OrbStack availability is not qualification. Workers receive no host Docker socket, SSH agent, publishing credentials or writable application state. No broad reset/clean/stash, force push, merge or automatic replay of uncertain native submissions.
- Cross-host routes to one physical llama endpoint must not establish independent capacity authorities. Until a supported shared authority exists, explicitly block concurrent cross-host eligibility; a test tunnel is not a scheduler guarantee.

## User input and autonomous work

| Plan | Work available without user input | Possible user gate |
| --- | --- | --- |
| 5.1 | Images, relay/admission changes, qualification storage, synthetic tests; local Hermes tests when services are available | Codex authentication/provider path with established no-extra-spend eligibility; runtime/Mac availability if lost |
| 5.2 | Repository/journal/authority implementation and disposable fixture execution | Real repository enrollment, bases/branches, dirty-work choice, profile/policy/budgets |
| 5.3 | Recovery, snapshot/restore algorithms and failure tests in disposable repositories | Restore/reconcile a real checkout when exact authorization or ambiguity resolution is needed |
| 5.4 | Check/review/repair/manual-gate implementation and fixtures | Real check definitions, baseline exceptions, manual functional outcomes and configured human acceptance |
| 5.5 | UI, scheduler, typed tools and planning fixtures | Approve the real spec/plan/criteria and resolve actual clarification requests |
| 5.6 | Local commits/bare-remote/fake-hosting tests, archive/export/retention implementation | Exact real push/draft destination and scoped authority; credentials if unavailable |
| 5.7 | Test harness, scenarios, synthetic failure matrix, local evidence | Live qualified profiles plus real human/manual decisions and authorized draft delivery |

No immediate answer is required to begin implementation. A plan may be autonomous to implement yet still require user participation to demonstrate its real-world acceptance. Record such gates in [pending decisions](../pending-decisions.md), with the exact action/resource needed; do not ask for broad blanket approval or count a fixture's simulated approval as the user's real decision.

## Completion tracking

- [ ] 5.1 qualification and trusted eligibility complete.
  - Implemented: offline qualification/admission/lifecycle contract and two-platform containment matrix. Pending: live Codex/provider-idle and 5.2 integration.
- [ ] 5.2 repository preparation and persisted execution complete.
  - Implemented and independently accepted offline: checkpoints A–D, R1–R9 remediation through `d34f894`, synthetic crash matrix and disposable CLI execution on WSL/native Mac. Pending completion gate: qualified live combinations.
- [ ] 5.3 recovery and controls complete.
  - Implemented and independently accepted offline through `a182152`: durable controls, verified multi-repository checkpoints, scoped clear/restore, exact/fresh recovery and cumulative budgets; R1–R10 are closed. Pending completion gate: qualified live recovery combinations.
- [ ] 5.4 quality and acceptance complete.
  - Independently accepted offline at `cba322b`: R2 and R11 are closed, and R10 with R1/R3/R4/R5/R6/R7/R8/R9 remain closed. Supervisor-owned readiness/cleanup proof, fail-closed supervisor loss, pidfd-bound descendant signals, coordinated cancellation and explicit configuration-resource retirement preserve the earlier fixture-only checks, copied modes, integrity, accounting and acceptance fences. P3 findings F1–F3 are remediated at `99cd6c0` and await independent follow-up. Open separately: the Darwin fork-accounting limitation. Pending completion gates: qualified live review/runtime routes, native macOS validation and real user/manual decisions.
- [ ] 5.5 interactive workflow and planning complete.
- [ ] 5.6 delivery and finalization complete.
- [ ] 5.7 accepted milestone demonstrated, limitations recorded.

These are completion gates, not implementation progress percentages. Stage 6 remains inactive until the agreed Stage 5 milestone is satisfied or the user explicitly revises scope.
