-- Stage 5.4 bounded exhaustion assessments are immutable advisory evidence.
CREATE TABLE supervisor_assessments_v2 (
 id TEXT PRIMARY KEY,
 effect_id TEXT NOT NULL UNIQUE REFERENCES quality_effects_v2(id),
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 source_kind TEXT NOT NULL CHECK(source_kind IN('repair_exhaustion','budget_exhaustion')),
 source_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN('clarify','revise_or_split','eligible_reassignment','remain_blocked')),
 rationale TEXT NOT NULL,
 result_artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 result_artifact_digest TEXT NOT NULL CHECK(length(result_artifact_digest)=64),
 actor TEXT NOT NULL CHECK(actor IN('fixture','qualified_runtime')),
 assessed_at INTEGER NOT NULL,
 UNIQUE(scope_id,source_kind,source_id)
) STRICT;
CREATE TRIGGER supervisor_assessment_no_update_v2 BEFORE UPDATE ON supervisor_assessments_v2
 BEGIN SELECT RAISE(ABORT,'supervisor assessment is immutable'); END;
CREATE TRIGGER supervisor_assessment_no_delete_v2 BEFORE DELETE ON supervisor_assessments_v2
 BEGIN SELECT RAISE(ABORT,'supervisor assessment is immutable'); END;
