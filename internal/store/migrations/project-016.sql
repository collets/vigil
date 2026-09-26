-- Stage 5.5 pre-plan service budget and crash-reconcilable planning attempts.
-- A proposed plan does not exist in plans yet, so this ledger is keyed by the
-- immutable proposal identity and is transferred to plan_services on apply.
PRAGMA foreign_keys = ON;

CREATE UNIQUE INDEX one_active_tool_generation ON tool_sessions(native_session_id) WHERE retired_at IS NULL;

CREATE TABLE planning_service_ledgers (
 expected_plan_id TEXT PRIMARY KEY,
 active_limit_ms INTEGER NOT NULL CHECK(active_limit_ms>0 AND active_limit_ms<=1800000),
 charged_ms INTEGER NOT NULL DEFAULT 0 CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL DEFAULT 0 CHECK(unknown_ms>=0),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_at INTEGER NOT NULL,
 CHECK(charged_ms+unknown_ms<=active_limit_ms)
) STRICT;

CREATE TABLE planning_attempts (
 id TEXT PRIMARY KEY,
 proposal_id TEXT NOT NULL,
 expected_plan_id TEXT NOT NULL REFERENCES planning_service_ledgers(expected_plan_id),
 specification_id TEXT NOT NULL,
 specification_revision INTEGER NOT NULL,
 profile_id TEXT NOT NULL,
 profile_revision INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN('executing','completed','failed','unknown')),
 active_limit_ms INTEGER NOT NULL CHECK(active_limit_ms>0 AND active_limit_ms<=300000),
 started_at INTEGER NOT NULL,
 ended_at INTEGER,
 charged_ms INTEGER NOT NULL DEFAULT 0 CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL DEFAULT 0 CHECK(unknown_ms>=0),
 output_digest TEXT,
 proposal_revision INTEGER,
 provider_idle INTEGER NOT NULL DEFAULT 0 CHECK(provider_idle IN(0,1)),
 error_text TEXT,
 command_id TEXT NOT NULL UNIQUE,
 FOREIGN KEY(specification_id,specification_revision) REFERENCES specification_revisions(id,revision),
 FOREIGN KEY(profile_id,profile_revision) REFERENCES profiles(id,revision),
 CHECK((state='executing')=(ended_at IS NULL))
) STRICT;
CREATE INDEX planning_attempts_proposal ON planning_attempts(proposal_id,started_at,id);
CREATE UNIQUE INDEX one_active_planning_attempt ON planning_attempts(expected_plan_id) WHERE state='executing';

CREATE TABLE planning_budget_transfers (
 attempt_id TEXT PRIMARY KEY REFERENCES planning_attempts(id),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 charged_ms INTEGER NOT NULL CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL CHECK(unknown_ms>=0),
 transferred_at INTEGER NOT NULL
) STRICT;

CREATE TRIGGER planning_attempt_terminal_no_update
BEFORE UPDATE ON planning_attempts
WHEN OLD.state!='executing'
BEGIN SELECT RAISE(ABORT,'terminal planning attempt is immutable'); END;

CREATE TRIGGER planning_attempt_no_delete BEFORE DELETE ON planning_attempts
BEGIN SELECT RAISE(ABORT,'planning attempt is immutable'); END;
