# Stage 5.5 blocker log

This append-only log separates offline fixture evidence from decisions and runtime
evidence that require the unavailable user. Fixture actors never resolve these items.

## B1 — Real specification, plan and criteria approval
- User input: PARTIALLY RESOLVED 2026-09-26 — use a small qualification
  specification in an agent-owned disposable copy of `cardtracker`; never change
  or commit the real checkout. The immutable proposal now exists as
  `qualification-proposal` revision 1, digest
  `d90d3e5b47e30dd66d52e94702859c70e2ce7c9c144519ef787b2f08bfe64a6b`;
  exact proposal/criteria inspection and approval remain pending.
- Checkpoint: C
- Requirement or checklist item: R33
- What is blocked and why it needs the user: Applying Stage 5.5 to a real project requires the user to select and inspect the specification, answer its clarifications, and approve the exact spec, plan and criteria revisions.
- Exact action/resource/decision needed: The user must select a real local Markdown specification, answer any resulting clarification requests, and explicitly approve the displayed immutable proposal revision.
- What was implemented instead, and what remains unproven: The revisioned import, proposal, clarification and approval mechanisms are implemented and exercised only with labelled disposable fixtures; real intent and approval remain unproven.
- Safe to resume when: The user is present and can inspect the exact persisted content and revision before issuing the human approval command.
- Logged: 2026-09-26

## B2 — Live planning profile and provider-idle proof
- User authorization: RESOLVED 2026-09-26 — online research, the existing local
  llama route and bounded Codex ChatGPT included-subscription usage are authorized;
  metered API and paid/extra-credit fallback remain prohibited. The supported
  contained Codex route and provider-idle evidence are still technical gates.
- Local Hermes planning: RESOLVED 2026-09-26 — the exact selected local profile
  produced proposal revision 1 under the five-minute cap, charged 56,038 ms and
  persisted terminal provider-idle evidence. This does not qualify production
  model/reviewer dispatch or the unavailable Codex route.
- Checkpoint: C
- Requirement or checklist item: R18
- What is blocked and why it needs the user: A real Codex or llama-backed planning turn and provider-idle observation are prohibited for this unattended task and could consume an authorized account or live inference resource.
- Exact action/resource/decision needed: In an attended session, confirm the chosen already-qualified planning profile and no-additional-charge route, then run the bounded planning integration and provider-idle checks.
- What was implemented instead, and what remains unproven: Closed proposal validation and a deterministic fixture planner are tested offline; live model quality, route identity and idle accounting remain unproven.
- Safe to resume when: The user is present and explicitly authorizes the specific live profile run under the existing spending constraints.
- Logged: 2026-09-26

## B3 — Native model-tool integration qualification
- User authorization: RESOLVED 2026-09-26 — qualify both Hermes and Codex, using
  local llama where it is the available safe route and only included ChatGPT quota
  for Codex. The actual native/session evidence remains pending.
- Attempted 2026-09-26 — the first Hermes turn correctly failed qualification
  with zero audited calls because its prepared toolset pin hid MCP. That defect is
  fixed at `192c6ba`; the rerun stopped before launch because the existing llama
  endpoint at WSL loopback was unavailable. No substitute server was started.
- Checkpoint: D
- Requirement or checklist item: R20
- What is blocked and why it needs the user: Native Codex/Hermes tool-session integration cannot be qualified without an attended live harness turn and eligible runtime/session identity.
- Exact action/resource/decision needed: Provide an attended qualified native session for each supported harness and verify role/session isolation, stale-generation rejection, and no takeover or steering.
- What was implemented instead, and what remains unproven: The application handlers and shared bounded transport are tested with injected fixture authority; native harness behavior remains unproven and production dispatch stays disabled.
- Safe to resume when: A qualified contained runtime is available and the user authorizes the bounded live tool test.
- Logged: 2026-09-26

## B4 — Native macOS Stage 5.5 validation
- Host availability: RESOLVED 2026-09-26 — the user made the documented Mac host
  available and selected native validation. The validation run remains pending the
  final implementation commit `192c6ba` and documentation handoff.
- Attempted 2026-09-26 — `ssh -o BatchMode=yes -o ConnectTimeout=8
  simonecoletta@192.168.0.203 ...` returned `No route to host`; no remote directory
  or process was created. Native validation therefore remains blocked by host
  reachability, not by missing authorization.
- Checkpoint: D
- Requirement or checklist item: R60
- What is blocked and why it needs the user: Native macOS access and attended validation are unavailable for this task.
- Exact action/resource/decision needed: Run the Stage 5.5 focused, race, CLI/PTY and migration-upgrade suites natively on the documented macOS host at the exact implementation commit.
- What was implemented instead, and what remains unproven: Linux tests and Darwin cross-build cover compilation only; native terminal, SQLite locking and runtime behavior remain unproven.
- Safe to resume when: The documented macOS host is available in an attended session without changing user security settings or checkouts.
- Logged: 2026-09-26
