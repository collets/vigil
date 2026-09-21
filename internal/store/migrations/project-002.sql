-- Immutable trusted execution qualification records. These records are written
-- only by the core qualification recorder; profile/config commands cannot add
-- or alter them.
CREATE TABLE execution_qualifications (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 input_digest TEXT NOT NULL,
 record_digest TEXT NOT NULL UNIQUE CHECK(record_digest=id),
 status TEXT NOT NULL CHECK(status IN('supported','unsupported','unverified')),
 observed_at INTEGER NOT NULL CHECK(observed_at>0),
 recorded_at INTEGER NOT NULL CHECK(recorded_at>0),
 record_json TEXT NOT NULL CHECK(json_valid(record_json))
) STRICT;
CREATE INDEX execution_qualification_lookup
 ON execution_qualifications(input_digest,sequence DESC);
CREATE TRIGGER execution_qualification_no_update
 BEFORE UPDATE ON execution_qualifications
 BEGIN SELECT RAISE(ABORT,'execution qualification is immutable'); END;
CREATE TRIGGER execution_qualification_no_delete
 BEFORE DELETE ON execution_qualifications
 BEGIN SELECT RAISE(ABORT,'execution qualification is immutable'); END;
