-- Stage 5.3 checkpoint D: recovery-attempt identity and durable exhaustion evidence.
CREATE TABLE recovery_attempt_links (
 choice_id TEXT PRIMARY KEY REFERENCES recovery_choices(id),
 source_run_id TEXT NOT NULL REFERENCES runs(id),
 prepared_run_id TEXT REFERENCES runs(id),
 prepared_generation_id TEXT REFERENCES run_generations(id),
 mode TEXT NOT NULL CHECK(mode IN('exact_resume','fresh_context')),
 expected_project_revision INTEGER NOT NULL CHECK(expected_project_revision>0),
 created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE recovery_choice_checkpoints (
 choice_id TEXT PRIMARY KEY REFERENCES recovery_choices(id),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id),
 created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE budget_exhaustions (
 id TEXT PRIMARY KEY,
 ledger_id TEXT NOT NULL REFERENCES budget_ledgers(id),
 run_id TEXT NOT NULL REFERENCES runs(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 stage TEXT NOT NULL,
 charged_ms INTEGER NOT NULL CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL CHECK(unknown_ms>=0),
 limit_ms INTEGER NOT NULL CHECK(limit_ms>0),
 state TEXT NOT NULL CHECK(state IN('pending_supervisor','assessed')),
 created_at INTEGER NOT NULL,
 UNIQUE(run_id,stage)
) STRICT;
CREATE INDEX budget_exhaustion_pending ON budget_exhaustions(state,created_at);

CREATE TRIGGER recovery_attempt_link_no_update BEFORE UPDATE ON recovery_attempt_links
 BEGIN SELECT RAISE(ABORT,'recovery attempt identity is immutable'); END;
CREATE TRIGGER recovery_attempt_link_no_delete BEFORE DELETE ON recovery_attempt_links
 BEGIN SELECT RAISE(ABORT,'recovery attempt identity is recovery evidence'); END;
CREATE TRIGGER recovery_choice_checkpoint_no_update BEFORE UPDATE ON recovery_choice_checkpoints
 BEGIN SELECT RAISE(ABORT,'recovery checkpoint authority is immutable'); END;
CREATE TRIGGER recovery_choice_checkpoint_no_delete BEFORE DELETE ON recovery_choice_checkpoints
 BEGIN SELECT RAISE(ABORT,'recovery checkpoint authority is recovery evidence'); END;
CREATE TRIGGER budget_exhaustion_no_delete BEFORE DELETE ON budget_exhaustions
 BEGIN SELECT RAISE(ABORT,'budget exhaustion is recovery evidence'); END;
