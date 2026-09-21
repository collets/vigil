-- Stage 5.3 checkpoints B-C: verified set metadata and per-path recovery journals.
CREATE TABLE checkpoint_manifests (
 checkpoint_id TEXT PRIMARY KEY REFERENCES checkpoint_sets(id),
 manifest_digest TEXT NOT NULL,
 set_digest TEXT NOT NULL,
 store_relative_path TEXT NOT NULL,
 repository_count INTEGER NOT NULL CHECK(repository_count>0),
 verified_at INTEGER NOT NULL
) STRICT;

CREATE TABLE recovery_operations (
 id TEXT PRIMARY KEY REFERENCES operations(id),
 kind TEXT NOT NULL CHECK(kind IN('save','clear','restore')),
 checkpoint_id TEXT NOT NULL REFERENCES checkpoint_sets(id),
 baseline_checkpoint_id TEXT REFERENCES checkpoint_sets(id),
 destination_checkpoint_id TEXT REFERENCES checkpoint_sets(id),
 authority_digest TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain','conflicted')),
 created_at INTEGER NOT NULL,
 observed_at INTEGER
) STRICT;
CREATE INDEX recovery_operation_status ON recovery_operations(state,created_at);

CREATE TABLE checkpoint_path_progress (
 operation_id TEXT NOT NULL REFERENCES recovery_operations(id),
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 path TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN('write','symlink','delete','index','head','preserve','conflict')),
 state TEXT NOT NULL CHECK(state IN('prepared','applied','preserved','conflicted','uncertain')),
 expected_digest TEXT NOT NULL,
 desired_digest TEXT NOT NULL,
 observation_json TEXT NOT NULL CHECK(json_valid(observation_json)),
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(operation_id,repository_id,path)
) STRICT;
CREATE INDEX checkpoint_progress_status ON checkpoint_path_progress(operation_id,state,repository_id,path);

CREATE TRIGGER checkpoint_manifest_no_update BEFORE UPDATE ON checkpoint_manifests
 BEGIN SELECT RAISE(ABORT,'verified checkpoint manifests are immutable'); END;
CREATE TRIGGER checkpoint_manifest_no_delete BEFORE DELETE ON checkpoint_manifests
 BEGIN SELECT RAISE(ABORT,'verified checkpoint manifests are recovery evidence'); END;
CREATE TRIGGER recovery_operation_no_delete BEFORE DELETE ON recovery_operations
 BEGIN SELECT RAISE(ABORT,'recovery operations are recovery evidence'); END;
CREATE TRIGGER checkpoint_progress_no_delete BEFORE DELETE ON checkpoint_path_progress
 BEGIN SELECT RAISE(ABORT,'checkpoint progress is recovery evidence'); END;
