-- Stage 5.4: authoritative immutable quality evidence and acceptance records.
-- The similarly named migration-001 tables were a design draft and remain
-- preserved for historical compatibility; Stage 5.4 does not read them.
CREATE TABLE quality_scopes_v2 (
 id TEXT PRIMARY KEY CHECK(length(id)=64),
 target_kind TEXT NOT NULL CHECK(target_kind IN('task','plan')),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 plan_revision INTEGER NOT NULL CHECK(plan_revision>0),
 task_id TEXT REFERENCES tasks(id),
 task_revision INTEGER,
 repository_set_digest TEXT NOT NULL CHECK(length(repository_set_digest)=64),
 repository_manifest_json TEXT NOT NULL CHECK(json_valid(repository_manifest_json)),
 criteria_digest TEXT NOT NULL CHECK(length(criteria_digest)=64),
 definition_digest TEXT NOT NULL CHECK(length(definition_digest)=64),
 config_digest TEXT NOT NULL CHECK(length(config_digest)=64),
 check_set_digest TEXT NOT NULL CHECK(length(check_set_digest)=64),
 reviewer_profile_id TEXT NOT NULL,
 reviewer_profile_revision INTEGER NOT NULL CHECK(reviewer_profile_revision>0),
 reviewer_profile_digest TEXT NOT NULL CHECK(length(reviewer_profile_digest)=64),
 instruction_digest TEXT NOT NULL CHECK(length(instruction_digest)=64),
 created_at INTEGER NOT NULL,
 CHECK((target_kind='task')=(task_id IS NOT NULL)),
 CHECK((task_id IS NULL)=(task_revision IS NULL)),
 FOREIGN KEY(task_id,task_revision) REFERENCES task_revisions(task_id,revision)
) STRICT;
CREATE INDEX quality_scope_target_v2 ON quality_scopes_v2(target_kind,plan_id,task_id,created_at);

CREATE TABLE quality_effects_v2 (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 kind TEXT NOT NULL CHECK(kind IN('check','review','supervisor_assessment')),
 definition_id TEXT NOT NULL,
 definition_digest TEXT NOT NULL CHECK(length(definition_digest)=64),
 actor TEXT NOT NULL CHECK(actor IN('core','fixture','qualified_runtime')),
 state TEXT NOT NULL CHECK(state IN('prepared','executing','observed','uncertain','reconciled','cancelled')),
 intent_json TEXT NOT NULL CHECK(json_valid(intent_json)),
 observation_json TEXT CHECK(observation_json IS NULL OR json_valid(observation_json)),
 prepared_at INTEGER NOT NULL,
 started_at INTEGER,
 observed_at INTEGER,
 UNIQUE(scope_id,kind,definition_id,id)
) STRICT;
CREATE INDEX quality_effect_recovery_v2 ON quality_effects_v2(state,prepared_at);

CREATE TABLE check_results_v2 (
 id TEXT PRIMARY KEY,
 effect_id TEXT NOT NULL UNIQUE REFERENCES quality_effects_v2(id),
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 check_id TEXT NOT NULL,
 definition_digest TEXT NOT NULL CHECK(length(definition_digest)=64),
 status TEXT NOT NULL CHECK(status IN('pass','fail','timeout','interrupted','error','source_mutated','missing_output','output_overflow')),
 exit_code INTEGER,
 failure_identities_json TEXT NOT NULL CHECK(json_valid(failure_identities_json)),
 required_outputs_json TEXT NOT NULL CHECK(json_valid(required_outputs_json)),
 output_artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 output_artifact_digest TEXT NOT NULL CHECK(length(output_artifact_digest)=64),
 evaluated_repository_set_digest TEXT NOT NULL CHECK(length(evaluated_repository_set_digest)=64),
 observed_repository_set_digest TEXT NOT NULL CHECK(length(observed_repository_set_digest)=64),
 started_at INTEGER NOT NULL,
 ended_at INTEGER NOT NULL CHECK(ended_at>=started_at),
 duration_ms INTEGER NOT NULL CHECK(duration_ms>=0),
 result_digest TEXT NOT NULL UNIQUE CHECK(length(result_digest)=64)
) STRICT;
CREATE INDEX check_result_gate_v2 ON check_results_v2(scope_id,check_id,ended_at);

CREATE TABLE baseline_exceptions_v2 (
 id TEXT PRIMARY KEY,
 check_id TEXT NOT NULL,
 check_definition_digest TEXT NOT NULL CHECK(length(check_definition_digest)=64),
 base_repository_set_digest TEXT NOT NULL CHECK(length(base_repository_set_digest)=64),
 failure_identities_json TEXT NOT NULL CHECK(json_valid(failure_identities_json)),
 paths_json TEXT NOT NULL CHECK(json_valid(paths_json)),
 rationale TEXT NOT NULL CHECK(length(rationale)>0),
 actor TEXT NOT NULL CHECK(actor IN('human','fixture_human')),
 command_id TEXT NOT NULL UNIQUE REFERENCES command_receipts(id) DEFERRABLE INITIALLY DEFERRED,
 approved_at INTEGER NOT NULL
) STRICT;

CREATE TABLE review_results_v2 (
 id TEXT PRIMARY KEY,
 effect_id TEXT NOT NULL UNIQUE REFERENCES quality_effects_v2(id),
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 schema_version INTEGER NOT NULL CHECK(schema_version=1),
 status TEXT NOT NULL CHECK(status IN('pass','request_changes','malformed','interrupted','write_denied','error')),
 reviewer_session_id TEXT NOT NULL UNIQUE,
 reviewer_native_identity TEXT NOT NULL UNIQUE,
 read_only_verified INTEGER NOT NULL CHECK(read_only_verified IN(0,1)),
 evidence_manifest_digest TEXT NOT NULL CHECK(length(evidence_manifest_digest)=64),
 result_artifact_id TEXT NOT NULL REFERENCES artifacts(id),
 result_artifact_digest TEXT NOT NULL CHECK(length(result_artifact_digest)=64),
 result_digest TEXT NOT NULL UNIQUE CHECK(length(result_digest)=64),
 started_at INTEGER NOT NULL,
 ended_at INTEGER NOT NULL CHECK(ended_at>=started_at)
) STRICT;
CREATE INDEX review_result_gate_v2 ON review_results_v2(scope_id,ended_at);

CREATE TABLE quality_findings_v2 (
 review_id TEXT NOT NULL REFERENCES review_results_v2(id),
 finding_id TEXT NOT NULL,
 severity TEXT NOT NULL CHECK(severity IN('critical','high','medium','low','suggestion')),
 blocking INTEGER NOT NULL CHECK(blocking IN(0,1)),
 repository_id TEXT NOT NULL,
 path TEXT,
 line INTEGER CHECK(line IS NULL OR line>0),
 evidence TEXT NOT NULL,
 recommendation TEXT NOT NULL,
 resolution TEXT NOT NULL CHECK(resolution IN('open','repaired','accepted_suggestion','superseded')),
 PRIMARY KEY(review_id,finding_id)
) STRICT;

CREATE TABLE manual_results_v2 (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 criterion_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('pending','pass','fail','cannot_verify')),
 evaluator TEXT NOT NULL,
 notes TEXT NOT NULL,
 artifact_id TEXT REFERENCES artifacts(id),
 artifact_digest TEXT,
 actor TEXT NOT NULL CHECK(actor IN('human','fixture_human')),
 command_id TEXT NOT NULL UNIQUE REFERENCES command_receipts(id) DEFERRABLE INITIALLY DEFERRED,
 evaluated_at INTEGER NOT NULL,
 CHECK((artifact_id IS NULL)=(artifact_digest IS NULL))
) STRICT;
CREATE INDEX manual_result_gate_v2 ON manual_results_v2(scope_id,criterion_id,evaluated_at);

CREATE TABLE human_decisions_v2 (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 action TEXT NOT NULL CHECK(action IN('accept','request_changes','clarify','stop')),
 actor TEXT NOT NULL CHECK(actor IN('human','fixture_human')),
 rationale TEXT NOT NULL,
 command_id TEXT NOT NULL UNIQUE REFERENCES command_receipts(id) DEFERRABLE INITIALLY DEFERRED,
 decided_at INTEGER NOT NULL
) STRICT;
CREATE INDEX human_decision_gate_v2 ON human_decisions_v2(scope_id,decided_at);

CREATE TABLE evidence_staleness_v2 (
 id TEXT PRIMARY KEY,
 evidence_kind TEXT NOT NULL CHECK(evidence_kind IN('check','review','manual','human_decision','task_acceptance','plan_acceptance')),
 evidence_id TEXT NOT NULL,
 prior_scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 current_scope_id TEXT REFERENCES quality_scopes_v2(id),
 reason TEXT NOT NULL CHECK(reason IN('repository_content','plan_revision','task_revision','criteria','definition','configuration','check_definition','check_set','reviewer_profile','reviewer_instructions','artifact_missing_or_corrupt','superseded','criteria_retired')),
 details_json TEXT NOT NULL CHECK(json_valid(details_json)),
 detected_at INTEGER NOT NULL,
 UNIQUE(evidence_kind,evidence_id,current_scope_id,reason)
) STRICT;

CREATE TABLE quality_acceptance_attempts_v2 (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 target_kind TEXT NOT NULL CHECK(target_kind IN('task','plan')),
 outcome TEXT NOT NULL CHECK(outcome IN('accepted','rejected','raced')),
 reasons_json TEXT NOT NULL CHECK(json_valid(reasons_json)),
 command_id TEXT NOT NULL UNIQUE REFERENCES command_receipts(id) DEFERRABLE INITIALLY DEFERRED,
 attempted_at INTEGER NOT NULL
) STRICT;

CREATE TABLE quality_acceptances_v2 (
 id TEXT PRIMARY KEY,
 scope_id TEXT NOT NULL UNIQUE REFERENCES quality_scopes_v2(id),
 target_kind TEXT NOT NULL CHECK(target_kind IN('task','plan')),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 task_id TEXT REFERENCES tasks(id),
 evidence_manifest_id TEXT NOT NULL REFERENCES artifacts(id),
 evidence_manifest_digest TEXT NOT NULL CHECK(length(evidence_manifest_digest)=64),
 actor TEXT NOT NULL CHECK(actor IN('core','fixture_core')),
 accepted_at INTEGER NOT NULL,
 invalidated_at INTEGER,
 invalidation_reason TEXT,
 CHECK((target_kind='task')=(task_id IS NOT NULL))
) STRICT;
CREATE UNIQUE INDEX one_current_quality_task_acceptance_v2 ON quality_acceptances_v2(task_id) WHERE task_id IS NOT NULL AND invalidated_at IS NULL;
CREATE UNIQUE INDEX one_current_quality_plan_acceptance_v2 ON quality_acceptances_v2(plan_id) WHERE target_kind='plan' AND invalidated_at IS NULL;

CREATE TRIGGER quality_scope_no_update_v2 BEFORE UPDATE ON quality_scopes_v2 BEGIN SELECT RAISE(ABORT,'quality scope is immutable'); END;
CREATE TRIGGER quality_scope_no_delete_v2 BEFORE DELETE ON quality_scopes_v2 BEGIN SELECT RAISE(ABORT,'quality scope is immutable'); END;
CREATE TRIGGER check_result_no_update_v2 BEFORE UPDATE ON check_results_v2 BEGIN SELECT RAISE(ABORT,'check result is immutable'); END;
CREATE TRIGGER check_result_no_delete_v2 BEFORE DELETE ON check_results_v2 BEGIN SELECT RAISE(ABORT,'check result is immutable'); END;
CREATE TRIGGER baseline_exception_no_update_v2 BEFORE UPDATE ON baseline_exceptions_v2 BEGIN SELECT RAISE(ABORT,'baseline authority is immutable'); END;
CREATE TRIGGER baseline_exception_no_delete_v2 BEFORE DELETE ON baseline_exceptions_v2 BEGIN SELECT RAISE(ABORT,'baseline authority is immutable'); END;
CREATE TRIGGER review_result_no_update_v2 BEFORE UPDATE ON review_results_v2 BEGIN SELECT RAISE(ABORT,'review result is immutable'); END;
CREATE TRIGGER review_result_no_delete_v2 BEFORE DELETE ON review_results_v2 BEGIN SELECT RAISE(ABORT,'review result is immutable'); END;
CREATE TRIGGER quality_finding_no_update_v2 BEFORE UPDATE ON quality_findings_v2 BEGIN SELECT RAISE(ABORT,'review finding is immutable'); END;
CREATE TRIGGER quality_finding_no_delete_v2 BEFORE DELETE ON quality_findings_v2 BEGIN SELECT RAISE(ABORT,'review finding is immutable'); END;
CREATE TRIGGER manual_result_no_update_v2 BEFORE UPDATE ON manual_results_v2 BEGIN SELECT RAISE(ABORT,'manual result is immutable'); END;
CREATE TRIGGER manual_result_no_delete_v2 BEFORE DELETE ON manual_results_v2 BEGIN SELECT RAISE(ABORT,'manual result is immutable'); END;
CREATE TRIGGER human_decision_no_update_v2 BEFORE UPDATE ON human_decisions_v2 BEGIN SELECT RAISE(ABORT,'human decision is immutable'); END;
CREATE TRIGGER human_decision_no_delete_v2 BEFORE DELETE ON human_decisions_v2 BEGIN SELECT RAISE(ABORT,'human decision is immutable'); END;
CREATE TRIGGER evidence_staleness_no_update_v2 BEFORE UPDATE ON evidence_staleness_v2 BEGIN SELECT RAISE(ABORT,'staleness evidence is append-only'); END;
CREATE TRIGGER evidence_staleness_no_delete_v2 BEFORE DELETE ON evidence_staleness_v2 BEGIN SELECT RAISE(ABORT,'staleness evidence is append-only'); END;
CREATE TRIGGER acceptance_attempt_no_update_v2 BEFORE UPDATE ON quality_acceptance_attempts_v2 BEGIN SELECT RAISE(ABORT,'acceptance attempt is immutable'); END;
CREATE TRIGGER acceptance_attempt_no_delete_v2 BEFORE DELETE ON quality_acceptance_attempts_v2 BEGIN SELECT RAISE(ABORT,'acceptance attempt is immutable'); END;
CREATE TRIGGER quality_acceptance_no_delete_v2 BEFORE DELETE ON quality_acceptances_v2 BEGIN SELECT RAISE(ABORT,'acceptance is immutable evidence'); END;
CREATE TRIGGER quality_acceptance_update_guard_v2 BEFORE UPDATE ON quality_acceptances_v2
 WHEN OLD.invalidated_at IS NOT NULL OR NEW.id!=OLD.id OR NEW.scope_id!=OLD.scope_id OR NEW.target_kind!=OLD.target_kind OR NEW.plan_id!=OLD.plan_id OR coalesce(NEW.task_id,'')!=coalesce(OLD.task_id,'') OR NEW.evidence_manifest_id!=OLD.evidence_manifest_id OR NEW.evidence_manifest_digest!=OLD.evidence_manifest_digest OR NEW.actor!=OLD.actor OR NEW.accepted_at!=OLD.accepted_at OR NEW.invalidated_at IS NULL OR NEW.invalidation_reason IS NULL
 BEGIN SELECT RAISE(ABORT,'acceptance may only be invalidated once'); END;
