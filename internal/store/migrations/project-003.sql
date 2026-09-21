-- Stage 5.2 checkpoint A: explicit, revisioned repository enrollment,
-- content baselines and crash-reconcilable branch preparation.
CREATE TABLE repository_revisions (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 root_identity_json TEXT NOT NULL CHECK(json_valid(root_identity_json)),
 remote_name TEXT,
 remote_identity TEXT,
 base_ref TEXT NOT NULL,
 base_oid TEXT NOT NULL,
 plan_branch TEXT NOT NULL,
 dirty_choice TEXT NOT NULL CHECK(dirty_choice IN('clean','include','postpone')),
 included_paths_json TEXT NOT NULL CHECK(json_valid(included_paths_json)),
 nested_boundaries_json TEXT NOT NULL CHECK(json_valid(nested_boundaries_json)),
 fingerprint_id TEXT NOT NULL,
 enrolled_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id,revision)
) STRICT;
CREATE TABLE repository_fingerprints (
 id TEXT PRIMARY KEY,
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 repository_revision INTEGER NOT NULL,
 head_oid TEXT NOT NULL,
 head_ref TEXT,
 index_digest TEXT NOT NULL,
 content_digest TEXT NOT NULL,
 dirty INTEGER NOT NULL CHECK(dirty IN(0,1)),
 dirty_paths_json TEXT NOT NULL CHECK(json_valid(dirty_paths_json)),
 exclusions_json TEXT NOT NULL CHECK(json_valid(exclusions_json)),
 created_at INTEGER NOT NULL,
 FOREIGN KEY(repository_id,repository_revision)
   REFERENCES repository_revisions(repository_id,revision)
) STRICT;
CREATE TABLE repository_branch_operations (
 id TEXT PRIMARY KEY REFERENCES operations(id),
 repository_id TEXT NOT NULL,
 repository_revision INTEGER NOT NULL,
 expected_base_oid TEXT NOT NULL,
 branch_ref TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('prepared','observed','uncertain','reconciled')),
 observed_oid TEXT,
 observed_head_ref TEXT,
 created_at INTEGER NOT NULL,
 observed_at INTEGER,
 FOREIGN KEY(repository_id,repository_revision)
   REFERENCES repository_revisions(repository_id,revision)
) STRICT;
CREATE UNIQUE INDEX one_repository_branch_preparation
 ON repository_branch_operations(repository_id,repository_revision);
CREATE TRIGGER repository_revision_no_update BEFORE UPDATE ON repository_revisions
 BEGIN SELECT RAISE(ABORT,'repository revision is immutable'); END;
CREATE TRIGGER repository_revision_no_delete BEFORE DELETE ON repository_revisions
 BEGIN SELECT RAISE(ABORT,'repository revision is immutable'); END;
CREATE TRIGGER repository_fingerprint_no_update BEFORE UPDATE ON repository_fingerprints
 BEGIN SELECT RAISE(ABORT,'repository fingerprint is immutable'); END;
CREATE TRIGGER repository_fingerprint_no_delete BEFORE DELETE ON repository_fingerprints
 BEGIN SELECT RAISE(ABORT,'repository fingerprint is immutable'); END;
