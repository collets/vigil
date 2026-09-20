-- Stage 4 executable design, NOT an installed application migration.
-- One project per database; connections must enable foreign_keys themselves.
PRAGMA foreign_keys = ON;
CREATE TABLE schema_migrations (
 version INTEGER PRIMARY KEY CHECK(version>0), digest TEXT NOT NULL, applied_at INTEGER NOT NULL
) STRICT;
CREATE TABLE project (
 id TEXT PRIMARY KEY, singleton INTEGER NOT NULL DEFAULT 1 UNIQUE CHECK(singleton=1),
 root TEXT NOT NULL, identity_json TEXT NOT NULL CHECK(json_valid(identity_json)),
 revision INTEGER NOT NULL CHECK(revision>0), policy_epoch INTEGER NOT NULL CHECK(policy_epoch>0),
 state TEXT NOT NULL CHECK(state IN('ready','paused','recovering','quarantined')),
 model_policy TEXT CHECK(model_policy IN('local_only','hybrid','cloud_allowed')),
 created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE repositories (
 id TEXT PRIMARY KEY, root TEXT NOT NULL UNIQUE, common_git_identity TEXT NOT NULL,
 base_ref TEXT NOT NULL, base_oid TEXT NOT NULL, identity_json TEXT NOT NULL CHECK(json_valid(identity_json)),
 inclusion TEXT NOT NULL CHECK(inclusion IN('participating','read_only','excluded'))
) STRICT;
CREATE TABLE config_snapshots (
 id TEXT PRIMARY KEY, digest TEXT NOT NULL UNIQUE, schema_version INTEGER NOT NULL CHECK(schema_version>0),
 resolved_json TEXT NOT NULL CHECK(json_valid(resolved_json)), origins_json TEXT NOT NULL CHECK(json_valid(origins_json)),
 created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE profiles (
 id TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>0), config_id TEXT NOT NULL REFERENCES config_snapshots(id),
 capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
 PRIMARY KEY(id,revision)
) STRICT;
CREATE TABLE project_configurations (
 project_id TEXT NOT NULL REFERENCES project(id), revision INTEGER NOT NULL CHECK(revision>0),
 policy_epoch INTEGER NOT NULL CHECK(policy_epoch>0), config_id TEXT NOT NULL REFERENCES config_snapshots(id),
 PRIMARY KEY(project_id,revision)
) STRICT;
CREATE TABLE plans (
 id TEXT PRIMARY KEY, revision INTEGER NOT NULL CHECK(revision>0), queue_rank INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN('draft','ready','queued','active','blocked','paused','verifying','finalizing','finalization_pending','completed','stopped')),
 service_limit_ms INTEGER NOT NULL CHECK(service_limit_ms>0), created_at INTEGER NOT NULL, completed_at INTEGER,
 CHECK(state!='completed' OR completed_at IS NOT NULL)
) STRICT;
CREATE UNIQUE INDEX one_active_plan ON plans((1)) WHERE state IN('active','blocked','paused','verifying','finalizing','finalization_pending');
CREATE INDEX plan_queue ON plans(state,queue_rank,id);
CREATE TABLE plan_revisions (
 plan_id TEXT NOT NULL REFERENCES plans(id), revision INTEGER NOT NULL CHECK(revision>0),
 spec_digest TEXT NOT NULL, definition_json TEXT NOT NULL CHECK(json_valid(definition_json)),
 author TEXT NOT NULL, accepted_at INTEGER, PRIMARY KEY(plan_id,revision)
) STRICT;
CREATE TABLE plan_repositories (
 plan_id TEXT NOT NULL REFERENCES plans(id), repository_id TEXT NOT NULL REFERENCES repositories(id),
 branch TEXT NOT NULL, base_oid TEXT NOT NULL, observed_head TEXT NOT NULL,
 PRIMARY KEY(plan_id,repository_id), UNIQUE(repository_id,branch)
) STRICT;
CREATE TABLE tasks (
 id TEXT PRIMARY KEY, plan_id TEXT NOT NULL REFERENCES plans(id), revision INTEGER NOT NULL CHECK(revision>0),
 kind TEXT NOT NULL CHECK(kind IN('implementation','planning','supervision','plan_verification','finalization')),
 state TEXT NOT NULL CHECK(state IN('draft','ready','running','checking','reviewing','awaiting_human','accepted','needs_repair','blocked','stopped')),
 rank INTEGER NOT NULL, block_reason TEXT, active_limit_ms INTEGER NOT NULL CHECK(active_limit_ms>0),
 repair_limit INTEGER NOT NULL CHECK(repair_limit>=0), infra_limit INTEGER NOT NULL CHECK(infra_limit>=0),
 UNIQUE(plan_id,id)
) STRICT;
CREATE INDEX ready_tasks ON tasks(plan_id,state,rank,id);
CREATE TABLE task_revisions (
 task_id TEXT NOT NULL REFERENCES tasks(id), revision INTEGER NOT NULL CHECK(revision>0),
 definition_json TEXT NOT NULL CHECK(json_valid(definition_json)), criteria_digest TEXT NOT NULL,
 definition_digest TEXT NOT NULL, author TEXT NOT NULL, human_criteria_authorization TEXT,
 PRIMARY KEY(task_id,revision)
) STRICT;
CREATE TABLE task_dependencies (
 plan_id TEXT NOT NULL, task_id TEXT NOT NULL, dependency_id TEXT NOT NULL, PRIMARY KEY(task_id,dependency_id),
 FOREIGN KEY(plan_id,task_id) REFERENCES tasks(plan_id,id),
 FOREIGN KEY(plan_id,dependency_id) REFERENCES tasks(plan_id,id), CHECK(task_id!=dependency_id)
) STRICT;
CREATE INDEX dependency_reverse ON task_dependencies(dependency_id,task_id);
-- The command transaction must reject multi-edge cycles; SQL checks only self/cross-plan edges.
CREATE TABLE runs (
 id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, plan_revision INTEGER NOT NULL,
 task_id TEXT NOT NULL, task_revision INTEGER NOT NULL, config_id TEXT NOT NULL REFERENCES config_snapshots(id),
 profile_id TEXT NOT NULL, profile_revision INTEGER NOT NULL,
 role TEXT NOT NULL CHECK(role IN('implementation','check','review','supervisor','finalization')),
 attempt_kind TEXT NOT NULL CHECK(attempt_kind IN('initial','repair','infrastructure','continuation')),
 state TEXT NOT NULL CHECK(state IN('queued','prepared','starting','active','waiting_input','stopping','completed','failed','interrupted','unknown')),
 writer_state TEXT NOT NULL CHECK(writer_state IN('unconfirmed','observed_stopped','contained_stopped')),
 active_limit_ms INTEGER NOT NULL CHECK(active_limit_ms>0), wall_limit_ms INTEGER NOT NULL CHECK(wall_limit_ms>0),
 created_at INTEGER NOT NULL, started_at INTEGER, ended_at INTEGER,
 FOREIGN KEY(plan_id,plan_revision) REFERENCES plan_revisions(plan_id,revision),
 FOREIGN KEY(plan_id,task_id) REFERENCES tasks(plan_id,id),
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision),
 FOREIGN KEY(profile_id,profile_revision) REFERENCES profiles(id,revision)
) STRICT;
CREATE UNIQUE INDEX one_execution ON runs((1)) WHERE state IN('prepared','starting','active','waiting_input','stopping') OR (state='unknown' AND writer_state='unconfirmed');
CREATE INDEX run_history ON runs(task_id,created_at,id);
CREATE TABLE sessions (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), generation TEXT NOT NULL UNIQUE,
 harness TEXT NOT NULL, durable_id TEXT, runtime_id TEXT, native_turn_id TEXT,
 native_home_ref TEXT NOT NULL, workspace_identity TEXT NOT NULL, profile_digest TEXT NOT NULL,
 capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)), last_sequence INTEGER NOT NULL DEFAULT 0 CHECK(last_sequence>=0)
) STRICT;
CREATE TABLE time_segments (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), category TEXT NOT NULL CHECK(category IN('active','human_wait','resource_wait','unknown')),
 started_at INTEGER NOT NULL, ended_at INTEGER, duration_ms INTEGER CHECK(duration_ms>=0), charged_ms INTEGER CHECK(charged_ms>=0),
 CHECK(ended_at IS NULL OR ended_at>=started_at), CHECK((ended_at IS NULL)=(duration_ms IS NULL))
) STRICT;
CREATE UNIQUE INDEX one_open_segment ON time_segments(run_id) WHERE ended_at IS NULL;
CREATE TABLE usage_observations (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), observed_at INTEGER NOT NULL,
 scope TEXT NOT NULL CHECK(scope IN('turn','session_cumulative')), provenance TEXT NOT NULL CHECK(provenance IN('observed','estimated')),
 input_tokens INTEGER CHECK(input_tokens>=0), output_tokens INTEGER CHECK(output_tokens>=0), cached_tokens INTEGER CHECK(cached_tokens>=0),
 cost_microunits INTEGER CHECK(cost_microunits>=0), currency TEXT, CHECK(cost_microunits IS NULL OR currency IS NOT NULL)
) STRICT;
CREATE TABLE operations (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, resource_digest TEXT NOT NULL, args_digest TEXT NOT NULL,
 policy_epoch INTEGER NOT NULL, state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain','reconciled','cancelled')),
 plan_id TEXT REFERENCES plans(id), task_id TEXT REFERENCES tasks(id), run_id TEXT REFERENCES runs(id),
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)), created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX uncertain_operations ON operations(state,created_at);
CREATE TABLE grants (
 id TEXT PRIMARY KEY, category TEXT NOT NULL,
 scope TEXT NOT NULL CHECK(scope IN('once','task','plan','project_permanent')),
 plan_id TEXT REFERENCES plans(id), task_id TEXT REFERENCES tasks(id), resource_pattern TEXT NOT NULL,
 revision_policy_json TEXT NOT NULL CHECK(json_valid(revision_policy_json)), policy_epoch INTEGER NOT NULL,
 granted_by TEXT NOT NULL, granted_at INTEGER NOT NULL, expires_at INTEGER, revoked_at INTEGER,
 reserved_operation TEXT REFERENCES operations(id),
 CHECK(scope!='task' OR task_id IS NOT NULL), CHECK(scope!='plan' OR plan_id IS NOT NULL),
 CHECK(scope='once' OR reserved_operation IS NULL), CHECK(expires_at IS NULL OR expires_at>granted_at)
) STRICT;
CREATE INDEX grant_lookup ON grants(category,scope,revoked_at,expires_at);
CREATE TRIGGER once_grant_not_reused BEFORE UPDATE OF reserved_operation ON grants
 WHEN OLD.reserved_operation IS NOT NULL AND NEW.reserved_operation IS NOT OLD.reserved_operation
 BEGIN SELECT RAISE(ABORT,'once grant already consumed'); END;
CREATE TABLE requests (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN('approval','input','failed_check','review','manual_check','recovery')),
 state TEXT NOT NULL CHECK(state IN('pending','resolved','denied','cancelled','expired')),
 plan_id TEXT REFERENCES plans(id), task_id TEXT REFERENCES tasks(id), task_revision INTEGER,
 run_id TEXT REFERENCES runs(id), session_id TEXT REFERENCES sessions(id), native_request_key TEXT,
 operation_id TEXT REFERENCES operations(id), grant_id TEXT REFERENCES grants(id),
 context_json TEXT NOT NULL CHECK(json_valid(context_json)), result_json TEXT CHECK(result_json IS NULL OR json_valid(result_json)),
 blocking INTEGER NOT NULL CHECK(blocking IN(0,1)), created_at INTEGER NOT NULL, deadline INTEGER, resolved_at INTEGER,
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision),
 CHECK((session_id IS NULL)=(native_request_key IS NULL)), UNIQUE(session_id,native_request_key)
) STRICT;
CREATE INDEX pending_inbox ON requests(state,blocking,created_at);
CREATE TABLE artifacts (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, digest TEXT NOT NULL, relative_path TEXT NOT NULL,
 byte_count INTEGER NOT NULL CHECK(byte_count>=0), state TEXT NOT NULL CHECK(state IN('available','expired','corrupt')),
 retention TEXT NOT NULL CHECK(retention IN('durable','transcript','unfinished')),
 created_at INTEGER NOT NULL, expires_at INTEGER,
 CHECK(relative_path NOT LIKE '/%' AND relative_path NOT LIKE '../%' AND relative_path NOT LIKE '%/../%')
) STRICT;
CREATE TABLE run_artifacts (
 run_id TEXT NOT NULL REFERENCES runs(id), artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 purpose TEXT NOT NULL, PRIMARY KEY(run_id,artifact_id,purpose)
) STRICT;
CREATE TABLE quality_evidence (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL, task_revision INTEGER NOT NULL,
 run_id TEXT NOT NULL REFERENCES runs(id), kind TEXT NOT NULL CHECK(kind IN('check','review','baseline')),
 code_digest TEXT NOT NULL, definition_digest TEXT NOT NULL, artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 result TEXT NOT NULL CHECK(result IN('pass','fail','accepted_baseline','invalid','interrupted')),
 evaluator_session TEXT REFERENCES sessions(id), baseline_authorization TEXT REFERENCES requests(id),
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision),
 CHECK(result!='accepted_baseline' OR baseline_authorization IS NOT NULL), CHECK(kind!='review' OR evaluator_session IS NOT NULL)
) STRICT;
CREATE TABLE findings (
 id TEXT PRIMARY KEY, evidence_id TEXT NOT NULL REFERENCES quality_evidence(id), severity TEXT NOT NULL CHECK(severity IN('critical','high','medium','low','suggestion')),
 blocking INTEGER NOT NULL CHECK(blocking IN(0,1)), details_json TEXT NOT NULL CHECK(json_valid(details_json)),
 resolution TEXT NOT NULL CHECK(resolution IN('open','repaired','accepted_suggestion','superseded'))
) STRICT;
CREATE TABLE manual_checks (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL, task_revision INTEGER NOT NULL, code_digest TEXT NOT NULL,
 description TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN('pending','pass','fail','cannot_verify')),
 evaluator TEXT, notes TEXT, artifact_id TEXT REFERENCES artifacts(id), evaluated_at INTEGER,
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision),
 CHECK(state='pending' OR (evaluator IS NOT NULL AND evaluated_at IS NOT NULL))
) STRICT;
CREATE TABLE acceptances (
 id TEXT PRIMARY KEY, task_id TEXT NOT NULL, task_revision INTEGER NOT NULL, code_digest TEXT NOT NULL,
 evidence_manifest TEXT NOT NULL REFERENCES artifacts(id), policy_epoch INTEGER NOT NULL,
 actor TEXT NOT NULL, accepted_at INTEGER NOT NULL, invalidated_at INTEGER,
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision)
) STRICT;
CREATE UNIQUE INDEX one_current_acceptance ON acceptances(task_id) WHERE invalidated_at IS NULL;
CREATE TABLE checkpoint_sets (
 id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), operation_id TEXT NOT NULL REFERENCES operations(id),
 state TEXT NOT NULL CHECK(state IN('capturing','verified','clearing','saved','restoring','conflicted','restored','incomplete')),
 manifest_id TEXT REFERENCES artifacts(id), created_at INTEGER NOT NULL,
 CHECK(state NOT IN('verified','clearing','saved','restoring','restored') OR manifest_id IS NOT NULL)
) STRICT;
CREATE TABLE checkpoint_repositories (
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id), repository_id TEXT NOT NULL REFERENCES repositories(id),
 base_oid TEXT NOT NULL, before_digest TEXT NOT NULL, captured_digest TEXT NOT NULL,
 artifact_id TEXT REFERENCES artifacts(id), progress_json TEXT NOT NULL CHECK(json_valid(progress_json)),
 PRIMARY KEY(checkpoint_id,repository_id)
) STRICT;
CREATE TABLE deliveries (
 id TEXT PRIMARY KEY, plan_id TEXT NOT NULL REFERENCES plans(id), repository_id TEXT NOT NULL REFERENCES repositories(id),
 operation_id TEXT NOT NULL UNIQUE REFERENCES operations(id), kind TEXT NOT NULL CHECK(kind IN('commit','push','draft_request')),
 state TEXT NOT NULL CHECK(state IN('prepared','pending','uncertain','succeeded','failed')),
 remote_identity TEXT, head_oid TEXT NOT NULL, base_ref TEXT, external_id TEXT, url TEXT,
 draft INTEGER NOT NULL DEFAULT 1 CHECK(draft=1)
) STRICT;
CREATE TABLE archives (
 plan_id TEXT NOT NULL REFERENCES plans(id), revision INTEGER NOT NULL CHECK(revision>0),
 factual_manifest TEXT NOT NULL REFERENCES artifacts(id), narrative TEXT REFERENCES artifacts(id),
 state TEXT NOT NULL CHECK(state IN('factual_ready','narrative_pending','verified')), created_at INTEGER NOT NULL,
 PRIMARY KEY(plan_id,revision), CHECK(state!='verified' OR narrative IS NOT NULL)
) STRICT;
CREATE TABLE command_receipts (
 id TEXT PRIMARY KEY, actor TEXT NOT NULL, args_digest TEXT NOT NULL, result_json TEXT NOT NULL CHECK(json_valid(result_json)), committed_at INTEGER NOT NULL
) STRICT;
CREATE TABLE events (
 sequence INTEGER PRIMARY KEY, schema_version INTEGER NOT NULL CHECK(schema_version>0),
 command_id TEXT REFERENCES command_receipts(id) DEFERRABLE INITIALLY DEFERRED,
 run_id TEXT REFERENCES runs(id), generation TEXT, source_sequence INTEGER,
 kind TEXT NOT NULL, occurred_at INTEGER NOT NULL, payload_json TEXT NOT NULL CHECK(json_valid(payload_json) AND length(CAST(payload_json AS BLOB))<=65536),
 UNIQUE(generation,source_sequence)
) STRICT;
CREATE INDEX events_for_run ON events(run_id,sequence);
CREATE TRIGGER events_no_update BEFORE UPDATE ON events BEGIN SELECT RAISE(ABORT,'events are append-only'); END;
CREATE TRIGGER events_no_delete BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'events are append-only'); END;
CREATE TRIGGER config_no_update BEFORE UPDATE ON config_snapshots BEGIN SELECT RAISE(ABORT,'config snapshot is immutable'); END;
CREATE TRIGGER task_revision_no_update BEFORE UPDATE ON task_revisions BEGIN SELECT RAISE(ABORT,'task revision is immutable'); END;
CREATE TRIGGER plan_revision_no_update BEFORE UPDATE ON plan_revisions BEGIN SELECT RAISE(ABORT,'plan revision is immutable'); END;
CREATE TRIGGER config_no_delete BEFORE DELETE ON config_snapshots BEGIN SELECT RAISE(ABORT,'config snapshot is immutable'); END;
CREATE TRIGGER task_revision_no_delete BEFORE DELETE ON task_revisions BEGIN SELECT RAISE(ABORT,'task revision is immutable'); END;
CREATE TRIGGER plan_revision_no_delete BEFORE DELETE ON plan_revisions BEGIN SELECT RAISE(ABORT,'plan revision is immutable'); END;
CREATE TRIGGER receipt_no_update BEFORE UPDATE ON command_receipts BEGIN SELECT RAISE(ABORT,'command receipt is immutable'); END;
CREATE TRIGGER receipt_no_delete BEFORE DELETE ON command_receipts BEGIN SELECT RAISE(ABORT,'command receipt is immutable'); END;
CREATE TRIGGER profile_no_update BEFORE UPDATE ON profiles BEGIN SELECT RAISE(ABORT,'profile revision is immutable'); END;
CREATE TRIGGER profile_no_delete BEFORE DELETE ON profiles BEGIN SELECT RAISE(ABORT,'profile revision is immutable'); END;
CREATE TRIGGER project_configuration_no_update BEFORE UPDATE ON project_configurations BEGIN SELECT RAISE(ABORT,'project config revision is immutable'); END;
CREATE TRIGGER project_configuration_no_delete BEFORE DELETE ON project_configurations BEGIN SELECT RAISE(ABORT,'project config revision is immutable'); END;
