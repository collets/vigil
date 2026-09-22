-- Stage 5.4 repair cycles may legitimately produce the same normalized result
-- in distinct attempts. Run identity, not content digest, is the authority.
DROP TRIGGER execution_result_no_update;
DROP TRIGGER execution_result_no_delete;

CREATE TABLE execution_results_v11 (
 run_id TEXT PRIMARY KEY REFERENCES runs(id),
 generation_id TEXT NOT NULL REFERENCES run_generations(id),
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 status TEXT NOT NULL CHECK(status IN('completed','failed','interrupted','unknown')),
 summary TEXT NOT NULL,
 changed_paths_json TEXT NOT NULL CHECK(json_valid(changed_paths_json)),
 repository_fingerprints_json TEXT NOT NULL CHECK(json_valid(repository_fingerprints_json)),
 artifact_id TEXT REFERENCES artifacts(id),
 result_digest TEXT NOT NULL,
 validated_at INTEGER NOT NULL
) STRICT;

INSERT INTO execution_results_v11(run_id,generation_id,schema_version,status,summary,changed_paths_json,repository_fingerprints_json,artifact_id,result_digest,validated_at)
 SELECT run_id,generation_id,schema_version,status,summary,changed_paths_json,repository_fingerprints_json,artifact_id,result_digest,validated_at
 FROM execution_results;

DROP TABLE execution_results;
ALTER TABLE execution_results_v11 RENAME TO execution_results;
CREATE INDEX execution_result_digest_v11 ON execution_results(result_digest);
CREATE TRIGGER execution_result_no_update BEFORE UPDATE ON execution_results
 BEGIN SELECT RAISE(ABORT,'execution result is immutable'); END;
CREATE TRIGGER execution_result_no_delete BEFORE DELETE ON execution_results
 BEGIN SELECT RAISE(ABORT,'execution result is immutable'); END;
