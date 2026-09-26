-- Stage 5.5 workflow, immutable specification/proposal state and model-tool sessions.
-- Existing plan/task ranks remain authoritative; this migration adds only the
-- explicit workflow policy and revisioned planning/tool boundary.
PRAGMA foreign_keys = ON;

CREATE TABLE workflow_controls (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 automatic_plan_advance INTEGER NOT NULL DEFAULT 0 CHECK(automatic_plan_advance=0),
 updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO workflow_controls(singleton,automatic_plan_advance,updated_at) VALUES(1,0,0);

CREATE TABLE workflow_dispatches (
 id TEXT PRIMARY KEY,
 selection_command_id TEXT NOT NULL UNIQUE,
 plan_id TEXT NOT NULL REFERENCES plans(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 phase TEXT NOT NULL CHECK(phase IN('implementation','check','review','supervisor','finalization')),
 project_revision INTEGER NOT NULL CHECK(project_revision>0),
 state TEXT NOT NULL CHECK(state IN('selected','consumed','retired')),
 selected_at INTEGER NOT NULL,
 consumed_run_id TEXT REFERENCES runs(id),
 CHECK(state!='consumed' OR consumed_run_id IS NOT NULL)
) STRICT;
CREATE UNIQUE INDEX one_selected_dispatch ON workflow_dispatches((1)) WHERE state='selected';

CREATE TABLE specification_revisions (
 id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),
 artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 content_digest TEXT NOT NULL,
 source_name TEXT NOT NULL,
 byte_count INTEGER NOT NULL CHECK(byte_count>=0 AND byte_count<=65536),
 actor TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 approved_at INTEGER,
 PRIMARY KEY(id,revision)
) STRICT;
CREATE UNIQUE INDEX specification_content_revision ON specification_revisions(id,content_digest);

CREATE TABLE planning_proposals (
 id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),
 specification_id TEXT NOT NULL,
 specification_revision INTEGER NOT NULL,
 expected_plan_id TEXT,
 expected_plan_revision INTEGER,
 profile_id TEXT NOT NULL,
 profile_revision INTEGER NOT NULL,
 definition_json TEXT NOT NULL CHECK(json_valid(definition_json) AND length(CAST(definition_json AS BLOB))<=65536),
 definition_digest TEXT NOT NULL,
 operation TEXT NOT NULL CHECK(operation IN('create','edit','split','merge','reorder')),
 affected_tasks_json TEXT NOT NULL CHECK(json_valid(affected_tasks_json)),
 rationale TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('proposed','approved','applied','rejected','stale')),
 request_id TEXT REFERENCES requests(id),
 actor TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 approved_at INTEGER,
 applied_at INTEGER,
 PRIMARY KEY(id,revision),
 FOREIGN KEY(specification_id,specification_revision) REFERENCES specification_revisions(id,revision),
 FOREIGN KEY(profile_id,profile_revision) REFERENCES profiles(id,revision),
 CHECK((expected_plan_id IS NULL)=(expected_plan_revision IS NULL))
) STRICT;
CREATE INDEX planning_proposals_state ON planning_proposals(state,created_at,id,revision);

CREATE TABLE tool_sessions (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES project(id),
 role TEXT NOT NULL CHECK(role IN('implementation','review','supervisor','planning','finalization')),
 run_id TEXT REFERENCES runs(id),
 native_session_id TEXT NOT NULL,
 generation TEXT NOT NULL,
 capabilities_json TEXT NOT NULL CHECK(json_valid(capabilities_json)),
 created_at INTEGER NOT NULL,
 retired_at INTEGER,
 UNIQUE(native_session_id,generation)
) STRICT;
CREATE INDEX tool_sessions_active ON tool_sessions(role,retired_at);

CREATE TABLE blocked_observations (
 id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL REFERENCES tool_sessions(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 task_revision INTEGER NOT NULL,
 reason TEXT NOT NULL,
 missing_facts_json TEXT NOT NULL CHECK(json_valid(missing_facts_json)),
 evidence_ids_json TEXT NOT NULL CHECK(json_valid(evidence_ids_json)),
 observed_at INTEGER NOT NULL,
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision)
) STRICT;

CREATE TRIGGER specification_revision_no_update BEFORE UPDATE ON specification_revisions
BEGIN SELECT RAISE(ABORT,'specification revision is immutable'); END;
CREATE TRIGGER specification_revision_no_delete BEFORE DELETE ON specification_revisions
BEGIN SELECT RAISE(ABORT,'specification revision is immutable'); END;
CREATE TRIGGER blocked_observation_no_update BEFORE UPDATE ON blocked_observations
BEGIN SELECT RAISE(ABORT,'blocked observation is immutable'); END;
CREATE TRIGGER blocked_observation_no_delete BEFORE DELETE ON blocked_observations
BEGIN SELECT RAISE(ABORT,'blocked observation is immutable'); END;
