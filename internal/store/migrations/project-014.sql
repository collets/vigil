-- Stage 5.4 follow-up: reconcile pre-v13 quality effects and assessment
-- consumption before the v13 reservation/segment tables can be trusted.

-- A completed v12 assessment consumed its logical task/source even when a
-- later scope revision produced a different scope ID. Keep the first durable
-- observation as task-wide source authority; every such source remains spent.
INSERT OR IGNORE INTO quality_assessment_sources_v2(scope_id,plan_id,task_id,source_kind,source_id,effect_id,command_id,state,reserved_at,observed_at)
SELECT scope_id,plan_id,task_id,source_kind,source_id,effect_id,'legacy-observed:'||effect_id,'observed',reserved_at,observed_at
FROM (
 SELECT a.scope_id,s.plan_id,s.task_id,a.source_kind,a.source_id,a.effect_id,
        coalesce(e.started_at,e.prepared_at) AS reserved_at,a.assessed_at AS observed_at,
        row_number() OVER (PARTITION BY s.task_id,a.source_kind,a.source_id ORDER BY a.assessed_at,a.id) AS ordinal
 FROM supervisor_assessments_v2 a
 JOIN quality_scopes_v2 s ON s.id=a.scope_id AND s.target_kind='task'
 JOIN quality_effects_v2 e ON e.id=a.effect_id
)
WHERE ordinal=1;

-- v12 could leave a supervisor call executing/uncertain without terminal
-- evidence. Reserve that source conservatively before changing effect state.
INSERT OR IGNORE INTO quality_assessment_sources_v2(scope_id,plan_id,task_id,source_kind,source_id,effect_id,command_id,state,reserved_at,observed_at)
SELECT e.scope_id,s.plan_id,s.task_id,
       CASE WHEN e.definition_id LIKE 'budget_exhaustion:%' THEN 'budget_exhaustion' ELSE 'repair_exhaustion' END,
       substr(e.definition_id,instr(e.definition_id,':')+1),e.id,'legacy-uncertain:'||e.id,'uncertain',
       coalesce(e.started_at,e.prepared_at),coalesce(e.observed_at,unixepoch('subsec')*1000)
FROM quality_effects_v2 e
JOIN quality_scopes_v2 s ON s.id=e.scope_id AND s.target_kind='task'
WHERE e.kind='supervisor_assessment'
  AND e.state IN('executing','uncertain')
  AND instr(e.definition_id,':')>0
ORDER BY e.prepared_at,e.id;

-- Reconstruct conservative budget authority for every legacy effect that had
-- crossed the external boundary without a v13 segment. A temporary manifest
-- makes the ledger increment exact and prevents charging existing segments.
CREATE TEMP TABLE stage54_legacy_quality_segments(
 effect_id TEXT PRIMARY KEY,
 ledger_id TEXT NOT NULL,
 started_at INTEGER NOT NULL,
 ended_at INTEGER NOT NULL,
 unknown_ms INTEGER NOT NULL
) STRICT;

INSERT INTO stage54_legacy_quality_segments(effect_id,ledger_id,started_at,ended_at,unknown_ms)
SELECT e.id,l.id,coalesce(e.started_at,e.prepared_at),unixepoch('subsec')*1000,
       max(1,(unixepoch('subsec')*1000)-coalesce(e.started_at,e.prepared_at))
FROM quality_effects_v2 e
JOIN quality_scopes_v2 s ON s.id=e.scope_id
JOIN budget_ledgers l ON l.plan_id=s.plan_id
 AND ((s.target_kind='task' AND l.scope='task' AND l.task_id=s.task_id)
   OR (s.target_kind='plan' AND l.scope='plan_services' AND l.task_id IS NULL))
WHERE e.state IN('executing','uncertain')
  AND NOT EXISTS(SELECT 1 FROM quality_budget_segments_v2 b WHERE b.effect_id=e.id);

INSERT INTO quality_budget_segments_v2(effect_id,ledger_id,state,started_at,ended_at,charged_ms,unknown_ms)
SELECT effect_id,ledger_id,'uncertain',started_at,ended_at,0,unknown_ms
FROM stage54_legacy_quality_segments;

UPDATE budget_ledgers
SET unknown_ms=unknown_ms+coalesce((SELECT sum(m.unknown_ms) FROM stage54_legacy_quality_segments m WHERE m.ledger_id=budget_ledgers.id),0),
    revision=revision+CASE WHEN EXISTS(SELECT 1 FROM stage54_legacy_quality_segments m WHERE m.ledger_id=budget_ledgers.id) THEN 1 ELSE 0 END,
    updated_at=CASE WHEN EXISTS(SELECT 1 FROM stage54_legacy_quality_segments m WHERE m.ledger_id=budget_ledgers.id) THEN unixepoch('subsec')*1000 ELSE updated_at END
WHERE EXISTS(SELECT 1 FROM stage54_legacy_quality_segments m WHERE m.ledger_id=budget_ledgers.id);

UPDATE quality_effects_v2
SET state='uncertain',
    observation_json=json_object('reason','v12 upgrade found an external quality effect without terminal timing authority'),
    observed_at=unixepoch('subsec')*1000
WHERE state='executing'
  AND id IN(SELECT effect_id FROM stage54_legacy_quality_segments);

DROP TABLE stage54_legacy_quality_segments;
