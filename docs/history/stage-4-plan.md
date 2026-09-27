# Stage 4 — application core specification plan

<!-- vigil-tier: history -->

> **Superseded record.** Kept for provenance only. See
> [`README.md`](README.md) in this folder for what replaced it; do not
> implement from or cite it as current behavior.

Prepared 2026-09-20 alongside Stage 3 before implementation; specification completed the same day. This stage produces implementable architecture/contracts and a sequenced Stage 5 backlog, **not** the scheduler/dashboard itself. Resolve routine design choices using R01–R71 and Stage 3 evidence; keep unsupported runtime promises gated.

## Decisions that do not require the user's return

- Keep the accepted Go/SQLite/foreground/sequential architecture. Select mechanisms and conservative configurable numerical defaults as design decisions; do not change existing global configuration.
- Require explicit profile/model-policy selection at project readiness instead of silently selecting a paid/default model policy. Approval-first remains the default.
- Keep hard gated-action requirements intact. If native execution cannot enforce them, strict execution is unavailable until a tested boundary exists. Offer isolation designs and an implementation gate; no silent advisory substitute.
- Use Linux evidence now and retain macOS qualification as a release gate. Do not block specification on the absence of a macOS host.

## Deliverables and review order

1. **State and command contract.** Define project/plan/task/run/session identities, revision checks, task readiness, dispatch, checks, fresh review, acceptance, blocked work, pause vs stop, crash reconciliation, finalization and delivery. Include transition tables and adverse-event examples. Execution success never implies task acceptance. Commands have idempotency keys, expected revisions, and transaction boundaries.
2. **Storage specification.** Define SQLite tables, keys/constraints/indexes, migration/versioning policy, append-only event envelopes, immutable config snapshots, artifact manifests and file/DB crash ordering. Include approval, check/review, manual verification, checkpoint, coordination, budget and delivery records. Validate an executable draft schema in a disposable SQLite database; no application migration is installed in this stage.
3. **Configuration and capabilities.** Specify precedence and merge rules, role/profile eligibility, instruction references, secret references, evidence freshness and capability guarantee levels. Local-only applies to every inference route; offline egress is a separate option. Missing required guarantees block dispatch with actionable reasons.
4. **Approval and typed tool specification.** Resolve category/scope/resource/revision matching, project denial precedence, once/task/plan/project/global grants, expiry/revocation and in-flight actions. Define supervisor read/propose/reorder tools, approval-bound edits/split/merge, immutable acceptance criteria without explicit human change, structured execution/review results, and bounded artifact references. Native approvals do not confer application authorization.
5. **Scheduling, accounting and coordination.** Define deterministic eligible-task selection, one active plan/project, fair shared endpoint queue, canonical folder/common-Git identity, overlap checks, crash-safe ownership and stale-owner recovery without a daemon. Specify active-time segments, task cumulative allowances across roles/retries, excluded human/resource waits, repair vs infrastructure retry and conservative defaults. No replay after uncertain delivery.
6. **Repository and checkpoint design.** Specify plan branches, nested repository boundaries, dirty-work decisions, initial ownership manifests, staged/unstaged/untracked/ignored preservation, all-repository snapshot verification before clearing, explicit approved restoration, conflict retention and crash journaling. No managed worktrees, global stash ownership assumptions, or blind reset/clean. Unknown writers freeze mutation.
7. **Checks, review, delivery and retention.** Bind evidence to revisions and content digests; formalize baseline failures, blocking findings, manual Pass/Fail/Cannot verify and human rejection. Define application-owned commits/push/draft request creation, idempotent delivery reconciliation, factual archive first/narrative later, and 30-day transcript expiry without deleting unfinished recovery.
8. **Cross-document review and handoff.** Map every R01–R71 requirement to a contract, explicit deferral, or enforcement gate. Walk success, interruption, stale approval, mixed edits, multi-repository partial recovery and summary/delivery failure scenarios. List package boundaries and small Stage 5 implementation slices with executable acceptance checks. Update prior architecture/proposal status to avoid competing specifications; commit the Stage 4 deliverables.

## Exit criteria

- A developer can implement the narrow Stage 5 slice without inventing state transitions, permission precedence, ownership or recovery rules.
- The SQLite draft validates and expresses core integrity constraints; tests target corruption/invariant risks, not formatting.
- Requirements traceability has no unexplained omissions. Any unresolved product choice has a conservative explicit behavior and does not masquerade as accepted user configuration.
- Stage 3 limitations appear in dispatch/recovery/release gates. Hard enforcement, macOS evidence and deferred integrations remain visible.
- No production schema, background service, hosting action, or new application workflow is introduced by this specification stage.

## Completion evidence

Delivered [core specification](../core/core-spec.md), executable [project](../spec/project.sql) and [coordination](../spec/coordination.sql) drafts, and [Stage 5 backlog with full R01–R71 coverage](../plans/stage-5/stage-5-plan.md). Draft schema invariants are tested in `internal/storage/spec_test.go` against bundled SQLite 3.53.4, without installing application tables. Normal/race checks and four cross-builds pass. The [review record](../research/stage-4/results.md) lists scenario coverage and remaining qualification/setup choices.
