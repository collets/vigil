# Proposed autonomy presets

<!-- vigil-tier: history -->

> **Superseded record.** Kept for provenance only. See
> [`README.md`](README.md) in this folder for what replaced it; do not
> implement from or cite it as current behavior.

Status: the two-axis approach is accepted as an initial direction, 2026-09-20. Manual configuration is sufficient; guided onboarding is deferred. Approval-first is the default; the default model policy and numerical limits remain open. Role mappings below are proposed preset definitions.

Stage 4 follow-up: [core-spec.md](../core/core-spec.md) resolves the mechanism/default questions below, and [Stage 5](../plans/stage-5/stage-5-plan.md) defines implementation checks. Earlier proposed alternatives are retained for provenance; the core specification takes precedence for implementation. No production workflow or user configuration is installed by the specification.

## Separate model eligibility from human approvals

Recommend two independent settings instead of one increasingly large collection of combined presets. A model policy determines eligible profiles for each role. An approval mode determines which eligible actions need human input. Changing one must not silently change the other.

| Model policy | Planning and supervision | Implementation | Review | Escalation |
| --- | --- | --- | --- | --- |
| Local only | Local | Local | Local | Eligible local profiles only; otherwise block |
| Hybrid | Approved frontier profiles | Local first | Difficulty-based eligible local/frontier profile | Frontier within configured authorization and budget |
| Cloud allowed | Approved profiles | Difficulty-based approved profile | Difficulty-based approved profile | Within configured authorization and budget |

In the proposed local-only policy, any model-based routing advisor must also be local or disabled. Local-only describes model inference, not offline operation: code hosting and dependencies may still require network access. Clarify whether the user additionally needs an offline/data-egress policy.

| Approval mode | Proposed behavior |
| --- | --- |
| Supervised | Require the configured human gates at plan/task acceptance and sensitive actions |
| Autonomous within limits | Waive selected human gates through explicit configuration while keeping automated checks, review, model eligibility, retry limits, and resource constraints |

For autonomous delivery, commit, push, and request creation require appropriate preauthorization. Automatic merging is excluded in both modes. An autonomous plan may still block when a decision exceeds its granted authority; autonomy does not imply changing acceptance criteria to force completion.

## Configuration presentation

Before execution, show the resolved model policy, role profiles, human gates, retry limits, budget/time limits, repositories/branches, and delivery endpoint. Show overrides and their originating scope. Persist the resolved configuration with the run so later configuration changes do not rewrite history.

## Open decisions

- Select the default model policy, with exact role profiles configured by the user; retain approval-first as the default approval mode.
- Set numerical repair-attempt, infrastructure-retry, per-attempt execution-time, and cumulative task execution-time limits. Required limit categories are settled; cost limits are optional where measurable. Specify behavior when a harness cannot stop an in-flight request.
- Define detailed permission matching and revocation; project restrictions already take precedence over task/agent grants. Permanent grants default to project scope, with explicit global opt-in.
- GitHub and GitLab with draft MR/PR defaults are accepted; clarify self-hosted coverage.
