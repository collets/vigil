# Persisted planning CLI

Stage 5 persists definitions, repository setup and a journaled execution lifecycle. Production agents remain qualification-gated; the only CLI execution driver in Stage 5.2 is an explicitly selected, marker-gated synthetic driver for disposable repositories. Execution can move a task to `checking`, never acceptance or delivery. `dashboard PROJECT_ID` remains read-only and `spike` remains a separate diagnostic runner with no production fallback.

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
```

Use the ID returned by initialization. State defaults to `$XDG_STATE_HOME/vigil` or `~/.local/state/vigil`; `--state-dir /absolute/private/path` overrides it. State must be outside managed checkout trees. Existing state directories/files must be private. Initialization reuses a physical root across symlink aliases and refuses overlapping registered projects. Project initialization installs embedded application migrations; the older `hello --db` only checks SQLite.

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
    "check_definitions": [{"id":"unit-tests","argv":["go","test","./..."],"cwd":".","timeout_ms":300000}],
    "task_limit_ms": 2700000,
    "attempt_limit_ms": 600000,
    "repair_limit": 2,
    "supervisor_profile": "local",
    "approval_mode": "supervised"
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

Plan import validates dependency graphs and retains immutable revisions. Existing tasks cannot be silently removed. Criteria changes require an explicit human flag. `plan.reorder` takes `{"plan_id":"first-plan","tasks":["first-task"]}` and preserves the exact task set and task revisions. Readiness reports missing profiles, unapproved specifications, unresolved questions, dependencies and policy constraints. Actual check/manual-result execution remains pending Stage 5.4; production eligibility remains false until an exact trusted launch/recovery qualification exists.

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

The repository must contain a committed regular `.vigil-disposable-fixture` marker. The disposable profile's `endpoint_id` must be `fixture-endpoint`. Register that synthetic capacity identity; this URL is never contacted by the fixture driver:

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

Clear derives agent-owned paths only from the validated execution result; there is no arbitrary path flag. It verifies both complete sets before changing any repository. Mixed or concurrent edits fail closed.

Restore additionally requires a checkpoint of the current destination. The restore command is the explicit human approval bound to all three set IDs and the current project revision:

```sh
./bin/vigil project checkpoint-save PROJECT_ID RUN_ID \
  --command-id save-destination-001 --expected-revision 12
./bin/vigil project checkpoint-restore PROJECT_ID TARGET_CHECKPOINT_ID BASELINE_CHECKPOINT_ID DESTINATION_CHECKPOINT_ID \
  --command-id restore-001 --expected-revision 13
```

If restore reports conflicts or is interrupted, do not discard any checkpoint. Inspect the persisted operation/path journal and repeat the identical command only after destination state still matches an expected or already-applied state.

## Exact resume and new attempts

The current CLI history inspector is intentionally synthetic-only. Record an explicit recovery choice after containment and a verified checkpoint:

```sh
./bin/vigil project execution-recovery-choose PROJECT_ID RUN_ID \
  --command-id recovery-001 --expected-revision 14 \
  --mode exact_resume --history-state readable --history-class interrupted \
  --synthetic-fixture
```

`--mode fresh_context` accepts `missing`, `corrupt` or `unsupported` history only when writer containment and a full verified checkpoint are available. `--mode remain_blocked` records the safe decision without creating an attempt.

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
```

The dashboard reads one consistent database snapshot for readiness, tasks, the first 100 pending/expired decisions and the latest 100 history events. It refreshes asynchronously every two seconds; Tab or 1–4 changes views, arrows/Page Up/Page Down scroll, `r` refreshes and `q` quits. Failed refreshes retain the previous snapshot with a visible warning. Project text is stripped of terminal control sequences. Decisions remain read-only here; use `project inbox` for full context and `project apply` for versioned human actions. No dashboard key starts a model, approves a request, accepts a task or performs delivery.


## Resource intent inspection

`vigil project reservation PROJECT_ID OPERATION_ID` reads a trusted core reservation journal: owning instance, intended run, all registered/enrolled participating roots, observed claim/ticket generations and phase. It neither acquires nor releases resources. The explicit synthetic start command acquires through this journal; production acquisition remains qualification-gated. A retired intent is explicitly `uncertain`; its coordinator quarantine remains separate. The journal supports same-live-owner recovery across project/coordinator persistence gaps, not adoption by a new owner after a crash.


## Check definitions and manual setup prerequisites

Project `check_definitions` specify an ID, exact argv array, explicit project-relative `cwd` and positive `timeout_ms` within the project attempt ceiling. Definitions are immutable within their configuration snapshot. This declares a check; it does not execute it. Readiness blocks any required or task check whose definition is absent. Tasks can add checks and cannot remove the project-required set.

Tasks can also carry `manual_prerequisites`, for example:

```json
[{"id":"device","description":"Required test device connected","satisfied":false}]
```

An unsatisfied prerequisite blocks readiness. A human plan revision may set `satisfied` to true only with nonempty `evidence` text. This records setup evidence only: it creates no manual functional Pass, quality evidence, acceptance or run. Manual criteria on finished code still need the later fingerprint-bound review flow. Definition changes remain versioned and invalidate prior operation authority.


## Read-only repository discovery

`vigil project discover PROJECT_ID` validates the registered root identity and reports existing nested Git roots, canonical/common-Git identities, current HEAD commit/ref when available, gitfile layouts and explicit unsupported/unborn issues. It excludes Git administration trees from traversal and skips directory symlinks. Discovery is bounded to 100 repositories, 100,000 entries and 15 seconds. Git output is bounded, ambient Git overrides are removed, and no status/filter/hook, repository script, submodule update or mutation runs.

This is an observed inventory. It does not enroll repositories, select bases, create branches, infer a dirty-work choice or qualify unsupported layouts. Those remain explicit setup and execution steps.
