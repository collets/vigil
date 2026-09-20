# Persisted planning CLI

Stage 5's first slice persists definitions and decisions. It does not launch production agents, accept tasks, or deliver changes. `dashboard PROJECT_ID` provides read-only persisted views; `spike` remains a separate diagnostic runner.

## Initialize and inspect

```sh
make build
./bin/vigil project init /absolute/path/to/project
./bin/vigil project list
./bin/vigil project status PROJECT_ID
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

Plan import validates dependency graphs and retains immutable revisions. Existing tasks cannot be silently removed. Criteria changes require an explicit human flag. `plan.reorder` takes `{"plan_id":"first-plan","tasks":["first-task"]}` and preserves the exact task set and task revisions. Readiness reports missing profiles, unapproved specifications, unresolved questions, dependencies and policy constraints. Full repository/check/manual-prerequisite definitions are still pending; production eligibility remains false.

## Permissions and artifacts

`operation.request` records intent using `category`, `resource_digest`, `arguments_digest`, `plan_id`, `task_id`, and `task_revision`. Digests must identify exact resources/arguments; this command performs no external effect. The inbox returns a request ID. `permission.grant` takes `request_id`, `decision` (`allow`/`deny`), `scope` (`once`/`task`/`plan`/`project_permanent`) and optional `expires_at` in Unix milliseconds. `permission.revoke` takes `grant_id`. All use the same command envelope and expected revision. Global scope is rejected until coordinator authorization is connected. Effect start is internal-only and performs no external action in this slice.

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
