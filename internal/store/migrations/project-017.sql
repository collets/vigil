-- Stage 5.6: one live/uncertain draft creation identity per exact plan head/base.
-- A second operation cannot race a first ambiguous POST into a duplicate.
CREATE UNIQUE INDEX one_draft_delivery_per_head_base
 ON deliveries(plan_id,repository_id,remote_identity,head_oid,base_ref)
 WHERE kind='draft_request' AND state IN('prepared','pending','uncertain','succeeded');
