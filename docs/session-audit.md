# Session continuity audit

Audited 2026-09-20 against the available conversation and project documents. Purpose: preserve product intent, accepted decisions, alternatives, unresolved questions, and implementation evidence for a new session.

## Reading order and authority

1. [Next steps](next-steps.md): where to resume and what to implement next.
2. [Requirements](requirements.md): current product baseline, R01–R71, and open questions.
3. [Discovery history](discovery-notes.md): rationale, earlier options, and superseded interpretations.
4. [Architecture](architecture.md) and topic documents below: design direction and proposals.
5. [Harness investigation](harness-capabilities.md) and [probe evidence](research/harness-probe-results.json): observed capabilities versus untested assumptions.

The discovery history is chronological; an early “open” statement does not override a later accepted decision. Research recommendations and the next-step plan are not new user-approved product requirements. The application remains a hello-world scaffold.

Provenance limit: several available user messages answer numbered questions with “yes,” “your suggestion,” or similar shorthand, while the corresponding earlier assistant proposals are not available verbatim in the conversation supplied for this audit. Existing discovery notes preserve their interpreted meaning. Those interpretations are retained, not presented as reconstructed quotations. No verbatim transcript or guarantee of recovering every unavailable proposal is claimed. If an ambiguity affects implementation, use the explicit requirements and ask a focused question rather than inventing the missing exchange.

## Decision and options index

| Topic | Current decision, alternatives, or unresolved boundary | Durable record |
| --- | --- | --- |
| Product purpose | Control existing harnesses; combine paid frontier and local agents to reduce cost while making coordination reliable; do not build another harness | Requirements R01–R08; discovery introduction/round 1 |
| Existing workflow | Sonnet main agent, Claude subagents or silent pi local workers, Markdown plans/state/worklogs/decisions, review/repair loop, separate testing agent; LLM-driven repeatable coordination is the pain point | Discovery round 1 |
| Name and collaboration | Vigil selected after Stage 1 on 2026-09-20; user wants detailed discovery and constructive challenges; scaffold before functional analysis, then implementation | Discovery introduction; next steps |
| Language alternatives | Go selected over Python; TypeScript declined by preference, not a demonstrated performance failure; Python ecosystem advantage and Go distribution/concurrency tradeoffs retained | Technology |
| Foundation | Go, Cobra, Bubble Tea/Bubbles/Lip Gloss, SQLite; versioned JSON proposed, TOML possible later; no ORM; tmux optional, not the state owner | Technology; architecture |
| Installation/platform | Latest necessary tools may be installed; Linux/macOS targets; hardware architectures and checked versions recorded in scaffold docs | Technology; README |
| Harness rollout | Original five: Claude Code, Codex, pi, Hermes, OpenCode. Codex/Hermes first; others deferred, architecture capability-aware | R10, R42; discovery rounds 1/8 |
| Supervisor alternatives | Always-on conversation and fresh supervisor at every task boundary considered; fresh event/checkpoint-triggered sessions selected to limit accumulated context and unnecessary calls | R20; supervision options |
| Supervisor responsibilities | Plan adherence, progress, cross-task assessment and exception advice; application owns repeatable scheduling/state/retry actions | R18, R20–R23; supervision options |
| Workflow composition | Agent-mixed building blocks considered; simple application-controlled process selected. Arbitrary agent-authored workflows excluded; typed stages remain a design option | Discovery rounds 2/3; supervision options |
| Replanning | Reorder remaining tasks; edits/splits/merges require applicable approval. No autonomous acceptance-criteria changes; dependency/revision/evidence rules still to design | R23; supervision options |
| Failure escalation | Main model may propose stronger eligible model, questions/confirmation, task revision or splitting. Acceptance-criteria revision was an early suggestion, constrained by later prohibition on autonomous changes | R18, R23; discovery rounds 2/3 |
| Role/model routing | Frontier main model supported, difficulty-based workers/reviewers, manual overrides, local-only/hybrid/cloud-allowed policies. Exact difficulty scoring and role profiles not fixed | R03–R04, R31, R36; presets |
| Jev | Optional future model/task routing advisor; no chosen integration or evidence of benefit yet | R09; architecture; discovery introduction |
| Intake and readiness | Interactive clarification, pasted requirements, local Markdown; issue URLs desired in discovery with scope/provider details open. All readiness fields required before dispatch | R33–R35; discovery round 5 |
| Task granularity | Small, reproducible, testable, isolated tasks are the desired planning style; no numerical size/difficulty threshold chosen | Discovery round 1; R34 |
| State and context | SQLite/application authoritative; bounded model tools, fresh-session context assembled from requirements, decisions, artifacts, skills, and results; Markdown is evidence/export, not competing live state | R21, R58; supervision options |
| Project relationships | Multi-repository project intent retained; possible subprojects, initially independent. Detect parent and warn; first/returning/child-folder initialization remains open | R13, R19, R39; requirements open question 10 |
| Plans/concurrency | Multiple plans, one active per project, explicit queue and optional advance; sequential agents initially; concurrent nonoverlapping projects; within-project cloud parallelism deferred | R22, R57, R62 |
| Shared coordination | Small same-machine mechanism for folder ownership and local slots; one active managed local agent per endpoint by default; queue others; external clients not controlled | R60, R71; architecture |
| Autonomy | Approval-first default; fully autonomous sessions possible within explicit constraints. Model eligibility separate from human gates; no default model policy selected | R12, R27–R28, R36; presets |
| Approval categories/scopes | Plans, assignment, spending, scope, commits, push, request creation, restore; per task/agent. Once/task/plan/permanent, suggested Y/T/P/A. Permanent project by default, explicit global opt-in, visible/revocable | R17, R63; accepted defaults; discovery round 3 |
| Ordinary operations | Edits, ordinary commands, bounded retries need no separate app approval by default, but cannot bypass gated actions. Native harness permissions remain separate | Requirements defaults; harness investigation |
| Merge decision evolution | Merging initially listed as an approval category; later automatic merging excluded entirely. Endpoint is draft GitHub PR/GitLab MR | R41, R44; discovery rounds 2/5/6 |
| Quality/acceptance | Configured lint/build/unit/other tests, fresh agent review, task and plan acceptance against requirements. Human review waivable, explicit manual verification remains required | R24, R26–R31, R48, R52 |
| Quality setup/baseline | Discover/propose commands for approval; tasks can strengthen but not remove project checks. Explicit accepted pre-existing failures stay visible as unhealthy | R29–R30 |
| Reviewer | Fresh session, model chosen at task creation by difficulty, findings/suggestions only; separate implementation attempts repair; no requirement to use a different model | R31, R53; discovery round 4 |
| Human review/inbox | Findings, implementation summary, checks; IDE for code/diff. Manual Pass/Fail/Cannot verify with notes/evidence. Reject via request changes, clarify, or stop | R47–R48, R51–R54 |
| Dashboard priority | Task progression, actionable inbox, optional live agent, compact quality. Detailed changes/checks/cost behind navigation, not implicitly postponed beyond MVP | R45–R47; discovery round 6 |
| Intervention | Native session preferred where feasible; steering, pause, replacement/model change, task edits, manual takeover desired. Exact attach/handoff fallback still unproven | R11, R16; discovery round 2; harness investigation |
| Exit/pause/stop | Foreground; close requests stop now. Pause finishes current attempt then prevents dispatch; stop interrupts/preserves. Resume native if supported or fresh by choice | R15–R16, R56 |
| Limits | Mandatory repair/execution limits; infrastructure retry allowance separate; per-attempt and cumulative task time; approval/slot waits excluded; retry does not reset total; optional measurable cost limits | R55, R61; discovery rounds 8/9 |
| Branches/repositories | Dedicated branch per plan per affected repo, reused on resume; nested repo map always in context. Discovery alone does not authorize changing every repo | R38–R40, R43; discovery round 5 |
| Worktrees/manual work | Managed worktrees deferred due ignored env/config setup burden. Current checkout; explicitly handle dirty baseline, never delete it, pause before manual edits | R40; discovery rounds 4/5 |
| Stash versus stage | User corrected “stage” to “stash”; identifiable recovery checkpoints wanted, not failed-attempt delivery commits. Ordinary stash/custom refs both remain implementation options | Checkpoints; discovery rounds 6–8 |
| Preservation/restore | Auto save and clear agent-owned changes allowed, preserve user baseline; restore requires approval. No unrecoverable auto discard. Recovery may replace implementation after snapshot; unfinished checkpoint removal explicit | R25, R32, R49, R64; checkpoints |
| Blocked tasks | Continue independent eligible work only after checkout safety. Preserve failed work and pending questions; dependency independence alone is insufficient | R37; checkpoints |
| Commits/delivery | Per-task commits, multiple for clarity, governed by policy; GitHub/GitLab draft requests; self-hosted support unresolved | R44, R50; requirements open questions |
| Profiles/skills/onboarding | Manual profiles first; reference existing resources and project/role instructions. Guided exploration, generated testing/deployment skills and sandbox setup desirable but deferred; developer supplies initial setup | R42, R58; discovery rounds 4/8 |
| Local inference | Existing llama.cpp service; no installation/loading/unloading management. Local-only inference does not automatically mean offline operation | R59–R60; presets; harness investigation |
| Finalization | Visible final task, factual evidence plus narrative proposed, local Git-ignored plan archive, explicit export/commit. Failure pending, retry summary only, explicit draft delivery still possible | R65–R67; plan finalization |
| Retention | Durable records indefinite by default; transcripts default 30 days after completion, configurable; preserve unfinished recovery. Native harness histories distinct from app records | R64, R68; finalization; harness investigation |
| Initial UI/session scope | Terminal only; manage only app-launched sessions. Browser UI, adoption of external sessions, background execution deferred/excluded as documented | R69–R71; requirements scope |
| Integration research | Codex app-server stdio and Hermes gateway stdio recommended; ACP alternative, HTTP not selected, exec JSON less suitable for interactive supervision; live validation pending | Harness investigation |
| Critical research caveats | Native approvals incomplete as product boundary; Hermes auto-continuation, nested/auxiliary inference, cancellation quiescence, unknown resume, native UI handoff and macOS runtime need validation | Harness investigation; next steps stages 1–3 |

## Open decisions to retain

The [requirements open-question list](requirements.md#open-design-and-feasibility-questions) is the main design backlog. In addition to that list, retain these details from supporting documents:

- Default model policy, precise preset role assignments, difficulty evaluation, and any separate offline/data-egress requirement.
- Exact supervisor triggers/frequency and context-packet format; do not assume invocation after every task.
- Numerical repair, infrastructure-retry and time defaults; attribution of review/check/supervisor time.
- Approval matching, revision invalidation, revocation, denial/defer UI, and whether advance task/plan grants satisfy saved-work restoration.
- Repository scan exclusions, submodules/linked worktrees, new/returning/child-folder initialization, project/subproject relationships.
- Checkpoint implementation, ignored-file preservation, mixed edit ownership, retention and explicit discard/cleanup UX.
- Evidence freshness after repairs and plan edits, baseline-failure comparison, human rejection accounting, and consequences for dependencies.
- Issue-URL import and self-hosted hosting coverage; multi-repository request linkage.
- Native session handoff and required input support per harness; no promise of concurrent control through two UIs.
- Exact config schemas/precedence, archive layout and export format, finalization ordering, credential references, and native-history retention boundaries.
- Hard permission enforcement and runtime validation on Linux/macOS. No container/sandbox implementation choice has been made.

## Audit corrections made

- Preserved the original proposal, language alternatives, routing idea, installation authorization, and request for constructive product/architecture sparring in discovery history.
- Removed stale statements treating approval-first, project policy precedence, draft hosting support, SQLite, and archive export direction as undecided.
- Promoted project/subproject and folder-initialization questions into the current open-question list.
- Kept historical rounds unchanged in meaning; documented that R38/R43 duplicate the branch decision instead of renumbering requirements.
- Added this index to distinguish product decisions, proposed options, observed research, and remaining uncertainty.

## Persistence and verification status

Subsequent implementation update (2026-09-20): the project is now named Vigil, the initial scaffold/Stage 1 and rename are committed, and `origin` is `git@github.com:collets/vigil.git`. Stage 2 implements the bounded Go transports and development runner. Both Codex and local Hermes passed real fixture execution; see [Stage 2 evidence](research/stage-2-results.md) and the [current handoff](next-steps.md). The original audit notes below are historical, not the current Git or validation status.

All continuity documents are saved in the project filesystem. At audit time the scaffold and docs are still untracked in the initialized Git repository; they are not a commit or remote backup. Closing the chat does not require a commit to retain these local files. No commit, push, or publication was performed by this audit.

This was a documentation consistency audit, not a fresh test of the scaffold or harnesses. Local Markdown links, requirement-ID continuity, and evidence JSON structure were checked. Future sessions should follow the baseline checks in next-steps.md before code changes.
