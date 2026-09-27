# Vigil application core specification

<!-- vigil-tier: core -->

Stage 4 design, 2026-09-20. Normative for the Stage 5 implementation backlog; it does not claim the core is implemented. Supersedes open mechanism choices in the earlier architecture/proposal documents while preserving [R01–R71](requirements.md). Runtime capabilities remain qualified by Stage 3 evidence and Stage 3.5 macOS checks.

## 1. Decisions and package boundaries

Use one foreground Go process as the owner of a project database and its execution context. UI sends typed commands to the core; adapters report observations. Only the core commits state changes. SQLite is a materialized state store with an append-only audit log, not an event-sourcing platform. No background agent daemon, arbitrary workflow graph, managed worktrees, automatic merging, or within-project parallel agents.

| Package | Responsibility | Must not own |
| --- | --- | --- |
| `internal/core` | Commands, transition guards, readiness, acceptance, revisions | Native RPC parsing or terminal rendering |
| `internal/store` | Transactions, migrations, immutable records, command receipts | Model decisions |
| `internal/policy` | Eligibility, grant matching, revision/resource binding | Native permanent allowlists |
| `internal/coordinator` | Host-local folder ownership, endpoint queues, quarantine | Agent loops or remote clients |
| `internal/supervisor` | Owned processes, time segments, cancellation, evidence ingestion | Task acceptance |
| `internal/harness` | Versioned native protocol and capability observations | App retry, budget or approval authority |
| `internal/workspace` | Repository map, branches, fingerprints, checkpoints | Broad reset/clean or global stash ownership |
| `internal/checks` / `review` | Check execution, findings, evidence freshness | Silent baseline waivers or repairs by reviewers |
| `internal/delivery` | Authorized commits/push/draft request reconciliation | Automatic merging |
| `internal/artifacts` | Bounded private files, manifests, retention | Secrets or competing task state |
| `internal/cli`, `internal/tui` | Commands, views, inbox | Mutable workflow state inferred from output |

Keep the current spike separate from production dispatch. Move tested primitives deliberately; its checkout-local lock, universal deny policy, single-use manifest, and tiny fixture are not a production scheduler.

## 2. Identity, revisions and commands

Generate opaque random application IDs for projects, repositories, plans, tasks, runs, sessions, requests, artifacts, checkpoint sets, grants and delivery operations. A task keeps its identity through repairs/reassignment. Each execution/review/supervision/check/finalization attempt has its own run and immutable profile/config snapshot. Native durable session, runtime session, native turn and transport generation are separate optional fields; an absent native field remains null.

Store project definition/policy versions, plan revisions and task revisions. Every revision stores a content digest and immutable definition. A run references exactly the versions it executed. File evidence references a repository-set fingerprint in addition to the task revision. A metadata reorder increments the plan revision but does not itself invalidate unchanged code evidence; editing a task, relevant instructions/profile/check definitions, dependencies or code does. Store affected-task sets with each change.

Every mutating command carries `command_id`, actor (`human`, `core`, or restricted model-role capability), target IDs, expected revisions, and canonical arguments digest. In one `BEGIN IMMEDIATE` transaction: check the command receipt; reject reused ID with different digest; verify expected revisions and policy; apply state updates; append audit event(s); persist the command result. Commit before returning success. Replaying an identical completed command returns its receipt. Never make native/model/Git/hosting calls inside a database transaction.

External effects use an operation journal: `prepared → executing → observed|uncertain → reconciled`. Persist intent/config/authorization before effect; persist observation afterward. A crash between those steps creates uncertainty, not permission to repeat. Before retrying a commit/push/request creation, inspect the actual ref or hosting request using the stored identity. Native prompt submission has no automatic replay after its delivery becomes uncertain.

## 3. State machines and transition guards

Project state is `ready`, `paused`, `recovering` or `quarantined`; discovery/read-only inspection is allowed before readiness. Execution requires canonical ownership and complete profile/policy configuration. Quarantine records a reason and resources withheld; stale PID/heartbeat alone cannot clear it.

| Entity | Transition | Required conditions / effects |
| --- | --- | --- |
| Plan | `draft → ready` | Accepted specification revision, acyclic ready tasks, explicit model policy/profiles, approved checks and scope, branch/base choices, required gates configured |
| Plan | `ready → queued → active` | Explicit queue order; exactly one active plan; workspace/profile/budget eligibility rechecked at activation |
| Plan | `active → blocked` | No runnable independent task; retain actionable inbox causes |
| Plan | `active|blocked → paused` | Pause-scheduling reaches a boundary; active attempt may finish, but no new review/check/model run dispatches after the pause request |
| Plan | `paused|blocked → active` | Explicit continue, resources reconciled, remaining readiness valid |
| Plan | `active → verifying → finalizing` | All required tasks accepted, plan-wide requirements/checks/fresh review/manual gates satisfied |
| Plan | `finalizing → completed` | Factual archive durable, narrative/reference validation and configured acceptance satisfied |
| Plan | `finalizing → finalization_pending` | Summary/reference failure; development remains accepted; retry only finalization |
| Task | `draft → ready` | R34 fields present; profile eligibility and dependencies validated; no hidden question or unsatisfied manual prerequisite |
| Task | `ready → running` | Dependencies accepted, applicable grants, resource ownership, clean/reconciled baseline, remaining budget; persist attempt before submission |
| Task | `running → checking` | Implementation completed and strict result validated; stopped/unknown native outcome cannot enter checking |
| Task | `checking → reviewing` | All required checks pass or match an explicitly approved baseline; record unhealthy baseline separately |
| Task | `reviewing → awaiting_human|accepted` | Fresh reviewer, no blocking findings; configured human gate decides; manual checks still require recorded outcomes |
| Task | `checking|reviewing|awaiting_human → needs_repair` | Failed check/blocking finding or human request-changes; preserve evidence; charge repair budget on new implementation attempt |
| Task | `needs_repair → ready` | Remaining repair/time allowance; checkpoint policy satisfied; evidence invalidated appropriately |
| Task | `* → blocked` | Missing input, scope change, exhausted allowance, writer uncertainty or failed prerequisite; reason/inbox link required |
| Task | `awaiting_human → draft` | Human clarifies requirements through an approved revision; criteria edits are explicitly human-authorized, never a model shortcut |
| Task | `* → stopped` | Human stops task; interrupt any active run, preserve changes/evidence; dependents remain blocked |

Task acceptance is a core transaction checking the current task definition, code fingerprint, required checks, fresh review, severity policy, human acceptance waiver or resolution, and every manual Pass. Suggestions are retained without requiring repair. `Fail` and `Cannot verify` never satisfy a manual check. Finalization is an application-created system task with reference/completeness checks; it does not recursively spawn finalization or repeat code checks without a code change.

Run state is `prepared → starting → active ↔ waiting_input → stopping → completed|failed|interrupted|unknown`. Pre-start resource wait is `queued` and is not execution. Native completion records an **outcome**, while `writer_state` independently remains `unconfirmed`, `observed_stopped` (specific probe/child only) or `contained_stopped` (tested containment emptied). Process exit, idle, no heartbeat change, and containment emptiness are separately timestamped facts.

Stop-now sets dispatch disabled first, retires request references, sends native interrupt once, waits bounded grace, terminates the owned execution boundary, then preserves outcome and writer evidence. Foreground exit uses the same sequence. If writers/inference are unconfirmed, leave ownership quarantined and keep recovery files; do not snapshot-and-clear while writes may continue. A controller crash is reconciled as unknown even if the last output said “done.”

## 4. Configuration, profiles and capability eligibility

Configuration layers are built-in defaults → explicit user configuration → project → plan → task → role/run override. This order applies to ordinary scalar preferences; **restrictions intersect**, required-check sets union, denies dominate, and limits take the strictest enclosing ceiling unless a separately authorized policy change raises that ceiling. Do not recursively merge unknown keys. Reject malformed/unknown configuration before readiness. Store origin/scope for every effective setting. Project configuration history links each project revision/policy epoch to an immutable resolved config snapshot; profile revisions likewise remain immutable.

There is no implicit model-policy default. Project readiness requires choosing `local_only`, `hybrid` or `cloud_allowed` plus named implementation/reviewer/supervisor profiles. Hybrid is not an automatic paid escalation authorization. Approval mode defaults to `supervised`; autonomous configuration explicitly waives specified human gates and grants allowed categories, never manual functional requirements, configured checks/review, project restrictions or budgets.

A versioned profile includes harness executable/version range and source fingerprint where needed; transport; model/provider; canonical endpoint/resource ID; credential **reference**; role eligibility; context/output limits; instruction/skill references with digests; enabled tools; auxiliary routes; continuation/delegation settings; network/filesystem boundary; and declared capabilities with verification evidence/platform. Credentials are resolved at launch into the minimum native environment or broker; values never enter config snapshots/events/archives. Symlink-sensitive references are canonicalized and revalidated.

Instruction precedence is system/organization restrictions → project → role → task. A profile may reference existing harness instructions/skills, but availability does not authorize installing or enabling them. Unsupported combinations block readiness. Summaries and retrieved repository text are untrusted task context, never authority to change grants or accepted criteria.

Capability entries are `{name, support, guarantee, platform, harness_version, evidence_id, verified_at}`. Support is `available|unsupported|unverified`; guarantee is `application_enforced|native_enforced|advisory`. Invalidate evidence on relevant harness/config/boundary change. Native exact resume is eligible only with the recorded home/identity/profile/workspace and no unresolved writer; fail to an explicit user recovery choice, not create-and-call-it-resume. Evidence must cover the recovery class: Stage 3's completed-session recall does not qualify interrupted/corrupt histories. Long histories beyond the adapter's bounded frame budget are currently unsupported until paginated inspection exists.

Local-only covers main, reviewer, supervisor, routing advisor, titles, compression and background/auxiliary **inference**. Disable unnecessary routes and delegation; pin every remaining route to eligible local endpoints. It is distinct from offline operation. A profile needing full offline/egress control requires a verified boundary. Neither a local model name nor a recording relay proves total egress restriction.

## 5. Permission resolution and revocation

Application categories: plan/spec acceptance, profile/model assignment, paid spending, scope/task-definition change, commit, push, draft-request creation and checkpoint restoration. Ordinary scoped edits/commands and bounded repairs need no extra application gate by default, but must not bypass those categories. Automatic merge is always unavailable. Human task/plan acceptance waivers are configuration, not native shell permission.

Match a grant against category, canonical project/repository/endpoint, optional plan/task/role, argument/resource digest, revision scope, limit/expiry and revocation state. Resolve restrictions first; then use the narrowest sufficient applicable grant; otherwise create an inbox request. Scope is `once`, `task`, `plan`, `project_permanent`, or explicit `global_permanent`. Global grants never defeat project denials. Show origin, resource bounds and revocation affordance. A once grant is reserved atomically to one operation ID and consumed on attempted effect, including uncertain delivery; it is not returned to the pool after a timeout.

Revision binding is action-specific: commits bind repository/base/ref/index/tree digest; pushes bind remote identity, exact ref and object ID (no wildcard/mirror); request creation binds hosting repository/head/base/draft flag; restore binds checkpoint-set and destination fingerprint; task edits bind proposed revision and affected tasks. Task/plan grants may allow several matching operations but remain bounded by their resource patterns and approved revision policy. Criteria edits always require an explicit human command identifying old/new criteria, regardless of a model's broader edit grant.

Revocation and dispatch/effect authorization serialize through the command transaction. Recheck immediately before effect using a reserved operation and current policy epoch. If revocation wins before effect begins, cancel; if an external effect has started, request interruption and record any irreversible result honestly. Revocation cannot erase a commit already made or a request already published. Outstanding native prompts are tied to run/turn/generation/request/deadline; cancellation, terminal state, transport replacement or expiry retires them. No request from an old generation can approve a new one.

For an explicitly global grant, authorization also references the user-state policy epoch and a durable effect-start reservation in the coordination DB. The transition to `executing` is the authorization linearization point: after it, revocation is an in-flight cancellation, even if the external syscall has not yet happened. Global revocation and that reservation serialize in the coordination transaction; project restrictions are still checked by the project owner before requesting it. Persist/reconcile both operation IDs because the two DBs cannot commit atomically. A crash with a shared start reservation but missing local observation is uncertain, never permission to repeat. Local and global revocation cannot retroactively roll back external effects.

Native grants are always translated to the narrowest supported one-time response. Never translate “permanent project” into a native global/session allowlist. If an offered native API cannot express denial, cancel/error the request and stop as needed; if it cannot express one-time allow, report unsupported. A native request is necessary evidence for that tool action, not a substitute for application policy.

## 6. Scheduling and execution-time accounting

One foreground execution context per project covers implementation, checks, review, supervisor and finalization. Pure database/UI commands remain responsive. Maintain explicit plan queue ranks; optional automatic advancement defaults off. For the active plan choose ready tasks whose dependencies are accepted and whose grants/profiles/budgets/resources are eligible, ordered by explicit task rank then stable ID. Blocked tasks keep their causes; independent tasks may proceed only after the checkout is preserved and safe. A supervisor may reorder remaining tasks without changing definitions or accepted evidence; graph cycles and reordering active work are rejected.

Design defaults (editable before dispatch within approved ceilings): two repair attempts, one infrastructure retry only when submission is proven not delivered, 10 minutes active per attempt, 45 minutes cumulative active per task, 30s startup, 15s control RPC, 5s interrupt and 5s terminate grace, 30 minutes human-request expiry, and 2 hours absolute attempt wall time. Keep spike limits unchanged. Task-scoped implementation, automated checks, review and recovery/supervisor assessment all charge the same task ledger; plan-wide planning/acceptance/finalization have a separate 30-minute active plan-services allowance so their cost is not hidden or charged twice.

Use monotonic active segments during a process lifetime; persist checkpoints at most every second and every transition. Human-input wait is excluded **only after native execution is demonstrably waiting**. Resource queue time is excluded because no process/turn is dispatched. If a harness continues background inference while waiting, that interval remains charged and retains its endpoint slot. Normal tool execution and check commands are active time. Retries/reassignment never reset the cumulative allowance. For crash gaps, charge a conservative bound from last checkpoint to proven termination; unknown time is displayed separately and cannot increase remaining allowance.

Monetary ceilings apply only when the profile can supply a defensible bound before dispatch and reliable accounting afterward. Unknown subscription/provider cost remains null; a profile with unknown cost cannot claim enforcement of a numeric currency ceiling. The user may explicitly choose time/token-bounded eligibility instead, but the system never substitutes that policy silently.

Repair means a new implementation attempt after task-specific quality rejection. Infrastructure retry means a recoverable launch/connect failure with no delivered prompt and no tool effects. A completed transport with a provider error is not automatically replayable. Retry exhaustion invokes one bounded fresh supervisor assessment proposing clarification, revision/split, or an eligible stronger model; any changed model/spending/scope still passes policy. Limit exhaustion stops and preserves; it never accepts partial results.

## 7. Shared workspace and endpoint coordination

Use a small user-state SQLite coordination database on the same host, plus advisory owner lock files held for each live controller. No background service. An application with a separate user account is outside the initial cooperative coordination domain. OS containment is separately required against worker bypass.

Canonical folder identity combines resolved absolute path components and filesystem identity (device/inode where available). Resolve symlinks and compare **path components**, not string prefixes (`/a/b` differs from `/a/bc`). Reserve project root and all participating roots; reject ancestor/descendant overlap. Record common Git directory identity too, so linked checkouts sharing ref/index administration cannot evade ownership even though managed worktrees are deferred. Detect path replacement and case/normalization aliases before every destructive transition; ambiguous identity fails closed. Network filesystems are unsupported for live state/coordination in v1.

Stage 3.5 observed case and Unicode aliases with equal inode identities but unequal resolved path strings on APFS. Existing ancestor identities must therefore participate in overlap detection; resolving symlinks or lowercasing strings alone is insufficient. The prototype also confirmed that owner death releases the OS lock while a persisted unknown-writer claim can survive. Production dispatch must consult that claim before reusing the resource.

Ownership rows contain instance ID, PID plus process start/boot identity, host identity, root identity, acquisition time, heartbeat, generation/fencing token and `active|releasing|quarantined` status. Heartbeats are diagnostics, not leases that silently expire. An advisory lock proves a cooperating owner process is present, but its release does not prove descendants stopped. On stale owner: prevent acquisition, inspect containment/native handles, preserve files, reconcile operations; clear quarantine only after evidence or an explicit user reconciliation with observed process/file state. No PID-only kill or lock-file deletion as automatic recovery.

Endpoint identity is an explicit resource ID shared by profiles, with canonical URL metadata (normalized scheme/host/port/path). Normalize loopback aliases and require aliases to the same physical local service to share resource ID. DNS aliases/remote deployments cannot be inferred reliably; conflicting config blocks readiness until mapped. Default capacity one. Queue tickets are monotonically ordered in the coordination transaction, with cancellation and oldest eligible ticket winning; a blocked unrelated cloud task can proceed in another project. Acquire full workspace ownership first, enqueue for the endpoint only when ready, and never hold an endpoint slot while waiting to acquire a workspace. Avoid multiple endpoints per run initially: profiles needing simultaneous inference across resources are unsupported.

The Stage 3.5 Mac loopback tunnel and WSL loopback both reached one native Windows llama.cpp service. Separate host-local databases cannot arbitrate that physical capacity. Production use requires a common authoritative capacity coordinator or explicit single-host eligibility; localhost URL spelling is not proof of physical locality or exclusive ownership. The diagnostic run used operator sequencing only.

Reserve a slot atomically before launching the native process because creation/resume may trigger auxiliary work. Release only after tested native idle plus all owned auxiliary work ends, or the execution boundary is proven empty. Controller loss leaves a quarantine row, even if the OS lock is free. Core application state and coordination DB are not one atomic transaction: use a stable reservation operation ID and reconcile prepared/committed ownership records on either-side crash. External clients/other machines can still use llama; display this limitation.

## 8. Execution boundary decision from Stage 3

The Linux experiments demonstrated exact resume and normal interruption, but also a Hermes writer surviving transport death and native commit/local-push operations bypassing application approvals. Therefore the current native profiles **do not qualify for strict production dispatch**. The existing `spike` remains an explicit diagnostic tool. Supervised/autonomous labels cannot imply enforcement that is merely prompt advice.

Preferred Linux implementation candidate: an isolated worker execution environment with a cgroup v2 boundary for all descendants; writable approved work paths; read-only bind mounts for each `.git`/common Git directory and instructions; private native homes; no host credential/config/socket access; no capability to remount or reach the host process namespace; egress only through an approved provider relay. Application-controlled Git/hosting actions execute outside this boundary with narrowly scoped credentials after policy checks. Remove ambient SSH agents, Docker sockets, D-Bus, host `/proc` and arbitrary localhost services. A shell command wrapper alone is insufficient. Read-only Git metadata must resist replacement via parent paths, alternate git-dir/work-tree settings and nested repositories.

Provider authentication needed by the harness is a separate concern from publishing credentials. Prefer a broker/relay credential valid only for the selected provider/model and bound execution, rather than exposing broad host secrets. If a native harness requires broad readable host auth or cannot operate with the boundary, mark that profile combination unsupported; do not silently weaken the boundary.

macOS candidate: an equivalent isolated VM/container execution environment with verified mount/network/process controls, assessed in Stage 3.5. Native sandbox/hooks can be defense in depth but are not assumed equivalent across operating systems. Installing/configuring a runtime is a separate user-visible implementation choice when needed. Stage 5 may implement state/coordination/read-only planning first, but its production editing/delivery acceptance gate includes containment and bypass tests before advertising autonomous readiness.

Stage 3.5 confirmed both macOS harnesses can leave detached writers after abrupt transport loss, unlike the tested Linux Codex case. Narrow Seatbelt deny rules worked for synthetic Git/secret/network probes, but the allow-default prototype does not qualify a complete worker boundary. Boundary selection, implementation and full bypass tests remain Stage 5 D; no VM/container runtime was installed during qualification.

## 9. Repository discovery, branches and checkpoints

Discovery is read-only. Ascend for an existing Vigil project; warn before initializing inside a parent. Returning folders reopen their project ID; unrelated repositories require explicit registration. Discover nested Git roots, record their boundaries/base refs/remotes/common directories, and stop traversal at registered roots before separately inspecting nested ones. Submodules are read-only initially unless explicitly enrolled as independent participating repositories with superproject link updates in scope; no recursive automatic submodule update. Self-hosted hosting is deferred until configured/validated; generic Git-only projects can still work locally.

Create one plan branch per affected repository from an explicit base object/ref. Validate branch names through Git, reject unrelated existing names, and reuse recorded branches on resume. Never switch branches over existing changes without the dirty-work decision. Before dispatch, capture HEAD/index/tracked/untracked fingerprints. The user must choose: preserve and include specified existing work in baseline, save it through an explicit recovery operation, or postpone. Manual edits require pause and reconciliation; ambiguous mixed edits stop automatic clearing.

Choose private content-addressed checkpoint bundles, **not the user's stash stack**. Each repository snapshot contains base HEAD/branch, a copy of index bytes plus index entries/tree reference, tracked working-file bytes/modes/symlink targets, untracked paths explicitly in scope, and a manifest of exclusions/digests. Use temporary indexes for any Git tree construction; preserve resulting objects with application refs using compare-and-swap. Never add arbitrary ignored files or secrets to Git objects. Ignored files are excluded from mutation unless a separate explicitly approved preservation rule exists. Binary files, deletions, modes and symlinks must round-trip; unsupported file kinds block automatic cleanup.

Saving and clearing are distinct journaled operations. Stop/prove writers first; capture all participating repositories to private temporary bundles; fsync files/manifests; atomically publish bundles; verify digests and recovery readability; mark the complete set verified. Only then clear **provably agent-owned** differences back to the recorded pre-attempt baseline using per-path compare-and-swap against the captured post-attempt fingerprint. Preserve original staged/unstaged/untracked distinctions. No `git reset --hard`, blanket `git clean`, broad stash pop or overwrite-if-different. If any repo snapshot fails, clear none. If clearing stops halfway, retain the verified set and per-repository/path progress; resume reconciliation, not execution.

Restore requires a grant naming checkpoint-set plus destination fingerprint. Take a verified snapshot of the destination first, preflight all repositories, then apply with a durable progress journal. Exact-baseline restore reproduces index/worktree state; changed destinations use a three-way/conflict workflow and never silently prefer saved or current content. Multi-repository restore is not atomic: partial apply is a blocked recovery state with both originals preserved. Scope/task grants may preauthorize restoration only if their resource/revision constraints explicitly cover that exact set/destination; autonomous mode alone cannot.

## 10. Quality, review, human decisions and delivery

Project-required checks form an immutable union with task additions. Discovery proposes commands; human approval binds argv/shell/cwd/environment/time limits and baseline. Run checks inside the execution boundary, capture bounded output artifacts and exit status, and hash the evaluated repository set. Exit zero alone is not sufficient if required outputs are missing or the process was interrupted. An approved pre-existing failure baseline specifies the exact check and normalized failure identity, affected paths, base fingerprint and rationale. A new/unclassified failure is blocking; baseline acceptance retains an unhealthy project indicator.

Review is a fresh read-only session using the task's preselected reviewer profile. Context contains accepted criteria, exact change fingerprint, check evidence and relevant baseline failures. Findings include stable ID, severity, path/location, evidence and remediation suggestion. Configured blocking thresholds apply; missing/invalid review is failure, not approval. Reviewer cannot modify code or invoke delivery tools. Repairs invalidate code-dependent check/review/human evidence; minor narrative edits do not automatically rerun code checks.

Human review displays implementation summary, blocking/suggested findings, check results and manual verification requirements. Support `request_changes` (repair loop), `clarify_requirements` (revision workflow), and `stop_task`. Manual items have `pending|pass|fail|cannot_verify`, notes, evidence and evaluator; a blanket waiver of human acceptance does not set them to Pass. After code changes, manual evidence remains valid only when explicitly scoped unaffected and revalidated by policy; default invalidates it.

Acceptance and commits are distinct. Task commits may be prepared after passing automated gates and applicable commit authorization, but an existing commit never accepts a task. App-owned commit effects bind exact trees and parents. Multiple commits per task are allowed; preserve correspondence to task/evidence revisions. Push sends only the approved plan ref/object to the approved remote; never `--mirror`, checkpoint refs, force updates or unintended branches. Draft GitHub/GitLab request creation is independently authorized and reconciled by repository/head/base plus stored operation ID; if the API result is uncertain, search before reissuing. Automatic merge has no command/tool implementation.

## 11. Storage, files, privacy and retention

Per-project state lives under the user's private application state directory keyed by project ID, outside worker-writable trees. `.vigil` inside the project is a local Git-ignored pointer/archive view, not trusted authorization state. User-wide coordination and explicit global grants live in a separate private host-state database. Workers never receive writable DB/artifact roots. Mode 0700 directories and 0600 files are default; OS permissions alone do not isolate same-UID unrestricted workers, hence the boundary gate.

The [project draft schema](../spec/project.sql) and [coordination draft](../spec/coordination.sql) define relationships/constraints; they are not installed migrations. Use the bundled Go SQLite engine, enable foreign keys on **every connection**, WAL on supported local filesystems, `synchronous=FULL`, busy timeout 5s, one writer connection and short read transactions. `BEGIN IMMEDIATE` gives predictable write contention. Bound contention retry before effect, not by rerunning an external action. SQLite permits one writer; WAL is host-local and does not make multi-database effects atomic. [Transactions](https://www.sqlite.org/lang_transaction.html), [WAL](https://www.sqlite.org/wal.html), [foreign keys](https://www.sqlite.org/foreignkeys.html).

Schema migrations have an ordered integer version, file digest and applied timestamp. Refuse unknown newer versions or changed historical digests. Back up via SQLite's consistent backup mechanism before destructive migration; take exclusive project ownership, apply transactionally where supported, verify `foreign_key_check`/integrity and restore only through an explicit recovery path. Do not copy only the main WAL database file while live. Never auto-downgrade.

Append-only events contain schema version, sequence, command/run IDs, UTC timestamp, source generation/sequence, kind, and bounded sanitized payload (64 KiB maximum). State rows are authoritative current projections; event/history rows remain immutable. Raw streaming data belongs in bounded private files, not unbounded SQLite blobs. Retain token usage as nullable observed/estimated fields with provenance; unknown cost is never zero. Secret references, not values, are allowed in persistent configs.

Artifact publication: write bounded private temp file → fsync → compute digest → rename into project-owned content-addressed storage → fsync directory → DB transaction records manifest/ref. A file without a committed reference is an orphan for delayed reconciliation; a DB ref with missing/wrong digest is corrupt and blocks dependent acceptance. Exports copy verified records and preserve portable relative references; external URLs are explicitly typed. Validate path containment without following worker-controlled symlinks. Event persistence failure stops dispatch, interrupts active work and marks recovery needed; never silently drop acceptance evidence.

A factual archive is generated from state before the optional model-written narrative. Narrative failure yields `finalization_pending`; retry finalization only. Delivery can be explicitly performed from this state and its URL appended to a new archive manifest revision. An exported archive commit never tries to include its own future object ID. Durable decisions/checks/findings/commit references persist indefinitely by default. Raw transcripts expire 30 days after plan completion, configurable; preserve extracted durable evidence and mark refs expired before deleting bytes. Unfinished plans/checkpoints never follow automatic completion TTL. Native harness histories have separate ownership-aware retention; do not delete global history to satisfy app retention.

## 12. Architectural walkthroughs and readiness

| Scenario | Expected core behavior |
| --- | --- |
| Successful local task | Plan/profile/grants ready → workspace + endpoint reserved → implementation → checks → fresh review → human/manual gates → accept; release slot only with evidence; commit/delivery separate |
| Resume after a clean stop | Reconcile checkout and budgets, verify exact native handle/profile/history, retire old requests, explicit resume choice and submission; no spontaneous work |
| Hermes controller dies with child writer | Run unknown, ownership and endpoint quarantined, partial files preserved; no clear/restore/next task until containment or explicit reconciliation proves safety |
| Approval arrives after generation replacement | Reject as stale; recorded decision cannot apply to a new turn or consume a fresh grant |
| Task changed while review waits | Revision mismatch rejects acceptance; invalidate affected evidence and approval requests; rebuild fresh context |
| Two instances request overlapping roots | Coordination transaction admits one; other waits/blocks with owner identity; stale heartbeat does not evict |
| Mixed pre-existing/manual edits | Preserve baseline, detect changed fingerprints; snapshot as evidence but do not clear ambiguous paths; ask for reconciliation |
| Snapshot of repo B fails | Keep repo A bundle, clear neither checkout; retain set as incomplete and block dispatch |
| Restore fails after repo A applied | Persist partial progress, retain destination snapshots plus original set, show conflicts; do not restart agents |
| Push succeeded but client timed out | Delivery uncertain; inspect approved remote ref and reconcile, never blindly re-push/create duplicate request |
| Summary agent fails after acceptance | Keep factual archive and accepted work; finalization pending; explicit draft delivery possible; no development rerun |

Stage 4 closes the mechanism questions above as implementable design defaults. Exact user role profiles, spending limits, repository bases and any host-runtime installation remain setup decisions, not assumed grants. The [Stage 5 backlog and requirement map](../plans/stage-5/stage-5-plan.md) tracks implementation and qualification gates. The completed [Stage 3.5](../history/stage-3.5-macos.md) investigation supplies macOS runtime evidence and explicit containment/recovery limits.
