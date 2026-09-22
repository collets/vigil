-- Stage 5.3 follow-up: identical exact-resume workspace content is valid for
-- distinct generations; generation_id remains the immutable authority key.
DROP TRIGGER generation_recovery_snapshot_no_update;
DROP TRIGGER generation_recovery_snapshot_no_delete;

CREATE TABLE generation_recovery_snapshots_v9 (
 generation_id TEXT PRIMARY KEY REFERENCES run_generations(id),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id),
 repository_snapshot_json TEXT NOT NULL CHECK(json_valid(repository_snapshot_json)),
 digest TEXT NOT NULL,
 created_at INTEGER NOT NULL
) STRICT;

INSERT INTO generation_recovery_snapshots_v9(generation_id,checkpoint_id,repository_snapshot_json,digest,created_at)
 SELECT generation_id,checkpoint_id,repository_snapshot_json,digest,created_at
 FROM generation_recovery_snapshots;

DROP TABLE generation_recovery_snapshots;
ALTER TABLE generation_recovery_snapshots_v9 RENAME TO generation_recovery_snapshots;
CREATE INDEX generation_recovery_snapshot_digest ON generation_recovery_snapshots(digest);

CREATE TRIGGER generation_recovery_snapshot_no_update BEFORE UPDATE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is immutable'); END;
CREATE TRIGGER generation_recovery_snapshot_no_delete BEFORE DELETE ON generation_recovery_snapshots
 BEGIN SELECT RAISE(ABORT,'generation recovery workspace authority is recovery evidence'); END;
