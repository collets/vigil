-- Stage 5.3 review: generation-scoped exact-resume workspace authority.
CREATE TABLE generation_recovery_snapshots (
 generation_id TEXT PRIMARY KEY REFERENCES run_generations(id),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id),
 repository_snapshot_json TEXT NOT NULL CHECK(json_valid(repository_snapshot_json)),
 digest TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL
) STRICT;

CREATE TRIGGER generation_recovery_snapshot_no_update BEFORE UPDATE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is immutable'); END;
CREATE TRIGGER generation_recovery_snapshot_no_delete BEFORE DELETE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is recovery evidence'); END;
