-- Global grant/effect-start serialization. The coordinator operation identity
-- is distinct from the project operation because the databases cannot commit
-- atomically.
ALTER TABLE global_grants ADD COLUMN policy_epoch INTEGER NOT NULL DEFAULT 1 CHECK(policy_epoch>0);
ALTER TABLE effect_authorizations ADD COLUMN project_operation_id TEXT;
ALTER TABLE effect_authorizations ADD COLUMN arguments_digest TEXT;
ALTER TABLE effect_authorizations ADD COLUMN observed_at INTEGER;
CREATE UNIQUE INDEX global_effect_project_operation ON effect_authorizations(project_id,project_operation_id);
CREATE TRIGGER global_grant_identity_immutable BEFORE UPDATE OF id,category,resources_json,explicit_global_choice,granted_at,policy_epoch ON global_grants
 BEGIN SELECT RAISE(ABORT,'global grant identity is immutable'); END;
