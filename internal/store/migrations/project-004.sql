-- Stage 5.2 checkpoints B-D: immutable execution snapshots, generations,
-- effect journal, normalized observations, validated results and budgets.
CREATE TABLE run_snapshots (
 run_id TEXT PRIMARY KEY REFERENCES runs(id),
 task_snapshot_json TEXT NOT NULL CHECK(json_valid(task_snapshot_json)),
 config_snapshot_json TEXT NOT NULL CHECK(json_valid(config_snapshot_json)),
 profile_snapshot_json TEXT NOT NULL CHECK(json_valid(profile_snapshot_json)),
 budget_snapshot_json TEXT NOT NULL CHECK(json_valid(budget_snapshot_json)),
 repository_snapshot_json TEXT NOT NULL CHECK(json_valid(repository_snapshot_json)),
 expected_project_revision INTEGER NOT NULL CHECK(expected_project_revision>0),
 expected_plan_revision INTEGER NOT NULL CHECK(expected_plan_revision>0),
 expected_task_revision INTEGER NOT NULL CHECK(expected_task_revision>0),
 snapshot_digest TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE run_generations (
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES runs(id),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 runtime_kind TEXT NOT NULL CHECK(runtime_kind IN('synthetic','docker','native')),
 runtime_resource_id TEXT NOT NULL UNIQUE,
 container_name TEXT UNIQUE,
 native_session_id TEXT,
 native_turn_id TEXT,
 transport_generation TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL CHECK(state IN('prepared','creating','created','starting','active','stopping','terminal','unknown','contained')),
 submission_state TEXT NOT NULL CHECK(submission_state IN('not_attempted','writing','delivered','proven_not_delivered','uncertain')),
 qualification_request_json TEXT NOT NULL CHECK(json_valid(qualification_request_json)),
 qualification_evidence_id TEXT,
 checkout_plan_digest TEXT NOT NULL,
 expected_routes_json TEXT NOT NULL CHECK(json_valid(expected_routes_json)),
 created_at INTEGER NOT NULL,
 terminal_at INTEGER,
 UNIQUE(run_id,ordinal)
) STRICT;
CREATE TABLE execution_effects (
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES runs(id),
 generation_id TEXT NOT NULL REFERENCES run_generations(id),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 kind TEXT NOT NULL CHECK(kind IN('runtime_create','runtime_start','runtime_attach','native_create','native_resume','prompt_write','prompt_ack','terminal_observe','artifact_publish','outcome_commit','containment_stop')),
 state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain','reconciled','cancelled')),
 stable_identity TEXT NOT NULL,
 intent_json TEXT NOT NULL CHECK(json_valid(intent_json)),
 observation_json TEXT CHECK(observation_json IS NULL OR json_valid(observation_json)),
 prepared_at INTEGER NOT NULL,
 observed_at INTEGER,
 UNIQUE(generation_id,ordinal),
 UNIQUE(generation_id,kind)
) STRICT;
CREATE INDEX execution_effect_recovery ON execution_effects(state,prepared_at);
CREATE TABLE normalized_run_events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 run_id TEXT NOT NULL REFERENCES runs(id),
 generation_id TEXT NOT NULL REFERENCES run_generations(id),
 source_sequence INTEGER NOT NULL CHECK(source_sequence>0),
 native_turn_id TEXT,
 kind TEXT NOT NULL,
 occurred_at INTEGER NOT NULL,
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND length(CAST(payload_json AS BLOB))<=65536),
 UNIQUE(generation_id,source_sequence)
) STRICT;
CREATE TABLE execution_results (
 run_id TEXT PRIMARY KEY REFERENCES runs(id),
 generation_id TEXT NOT NULL REFERENCES run_generations(id),
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 status TEXT NOT NULL CHECK(status IN('completed','failed','interrupted','unknown')),
 summary TEXT NOT NULL,
 changed_paths_json TEXT NOT NULL CHECK(json_valid(changed_paths_json)),
 repository_fingerprints_json TEXT NOT NULL CHECK(json_valid(repository_fingerprints_json)),
 artifact_id TEXT REFERENCES artifacts(id),
 result_digest TEXT NOT NULL UNIQUE,
 validated_at INTEGER NOT NULL
) STRICT;
CREATE TABLE budget_ledgers (
 id TEXT PRIMARY KEY,
 scope TEXT NOT NULL CHECK(scope IN('task','plan_services')),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 task_id TEXT REFERENCES tasks(id),
 active_limit_ms INTEGER NOT NULL CHECK(active_limit_ms>0),
 charged_ms INTEGER NOT NULL DEFAULT 0 CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL DEFAULT 0 CHECK(unknown_ms>=0),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at INTEGER NOT NULL,
 CHECK((scope='task')=(task_id IS NOT NULL)),
 UNIQUE(scope,plan_id,task_id)
) STRICT;
CREATE TABLE active_segments (
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES runs(id),
 ledger_id TEXT NOT NULL REFERENCES budget_ledgers(id),
 category TEXT NOT NULL CHECK(category IN('active','human_wait','resource_wait','unknown')),
 monotonic_started_ns INTEGER NOT NULL CHECK(monotonic_started_ns>=0),
 monotonic_checkpoint_ns INTEGER NOT NULL CHECK(monotonic_checkpoint_ns>=monotonic_started_ns),
 wall_started_at INTEGER NOT NULL,
 wall_checkpoint_at INTEGER NOT NULL,
 ended_at INTEGER,
 charged_ms INTEGER NOT NULL DEFAULT 0 CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL DEFAULT 0 CHECK(unknown_ms>=0)
) STRICT;
CREATE UNIQUE INDEX one_active_budget_segment ON active_segments(run_id) WHERE ended_at IS NULL;
CREATE TABLE global_effect_links (
 project_operation_id TEXT PRIMARY KEY REFERENCES operations(id),
 coordinator_operation_id TEXT NOT NULL UNIQUE,
 global_grant_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain','cancelled')),
 created_at INTEGER NOT NULL,
 observed_at INTEGER
) STRICT;
CREATE TRIGGER run_snapshot_no_update BEFORE UPDATE ON run_snapshots
 BEGIN SELECT RAISE(ABORT,'run snapshot is immutable'); END;
CREATE TRIGGER run_snapshot_no_delete BEFORE DELETE ON run_snapshots
 BEGIN SELECT RAISE(ABORT,'run snapshot is immutable'); END;
CREATE TRIGGER normalized_event_no_update BEFORE UPDATE ON normalized_run_events
 BEGIN SELECT RAISE(ABORT,'normalized events are append-only'); END;
CREATE TRIGGER normalized_event_no_delete BEFORE DELETE ON normalized_run_events
 BEGIN SELECT RAISE(ABORT,'normalized events are append-only'); END;
CREATE TRIGGER execution_result_no_update BEFORE UPDATE ON execution_results
 BEGIN SELECT RAISE(ABORT,'execution result is immutable'); END;
CREATE TRIGGER execution_result_no_delete BEFORE DELETE ON execution_results
 BEGIN SELECT RAISE(ABORT,'execution result is immutable'); END;
