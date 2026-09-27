# Stage 4 specification review record

<!-- vigil-tier: evidence -->

Completed 2026-09-20, after preparing both stage plans and incorporating the Linux Stage 3 findings. Deliverables are specification and executable schema validation, not a production scheduler or database installation.

- [Core specification](../../core/core-spec.md): package ownership, identities/revisions, transactional commands, state transitions, readiness/acceptance, configuration and capability rules, approvals/revocation, scheduling/time accounting, shared coordination, execution boundaries, branches/checkpoints, quality/delivery and retention.
- [Project schema](../../spec/project.sql) and [coordination schema](../../spec/coordination.sql): separate SQLite designs with explicit integrity constraints and cross-database reconciliation. They are loaded only into temporary test databases.
- [Stage 5 backlog and tool contracts](../../plans/stage-5/stage-5-plan.md): ten dependency-ordered slices, bounded model-facing contracts and explicit coverage of every R01–R71 ID. R38/R43 intentionally share the branch rule.
- [Stage 3.5](../../history/stage-3.5-macos.md): user-requested Mac setup/runtime qualification, scheduled after this work. No remote access or host runtime was installed.

## Review results

The design walkthrough covered normal task completion; clean native resume; controller death with surviving Hermes writers; stale/revoked approvals; task changes invalidating review; concurrent overlapping projects; mixed user/agent edits; failed multi-repository snapshot; partially applied restore; uncertain push/request outcomes; and failed narrative finalization after accepted development. Each has a named persisted state, preserved evidence and a permitted next action.

Key tightening decisions:

1. Native completion and task acceptance remain separate; writer state is independent from both.
2. Stale process locks/heartbeats never authorize eviction of unknown writers. Quarantine persists in host coordination after a controller crash.
3. Stage 3's current native profiles do not qualify for strict editing/delivery. A tested process/filesystem/credential/network boundary gates production dispatch; read-only core implementation can proceed independently.
4. Project restrictions dominate every grant, including explicit global grants. Once grants cannot be reassigned after reservation. A shared effect-start record handles global-grant revocation across project DBs; cross-DB crashes remain reconcilable uncertainty, not atomicity claims.
5. Checks/review/manual evidence bind revisions plus code fingerprints. Baseline failure approval preserves an unhealthy indicator. Human acceptance waivers never mark manual checks passed.
6. Checkpoint sets are verified across every repository before any clear. Destination snapshots and per-path comparisons preserve mixed edits; incomplete restore remains blocked with both recovery copies.
7. Time allowances carry across retries and roles. Numeric defaults are configurable design proposals; no user setting was changed. Unknown costs cannot satisfy a claimed hard monetary ceiling.
8. Factual archives precede model narratives; summary failure cannot rerun accepted development or block an explicitly authorized draft delivery.

## Executable validation

`make check` loads both SQL drafts using Vigil's bundled SQLite **3.53.4**, with foreign keys enabled. The installed system Python SQLite is older (3.34.1); it was not used to validate STRICT-table drafts.

Meaningful invariant checks cover: single active plan/run; cross-plan and self dependency rejection; nonexistent task revisions; immutable config/task revisions/events; duplicate native event sequence rejection; nonnegative/open time segments; consumed once-grant reuse; manual Pass without evaluator; nondraft delivery; exact/quarantined workspace collision; endpoint capacity and ticket identity; reservation immutability; explicit global-scope choice; transactional rollback preserving quarantined capacity; and SQLite integrity/foreign-key checks.

These database tests do not replace command-level authorization/state-machine tests. Multi-edge dependency cycles, ancestor-path overlap, full approval/resource matching, evidence completeness, OS containment and external-effect reconciliation remain implementation tests in the Stage 5 backlog. The schema is an integrity layer, not an authorization engine.

Passed normal Go checks, race checks for concurrency code, build and Linux/macOS amd64/arm64 cross-builds. Documentation links, research JSON, script syntax, known-credential absence and complete requirement-ID coverage were checked. No production application tables, workflow daemon, hosting action or user configuration was introduced.

## Handoff

Both requested stages are complete at their stated scope: Linux investigation with explicit failed/unqualified guarantees, then a concrete core specification. Remaining live qualification includes interrupted/corrupt native histories, live Codex clarification, broad auxiliary/egress boundaries and arbitrary descendant cleanup. Mac qualification is Stage 3.5 with the returning user. Next implementation is Stage 5 A–C; production editing remains gated by D and the relevant platform evidence.
