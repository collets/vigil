# Requirements discovery history

Historical discussion, preserved on 2026-09-20. Earlier rounds include alternatives and questions superseded by later decisions. [Current requirements](requirements.md) are authoritative.

## Before discovery: original proposal and working preferences

The initial idea was a CLI control panel, possibly using tmux, that combines a frontier main model such as GPT or Claude with local workers through existing harnesses. Task difficulty would influence assignment: paid frontier models for harder work, local models for suitable medium/smaller tasks. Exact difficulty estimation was not chosen. TypeSafe's [System One / Jev announcement](https://typesafe.ai/blog/introducing-system-one-models-and-jev) was suggested as a possible routing aid, not a dependency or commitment.

The user does not want to build another harness. Requested visibility includes the overall plan, current/upcoming/completed tasks, task detail, multiple plans, and project information. Reducing token usage and monetary cost is a central motivation; later discussion prioritizes reliability and task visibility over prominent cost displays.

The project began under a working name. After Stage 1, the user selected **Vigil**, inspired by the Prothean VI in Mass Effect, on 2026-09-20. The user preferred Go or Python over TypeScript, considering performance, ecosystem, and development ease; Go and the foundation stack were subsequently accepted. The technology document preserves the comparison and notes that TypeScript's performance was not established as a bottleneck.

The requested sequence was technology selection, verified hello-world setup without orchestration, then collaborative functional discovery before workflow implementation. Latest necessary software may be installed; Linux and macOS are the targets. The user explicitly wants a product-manager/software-architect sparring partner who challenges assumptions and asks detailed questions, not just agreement or immediate implementation.

See the [session audit](session-audit.md) for the current topic index and limits of reconstructing earlier shorthand replies.

## Discovery round 1: current practice and product intent

At work, the user uses Claude Code and pi. In their personal environment, they use Codex and Hermes. OpenCode is also requested in the MVP harness set. Support does not yet imply identical capabilities across harnesses.

The current Claude Code setup uses a Sonnet main agent and skills to break large plans into small, reproducible, testable, isolated tasks. Markdown files hold tasks, state, worklogs, and decisions. The main agent selects a task, evaluates it, and delegates to a Claude subagent or a silent pi instance using a local model. Another agent reviews the result; findings can trigger further development/review attempts until review passes. A separate agent handles testing.

This describes the existing practice, not an approved workflow specification for Vigil. The main pain is that repeatable coordination steps are entrusted to an LLM, exposing task progression to hallucinations, context degradation, and unpredictable omissions.

**Confirmed direction, refined in round 3:** application code enforces coordination and owns state. Agents provide plans, evaluations, implementations, and review findings through bounded tools. The main agent acts as an on-demand supervisor.

The desired interface is application-centric rather than a conversation with the main agent. Interaction with individual agents should be possible; chat is a potential mechanism for clarification or steering, not a mandated universal interface.

Approval-first operation and granular configurable autonomy are confirmed. Policy scopes, decision categories, defaults, and the relationship with harness-native permissions are unresolved.

The user currently sees no strong need for execution after dashboard closure and asks whether it adds value. Background execution is not accepted as a requirement. This is separate from preserving state and resuming later. A new folder, previously visited folder, and child folder of an existing project may need different initialization behavior.

Primary intended use: take requirements for a substantial feature or component, create a plan and tasks, and oversee largely autonomous completion with human involvement at quality and risk gates.

### Discovery round 2: decisions and open interpretations

- **Main agent:** The user requests advice on its role. Episodic plan/progress assessment, plan revision, skills, and persistent context artifacts are possibilities, not settled requirements. A permanently active session is not required.
- **Execution stages:** A default process with composable building blocks and agent-proposed adaptations is under consideration. The extent of customization and approval boundaries remain open.
- **Exit/resume:** Stop immediately on exit; on later restart choose native resume or a fresh agent. Immediate stop means beginning cancellation promptly; it cannot guarantee undoing external side effects or instantly stopping every harness operation.
- **Projects:** Multi-repository projects and subprojects are desired possibilities. Initial projects may be independent. Parent detection must warn rather than silently create a nested project. Representation and MVP scope are not finalized.
- **Approvals:** Explicit categories named by the user: plan acceptance, model assignment, spending, scope changes, committing, merging, and pushing. Per-task and per-agent configuration is requested. File editing does not need a separate application approval by default. The wording concerning commands and retries is interpreted as the same default, pending confirmation. Native harness permissions remain separate.
- **Intervention:** Opening the native harness session is the preferred convenient entry point where supported. Pause, redirect, replace agent/model, edit task, and manual takeover are desired options. Attachment mechanisms and fallbacks are not yet specified.
- **Repeated failure:** A configurable retry threshold triggers main-model assessment. Possible proposals include a stronger model, questions or confirmations for the user, task/acceptance-criteria revision, and task splitting. The application of these proposals remains subject to approval policy; scope changes are not silently authorized by escalation.
- **Harness coverage:** The requested five-harness direction is retained, but initial capability depth and rollout can be reduced according to integration feasibility. Adapter architecture should accommodate richer capabilities later.

Round 2 records the discussion at that point. The following decisions supersede its open alternatives.

### Discovery round 3: accepted boundaries

- **Supervisor:** The checkpoint-based model is accepted. Keep the execution process simple. The model may reorder remaining tasks and request approved edits, splits, or merges. Autonomous acceptance-criteria changes are excluded. Dependency validation and the handling of active tasks still need precise rules.
- **State:** The application is authoritative. The main model interacts through tools constrained by application policy. Markdown is not a competing source of task state; export and reference-artifact needs remain open.
- **Concurrency:** Sequential execution initially, motivated in part by local inference memory limits. Design for future cloud-agent concurrency but defer implementation. Whether the limit applies per application, project, or machine remains to be clarified.
- **Completion:** Require project-configured quality gates such as lint, build, unit tests, and other tests; agent review; then human final review. Human review can be skipped by task configuration. Feature-level acceptance has been raised but is not yet accepted or defined.
- **Approval defaults:** File editing, ordinary commands, and retries within configured limits do not require separate application approval by default. The previously named approval categories remain. This does not authorize commands to bypass commit, push, merge, spending, scope, or other configured gates.
- **Approval duration:** The user proposes Y = once, T = task, P = plan, A = permanently. The scope and matching conditions of a permanent grant, revocation, and policy precedence remain unresolved.
- **Partial work:** Never discard automatically. After cancellation, preserve work and offer an explicit discard action. After retry exhaustion, retain it for the recovery agent; the user wants that agent able to replace the implementation completely. Whether replacing requires a recoverable snapshot or separate approval needs clarification.

See [supervision options](supervision-options.md) for the accepted direction and remaining design questions.

### Discovery round 4: autonomous operation and quality

- **Plan acceptance:** Accepted as distinct from task acceptance. Human review at both levels can be configured away for an autonomous session. Automated quality gates and agent review remain required.
- **Bounded autonomy:** The user wants configurations such as local-agents-only, with small user stories executed autonomously up to merge-request creation. Whether local-only includes planning, supervision, review, and routing must be clarified. This is a product capability request, not authorization to publish this repository or create a merge request now.
- **Project preparation:** Approved discovery of likely test/build/lint commands is in scope. Broader exploratory onboarding, automatic project-skill creation, and sandbox-deployment setup may be deferred. Initially the developer can supply the skills and environment needed for autonomous execution. Skills do not replace programmatic quality-gate definitions.
- **Workspace:** Defer managed Git worktrees because ignored environment/configuration files complicate setup. Current-checkout execution is the resulting proposal. Protection of pre-existing edits, manual changes during runs, branch management, and cross-instance conflicts still need definition.
- **Recovery:** A recoverable snapshot is sufficient to let the recovery agent replace prior implementation. Automatic unrecoverable discard remains prohibited. Snapshot coverage, retention, and treatment of user-owned changes remain open.
- **Quality gates:** Detect and propose commands during project setup; approve them before use. Tasks may strengthen but not remove project-required gates. Accepted baseline failures must remain visible rather than becoming a claim that all checks passed. Criteria for determining new or worsened failures still need specification.
- **Reviewer:** Always use a fresh session. Select the reviewer model at task creation based on difficulty; using a different model from the implementer is not required. The reviewer reports findings and suggestions, while a subsequent implementation attempt makes changes. Findings format, severity thresholds, and review evidence remain open.

### Discovery round 5: intake, repository awareness, and delivery

- **Intake:** Interactive discussion with the main agent is a preferred entry point. Local Markdown or pasted requirements may skip the discussion. Issue URL intake is also desired among the proposed input modes; providers, authentication, and initial scope remain open. Skipping discussion does not waive validation or any configured plan-acceptance gate.
- **Task readiness:** All proposed task fields are required: objective, acceptance criteria, dependencies (which can be empty), relevant context, verification steps, and implementation/reviewer profiles. An underspecified task goes to the main agent for assessment and possible user clarification.
- **Presets:** Reusable presets with visible overrides are accepted; exact presets are requested as proposals. Local-only role coverage has not yet been explicitly confirmed.
- **Blocked work:** Proceed with independent eligible tasks when dependencies permit; preserve blocked items for later resolution. Partial code left by a blocked task may still interfere with other work in the shared checkout, so task dependency independence alone is not sufficient; the handling rule remains open.
- **Branches and repository map:** Always use a new dedicated branch. Traverse the project folder tree to identify nested repositories, track them, and provide their boundaries in agent context. This should prevent confusion about paths and ignore rules across repositories. Do not equate discovery with permission to modify every repository. Scan boundaries, exclusions, submodules, linked worktrees, and branch lifetime remain design questions.
- **Existing changes and manual work:** Ask explicitly when uncommitted changes exist; never delete them automatically. The user accepts pausing execution before editing manually.
- **Delivery:** MR/PR creation is the last possible automated delivery step. Automatic merging is explicitly excluded. Draft creation is the recommended default under discussion; GitHub/GitLab, self-hosted support, and initial integration priority still need explicit selection.
- **Profiles:** Guided onboarding is desirable. Manually configurable harness/model profiles are an acceptable first step if onboarding is too complex.

### Discovery round 6: checkpoints and dashboard

- **Presets and onboarding:** The proposed model-policy/autonomy split works for now. Guided onboarding is not needed initially. Keep manual configuration and visible overrides. A default model policy has not been selected.
- **Branch lifetime:** One dedicated branch per plan per affected repository is accepted; resume reuses that branch.
- **Checkpoint proposal:** The user proposes identifiable staging commits, using project/plan/task identity, to preserve incomplete work while clearing the working tree. The need is accepted for investigation; storage mechanism, commit authorization, restore policy, and treatment of user-owned changes remain unresolved. See [checkpoint proposal](checkpoints.md).
- **Hosting:** GitHub and GitLab are required. Draft MR/PR creation is accepted. Self-hosted coverage remains unspecified. Automatic merging remains excluded.
- **Dashboard:** Main view emphasizes task progression, items needing immediate user action, and possibly live-agent activity. Show compact overall quality status. Detailed changes, checks, and cost belong in secondary views, not future releases by implication.
- **Human review:** Show findings, implementation summary, and check results. Code and diff inspection can happen in the IDE; no embedded code/diff viewer is required. Show a manual functional checklist only when relevant, such as validating frontend behavior in a browser.

### Discovery round 7: recovery permissions and review actions

- **Terminology:** The user intended stash, not stage. The product needs identifiable saved attempts; Git implementation details remain open and should prefer existing primitives where suitable.
- **Checkpoint permissions:** Automatic saving is accepted; restoration must ask. Do not interpret saving as automatic authorization to clear or replace the live checkout. Whether a scoped advance approval may satisfy future restore requests is unresolved.
- **Commits:** Per-task commits are accepted, including multiple commits for clarity. This does not waive the configured commit approval policy or make partial commits evidence of task acceptance.
- **Inbox:** Combine approvals, questions, failed checks, and manual verification in a unified actionable inbox. Link each item to relevant task context. Nonblocking items allow eligible work to continue; required gates still block their affected task or plan.
- **Manual checks:** Pass / Fail / Cannot verify with notes/evidence references is accepted. A failed check may provide feedback for a new implementation attempt. Cannot verify does not count as passing.
- **Review findings:** Configurable blocking thresholds with nonblocking suggestions are accepted. Required quality gates are not waived by a review severity setting.
- **Human rejection:** Request changes, clarify requirements, and stop task are accepted as separate actions. Their retry accounting, approval effects, and dependency consequences remain to be specified.

### Discovery round 8: operational scope and first milestone

- **Stash semantics resolved:** Saving and clearing agent-owned changes may be automatic, while preserving pre-existing user work. Approval is required to bring saved changes back. This supersedes the earlier conservative interpretation of restoration approval.
- **Limits:** Repair-attempt and execution-time limits are mandatory for autonomous execution because they are measurable. Infrastructure failures use a separate retry allowance. Cost limits are optional where measurable; exact thresholds and timer accounting remain open.
- **Controls:** Pause scheduling allows the current attempt to finish and prevents further dispatch. Stop now interrupts and preserves work. Closing the application retains stop-now behavior. Neither control alone implies closing other independently running project instances.
- **Concurrency:** Separate projects may execute concurrently, but not within overlapping folder trees. Within-project agent execution remains sequential. Shared local inference capacity may still need coordination across independent projects; its policy is unresolved.
- **Instructions:** Profiles may reference existing harness resources. Project-specific and role-specific additions are accepted. Preserve native harness support and surface unsupported combinations rather than promise portable skills automatically.
- **First milestone:** Manual profiles, a small Markdown specification, approved tasks, sequential execution, a demonstrated review/repair cycle, checks and human review, and draft PR/MR creation. Codex and Hermes are the first harnesses; the other three are later targets. See [MVP acceptance](mvp-acceptance.md).

### Discovery round 9: resource policy and final records

- **Local inference:** Use an existing llama.cpp service. Do not manage server installation, model loading, or unloading initially.
- **Shared capacity:** One active local agent per endpoint across projects by default; queue others. Independent cloud execution may proceed. Coordination initially concerns application-managed sessions; unmanaged clients and multi-machine coordination require separate scoping.
- **Time limits:** Per-attempt and cumulative task execution limits are accepted. Exclude approval and resource-slot waiting. Retries retain accumulated execution usage. Specific defaults and attribution of supervisor/check time still need definition.
- **Plan queue:** Multiple plans per project, one executing at a time, with an explicit queue and optional automatic advancement.
- **Permissions:** Project restrictions cannot be silently weakened by task/agent overrides. Permanent grants default to the project, remain visible/revocable, and require an explicit choice to become global.
- **Retention:** Durable completion records remain by default; raw transcripts may expire on a configurable schedule. Unfinished checkpoints require explicit removal. Cleanup must not leave required evidence available only through deleted transcripts.
- **Finalization:** The user requests one last task to summarize all work, reference produced resources, and save everything in the plan folder. Exact archive location, content, completion/delivery ordering, and summary-failure behavior are still to be defined. See [plan finalization](plan-finalization.md).


## Discovery round 10: accepted final boundaries

- Archives default to a local Git-ignored plan folder; export or committing selected documents is explicit.
- Finalization failure leaves finalization pending and retains the factual record; retry only finalization. Draft request creation remains available as an explicit action.
- Raw transcript retention defaults to 30 days after plan completion, configurable per project; unfinished recovery context remains available.
- First release manages only sessions launched by this application and provides a terminal-only interface.
- A small shared coordination mechanism for workspace ownership and local inference slots is accepted; agent execution remains foreground-only.
