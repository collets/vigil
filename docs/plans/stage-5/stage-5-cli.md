# Persisted planning CLI

<!-- vigil-tier: plan -->

Stage 5 persists definitions, repository setup, a journaled execution lifecycle, the Stage 5.4 quality/acceptance path and Stage 5.5's queue/planning workflow. Production agents and reviewers remain qualification-gated; all currently exposed execution, review, human and acceptance drivers are explicitly selected, marker-gated fixture paths for disposable repositories. Implementation result persistence stops at `checking`; only `quality-accept` can accept current evidence, and it never creates delivery authority. `dashboard PROJECT_ID` now applies only the documented typed controls; `spike` remains a separate diagnostic runner with no production fallback.

Stage 5.6 adds a factual archive, fixture-only narrative validation, portable export and explicit transcript retention controls. These commands do not authorize commits, pushes, hosted requests or production model dispatch.

## Initialize and inspect

```sh
make build
./bin/vigil project init /absolute/path/to/project
./bin/vigil project list
./bin/vigil project status PROJECT_ID
./bin/vigil project discover PROJECT_ID
./bin/vigil project inbox PROJECT_ID
./bin/vigil project events PROJECT_ID --after 0
./bin/vigil resources status
./bin/vigil doctor
./bin/vigil hello
```

Use the ID returned by initialization. State defaults to `$XDG_STATE_HOME/vigil` or `~/.local/state/vigil`; `--state-dir /absolute/private/path` overrides it. State must be outside managed checkout trees. Existing state directories/files must be private. Initialization reuses a physical root across symlink aliases and refuses overlapping registered projects.

Two root-level entry points sit outside the persisted project command tree. `hello` queries `sqlite_version()` only: it creates no application tables, persists no task, and honours `--db PATH` (default `:memory:`). `dashboard PROJECT_ID` takes one project ID and uses only the typed actions documented below. Both accept the persistent `--db` flag, but **`--db` is inert for every other command** — all persisted state resolves through `--state-dir`, so passing `--db` to a `project`, `resources`, `doctor` or `spike` command is silently ignored rather than redirecting state.

## Apply a versioned human command

Write a JSON file, then run `vigil project apply PROJECT_ID --file COMMAND.json`. Every command carries a unique `command_id`, the current `expected_revision` from status, a known `kind`, and a closed `payload`. Successful commands return the new project revision. Repeating the identical command returns its durable receipt; changing its arguments or submitting a new command against an old revision fails. Unknown fields, duplicate JSON keys and documents over 64 KiB fail before mutation.

Example initial configuration:

```json
{
  "command_id": "configure-001",
  "expected_revision": 1,
  "kind": "project.configure",
  "payload": {
    "model_policy": "local_only",
    "deny": ["push"],
    "required_checks": ["unit-tests"],
    "check_definitions": [{"id":"unit-tests","argv":["/usr/local/go/bin/go","test","./..."],"cwd":".","environment":[],"required_outputs":[],"timeout_ms":300000,"max_output_bytes":1048576}],
    "task_limit_ms": 2700000,
    "attempt_limit_ms": 600000,
    "repair_limit": 2,
    "supervisor_profile": "local",
    "approval_mode": "supervised",
    "human_acceptance_required": true,
    "review_blocking_severity": "high"
  }
}
```

The next command can use `kind: "profile.put"`, revision 2, and this payload:

```json
{
  "id": "local",
  "harness": "hermes",
  "version": "0.21.3",
  "model": "qwen3.8-27b-local",
  "provider": "custom",
  "credential_ref": "env:OPENAI_API_KEY",
  "roles": ["implementation", "review", "supervisor"],
  "endpoint_id": "windows-llama",
  "local_inference": true,
  "auxiliary_local": true,
  "delegation_disabled": true,
  "capabilities": []
}
```

Credential references are names, never values. This profile records intent; declarations do not establish that routes, versions or containment have been verified.

At revision 3, `kind: "plan.put"` accepts:

```json
{
  "id": "first-plan",
  "title": "A small scoped change",
  "specification": "Implement the agreed behavior and verify it with unit tests.",
  "approved": true,
  "authorize_criteria_changes": false,
  "quality_criteria": [{"id":"plan-manual","text":"Plan-level fixture verification completed","manual":true}],
  "quality_checks": ["unit-tests"],
  "reviewer_profile": "local",
  "human_acceptance_required": true,
  "tasks": [{
    "id": "first-task",
    "objective": "Implement the specified behavior",
    "criteria": [{"id": "behavior", "text": "Agreed behavior passes its check", "manual": false}],
    "dependencies": [],
    "context": [],
    "scope": ["src/**"],
    "checks": [],
    "questions": [],
    "implementation_profile": "local",
    "reviewer_profile": "local",
    "difficulty": "small",
    "rationale": "One independently verifiable change",
    "active_limit_ms": 600000,
    "repair_limit": 2
  }]
}
```

Plan import validates dependency graphs and retains immutable revisions. Existing tasks cannot be silently removed. Criteria changes require explicit human revision authority. `plan.reorder` takes `{"plan_id":"first-plan","tasks":["first-task"]}` and preserves the exact task set and task revisions. Readiness reports missing profiles, unapproved specifications, unresolved questions, dependencies and policy constraints. Plan-wide quality fields are optional, but when present bind their own check/review/manual/human gates to the exact plan revision. Production eligibility remains false until exact trusted launch/recovery and reviewer qualification exist.

### Markdown specifications and fixture planning proposals

Local Markdown import accepts only a regular non-symlink `.md` file inside the
registered project root, from 1 byte through 64 KiB. It publishes a private,
content-addressed immutable artifact and returns the exact content/version for
inspection. Control sequences or embedded instructions remain untrusted text.
Issue-URL import is not implemented.

```sh
./bin/vigil project spec-import PROJECT_ID ./spec.md \
  --id spec-1 --command-id spec-import-001 --expected-revision 4
./bin/vigil project spec-show PROJECT_ID spec-1 1
```

`proposal-create` reads a closed JSON `ProposalRequest`, requires explicit
`profile_id` and current `profile_revision` fields for an eligible
planning/supervisor profile, validates complete task fields,
cycles, criteria, scope, checks and limits, and creates an approval inbox item.
It is offline fixture-only: it makes no model call and cannot approve anything.
Missing task questions also create distinct clarification items.

```sh
./bin/vigil project proposal-create PROJECT_ID --file proposal.json \
  --command-id proposal-001 --expected-revision 5 --synthetic-fixture
./bin/vigil project proposal-show PROJECT_ID proposal-1 1
./bin/vigil project proposal-apply PROJECT_ID proposal-1 1 \
  --command-id proposal-apply-001 --expected-revision 6
./bin/vigil project proposal-decide PROJECT_ID proposal-1 1 \
  --command-id proposal-reject-001 --expected-revision 6 \
  --decision reject --rationale "The proposal does not match the requested scope"
./bin/vigil project proposal-decide PROJECT_ID proposal-1 1 \
  --command-id proposal-revise-001 --expected-revision 6 \
  --decision request_revision --rationale "Split the first task and preserve its criteria"
./bin/vigil project input-resolve PROJECT_ID REQUEST_ID \
  --command-id input-answer-001 --expected-revision 6 \
  --decision answer --answer "Use the existing deterministic fixture format"
```

`proposal-apply` is the human application command bound to the exact proposal,
specification and expected plan revision. Criteria changes additionally require
`--authorize-criteria-changes`. Application is atomic and reuses `plan.put`'s
active/accepted-task protection and evidence invalidation. It creates no commit,
push, publication, spending or delivery authority.

`proposal-decide` records an exact revision-bound human rejection or replacement-
revision request. `reject` leaves the immutable revision in `rejected`;
`request_revision` leaves it in `stale`, so a planner can create a new immutable
revision without modifying the reviewed one. Both actions retire that revision's
pending approval and clarification items atomically. A later revision still needs
its own explicit application.

`input-resolve` handles only a visibly identified non-native `input` request.
`answer` requires a nonempty answer of at most 4096 bytes; `dismiss` forbids an
answer. Native session clarification remains a separate owner-routed action. An
answer to the final question on a proposal marks that immutable proposal revision
`stale`, so the answer must be incorporated into a replacement proposal before
application; it never silently edits or approves the reviewed definition.

## Repository enrollment and branch preparation

Discovery is read-only. Enrollment is an explicit versioned command:

```json
{
  "command_id": "repository-001",
  "expected_revision": 4,
  "kind": "repository.enroll",
  "payload": {
    "id": "primary",
    "plan_id": "first-plan",
    "root": "/tmp/vigil-disposable/work",
    "base_ref": "refs/heads/main",
    "plan_branch": "vigil/first-plan",
    "dirty_choice": "clean",
    "nested_boundaries": []
  }
}
```

`dirty_choice` is `clean`, `include` with exact `included_paths`, or `postpone`. `save` is rejected until Stage 5.3 supplies preservation. Included/postponed work is recorded but cannot be executed by this slice. `nested_boundaries` names repository-relative ordinary nested Git roots excluded from the parent fingerprint; enroll each participating nested repository separately. Remote identity is optional and credential-bearing URLs are rejected.

Inspect and prepare with explicit receipts:

```sh
./bin/vigil project repository PROJECT_ID primary
./bin/vigil project prepare-repository PROJECT_ID primary \
  --command-id branch-001 --expected-revision 5
```

Preparation requires the exact clean fingerprint and selected base. It creates/reuses only the recorded ref and symbolic HEAD; it never checks out over edits. A crash is reconciled from the observed exact ref.

## One disposable persisted execution

The repository must contain a committed regular `.vigil-disposable-fixture` marker. Register a synthetic capacity identity for the profile's `endpoint_id`; this URL is never contacted by the fixture driver. `--single-host` is **required** — the command fails without it, because other hosts and external clients are not coordinated. The fixtures in this repository use the ID `fixture-endpoint` by convention, but that literal is not enforced by code: any registered endpoint ID works so long as preparation and the profile agree.

```sh
./bin/vigil resources endpoint fixture-endpoint \
  http://127.0.0.1:1/v1 --capacity 1 --single-host
```

Persist preparation without launch, using the current revision after branch preparation:

```sh
./bin/vigil project execution-prepare PROJECT_ID first-task \
  --command-id run-001 --expected-revision 6 \
  --wall-limit-ms 60000 --synthetic-fixture
```

Then start explicitly with the returned run ID:

```sh
./bin/vigil project execution-start PROJECT_ID RUN_ID \
  --command-id start-001 --synthetic-fixture \
  --repository primary --path src/result.txt \
  --content 'persisted fixture execution' \
  --prompt 'Create the deterministic fixture result.'
```

The start command re-reads the exact durable reservation, owns every participating root before endpoint capacity, and holds the live owner capability through dispatch so quarantine/release cannot race effect start. It journals `executing` before each external phase and retries only after trusted proof that an interrupted effect did not occur. Fixture targets are checked against task scope, protected paths and enrollment exclusions before submission; descriptor-relative no-follow traversal rejects symlink escape and atomic replacement preserves an outside hardlink inode. It validates actual changed paths and releases fixture resources only after contained-stop proof. It returns `accepted: false`; successful completion leaves the task in `checking`.

Inspection is read-only. Reconciliation requires its own receipt and cannot submit a prompt:

```sh
./bin/vigil project execution-inspect PROJECT_ID RUN_ID
./bin/vigil project execution-reconcile PROJECT_ID RUN_ID \
  --command-id reconcile-001 --synthetic-fixture \
  --repository primary --path src/result.txt
```

An uncertain submission exposes only inspect/reconcile/stop as allowed next commands. A durably delivered generation is also never resubmitted, even when current driver inspection is unavailable. Successful outcome persistence atomically rechecks the persisted task ledger, run allowance, crash-gap uncertainty and any open active segment; reconciliation cannot turn an over-budget terminal observation into `completed`/`checking`. Proven native human/resource waits suspend the active await timer but not the absolute wall timeout or lease renewal; the active timer resumes from cumulative consumption when the wait ends. These fixture commands are not model dispatch and never fall back to `spike`. Production runtime drivers must independently report the exact runtime inputs and route set they will use; Vigil binds those to the persisted profile, endpoint, checkout roots/layout/mount digest and Stage 5.1 evidence before invoking core eligibility. No production driver currently satisfies/enables that contract.

## Pause, stop and checkpoint recovery

Pause prohibits later dispatch; continue rechecks unresolved runs, checkpoint operations and cumulative budgets:

```sh
./bin/vigil project pause PROJECT_ID \
  --command-id pause-001 --expected-revision 7
./bin/vigil project continue PROJECT_ID \
  --command-id continue-001 --expected-revision 8
```

Queue and task selection are separate, receipt-backed commands. Queueing requires
the exact accepted plan revision. Equal nonnegative ranks are stable by plan ID.
`advance` explicitly activates at most one queued plan and selects one eligible
task by task rank then ID; it launches nothing. Repeating either command ID with
the same arguments returns its receipt, while stale revisions fail. Automatic
next-plan advancement is unavailable, including after restart.

```sh
./bin/vigil project queue-list PROJECT_ID
./bin/vigil project queue PROJECT_ID PLAN_ID \
  --command-id queue-001 --expected-revision 8 --rank 10
./bin/vigil project continue PROJECT_ID \
  --command-id continue-002 --expected-revision 9
./bin/vigil project advance PROJECT_ID \
  --command-id advance-001 --expected-revision 10
```

An initial `execution-prepare` consumes the selected persisted dispatch and
rechecks project state, revisions, dependencies, profile/policy/budget and
repository authority. It cannot bypass pause by changing the project back to
ready. Check, review and supervisor preparation uses the same persisted project
dispatch state; stop and recovery retain their Stage 5.3 boundaries.

The available stop driver remains fixture-only. Stop first persists paused dispatch, request retirement and containment intent, then performs bounded interrupt/termination:

```sh
./bin/vigil project execution-stop PROJECT_ID RUN_ID \
  --command-id stop-001 --synthetic-fixture \
  --repository primary --path src/result.txt \
  --interrupt-grace-ms 100 --terminate-grace-ms 5000
```

Save is non-destructive and always includes every immutable participating repository. For a later scoped clear, retain a baseline set from before execution and a captured set from after contained execution:

```sh
./bin/vigil project checkpoint-save PROJECT_ID RUN_ID \
  --command-id save-baseline-001 --expected-revision 9
./bin/vigil project checkpoint-save PROJECT_ID RUN_ID \
  --command-id save-captured-001 --expected-revision 10
./bin/vigil project checkpoint-clear PROJECT_ID CAPTURED_CHECKPOINT_ID BASELINE_CHECKPOINT_ID \
  --command-id clear-001 --expected-revision 11
```

Clear derives agent-owned paths only from the validated execution result; there is no arbitrary path flag. A new command verifies the exact post-result repository fingerprint and immutable pre-attempt baseline, then acquires live fenced claims for the complete set and rechecks terminal writer containment before changing any repository. Repeating the identical command reconciles only its immutable receipt and exact per-path journal; captured or already-desired states are accepted, while substituted authority and unrelated edits fail closed.

Restore additionally requires a checkpoint of the current destination. The restore command is the explicit human approval bound to all three set IDs and the current project revision:

```sh
./bin/vigil project checkpoint-save PROJECT_ID RUN_ID \
  --command-id save-destination-001 --expected-revision 12
./bin/vigil project checkpoint-restore PROJECT_ID TARGET_CHECKPOINT_ID BASELINE_CHECKPOINT_ID DESTINATION_CHECKPOINT_ID \
  --command-id restore-001 --expected-revision 13
```

Restore acquires the same live full-set authority and rechecks writer containment. Worktree/index/object apply is descriptor-relative; staged objects are verified before the Git index is exposed. Missing intermediate directories beneath the verified root are recreated through held descriptors only when an authorized write requires them; symlink, non-directory and root replacement still fail closed. If restore reports conflicts or is interrupted, do not discard any checkpoint. Inspect the persisted operation/path journal and repeat the identical command only after destination state still matches an expected or already-applied state.

## Exact resume and new attempts

The current CLI history inspector is intentionally synthetic-only. Record an explicit recovery choice after containment and a verified checkpoint:

```sh
./bin/vigil project execution-recovery-choose PROJECT_ID RUN_ID \
  --command-id recovery-001 --expected-revision 14 \
  --mode exact_resume --history-state readable --history-class interrupted \
  --synthetic-fixture
```

`--mode fresh_context` accepts `missing`, `corrupt` or `unsupported` history only when writer containment and a full verified checkpoint are available. `--mode remain_blocked` records the safe decision without creating an attempt.

`--history-state` defaults to `missing` and `--history-class` to `interrupted`, so the example above is explicitly not the default path. `--history-automatic-work` records that the native history contains automatic or queued work; setting it **disqualifies** exact resume (`resumed native history has automatic or queued work`) and it is therefore a refusal input, not a convenience flag.

Each exact resume creates immutable generation-scoped workspace authority. Distinct generations may have the same content digest when the workspace is unchanged; this does not reuse attempt identity or reset cumulative allowances.

For eligible exact resume, prepare a new generation and invoke `execution-start` without `--prompt`; a replacement prompt is rejected:

```sh
./bin/vigil project execution-resume-prepare PROJECT_ID CHOICE_ID \
  --command-id resume-prepare-001 --expected-revision 15
./bin/vigil project execution-start PROJECT_ID RUN_ID \
  --command-id resume-start-001 --synthetic-fixture \
  --repository primary --path src/result.txt
```

Fresh reconstruction is a new attempt linked to the choice and bound checkpoint. Repair and infrastructure retries use the same command but separate kinds/limits; infrastructure retry additionally requires proof that no prompt or tool effect was delivered:

```sh
./bin/vigil project execution-followup-prepare PROJECT_ID SOURCE_RUN_ID \
  --command-id fresh-001 --expected-revision 16 \
  --kind fresh_context --choice-id CHOICE_ID --wall-limit-ms 60000
./bin/vigil project execution-followup-prepare PROJECT_ID SOURCE_RUN_ID \
  --command-id repair-001 --expected-revision 16 \
  --kind repair --wall-limit-ms 60000
./bin/vigil project execution-followup-prepare PROJECT_ID SOURCE_RUN_ID \
  --command-id infra-001 --expected-revision 16 \
  --kind infrastructure --wall-limit-ms 60000
```

All generations and attempts consume the same task ledger. Exact resume retains the remaining wall allowance; fresh/repair/infrastructure attempts do not reset cumulative active charge. Exhaustion blocks progression and leaves a pending supervisor assessment for Stage 5.4. Production resume/history drivers and live qualification remain unavailable.

## Permissions and artifacts

`operation.request` records intent using `category`, `resource_digest`, `arguments_digest`, `plan_id`, `task_id`, and `task_revision`. Digests must identify exact resources/arguments; this command performs no external effect. The inbox returns a request ID. `permission.grant` takes `request_id`, `decision` (`allow`/`deny`), `scope` (`once`/`task`/`plan`/`project_permanent`) and optional `expires_at` in Unix milliseconds. `permission.revoke` takes `grant_id`. All use the same command envelope and expected revision. Project commands still reject global scope; trusted coordinator APIs now serialize separately created explicit global grants and effect-start reservations with revocation. There is no user-facing global-grant command yet.

```sh
./bin/vigil project artifact PROJECT_ID evidence.txt --command-id evidence-001 --kind test-report
./bin/vigil project artifacts PROJECT_ID
```

Artifacts are bounded to 16 MiB, published by digest, and verified on read. Inspection reports orphaned/corrupt objects without deleting recovery evidence. Publishing a file is not task acceptance.

## Shared resources

```sh
./bin/vigil resources endpoint windows-llama \
  http://127.0.0.1:8080/v1 http://localhost:8080/v1 --capacity 1 --single-host
```

`--single-host` explicitly chooses this host as the sole Vigil capacity authority. Mac/WSL instances and external clients are not automatically coordinated. Register aliases under one physical endpoint ID, not separate capacities. The service remains native Windows llama.cpp; Vigil does not start it.

Library ownership tests cover FIFO capacity, overlap and real controller-process death. Resource status exposes owner IDs and fencing generations. A dead owner's OS lock becoming free quarantines its claims and slots. Recovery requires observed stopped writers/inference:

```sh
./bin/vigil resources reconcile OWNER_ID --observation 'Concrete evidence that this owner has no remaining writers or inference'
```

The observation is an explicit human attestation, not an automatic process cleanup tool. Do not release a real unknown owner just because its controller is gone.


## Restriction intersection

Project configuration, `plan.put` and each task accept an optional `restrictions` object:

```json
{"profile_ids":["local"],"operation_categories":["commit","checkpoint_restore"]}
```

Only those two dimensions are supported. Values are exact identifiers. An omitted dimension adds no restriction; an explicit empty list permits nothing. Null lists, duplicates, unknown dimensions/categories and wildcards are rejected. Project, plan and task sets intersect; a task cannot restore a value excluded by its plan or project. Project denies still dominate operation allowances. Profile restrictions apply to implementation, review and supervisor eligibility; status includes effective task restrictions and their origins.

Operation restrictions are checked when requesting permission, granting it and internally starting its effect. Replacing a plan definition now advances the project policy epoch and retires pending decisions, conservatively invalidating old operation/grant pairs even if only plan-level restrictions changed. Later widening does not reactivate them. Reordering alone preserves the policy epoch. An allowance is a restriction ceiling, not a grant and not permission to launch an effect.


## Terminal dashboard

```sh
./bin/vigil dashboard PROJECT_ID
# or: make dashboard PROJECT=PROJECT_ID

# Offline interaction mechanics in marked disposable synthetic repositories only:
./bin/vigil dashboard PROJECT_ID --synthetic-interactions \
  --history-state readable --history-class interrupted

# Optionally persist one visibly labelled fixture clarification for the same
# live dashboard owner (the session must already belong to the synthetic run):
./bin/vigil dashboard PROJECT_ID --synthetic-interactions \
  --clarification-run RUN_ID --clarification-session SESSION_RECORD_ID \
  --clarification-key fixture-request-1 \
  --clarification-prompt "Which fixture color should be recorded?"
```

The dashboard reads one consistent database snapshot for readiness, queue, task/plan evidence, the first 100 pending decisions and latest 100 history events. It refreshes asynchronously every two seconds. Failed refreshes retain the previous snapshot with a visible warning. Project text is stripped of terminal control sequences. Queue, continue, pause, advance and permission allow/deny use the same receipt/revision handlers as the CLI and run asynchronously with pending/success/failure feedback. In Tasks or Detail, `h` records explicit human acceptance, `m` records Pass for the exact displayed manual criterion, and `t` invokes core task acceptance through a registered coordinator owner. The focused task and exact displayed task revision are carried into and transactionally rechecked by each quality command; concurrent definition changes fail stale.

In Inbox, planning approval, rejection and replacement-revision requests are
distinct exact-revision actions. Recovery offers exact resume, fresh context and
remain blocked; exact/fresh choices require the injected live owner and its trusted
history/checkpoint inspector. Native clarification input enters a bounded text mode
and binds the visibly displayed request, session and native request key. The owner
persists the response intent before delivery; a failed or unproven delivery becomes
uncertain and is never replayed automatically. A standalone dashboard without the
owning runtime capability can still display these requests but fails closed if an
owner-only action is attempted. No key starts a model or reconstructs a provider
request handle from persisted or rendered text.

`--synthetic-interactions` is the reachable offline application path for those
owner-only mechanics. Every repository in the selected run must carry the
disposable-fixture marker and the run must have `runtime_kind=synthetic`.
`--history-state` accepts `readable`, `missing`, `corrupt` or `unsupported`;
`--history-class` and `--history-automatic-work` complete the explicit synthetic
history observation. The optional `--clarification-run`, `--clarification-session`,
`--clarification-key` and `--clarification-prompt` flags create one bounded,
visibly fixture-labelled request owned for that dashboard lifetime. Its successful
mechanical delivery writes a `fixture_native_clarification_delivered` event and is
never production/native-provider evidence. Without `--synthetic-interactions`, all
of these fixture flags are rejected and the normal dashboard gains no runtime
authority.

Keys:

| Key | Action |
| --- | --- |
| `1`–`5`, `tab`, `right` / `shift+tab`, `left` | Change view. Note the **horizontal** arrows change the view; they do not scroll |
| `j` / `down`, `k` / `up` | Move the visibly focused request in Inbox; scroll one line in other views |
| `pgdown`, `pgup` | Scroll one page |
| `home`, `end` | Jump to start / end |
| `r` | Refresh now |
| `p`, `c`, `a`, `u` | Pause, continue, advance, or queue the first displayed queueable plan |
| `g`, `v`, `n` | Apply, request replacement of, or reject the exact visibly focused planning proposal revision (`n` remains deny/cancel for the relevant non-proposal request) |
| `x`, `f`, `b` | Choose exact resume, fresh context or remain blocked for the visibly focused recovery request; resume choices require the live owner inspector |
| `i` | Answer the visibly focused native or non-native input request; `Enter` submits, `Esc` abandons local input and `Ctrl+C` exits. Native delivery remains owner-routed |
| `y`, `n` | Allow once or deny/reject/cancel/dismiss the visibly focused request according to its distinct type |
| `[`, `]` | Select the previous or next visibly rendered manual criterion for the focused task |
| `h`, `m`, `t` | On Tasks/Detail, record exact-revision human acceptance, Pass for the selected manual criterion, or core task acceptance |
| `q`, `esc`, `ctrl+c` | Quit |


## Resource intent inspection

`vigil project reservation PROJECT_ID OPERATION_ID` reads a trusted core reservation journal: owning instance, intended run, all registered/enrolled participating roots, observed claim/ticket generations and phase. It neither acquires nor releases resources. The explicit synthetic start command acquires through this journal; production acquisition remains qualification-gated. A retired intent is explicitly `uncertain`; its coordinator quarantine remains separate. The journal supports same-live-owner recovery across project/coordinator persistence gaps, not adoption by a new owner after a crash.


## Check definitions and manual setup prerequisites

Project `check_definitions` specify an ID, exact argv array, explicit project-relative `cwd`, a sorted explicit environment, sorted required output paths, positive `timeout_ms` within the project attempt ceiling and an optional output ceiling up to 16 MiB. Use an exact absolute executable or explicitly approve an absolute, canonical `PATH`; ambient executable lookup is never used. `HOME`, `TMPDIR`, `TMP` and `TEMP` are runner-owned and cannot be overridden. Definitions are immutable within their configuration snapshot. This declares a check; it does not execute it. Readiness blocks any required task/plan check whose definition is absent. Tasks and plans can add checks and cannot remove the project-required set.

Tasks can also carry `manual_prerequisites`, for example:

```json
[{"id":"device","description":"Required test device connected","satisfied":false}]
```

An unsatisfied prerequisite blocks readiness. A human plan revision may set `satisfied` to true only with nonempty `evidence` text. This records setup evidence only: it creates no manual functional Pass, quality evidence, acceptance or run. Manual criteria on finished code still need the later fingerprint-bound review flow. Definition changes remain versioned and invalidate prior operation authority.

## Offline quality and acceptance commands

The current commands require a regular `.vigil-disposable-fixture` marker in every enrolled root and explicit fixture flags. Core admission rechecks the markers before durable effect preparation; a caller-supplied actor cannot bypass it. `qualified_runtime` and all production check/reviewer routes remain unavailable. A task begins this sequence in `checking`; a plan begins it in `verifying` after all tasks are accepted.

This offline Stage 5.4 path is independently accepted through `cba322b`. P3 readiness, signal-evidence and early-cancellation corrections are implemented at `99cd6c0` and await independent follow-up; they do not reopen any accepted R-finding. Production dispatch remains disabled, and the Darwin fork-accounting observation plus native macOS validation remain separate evidence items.

Run every current required check by its definition ID. Add `--plan-wide` and use the plan ID as `TARGET_ID` for plan gates:

```sh
./bin/vigil project quality-check PROJECT_ID TASK_ID unit-tests \
  --command-id check-001 --synthetic-fixture
```

The runner opens a durable budget segment with the executing effect, verifies an exact-permission isolated copy against the enrolled source, executes the approved argv without user-supplied shell interpretation under the explicit environment/cwd/timeout, bounds output and inherited-pipe draining, and retires the process group plus every descendant before releasing owned claims. On Linux a per-check subreaper supervisor adopts and reaps detached descendants even when they clear their environment; failure to prove cleanup is containment uncertainty. The parent proceeds only after byte-exact private readiness proof, with the read bounded by both the caller context and a defence-in-depth timeout; cancellation/timeout closes and joins the reader, and missing, malformed, short or unreadable proof remains uncertain. Private readiness/completion descriptors retain `FD_CLOEXEC`. Signal-terminated checks record `128 + signal` from `syscall.WaitStatus.Signal()` while supervisor status 125 remains reserved and a child status collision remaps to 124. Darwin uses a fixed internal stop/exec wrapper to register fork observation before approved code executes. If a detached Darwin fork cannot be tied to the original group or an observed PID, containment is unavailable: the effect and budget segment become uncertain, claims remain held and no result can pass. Copied regular-file and directory modes are restored explicitly after creation, independent of the process umask. Only declared required-output paths may differ after execution. Nonzero exit, timeout, interruption, overflow, missing output, copied/original source mutation or missing/corrupt evidence cannot pass. Charging extends through post-process observation/artifact work into the terminal transaction. A crash or persistence failure charges unknown time, leaves the effect uncertain and blocks a fresh check/review/assessment command for the target across scope changes; it is never automatically replayed.

Supply a fresh fixture review in the closed schema; `SESSION_ID` and `NATIVE_ID` must be new, distinct from one another and from implementation/prior review identities:

```sh
./bin/vigil project quality-review PROJECT_ID TASK_ID \
  --command-id review-001 --result review.json \
  --session-id fixture-review-001 --native-identity fixture-native-001 \
  --synthetic-fixture
```

Example `review.json`:

```json
{"schema_version":1,"decision":"pass","summary":"fixture review only","findings":[]}
```

The application validates findings and derives blocking status from `review_blocking_severity`; reviewer claims cannot lower it. Review is read-only and has no code-writing, acceptance, publishing or delivery authority. Terminal review/assessment evidence is retained, but its task transition is conditional on the exact revision and in-progress state, so a late result cannot overwrite a human stop or request-changes action. A rejection routes to Stage 5.3's bounded repair flow; changed source requires fresh checks and a distinct fresh review without resetting the ledger. An exact baseline exception may restore `checking` for remaining checks/review only when no already-observed current check remains blocking.

Record each manual criterion and any configured human decision explicitly. Only manual `pass` satisfies a manual gate; a human `accept` does not manufacture it:

```sh
./bin/vigil project quality-manual PROJECT_ID TASK_ID CRITERION_ID \
  --command-id manual-001 --outcome pass --evaluator fixture-user \
  --notes 'fixture verification only' --synthetic-fixture
./bin/vigil project quality-decision PROJECT_ID TASK_ID \
  --command-id decision-001 --action accept \
  --rationale 'fixture acceptance only' --synthetic-fixture
```

Manual outcomes are `pending`, `pass`, `fail` or `cannot_verify`; decisions are `accept`, `request_changes`, `clarify` or `stop`. These offline commands always label their actor as fixture-human and are not evidence of real user acceptance.

Finally, atomically recheck every current revision, fingerprint, artifact, selected gate, budget and unresolved effect. Acceptance builds the committed manifest under a quality-authority epoch and compares that epoch inside its immediate write transaction, so a pending/manual or other evidence commit between gate reads and acceptance records `raced` instead of accepting:

```sh
./bin/vigil project quality-accept PROJECT_ID TASK_ID \
  --command-id accept-task-001 --synthetic-fixture
./bin/vigil project quality-accept PROJECT_ID PLAN_ID \
  --command-id accept-plan-001 --plan-wide --synthetic-fixture
```

Task acceptance can move the completed task to `accepted`; once all tasks are accepted, the plan moves to `verifying`. Staleness invalidates the prior acceptance, and a later task check can reopen the task for fresh evidence. Plan checks/review/manual/decision commands use `--plan-wide` and charge the plan-services ledger. Plan acceptance re-observes each child task and revalidates its non-invalidated acceptance, current gates and artifacts before moving only to `finalizing`. Neither acceptance command authorizes or performs a commit, push, publication or delivery.

## Bounded planning turn

`planning-run PROJECT_ID` performs one explicitly authorized local llama turn
through a freshly prepared Hermes fixture. It persists the intent and reserves
the pre-plan services budget before inference, accepts only a closed proposal,
and creates an approval inbox item; it never applies or approves the proposal.
The same command ID returns the persisted proposal without replaying inference.

```sh
./bin/vigil project planning-run PROJECT_ID \
  --live-local --manifest .cache/spike/stage1-EXAMPLE/launch.json \
  --command-id planning-001 --expected-revision 4 \
  --proposal-id proposal-001 --plan-id example-plan \
  --spec-id example-spec --spec-revision 1 \
  --profile-id local --profile-revision 1 --active-limit-ms 300000
```

| Flag | Required | Default | Meaning |
| --- | --- | --- | --- |
| `--live-local` | yes | `false` | Explicitly authorize this one prepared local inference turn |
| `--manifest PATH` | yes | — | Fresh prepared qualification manifest |
| `--key-file PATH` | no | — | Optional private mode-600 key for the loopback llama endpoint |
| `--command-id ID` | yes | — | Replay-safe planning command identity |
| `--expected-revision N` | yes | — | Exact displayed project revision |
| `--proposal-id ID` | yes | — | Stable proposal identity |
| `--plan-id ID` | yes | — | Server-selected plan identity the model cannot change |
| `--spec-id ID` | yes | — | Immutable imported specification identity |
| `--spec-revision N` | yes | — | Exact specification revision |
| `--profile-id ID` | yes | — | Explicit eligible planning profile |
| `--profile-revision N` | yes | — | Exact latest profile revision |
| `--active-limit-ms N` | no | `300000` | Active turn cap, 1–300000 ms |

If the controller restarts while a planning attempt is executing, inspect the
persisted attempt and explicitly reconcile that exact identity before retrying
or approving its proposal:

```sh
./bin/vigil project planning-reconcile PROJECT_ID ATTEMPT_ID \
  --command-id planning-reconcile-001
```

This receipt-backed human recovery marks the full reserved attempt cap unknown;
it never assumes the provider outcome or retries inference. Proposal application
fails while an attempt for that plan remains unresolved.

Provider requests for tools, approval or clarification are denied and fail the
turn. A crash leaves an executing attempt unresolved and prevents replay until
an explicit recovery records its entire reserved cap as unknown. Applying the
exact proposal later transfers completed/failed/unknown planning charges into
the ordinary 30-minute plan-services ledger. This command does not support a
Codex route: contained included-subscription use remains fail-closed until a
supported route avoids copying credentials or exposing the account home.

## Bounded native tool transport

`tool-server PROJECT_ID SESSION_ID` is the small stdio MCP adapter used by a
native harness after the application has opened an injected tool session. It is
not an operator authorization command: it cannot create a session, select a
project/role/run/generation, or widen capabilities. Its newline-delimited
JSON-RPC surface supports `initialize`, `tools/list` and `tools/call`; all calls
reuse the bounded application handlers, caps, persisted generation checks and
audit receipts. The MCP SDK's reserved top-level `_meta` transport member is
accepted and ignored within the same 64 KiB frame cap; application arguments
remain recursively closed and cannot supply project, role, run, session or other
authority fields.

```sh
./bin/vigil --state-dir STATE project tool-server PROJECT_ID SESSION_ID
```

`tool-qualify PROJECT_ID` performs one explicitly enabled local Hermes turn in
the prepared disposable fixture. It exposes only the injected Vigil MCP server,
requires exactly one audited `project.read`, proves native idle, retires the
generation and verifies that a stale call is rejected. It grants no execution,
acceptance, spending, delivery or publishing authority.

```sh
./bin/vigil --state-dir STATE project tool-qualify PROJECT_ID \
  --live-local --manifest .cache/spike/stage1-EXAMPLE/launch.json \
  --command-id hermes-tool-qualification-001 \
  --profile-id local --profile-revision 1
```

| Flag | Required | Default | Meaning |
| --- | --- | --- | --- |
| `--live-local` | yes | `false` | Authorize this one existing-local-llama qualification turn |
| `--manifest PATH` | yes | — | Prepared Hermes qualification manifest |
| `--command-id ID` | yes | — | Receipt and injected tool-session identity |
| `--profile-id ID` | yes | — | Profile whose persisted route identity the turn claims |
| `--profile-revision N` | yes | — | Exact current profile revision |

The qualifier copies only credential-free configuration into a temporary
private home and supplies the loopback credential by environment reference.
It opens the injected Vigil session first, then asks the native Hermes gateway to
reload MCP and refuses the turn unless `tools.list` exposes exactly the single
reduced capability `mcp__vigil__project_read`; the planning role's other ordinary
capabilities are not enabled for this qualification. Server-injected capability
subsets may reduce but never expand role authority and are receipt-bound.
The existing llama service must already be reachable; Vigil does not start or
reconfigure it. Codex native-tool qualification remains unavailable until a
contained supported ChatGPT-auth route exists.


## Read-only repository discovery

`vigil project discover PROJECT_ID` validates the registered root identity and reports existing nested Git roots, canonical/common-Git identities, current HEAD commit/ref when available, gitfile layouts and explicit unsupported/unborn issues. It excludes Git administration trees from traversal and skips directory symlinks. Discovery is bounded to 100 repositories, 100,000 entries and 15 seconds. Git output is bounded, ambient Git overrides are removed, and no status/filter/hook, repository script, submodule update or mutation runs.

This is an observed inventory. It does not enroll repositories, select bases, create branches, infer a dirty-work choice or qualify unsupported layouts. Those remain explicit setup and execution steps.


## Stage 5.6 factual archives and retention

`commit-prepare` reads a bounded JSON request with `command_id`, `plan_id`,
`repository_id`, `task_id`, exact repository-relative `paths`, `message`,
`author_name` and `author_email`. It requires a current accepted task scope,
previews a tree using temporary Git objects and index, then creates an inbox
approval request with the exact tree, parent, plan ref, paths and acceptance
identity. Retry the same command ID with identical arguments to retrieve the
same request. Pre-existing dirty paths must have been explicitly included at
repository enrollment. Paths outside the accepted task scope, nested repos,
the application-owned `.git` and `.vigil` directories, filters and hooks are
rejected or bypassed.

```sh
./bin/vigil --state-dir STATE project commit-prepare PROJECT_ID --file COMMIT.json
# Resolve its inbox request with the existing permission.grant command.
./bin/vigil --state-dir STATE project commit-execute PROJECT_ID OPERATION_ID --grant-id GRANT_ID
```

`commit-execute` consumes an exact grant once, rechecks accepted bytes and
identity, creates a deterministic commit object, journals it, and advances
only the approved non-checked-out plan ref by compare-and-swap. It never
changes the user's index, worktree or HEAD. Re-run without `--grant-id` only
to reconcile an already started operation after a crash. A commit is not
task acceptance, push authority or draft-request authority.

`push-prepare` observes the enrolled remote's single fetch/push URL, the
exact local plan ref and head commit, and the exact remote destination ref.
It requires a successful app-owned plan commit and issues a separate human
approval request. Local/bare remotes need no credential; SSH remotes require
`--credential-ref env:SSH_AUTH_SOCK` and an already available agent socket.
HTTPS token embedding, changed push URLs, configured extra refspecs, tags,
force/mirror pushes and checkpoint refs are not used. The operator must review
the destination identity, old and new object IDs in the returned intent.

```sh
./bin/vigil --state-dir STATE project push-prepare PROJECT_ID PLAN_ID REPOSITORY_ID \
  --command-id ID --remote NAME [--credential-ref env:SSH_AUTH_SOCK]
# Resolve its distinct inbox request with permission.grant.
./bin/vigil --state-dir STATE project push-execute PROJECT_ID OPERATION_ID --grant-id GRANT_ID
```

`push-execute` rechecks remote configuration and the local head before
consuming the grant, pushes exactly `HEAD_OID:PLAN_REF` without force, then
observes the exact remote ref. A lost or ambiguous response leaves the
operation uncertain; rerunning without `--grant-id` only reconciles the
remote observation and never automatically pushes again. These product
commands are not used against a real remote in the default test suite.

`draft-prepare` reads JSON with `command_id`, `plan_id`, `repository_id`,
`provider` (`github` or `gitlab`), exact `project`, `api_base`, `base_branch`,
`title`, optional `body`, and `credential_ref` (`env:NAME` for the trusted
application). It requires a succeeded exact-head push, a current plan
acceptance and factual archive, and binds the observed destination base OID.
Only official GitHub/GitLab API endpoints match real SSH remotes; a marked
disposable fixture may use credential-free loopback hosting with
`synthetic_fixture: true`. Production dispatch remains disabled.

```sh
./bin/vigil --state-dir STATE project draft-prepare PROJECT_ID --file DRAFT.json
# Resolve this third, distinct inbox request with permission.grant.
./bin/vigil --state-dir STATE project draft-execute PROJECT_ID OPERATION_ID --grant-id GRANT_ID
```

`draft-execute` searches bounded exact head/base results before a single POST,
requires a verified draft response, then appends its URL to a new factual
archive revision. A timeout or ambiguous result becomes uncertain; retry
without `--grant-id` performs observation only, never a second POST. A hosting
response that created the exact request as non-draft is journaled as an
uncertain external side effect requiring manual remediation. There is
no merge command or endpoint. Routine tests use fake hosting and local bare
remotes only; real publication needs its own exact operation grant.

```sh
./bin/vigil --state-dir STATE project delivery-status PROJECT_ID OPERATION_ID
./bin/vigil --state-dir STATE project delivery-cancel PROJECT_ID OPERATION_ID --command-id ID
./bin/vigil --state-dir STATE project delivery-reconcile PROJECT_ID OPERATION_ID --command-id ID
./bin/vigil --state-dir STATE project delivery-close-unobserved PROJECT_ID OPERATION_ID \
  --command-id ID --attest "VERIFIED_EXTERNAL_STATE"
```

`delivery-status` inspects one exact commit/push/draft operation and any
observed delivery journal row without changing anything. It also reports the
approved comparison operands (target ref, approved predecessor, approved
tree/head, or project/head/base) so a diverged observation can be resolved
without re-deriving the intent. `delivery-cancel` closes only a prepared,
never-started operation — no grant consumed, no delivery journal row — and
cancels its pending approval request. An executing or uncertain operation is
never cancelled: it may have an external effect.

`delivery-reconcile` resolves a stuck executing or uncertain operation from a
fresh exact observation. **Automatic closure requires positive proof that the
approved effect happened** — the operation becomes `observed` with a succeeded
delivery, and retention stops pinning the plan. Nothing automatic ever records
that an unobserved effect did *not* occur, because no durable state can
distinguish an executor mid-effect from one that crashed mid-effect.

For a **push** whose destination still holds the approved predecessor (the
common lost-response case, e.g. a dropped VPN), reconcile re-attempts the
identical non-force `OID:ref` push. That re-attempt is idempotent: a no-op if
the first push landed, the delivery if it did not, and a Git rejection (leaving
the operation open) if another writer moved the destination. No other kind is
retried, and reconciliation never moves a local ref or POSTs a draft.

Anything else — a plan ref or remote ref moved by a third party, or a hosting
listing without the exact draft — is reported with the approved operands above
and left open, because absence is not proof of non-delivery. Reconciliation
claims the operation while it works so no new executor starts an effect
mid-decision, and releases the claim when blocked.

`delivery-close-unobserved` is the explicit **human-attested** exit for an
operation the operator has verified externally did not take effect. The
required `--attest` text is recorded in a human receipt and echoed in the
result, so a later reader can always distinguish "the system proved this" from
"a person asserted this". The operation becomes `reconciled` with a failed
delivery, unblocking retention.

```sh
./bin/vigil --state-dir STATE project delivery-close-unobserved PROJECT_ID OPERATION_ID \
  --command-id ID --attest "verified refs/heads/x is still at the approved predecessor"
```

The archive controls below are local and do not create a delivery operation. `archive-build`
requires a current independently accepted plan and task set, unchanged accepted
repository fingerprints, and available durable evidence. It persists the factual
manifest before any narrative and creates one visible system finalization task.
The new archive is `factual_ready`; the first narrative attempt marks it
`narrative_pending`. The resulting plan remains `finalization_pending` until a
separately validated result completes it. Repeating the same `--command-id`
returns its receipt; building again with a new ID creates a new archive
revision (bounded). Every archive and narrative command also materializes the
immutable per-plan view inside the Git-ignored `.vigil` folder of each accepted
repository (`.vigil/plans/PLAN_ID/archive/`), kept out of `git status` through
the repository-local `.git/info/exclude`; the private artifact store stays
authoritative and the view is excluded from fingerprints, checkpoints and
commits. A view that cannot be written — for example when the operator owns a
`.vigil` *file* where the folder belongs — is reported as a `view_warning` on
the returned record, not as a command failure: the durable archive and its
state transition have already committed, and repeating the command re-verifies
the receipt. Existing user content inside a `.vigil` directory is never
modified.

```sh
./bin/vigil --state-dir STATE project archive-build PROJECT_ID PLAN_ID --command-id ID
./bin/vigil --state-dir STATE project archive-show PROJECT_ID PLAN_ID REVISION
./bin/vigil --state-dir STATE project archive-export PROJECT_ID PLAN_ID REVISION DESTINATION
```

`archive-show` re-verifies the manifest and all referenced durable artifacts.
`archive-export` requires a new absolute directory with no symlink ancestor,
copies verified content-addressed artifacts, and writes portable relative
references plus separately typed external URLs. It never overwrites an export.
The private application state directory remains authoritative; export is an
explicit copy, not a Git commit or publication.

`archive-narrative` is a synthetic fixture gate only. The repository must carry
the disposable-fixture marker and have fixture acceptance. The JSON file must
contain `command_id`, `plan_id`, `manifest_revision`, `manifest_digest`, bounded
`text`, `cited_ids` covering the plan and every task acceptance, and
`actor: "fixture"`. It cannot run a production model or accept a real plan.

```sh
./bin/vigil --state-dir STATE project archive-narrative PROJECT_ID PLAN_ID \
  --synthetic-fixture --file RESULT.json
```

`finalization-run` selects a current profile with the `finalization` role and
matching contained local Hermes provider. The live adapter is constructed
from the selected profile's own persisted route identity (version, endpoint,
credential reference), and the prepared manifest's secret environment name
must match that credential reference; the runner then requires the reported
identity to equal the selected eligible profile in all six fields. It is
limited to accepted disposable fixtures: the plan acceptance must have the
`fixture_core` actor and every enrolled repository must carry the
disposable-fixture marker. It reserves a
bounded allowance from the shared 30-minute plan-services ledger (at most ten
attempts per plan), creates one durable finalization run, sends only the
verified factual manifest to the provider, requires terminal idle, and
validates closed narrative citations before completing the plan. A failed
idle-observed result charges elapsed time and leaves the accepted plan
`finalization_pending`; a new command can retry only the narrative. An
unconfirmed provider outcome is `unknown` and is never replayed automatically.
It does not turn on production implementation or review dispatch and requires
explicit `--live-local` for the prepared llama route. No metered endpoint
fallback is permitted.

```sh
./bin/vigil --state-dir STATE project finalization-run PROJECT_ID PLAN_ID \
  --live-local --manifest PREPARED.json --command-id ID \
  --archive-revision N --manifest-digest SHA256 \
  --profile-id PROFILE --profile-revision N --active-limit-ms 300000 \
  [--key-file PRIVATE_KEY_FILE]
./bin/vigil --state-dir STATE project finalization-quarantine PROJECT_ID ATTEMPT_COMMAND_ID
```

`finalization-quarantine` conservatively accounts for an overdue, unconfirmed
fixture finalization attempt after its wall deadline plus a one-minute grace
period: the run becomes `unknown` with `unconfirmed` writer state — the global
execution fence stays held — the full reserved cap is charged as unknown time,
and the visible finalization task returns to `ready` for an explicit retry
with a new command. It never asserts that a native writer stopped.

Project policy `transcript_retention_days` is optional: `0` or omission means
30 days after plan completion; an explicit value may be 1–36500 days. Only
application-owned `transcript` artifacts linked to completed-plan runs become
eligible. Unfinished runs/plans, live pending requests, unresolved operations
and other typed references block expiry; a pending request whose deadline has
passed no longer blocks. Expiry is human-gated in two steps: `retention-inspect`
persists the exact dry-run candidate set as a durable human receipt, and
`retention-expire` consumes that receipt unchanged through a human envelope
with an expected-revision check — a changed candidate set or a missing or
already-consumed receipt refuses the expiry. Expiry marks references
unavailable before deleting unshared bytes; it does not touch durable
evidence, checkpoint artifacts or global harness history.

```sh
./bin/vigil --state-dir STATE project retention-inspect PROJECT_ID --command-id ID
./bin/vigil --state-dir STATE project retention-expire PROJECT_ID \
  --command-id ID --inspect-command-id INSPECT_ID
```

## Development-only spike runner

`spike` is a bounded Stage 1–3 experiment runner, not a production path. It probes
or drives a prepared harness fixture outside the persisted core, writes no
application state, and is never a fallback for any `project` command. Both
`--manifest` and `--harness` are **required**; the command fails without them.

```sh
# metadata probe only; starts no model turn
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness hermes

# one live model-backed fixture turn
./bin/vigil spike --manifest .cache/spike/stage1-EXAMPLE/launch.json --harness codex --live
```

| Flag | Required | Default | Meaning |
| --- | --- | --- | --- |
| `--manifest PATH` | yes | — | Prepared launch manifest from `scripts/spike/prepare.py` |
| `--harness NAME` | yes | — | `codex` or `hermes` |
| `--live` | no | `false` | Perform the real fixture turn instead of a metadata probe |
| `--llama-key-file PATH` | no | — | Private key file for a local llama route; avoids putting the key in argv or the manifest |
| `--scenario NAME` | no | — | Lifecycle scenario: `resume`, `interrupt`, `child`, `loss`, `clarify`, `approval-allow`, `approval-deny` |

`--live` starts a real model turn and therefore may consume configured model
capacity and touches the configured harness account. The separate
`project planning-run` command is the only model-backed persisted-core path and
is local-Hermes-only. Spike scenarios may intentionally fail and leave partial fixture
files; prepare a fresh fixture for every attempt. See
[Stage 1 contract](../../history/adapter-spike.md) and the [Stage 3 plan](../../history/stage-3-plan.md).
