# Stage 5.2 repository and persisted-execution results

<!-- vigil-tier: evidence -->

Date: 2026-09-21. Initial implementation commits: `69de374`, `697b366`, `0e83d10`, `bef5dd4`, `cdb8f75`, plus macOS portability fix `88cd2be`. Astra review remediations are `0d02a9f` (repository revisions) and `b23ee5e` (execution fencing, confinement, budgets and qualification binding); results and handoff updates are separate local documentation commits. Baseline was clean at `b167e10`. No repository-level `AGENTS.md` applied.

## Verdict

Independent follow-up review of `d34f894`: **R6/R8 resolved; all R1–R9 findings closed. The Stage 5.2 offline implementation review is accepted.** Persisted budget enforcement, evidence preservation, meaningful submission coverage and wait/resume timing passed independent reproduction. See [follow-up acceptance](astra-review.md#independent-follow-up-acceptance-of-d34f894). Live production qualification remains pending and dispatch stays disabled.

Checkpoints A–D and the corrections for all six P1 and three P2 findings in the 2026-09-21 Astra review are implemented and verified offline. All nine review findings are now independently closed; the shared live qualification gate remains open. A marked disposable repository completed one persisted synthetic execution through explicit repository enrollment, branch preparation, resource ownership, runtime/native/submission journals, strict result validation and the `checking` transition. It did not create acceptance, run quality checks, publish, push or contact a model provider.

This is not live production qualification. Production dispatch still fails closed unless an exact Stage 5.1 qualification record contains `boundary_execution`, live `provider_idle`, `production_launch_recovery`, every Stage 5.2 recovery class and retained evidence for the exact runtime/image/profile/topology. No currently recorded combination meets that gate. The safe contained Codex ChatGPT route and independent provider-idle proof remain missing; the joint crash matrix has passed only with the synthetic fixture driver.

## Implemented behavior

### Repository setup

- `repository.enroll` records stable repository IDs, immutable revisions, canonical filesystem/common-Git identities, selected base ref/OID, plan branch, credential-free remote identity, nested boundaries, dirty-work choice and a content baseline.
- Baselines cover HEAD, symbolic HEAD, raw index bytes and tracked/untracked file modes and bytes. Exclusions are explicit; nested enrolled repositories are excluded from their parent's fingerprint and fingerprinted independently.
- Git observations clear ambient `GIT_*` overrides, global/system configuration, hooks, credential helpers, external diff and attributes-file overrides. Branch preparation uses exact `update-ref` plus `symbolic-ref`; it does not run checkout filters or overwrite worktree bytes.
- Existing unrelated branch names, moved bases, unsupported layouts, incomplete include scopes and dirty `clean` enrollment fail closed. `save` reports that Stage 5.3 is required. Included or postponed work is recorded but cannot be dispatched by this slice.
- Branch intent commits before mutation. An exact existing ref/HEAD is reconciled after a crash; a changed checkout is never switched.

### Storage, authority and resources

- Project schema migrations 3 and 4 add immutable repository revisions/fingerprints, branch operations, run snapshots, generations, execution effects, bounded normalized events, results, budgets and cross-database global-effect links. Coordination migration 2 adds exact global-effect identities and policy epochs. Historical migration files were not edited.
- Populated v3-to-v4 upgrade, digest rejection and injected rollback are covered. Existing databases apply every pending migration in one `BEGIN IMMEDIATE` transaction.
- Resource intent now snapshots and atomically claims the registered primary root plus every participating enrolled root before endpoint FIFO/slot acquisition. Same-owner replay retains claim and slot fencing generations.
- Global grant start serializes with global revocation in the coordination database. Project and coordinator operation IDs remain distinct. A coordinator start with no local observation reconciles to `uncertain`; it never authorizes a repeated external effect.

### Persisted execution

- Preparation stores immutable task/config/profile/budget/repository snapshots, expected revisions, stable run/generation/transport/runtime identities and an exact qualification request.
- Runtime create/start/attach, native create, prompt write, prompt acknowledgement, terminal observation, artifact publication, outcome commit and containment stop each have separate durable effect records.
- `starting` and `submission_state=writing` persist before the driver call. Delivery becomes `delivered`, `proven_not_delivered` or `uncertain`. `uncertain` is never submitted again by start or reconcile.
- Production start rechecks revisions, repositories, remaining budget and exact retained Stage 5.1 eligibility immediately before the first effect. Post-create production drivers must supply effective mounts for `ValidateMountsForRole` before start. No native call occurs inside a database transaction.
- Normalized events are bounded and generation/sequence keyed. Conflicting duplicates fail. Usage remains absent when unavailable; observed/estimated values require provenance and currency when cost is present.
- Result JSON is closed-schema, must report `completed`, and must exactly match actual repository dirty paths inside task scope. Completion also requires delivered submission and `contained_stopped` writer proof. The transition is only `running → checking`; acceptance remains Stage 5.4.
- Active time is process-monotonic with persisted checkpoints. Queue time has no segment. Human wait is excluded only after explicit native-wait proof. Open crash segments consume a conservative wall-clock bound; an unproven gap is recorded as unknown and still reduces remaining allowance.

### Astra review remediations

- Interrupted effects enter durable `executing` before their external call. Recovery requires trusted inspection and explicit non-occurrence before retry; failed inspection becomes/remains uncertain. A durable delivered generation short-circuits submission even if current inspection is unavailable.
- Dispatch requires the live coordinator owner and the exact persisted reservation, claims, roots, fencing generations, endpoint ticket and slot. The owner capability is held exclusively throughout dispatch so close/release/replacement cannot race effect authorization.
- Branch recovery loads the exact repository revision named by its journal. Fingerprint observation IDs include repository revision, allowing unchanged content to be enrolled under a new policy revision.
- Non-synthetic preparation binds qualification to the selected immutable implementation profile. Start rechecks every persisted run identity/limit/snapshot, actual checkout roots/layout/mount digest, endpoint and host capacity authority, then requires an independently derived driver input/route binding before calling `Engine.ExecutionEligibility`. No production driver currently implements that contract, so production dispatch remains fail-closed.
- Synthetic writes validate scope/protected/excluded paths before submission and use descriptor-relative no-follow traversal plus atomic replacement. Target symlinks are rejected; replacing a hardlinked target does not truncate the outside inode.
- Remaining active allowance supplies deadlines to create/start/attach/native-create/submit/lease/await calls and is checked before result persistence. Proven human/resource waits are excluded consistently and wait transitions are atomic.
- Containment intent reaches durable `executing` before `Stop`. An explicitly reported bounded emergency stop remains the safety policy if storage is unavailable.
- Final success is budget-gated inside the outcome transaction from the persisted ledger/run limits and live segment. Reconciliation retains over-budget terminal evidence without producing a completed result or `checking` transition.
- Await keeps wall timeout separate from active allowance. Proven wait transitions suspend/recompute the active timer while wall timeout and lease renewal continue.

## Disposable CLI execution

Environment: Linux `6.18.33.2-microsoft-standard-WSL2` x86_64, Go `1.27.1`, Git `2.30.2`, Vigil `0.1.0-dev`.

An agent-owned repository under `/tmp/vigil-stage52.40gHXU/work` was initialized with a committed `.vigil-disposable-fixture` marker and `src/.keep`. It had no remote. The explicit plan branch was `vigil/stage-5.2-demo`; task and attempt limits were 30 seconds and 60 seconds.

Observed durable identities:

- project `258696c4d1008d7225cdd39aa0ca1f09`;
- branch operation `40c78d1141dd91059853ff28d7e9bbb082f99be39f302c27121a185f4b33ca6d`;
- run `2f09a65703a64137a4636a6347edfec6`;
- generation `e94e3ce5e6eb2979bf5f7c369f420a7f`;
- transport generation `eda622150d9321e008e39423e02267d7`.

The command sequence was project initialization; versioned configuration/profile/plan/enrollment; `prepare-repository`; endpoint registration; `execution-prepare --synthetic-fixture`; and explicit `execution-start --synthetic-fixture`. Final inspection reported:

- run `completed`, generation `terminal`, writer `contained_stopped`;
- submission `delivered`, with distinct native session and turn IDs;
- all nine normal effects `observed`;
- changed path exactly `demo-repo:src/result.txt`;
- task `checking`, `accepted: false`;
- zero remaining workspace claims or endpoint slots; the ticket was `released`.

The fixture directory was removed after evidence was recorded. No real checkout was enrolled or modified.

## Native macOS validation

The exact committed source was transferred as a verified Git bundle to an isolated `/tmp` checkout on macOS 26.6.2 arm64 with Go 1.27.1. The first native `make check` failed all supervisor execution cases before submission: macOS canonicalized `/tmp` to `/private/tmp`, while the fixture driver compared its requested root to the enrolled canonical root as a literal string.

Commit `88cd2be` fixes that fail-closed portability defect. The fixture driver now resolves the current filesystem/Git identity, matches the enrolled root, root-node identity and common-Git identity, and performs the deterministic write through the canonical root. A permanent symlink-alias regression reproduces the same identity mismatch without depending on macOS.

On a fresh bundle at `88cd2be`:

- native `make check` passed; `internal/supervisor` completed in 17.686 seconds;
- native `make check-race` passed; `internal/supervisor` completed in 27.540 seconds;
- native `make build` passed;
- an isolated CLI repository enrolled and prepared branch `vigil/stage-5.2-mac`, then completed run `041fc811e2601bb30286c5fbfe13271a` with all nine effects observed, exact changed path `mac-repo:src/result.txt`, writer `contained_stopped`, task `checking` and `accepted: false`;
- the endpoint ticket was released and no workspace claims or endpoint slots remained.

No inference, provider, Docker qualification flag or credential was used. The temporary source, Go caches, state and fixture were permanently removed after making the Go module cache owner-writable; the normal Mac checkout remained clean at its prior commit.

The remediation commit `b23ee5e` was independently bundled with SHA-256 `80c37f4fb71b5f4bce4b20241b34d7fd69ed0e6bed6451ea16eec128148a6` and tested in a new isolated checkout on the same macOS 26.6.2 arm64 host with Go 1.27.1. Native `make check` passed (`internal/supervisor` 25.089 seconds), `make check-race` passed (`internal/supervisor` 38.247 seconds), and `make build` passed. Explicit native reruns of filesystem symlink/hardlink confinement, reservation-close serialization and active-submit-budget enforcement all passed. The temporary checkout and bundle were removed; the normal Mac checkout was not accessed or changed.

The follow-up `d34f894` bundle (SHA-256 `0d2308ad5c86111809a3346ad2143743ec1b8e79f5266dc13c1715ff77e6d427`) passed focused R6/R8 tests, full check, race check and build in another isolated checkout on the same Mac. The focused run proved the slow submission reached its driver, over-budget reconciliation stayed non-successful, late outcome commit failed, and active await timing suspended/resumed across proven waits. The temporary checkout and bundle were removed.

## Crash and recovery matrix

The table reports the permanent synthetic tests in `internal/supervisor`. Every crash case reopens the project database and reloads the prepared run. When the terminal result is known, reconcile persists it; otherwise a later explicit start continues only effects proven not to have happened.

| Crash/failure point | Reopened evidence / allowed next action | Duplicate external action |
| --- | --- | --- |
| Before/after repository branch intent | Prepared intent; exact ref inspection, then explicit prepare/reconcile | No unrelated ref rewrite; user bytes preserved |
| After workspace claim | Same owner resumes journal; all roots remain owned | No duplicate claim/fence |
| After endpoint enqueue/slot | Same ticket/generation resumes | No duplicate queue ticket or slot |
| After runtime create response | Stable resource exists; create becomes `reconciled` | Create count remains 1 |
| Interrupted create plus failed inspection | Durable `executing` becomes uncertain; retry is refused | Create count remains 1 |
| Ambiguous runtime create error | Effect stays `uncertain`; inspect finds owned resource and reconciles | No blind recreation |
| After runtime start | Inspect proves started; start becomes `reconciled` | Start count remains 1 |
| After attach | Inspect proves attached | Attach count remains 1 |
| After native create | Durable native session identity is recovered | Native create count remains 1 |
| Before prompt write | Inspection proves `not_attempted`; only a later explicit start may submit | No automatic submission |
| After prompt write / before acknowledgement | Delivered observation reconciles write and acknowledgement | Prompt count remains 1 |
| Delivered generation plus failed inspection on restart | Durable generation delivery reconciles write/ack without driver submission | Prompt count remains 1 |
| Ambiguous native submission | `submission_state=uncertain`, run `unknown`; allowed commands are inspect/reconcile/stop | Start and reconcile never call submit |
| After terminal observation | Terminal/result recovered from the same generation | No prompt replay |
| Before result persistence | Terminal result validates and persists on reconcile | No prompt replay |
| After artifact publication | Content-addressed publication receipt replays; outcome remains pending until reconciled | No duplicate manifest/effect |
| Before outcome commit | Result/artifact remain durable; atomic outcome transaction retries | No partial checking transition |
| After outcome commit | Completed run/result/checking state remains terminal on reopen | Reconcile cannot regress state |
| Normalized-event database failure | Dispatch stops; bounded containment runs; no result/checking transition | No continued dispatch |
| Output flood (>1000 events) | Batch rejected; writer contained; task remains unaccepted | No unbounded persistence |
| Open active segment after crash | Conservative elapsed bound moves to charged/unknown ledger | Remaining allowance never increases |
| Submission exceeds remaining active allowance | Deadline/checkpoint prevents completion and invokes containment | No completed result/checking transition |
| Reconcile delivered terminal result after budget exhaustion | Final transaction reads exhausted persisted ledger and refuses progression | Terminal evidence retained; no result/checking transition |
| Delay after artifact publication and before outcome commit | Live segment is checkpointed and atomically rechecked at commit | Late success cannot commit |
| Proven human-wait checkpoint beyond active allowance | Category remains excluded; returning to active resumes cumulative accounting | No false budget exhaustion |
| Proven wait begins during await | Active timer suspends and is recomputed on resume; wall/lease remain live | Wait time is not charged or treated as active timeout |
| Owner closes or resources quarantine | Exact live owner/claim/ticket/slot validation rejects dispatch; close cannot race an active hold | Driver submission is not called |
| Containment after phase failure | `containment_stop` is durable and `executing` inside `Stop` | No unjournaled normal stop |
| Symlink/hardlink fixture target | Protected/symlink target is rejected; hardlink pathname is atomically replaced | Outside inode bytes remain unchanged |
| Superseded enrollment during branch recovery | Original journal loads its exact repository revision and branch | New enrollment is never substituted |
| Unchanged repository re-enrollment | Revision-scoped observation identity records the new enrollment | No fingerprint primary-key collision |
| Revocation before global effect start | Coordinator transaction rejects start | No authorization row/effect |
| Revocation after shared start | Existing exact authorization remains in-flight; new effects are denied | Same identity only reconciles |
| Missing/corrupt qualification artifact | Stage 5.1 eligibility returns unsupported | Production driver is not called |

Permanent tests also cover nested enrollment, branch collision/base drift, dirty tracked/staged/untracked bytes, saved-work rejection, scope mismatch, common resource fencing, stale policy and conflicting normalized events.

## Validation

- Starting baseline: `make check`; pass before changes.
- Final Linux `make check`; pass, including the permanent Astra regression tests.
- Final Linux `make check-race`; pass with `internal/supervisor` in race coverage.
- `make build`; pass.
- `make cross-build`; pass for Linux/macOS amd64/arm64.
- Focused `go test ./internal/supervisor ./internal/core ./internal/coordinator ./internal/cli`; pass.
- Synthetic crash matrix: eleven reopen points plus uncertain create/submission, persistence failure, output flood and budget cases; pass.
- Disposable CLI execution described above; pass.
- Native macOS 26.6.2 arm64 at `b23ee5e`: `make check`, `make check-race`, `make build` and explicit confinement/reservation/budget regression reruns; pass. The earlier disposable CLI execution at `88cd2be` also passed.
- Native macOS 26.6.2 arm64 at `d34f894`: focused R6/R8 tests, `make check`, `make check-race` and `make build`; pass.

No Docker/model/live-provider flag was enabled. No credential value was read or printed. No Codex subscription turn, llama.cpp turn, push, hosting request, purchase or publication occurred.

## Remaining live gates

1. Implement and independently review a scoped ChatGPT-compatible Codex route; verify effective `gpt-5.6-luna` with low reasoning and no additional charge. Unrestricted egress, account-home mounts and paid API fallback remain forbidden.
2. Produce trusted provider-idle evidence for every inference route, including auxiliary inference that could begin at native create/resume.
3. Run this exact create/start/attach/native/submit/result crash matrix through each advertised WSL and Mac runtime/harness/profile combination, with effective mount inspection and owned-resource discovery.
4. Establish one capacity authority for any Mac/WSL routes to the same Windows llama.cpp endpoint. Independent host-local authorities remain ineligible.
5. Stage 5.3 must add full stop/resume/checkpoint/save/restore user semantics. Stage 5.4 must run checks/review and own acceptance. This slice deliberately stops at `checking`.

Until all applicable gates pass and a new exact trusted qualification is recorded, production dispatch stays disabled. Synthetic success must not be entered as live qualification evidence.

## Security-sensitive Astra review scope

The original review report and inert reproduction sources are retained at [astra-review.md](astra-review.md) and [stage-5.2-review](review-probes/). Re-review should verify the R1–R9 resolution mapping above, particularly crash windows between native delivery and durable acknowledgement, exclusive reservation authority during effects, descriptor-relative filesystem races, active deadlines for context-insensitive drivers, and the new fail-closed production driver binding contract.

- Git command isolation, raw index/tree parsing, SHA-1/SHA-256 blob comparison, nested exclusions and races between pre-transaction observation and enrollment commit.
- Canonical-path and filesystem-identity handling, especially `/tmp` aliases, symlinked roots, replacement between inspection and effect, and the `88cd2be` fixture-driver fix.
- Migration 3/4 constraints/triggers and populated upgrade/rollback behavior.
- Multi-root reservation ordering, same-owner recovery and common-Git/ancestor overlap within one atomic claim set.
- Global grant creation/revocation policy epochs and the project/coordinator uncertainty boundary.
- Stable runtime naming and effect ordering, especially crash windows around native session persistence and prompt write/ack.
- Production eligibility completeness: exact claims/recovery classes, mount validation timing and retained evidence revalidation.
- Result path parsing/scope matching, normalized event bounds/deduplication, artifact authority/retention and the non-acceptance transition.
- Budget checkpoint concurrency, human-wait proof and conservative crash-gap accounting.
- CLI fixture marker and path validation; confirm it cannot become an implicit production/spike fallback.
