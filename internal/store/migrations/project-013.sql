-- Stage 5.4 review remediation: durable quality timing, atomic gate authority,
-- and pre-effect supervisor source reservations.
CREATE TABLE quality_budget_segments_v2 (
 effect_id TEXT PRIMARY KEY REFERENCES quality_effects_v2(id),
 ledger_id TEXT NOT NULL REFERENCES budget_ledgers(id),
 state TEXT NOT NULL CHECK(state IN('active','charged','uncertain')),
 started_at INTEGER NOT NULL,
 ended_at INTEGER,
 charged_ms INTEGER NOT NULL DEFAULT 0 CHECK(charged_ms>=0),
 unknown_ms INTEGER NOT NULL DEFAULT 0 CHECK(unknown_ms>=0),
 CHECK((state='active')=(ended_at IS NULL))
) STRICT;
CREATE INDEX quality_budget_segment_state_v2 ON quality_budget_segments_v2(state,started_at);

CREATE TABLE quality_authority_v2 (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 revision INTEGER NOT NULL CHECK(revision>0),
 updated_at INTEGER NOT NULL
) STRICT;
INSERT INTO quality_authority_v2 VALUES(1,1,unixepoch('subsec')*1000);

CREATE TABLE quality_assessment_sources_v2 (
 scope_id TEXT NOT NULL REFERENCES quality_scopes_v2(id),
 plan_id TEXT NOT NULL REFERENCES plans(id),
 task_id TEXT NOT NULL REFERENCES tasks(id),
 source_kind TEXT NOT NULL CHECK(source_kind IN('repair_exhaustion','budget_exhaustion')),
 source_id TEXT NOT NULL,
 effect_id TEXT NOT NULL UNIQUE REFERENCES quality_effects_v2(id),
 command_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('reserved','observed','uncertain')),
 reserved_at INTEGER NOT NULL,
 observed_at INTEGER,
 PRIMARY KEY(task_id,source_kind,source_id)
) STRICT;

CREATE TRIGGER quality_authority_config_v2 AFTER INSERT ON project_configurations
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_project_update_v2 AFTER UPDATE ON project
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_profile_v2 AFTER INSERT ON profiles
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_plan_revision_v2 AFTER INSERT ON plan_revisions
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_task_revision_v2 AFTER INSERT ON task_revisions
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_effect_insert_v2 AFTER INSERT ON quality_effects_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_effect_update_v2 AFTER UPDATE ON quality_effects_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_check_v2 AFTER INSERT ON check_results_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_baseline_v2 AFTER INSERT ON baseline_exceptions_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_review_v2 AFTER INSERT ON review_results_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_finding_v2 AFTER INSERT ON quality_findings_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_manual_v2 AFTER INSERT ON manual_results_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_human_v2 AFTER INSERT ON human_decisions_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_staleness_v2 AFTER INSERT ON evidence_staleness_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_acceptance_insert_v2 AFTER INSERT ON quality_acceptances_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_acceptance_update_v2 AFTER UPDATE ON quality_acceptances_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_budget_v2 AFTER UPDATE ON budget_ledgers
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_assessment_v2 AFTER INSERT ON supervisor_assessments_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_budget_segment_insert_v2 AFTER INSERT ON quality_budget_segments_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_budget_segment_update_v2 AFTER UPDATE ON quality_budget_segments_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_assessment_source_insert_v2 AFTER INSERT ON quality_assessment_sources_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;
CREATE TRIGGER quality_authority_assessment_source_update_v2 AFTER UPDATE ON quality_assessment_sources_v2
 BEGIN UPDATE quality_authority_v2 SET revision=revision+1,updated_at=unixepoch('subsec')*1000 WHERE singleton=1; END;

CREATE TRIGGER quality_budget_segment_no_delete_v2 BEFORE DELETE ON quality_budget_segments_v2
 BEGIN SELECT RAISE(ABORT,'quality budget segment is durable evidence'); END;
CREATE TRIGGER quality_budget_segment_update_guard_v2 BEFORE UPDATE ON quality_budget_segments_v2
 WHEN OLD.state!='active' OR NEW.effect_id!=OLD.effect_id OR NEW.ledger_id!=OLD.ledger_id OR NEW.started_at!=OLD.started_at OR NEW.state NOT IN('charged','uncertain') OR NEW.ended_at IS NULL
 BEGIN SELECT RAISE(ABORT,'quality budget segment may only close once'); END;
CREATE TRIGGER quality_assessment_source_no_delete_v2 BEFORE DELETE ON quality_assessment_sources_v2
 BEGIN SELECT RAISE(ABORT,'quality assessment source reservation is durable evidence'); END;
CREATE TRIGGER quality_assessment_source_update_guard_v2 BEFORE UPDATE ON quality_assessment_sources_v2
 WHEN OLD.state!='reserved' OR NEW.scope_id!=OLD.scope_id OR NEW.plan_id!=OLD.plan_id OR NEW.task_id!=OLD.task_id OR NEW.source_kind!=OLD.source_kind OR NEW.source_id!=OLD.source_id OR NEW.effect_id!=OLD.effect_id OR NEW.command_id!=OLD.command_id OR NEW.reserved_at!=OLD.reserved_at OR NEW.state NOT IN('observed','uncertain') OR NEW.observed_at IS NULL
 BEGIN SELECT RAISE(ABORT,'quality assessment source may only be resolved once'); END;
