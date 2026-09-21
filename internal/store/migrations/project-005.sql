-- Stage 5.3 checkpoint A: durable execution controls and recovery choices.
CREATE TABLE execution_controls (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN('stop','foreground_shutdown','persistence_failure')),
 run_id TEXT NOT NULL REFERENCES runs(id),
 generation_id TEXT NOT NULL REFERENCES run_generations(id),
 state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain')),
 interrupt_state TEXT NOT NULL CHECK(interrupt_state IN('pending','sent','unsupported','failed')),
 writer_state TEXT NOT NULL CHECK(writer_state IN('unconfirmed','observed_stopped','contained_stopped')),
 inference_state TEXT NOT NULL CHECK(inference_state IN('unknown','not_started','idle','active')),
 prepared_at INTEGER NOT NULL,
 observed_at INTEGER,
 observation_json TEXT NOT NULL CHECK(json_valid(observation_json)),
 UNIQUE(run_id,generation_id,kind)
) STRICT;
CREATE INDEX execution_control_recovery ON execution_controls(state,prepared_at);

CREATE TABLE recovery_choices (
 id TEXT PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES runs(id),
 source_generation_id TEXT NOT NULL REFERENCES run_generations(id),
 mode TEXT NOT NULL CHECK(mode IN('exact_resume','fresh_context','remain_blocked')),
 state TEXT NOT NULL CHECK(state IN('prepared','eligible','ineligible','consumed')),
 eligibility_json TEXT NOT NULL CHECK(json_valid(eligibility_json)),
 context_artifact_id TEXT REFERENCES artifacts(id),
 created_at INTEGER NOT NULL,
 consumed_at INTEGER
) STRICT;
CREATE INDEX recovery_choice_run ON recovery_choices(run_id,created_at,id);

CREATE TRIGGER execution_control_no_delete BEFORE DELETE ON execution_controls
 BEGIN SELECT RAISE(ABORT,'execution controls are recovery evidence'); END;
CREATE TRIGGER recovery_choice_no_delete BEFORE DELETE ON recovery_choices
 BEGIN SELECT RAISE(ABORT,'recovery choices are recovery evidence'); END;
