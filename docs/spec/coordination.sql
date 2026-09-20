-- Separate user-state DB. No transaction is atomic with the per-project DB.
PRAGMA foreign_keys = ON;
CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,digest TEXT NOT NULL,applied_at INTEGER NOT NULL) STRICT;
CREATE TABLE instances (
 id TEXT PRIMARY KEY, host_identity TEXT NOT NULL, boot_identity TEXT NOT NULL, pid INTEGER NOT NULL CHECK(pid>0),
 process_start TEXT NOT NULL, lock_path TEXT NOT NULL, heartbeat_at INTEGER NOT NULL
) STRICT;
CREATE TABLE workspace_claims (
 id TEXT PRIMARY KEY, operation_id TEXT NOT NULL UNIQUE, project_id TEXT NOT NULL, instance_id TEXT NOT NULL REFERENCES instances(id),
 canonical_root TEXT NOT NULL, filesystem_identity TEXT NOT NULL, common_git_identity TEXT,
 generation INTEGER NOT NULL CHECK(generation>0), state TEXT NOT NULL CHECK(state IN('active','releasing','quarantined')),
 acquired_at INTEGER NOT NULL, quarantine_reason TEXT
) STRICT;
CREATE UNIQUE INDEX exact_root_owner ON workspace_claims(canonical_root);
CREATE UNIQUE INDEX physical_root_owner ON workspace_claims(filesystem_identity);
CREATE INDEX shared_git_owner ON workspace_claims(common_git_identity);
-- Ancestor/descendant and common-Git collisions are checked in the acquisition
-- BEGIN IMMEDIATE transaction across every root in the requested project set.
CREATE TABLE endpoints (
 id TEXT PRIMARY KEY, canonical_url TEXT NOT NULL, capacity INTEGER NOT NULL DEFAULT 1 CHECK(capacity>0),
 config_revision INTEGER NOT NULL CHECK(config_revision>0)
) STRICT;
CREATE TABLE endpoint_aliases(alias TEXT PRIMARY KEY,endpoint_id TEXT NOT NULL REFERENCES endpoints(id)) STRICT;
CREATE TABLE queue_tickets (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, operation_id TEXT NOT NULL UNIQUE,
 endpoint_id TEXT NOT NULL REFERENCES endpoints(id), instance_id TEXT NOT NULL REFERENCES instances(id),
 project_id TEXT NOT NULL, run_id TEXT NOT NULL, enqueued_at INTEGER NOT NULL,
 state TEXT NOT NULL CHECK(state IN('waiting','reserved','released','cancelled','quarantined'))
) STRICT;
CREATE INDEX endpoint_fifo ON queue_tickets(endpoint_id,state,sequence);
CREATE TABLE endpoint_slots (
 endpoint_id TEXT NOT NULL REFERENCES endpoints(id), slot_number INTEGER NOT NULL CHECK(slot_number>=0),
 ticket INTEGER NOT NULL UNIQUE REFERENCES queue_tickets(sequence), generation INTEGER NOT NULL CHECK(generation>0),
 state TEXT NOT NULL CHECK(state IN('reserved','active','quarantined')),
 PRIMARY KEY(endpoint_id,slot_number)
) STRICT;
CREATE TRIGGER valid_slot BEFORE INSERT ON endpoint_slots WHEN NEW.slot_number >= (SELECT capacity FROM endpoints WHERE id=NEW.endpoint_id)
 BEGIN SELECT RAISE(ABORT,'endpoint capacity exceeded'); END;
CREATE TRIGGER matching_ticket BEFORE INSERT ON endpoint_slots WHEN NEW.endpoint_id != (SELECT endpoint_id FROM queue_tickets WHERE sequence=NEW.ticket)
 BEGIN SELECT RAISE(ABORT,'ticket endpoint mismatch'); END;
CREATE TRIGGER slot_identity_immutable BEFORE UPDATE OF endpoint_id,slot_number,ticket,generation ON endpoint_slots
 BEGIN SELECT RAISE(ABORT,'replace reservations transactionally'); END;
CREATE TRIGGER capacity_not_below_occupied BEFORE UPDATE OF capacity ON endpoints
 WHEN NEW.capacity <= (SELECT max(slot_number) FROM endpoint_slots WHERE endpoint_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'occupied capacity cannot shrink'); END;
CREATE TABLE global_grants (
 id TEXT PRIMARY KEY, category TEXT NOT NULL, resources_json TEXT NOT NULL CHECK(json_valid(resources_json)),
 explicit_global_choice INTEGER NOT NULL CHECK(explicit_global_choice=1), granted_at INTEGER NOT NULL, revoked_at INTEGER
) STRICT;
CREATE TABLE global_policy (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), epoch INTEGER NOT NULL CHECK(epoch>0)
) STRICT;
INSERT INTO global_policy VALUES(1,1);
CREATE TABLE effect_authorizations (
 operation_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, instance_id TEXT NOT NULL REFERENCES instances(id),
 grant_id TEXT NOT NULL REFERENCES global_grants(id), policy_epoch INTEGER NOT NULL CHECK(policy_epoch>0),
 resource_digest TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN('executing','observed','uncertain','cancelled')),
 authorized_at INTEGER NOT NULL
) STRICT;
CREATE TABLE reconciliation_log (
 sequence INTEGER PRIMARY KEY, operation_id TEXT NOT NULL, instance_id TEXT REFERENCES instances(id),
 observed_at INTEGER NOT NULL, evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json))
) STRICT;
