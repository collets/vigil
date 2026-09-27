# Plan finalization and retention

<!-- vigil-tier: core -->

Status: finalization behavior, local storage, and retention defaults accepted, 2026-09-20. Detailed content structure and implementation below remain proposals.

Stage 4 follow-up: [core-spec.md](core-spec.md) resolves the mechanism/default questions below, and [Stage 5](../plans/stage-5/stage-5-plan.md) defines implementation checks. Earlier proposed alternatives are retained for provenance; the core specification takes precedence for implementation. No production workflow or user configuration is installed by the specification.

## Accepted behavior

At the end of a plan, create a visible finalization task that summarizes the work, references previously produced resources, and saves the completion record in the plan folder. Preserve summaries, decisions, checks, and commit references by default. Raw transcripts can expire after a configurable interval. Unfinished recovery checkpoints require explicit cleanup.

## Proposed finalization contract

Make this an application-created finalization task with bounded scope, rather than an ordinary coding task that recursively needs another final summary. Construct the factual manifest from authoritative state; an agent writes the narrative from that manifest and its referenced evidence. Apply the selected model policy, resource queue, and execution limits to the summary agent.

Proposed archive contents:

- Original requirements and accepted revisions, including explicit deviations.
- Task outcomes, attempts, unresolved or deferred findings, and decisions.
- Required checks and their results, accepted pre-existing failures, review outcomes, and relevant human verification.
- Repository paths/identities, branches, commit IDs, and eventual PR/MR references.
- Durable copies or references to produced artifacts and recoverable attempts as appropriate.
- Effective configuration and resource-usage records, with secrets excluded.
- A human-readable summary and machine-readable manifest with stable identifiers.

The archive is an export/evidence bundle, not a competing source of application state. Relative internal references should remain usable when the folder is moved. Clearly distinguish local files from external references and unavailable/expired data.

## Completion behavior

Use a visible finalizing state before completed. If the agent-generated summary fails, retain the factual record and show finalization pending. Retry finalization without re-running development. Draft PR/MR creation remains available as an explicit action; it does not falsely mark finalization complete.

Delivery and archiving must not form a circular dependency: the record can be prepared before request creation, then receive its URL/status afterward. If the user elects to commit archive files, avoid trying to embed that same commit's own ID or a future request URL into the commit it describes.

The finalization task's review should validate completeness and evidence references, rather than re-run unrelated application-code checks merely because it writes a summary. Required human acceptance and artifact checks still follow configured policy.

## Retention boundaries

Before deleting raw transcripts, preserve required decisions and evidence in durable records and mark transcript links as expired. Accepted default: 30 days after plan completion, configurable per project. Active/unfinished plans must not lose needed recovery context under the completion-retention timer.

Store archives in a local Git-ignored plan folder by default. Exporting or committing selected documents is explicit. The exact directory layout remains a design choice. "Save everything" distinguishes durable project evidence from raw transcripts, temporary outputs, and credentials.
