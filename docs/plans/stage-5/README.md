# Stage 5.1–5.7 execution plans

<!-- vigil-tier: plan -->
<!-- vigil-status: stage=5.6; stage_accepted=true; implementation_commit=b0a085b5551bf58ab8412e649405eef65a91f61d -->

Prepared 2026-09-20 against foundation commit `930ed37`. These seven plans organize the **remaining** Stage 5 work; they do not mark it implemented or move unfinished requirements into Stage 6. Recheck the checkout and newer evidence before starting.

## Why these seven slices

Keep all seven. Each has a distinct result and acceptance boundary. The larger slices have internal checkpoints suitable for separate commits and context resets. The original [A–J backlog](stage-5-plan.md) remains the requirement coverage map; [core-spec.md](../../core/core-spec.md) remains normative. These plans supply the next execution order and handoffs. If implementation evidence conflicts with the design, record the discrepancy instead of silently dropping requirements.

| Plan | Result | Original work | Dependencies for completion |
| --- | --- | --- | --- |
| [5.1 Execution qualification](5.1-execution-qualification.md) | Trusted evidence for an exact harness/profile/platform boundary | B, C, D | Existing foundation; consumes final launch integration from 5.2 |
| [5.2 Repository setup and execution](5.2-repositories-and-execution.md) | One durably journaled task, with safe repository preparation | B, C, E | 5.1 for live dispatch; 5.3 for save/restore dirty-work choices |
| [5.3 Recovery and controls](5.3-recovery-and-controls.md) | Safe stop/restart, checkpoints, restoration and cumulative limits | C, E, F | 5.2 identities/journals; 5.1 for real process qualification |
| [5.4 Quality and acceptance](5.4-quality-and-acceptance.md) | Actual checks, fresh review, bounded repairs and human acceptance | B, G | 5.1–5.3 for live execution and safe repair |
| [5.5 Interactive workflow and planning](5.5-workflow-and-planning.md) | Usable foreground controls and Markdown-to-approved-plan flow | H, I | 5.2–5.4 commands; 5.1 for model-backed planning |
| [5.6 Delivery and finalization](5.6-delivery-and-finalization.md) | Authorized delivery, factual archive and independent finalization | J | 5.2–5.4 evidence/authority; 5.5 workflow integration |
| [5.7 End-to-end qualification](5.7-end-to-end-qualification.md) | Autonomous end-to-end evidence in disposable offline scope | A–J integration | 5.1–5.6 |

**Stages 6–8 are outside this index.** The roadmap defined Stages 1–5 only; the user added 6, 7 and 8 by explicit scope revision on 2026-09-29. Start at [Stage 6 scope](../stage-6/stage-6.md), [Stage 7 scope](../stage-7/stage-7.md) or [Stage 8 scope](../stage-8/stage-8.md). All human-gated work deferred out of 5.6 and 5.7 now belongs to Stage 8.

The numbers express a working order, not permission to bypass a dependency. Build 5.1's boundary/evidence contract first; build 5.2's journal against synthetic workers next; then close their shared real-launch qualification gate. This resolves their integration dependency without pretending either alone proves production safety. Likewise, 5.2 can initially support a clean disposable checkout while 5.3 adds saved-work handling. Keep those paths visibly unavailable until implemented.

Current implementation status: 5.1–5.5 are independently accepted offline, and 5.6 is accepted for its autonomous scope at `b0a085b`. Stage 5.5 checkpoints A–D and review remediation are implemented through `84c0275` (checkpoint A `4dbb444`); narrow follow-up closes 5.5-R1–5.5-R7 and accepts 5.5-R8's authority-neutral boundary. Exact-commit native macOS full/race/build/documentation/boundary/cross-build gates and native Hermes one-tool qualification pass. Production Codex/reviewer qualification and real decisions remain disabled future production gates. Production dispatch remains disabled.

Suggested sequence: 5.1 offline work → 5.2 offline work → 5.3 offline recovery → 5.4 → 5.5 → 5.6 → 5.7, then Stage 6 → Stage 7 → Stage 8. Live qualification is no longer a Stage 5 gate: it is Stage 8's, together with every human decision deferred out of 5.6 and 5.7. If a live gate needs the user, continue the next plan's explicitly independent work. Do not mark a live or review gate complete from local implementation evidence.

## Shared operating contract

Each plan repeats its critical limits and links here for the full handoff protocol. A fresh agent should:

1. Work in the Vigil repository (current Linux path `/home/scoletta/development/scdeveloper/vigil`). Inspect `git status`, recent commits and applicable `AGENTS.md` instructions. Preserve existing uncommitted work.
2. Read the selected plan, its listed references, [pending decisions](../../process/pending-decisions.md), [current CLI](stage-5-cli.md) and latest [results](../../research/stage-5/foundation-results.md). Existing docs contain chronological evidence; later observations can supersede earlier pending notes.
3. Read the named implementation files before proposing new packages. Treat `docs/spec/*.sql` as design drafts, and `internal/store/migrations/` as installed schema history. Never edit an applied migration to add new behavior. Before a second schema version, extend the current migration runner and test upgrade from a populated v1 database, digest/version rejection and rollback. Destructive migration needs the core's consistent-backup/recovery procedure.
4. Establish `make check` before implementation. Mutating handlers use command receipts, expected revisions, closed bounded inputs and atomic events. External effects happen outside SQLite transactions, with durable intent and uncertainty reconciliation. Update race coverage when adding concurrent packages.
5. Implement one internal checkpoint at a time. Run focused tests, then `make check`, applicable `make check-race` and `make build`. Run `make cross-build` for platform changes and native Mac checks for filesystem/process/locking behavior. Default tests must not launch models, contact hosting or require Docker. Explicitly report skipped opt-in suites.
6. Record commands, platform/runtime/harness identities, actual results and limitations in a durable result document under `docs/research/`; link it from the selected plan. Do not rely solely on `.cache`, a temporary Mac copy or chat. Leave a reviewable diff/local commit under existing authorization; never push by inference.
7. Update the selected plan's checkboxes, parent status and `docs/process/next-steps.md`. Handoff must state last completed checkpoint, next exact action, unresolved gates, test evidence and any owned running resources/recovery records. Do not mark completion from code presence alone.

### Machine and spending constraints

- No additional spending, paid API calls, purchases, subscription changes, destructive host configuration or unapproved publication. Local implementation, fixture tests, existing local llama inference, online implementation research and bounded use of the existing Codex ChatGPT included quota are authorized. Stop at included limits; unknown provider cost stays unknown and must never trigger a paid or extra-credit fallback.
- Confirmed Stage 5.1 choice, clarified 2026-09-26: bounded existing Codex ChatGPT subscription usage and necessary online research are authorized. Verify the effective client uses ChatGPT login rather than a metered API key, inspect remaining included usage when available, and stop instead of purchasing or consuming additional credits. Do not ask again for this scoped permission.
- GPT qualification tests use `gpt-5.6-luna` with low reasoning effort to reduce usage. Repository spike templates now select it; future Stage 5 test profiles must do likewise. Verify effective selection and do not automatically fall back to a larger model. This is a test-call setting, not an instruction to change the implementing agent's model.
- Confirmed Stage 5.2 choice: demonstrate repository setup/execution only in disposable repositories, with agent-selected fixture branches, checks and conservative budgets. Defer real-project setup choices.
- Confirmed Stage 5.3 choice: destructive crash/checkpoint/clear/restore tests are authorized only in agent-owned disposable fixtures. Preserve real checkout changes and defer ambiguous real recovery. No further fixture-test permission is needed.
- llama.cpp runs on **native Windows**, accessed from WSL through localhost. `OPENAI_BASE_URL` and `OPENAI_API_KEY` are existing credential sources; inspect presence, never print values or store them in docs/events. Do not start a new model server.
- Mac access was verified over SSH at `simonecoletta@192.168.0.108` on 2026-09-27; verify the current address and availability before use. OrbStack supplies Docker. Use an isolated temporary source/fixture directory, preserving the user's checkout. Mac model access previously used a temporary SSH tunnel, now closed. Recreate only when needed and clean up only resources created for the test.
- WSL Docker and Mac OrbStack availability is not qualification. Workers receive no host Docker socket, SSH agent, publishing credentials or writable application state. No broad reset/clean/stash, force push, merge or automatic replay of uncertain native submissions.
- Cross-host routes to one physical llama endpoint must not establish independent capacity authorities. Until a supported shared authority exists, explicitly block concurrent cross-host eligibility; a test tunnel is not a scheduler guarantee.

## Identifier conventions

Two independent numbering schemes are in use, and mixing them is a real source of
misreading. Qualify review findings with their stage whenever the surrounding
sentence spans more than one stage.

| Form | Meaning | Defined in |
| --- | --- | --- |
| `R01`–`R71` (two digits) | Functional requirements | [`requirements.md`](../../core/requirements.md), mapped in [`stage-5-plan.md`](stage-5-plan.md) |
| `5.4-R2`, `5.2-R6` (stage prefix) | A finding of one stage's independent review | That stage's `docs/research/stage-5/5.N/astra-review.md` |
| `F1`–`F3` | P3 findings raised by the Stage 5.4 follow-up review | [`stage-5.4-astra-review.md`](../../research/stage-5/5.4/astra-review.md#independent-follow-up-of-cba322b) |

The same bare label means different findings in different reviews — `R1`–`R9`
exist in both the Stage 5.2 and Stage 5.4 records, and `R10` in both Stage 5.3 and
Stage 5.4. A per-stage document is self-scoping; a shared status document is not.

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

No immediate answer is required to begin implementation. A plan may be autonomous to implement yet still require user participation to demonstrate its real-world acceptance. Record such gates in [pending decisions](../../process/pending-decisions.md), with the exact action/resource needed; do not ask for broad blanket approval or count a fixture's simulated approval as the user's real decision.

## Completion tracking

- [ ] 5.1 qualification and trusted eligibility complete.
  - Implemented: offline qualification/admission/lifecycle contract and two-platform containment matrix. Pending: live Codex/provider-idle and 5.2 integration.
- [ ] 5.2 repository preparation and persisted execution complete.
  - Implemented and independently accepted offline: checkpoints A–D, 5.2-R1–5.2-R9 remediation through `d34f894`, synthetic crash matrix and disposable CLI execution on WSL/native Mac. Pending completion gate: qualified live combinations.
- [ ] 5.3 recovery and controls complete.
  - Implemented and independently accepted offline through `a182152`: durable controls, verified multi-repository checkpoints, scoped clear/restore, exact/fresh recovery and cumulative budgets; R1–R10 are closed. Pending completion gate: qualified live recovery combinations.
- [ ] 5.4 quality and acceptance complete.
  - Independently accepted offline at `cba322b`: 5.4-R2 and 5.4-R11 are closed, and 5.4-R10 with 5.4-R1/5.4-R3/5.4-R4/5.4-R5/5.4-R6/5.4-R7/5.4-R8/5.4-R9 remain closed. Supervisor-owned readiness/cleanup proof, fail-closed supervisor loss, pidfd-bound descendant signals, coordinated cancellation and explicit configuration-resource retirement preserve the earlier fixture-only checks, copied modes, integrity, accounting and acceptance fences. P3 findings F1–F3 are remediated at `99cd6c0` and await independent follow-up. Open separately: the Darwin fork-accounting limitation. Pending completion gates: qualified live review/runtime routes, native macOS validation and real user/manual decisions.
- [x] 5.5 interactive workflow and planning complete offline.
  - Independently accepted offline at remediation commit `84c0275`: checkpoint A at `4dbb444` implements migration 015, the explicit ranked queue and shared dispatch gate. Migration 016 adds bounded crash-reconcilable planning; fixture-gated proposal/recovery/clarification workflows, native Hermes one-tool isolation and exact-commit native macOS gates pass. 5.5-R1–5.5-R7 are closed and 5.5-R8's remaining explicit-retirement observation is accepted as non-authorizing. Production/live/real-decision gates remain separate and disabled.
- [x] 5.6 delivery and finalization complete for its **autonomous** scope.
  - **Accepted at `b0a085b`**, narrowed 2026-09-29 to implementation,
    independent review and validation only. The delivery, finalization, archive
    and retention paths are implemented, reviewed across eight antagonist rounds,
    and validated natively on macOS 26.6.2 arm64 including native `make
    check-race`. Every human-gated operation (real push, real draft, delivery
    attestation, retention expiry, the `delivery-cancel`/`delivery-reconcile`/
    `retention-expire` commands, human narrative review) is deferred to
    [Stage 8](../stage-8/stage-8.md). This acceptance is **not** evidence that
    any of those paths works against a real remote or a real hosting provider;
    that evidence does not exist yet.
- [ ] 5.7 autonomous end-to-end evidence produced, limitations recorded.
  - **No longer closes Stage 5**, and no longer claims the milestone is
    demonstrated. Its human steps moved to
    [Stage 8](../stage-8/stage-8.md); a partially completed walkthrough recorded
    honestly is the acceptable outcome, a walkthrough reported as passed when it
    did not happen is not.
- [ ] Stage 6 terminal interface parity and its complete feature list.
- [ ] Stage 7 user-facing documentation website.
- [ ] Stage 8 human review of the product, with a bulk finding report.

These are completion gates, not implementation progress percentages.

**Stage 6 is active by explicit user scope revision (2026-09-29), not by Stage 5
completing.** The original rule — Stage 6 inactive until the agreed Stage 5
milestone is satisfied — was superseded by the user, on the grounds that the
terminal interface is the primary interface (R11) and is far from ready, while
the deferred work is human work that cannot advance without a human anyway. The
milestone status is therefore decided in
[Stage 8](../stage-8/stage-8.md), not by 5.7.
