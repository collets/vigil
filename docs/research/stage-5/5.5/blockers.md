# Stage 5.5 blocker log

This append-only log separates offline fixture evidence from decisions and runtime
evidence that require the unavailable user. Fixture actors never resolve these items.

## B1 — Real specification, plan and criteria approval
- Offline disposition: RESOLVED 2026-09-27 — the user directed autonomous
  fixture coverage instead of applying the example. Approve, reject and request-
  revision mechanics now pass through exact persisted revisions and a real PTY;
  the example remains deliberately unapproved. Real-project intent remains the
  production gate described below and is not required to complete offline 5.5.
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
- Offline disposition: RESOLVED 2026-09-27 — shared handlers/transport, native
  Hermes isolation and the fixture-gated owner interaction route satisfy Stage 5.5
  offline implementation. Native Codex and production reviewer qualification remain
  disabled production gates; no API-key fallback or simulated qualification was used.
- Hermes portion: RESOLVED 2026-09-27 — exact pinned Hermes `0.21.3`
  (`6a627e6…`) in a disposable clone performed exactly one
  `mcp__vigil__project_read` through generation `tool-qualification-1`, then
  reached native idle and rejected a stale post-retirement call. Commit
  `959eaaf` reloads MCP only after the injected session exists, permits only
  reserved transport `_meta`, and reduces the planning role to the one required
  capability. The installed Hermes canary was not changed. Codex native-tool
  qualification remains pending for a supported contained ChatGPT route.
- User authorization: RESOLVED 2026-09-26 — qualify both Hermes and Codex, using
  local llama where it is the available safe route and only included ChatGPT quota
  for Codex. The actual native/session evidence remains pending.
- Attempted 2026-09-26 — the first Hermes turn correctly failed qualification
  with zero audited calls because its prepared toolset pin hid MCP. That defect is
  fixed at `192c6ba`; the rerun stopped before launch because the existing llama
  endpoint at WSL loopback was unavailable. No substitute server was started.
- Checkpoint: D
- Requirement or checklist item: R20
- What is blocked and why it needs the user: Hermes is qualified for the bounded
  one-tool path. Native Codex tool integration still lacks a supported contained
  ChatGPT-subscription route; an API key must not be treated as permission for a
  paid fallback.
- Exact action/resource/decision needed: Supply or implement the supported
  contained Codex ChatGPT route, then run the same role/session isolation,
  stale-generation and no-takeover qualification without metered API use.
- What was implemented instead, and what remains unproven: Shared handlers and
  transport plus native Hermes behavior are proven; Codex native behavior and
  production model/reviewer qualification remain unproven and disabled.
- Safe to resume when: The contained Codex subscription route exists and its
  bounded qualification can run without paid fallback or credential exposure.
- Logged: 2026-09-26

## B4 — Native macOS Stage 5.5 validation
- REVALIDATED 2026-09-27 — the final interactive implementation was transferred
  as a verified complete-history bundle (SHA-256
  `c32a3b90a4873409130faaf079b37dcd5d5e00f45148e4bf25b429b974150516`)
  and detached at exact commit
  `bdd6e3341fcbd88e773b885ce71ec487bd6cf79f` on Darwin 25.6.0 arm64,
  macOS 26.6.2, Go 1.27.1. Native full, race, application, documentation,
  boundary and four-target cross-build gates passed. The isolated checkout/cache
  and local/remote bundles were removed and verified absent; the normal Mac and
  Cardtracker checkouts were untouched. The first non-login invocation lacked the
  Homebrew Go path and stopped before tests; the corrected invocation supplied the
  installed toolchain explicitly.
- REVALIDATED 2026-09-27 — after the checkpoint-B quality-control follow-up,
  bundle SHA-256
  `698bb5fd349c26970affb049620684d8565738abf8672b3c8026c8205e9b39ea`
  was reverified and detached at exact commit
  `669467e49b40b634e9104fda972854dae261b104`; the same native full, race,
  application, documentation, boundary and four-target cross-build gates passed.
  The second isolated remote tree/cache and local bundle were removed and verified
  absent.
- RESOLVED 2026-09-27 — the corrected host `192.168.0.108` received a verified
  Git bundle (SHA-256
  `bf3c75cdd6ea0bed1be3382a5d9e40d3e91280445ec57719e19a71d6df3ce436`)
  and checked out exact commit `959eaafe8165e45e2805c3089f66bac2739c0617` in
  an isolated temporary directory. Native Darwin 25.6.0 arm64 with Go 1.27.1
  passed `make check`, `make check-race`, `make build`, `make docs-check`,
  `make build-boundary` and `make cross-build`. The normal Mac checkout was not
  touched. This does not close the inherited Darwin fork-accounting limitation.
- Host availability: RESOLVED 2026-09-26 — the user made the documented Mac host
  available and selected native validation. The then-pending `192c6ba` handoff is
  superseded by the completed exact-`959eaaf` run recorded above.
- Attempted 2026-09-26 — `ssh -o BatchMode=yes -o ConnectTimeout=8
  simonecoletta@192.168.0.203 ...` returned `No route to host`; no remote directory
  or process was created. Native validation therefore remains blocked by host
  reachability, not by missing authorization.
- Checkpoint: D
- Requirement or checklist item: R60
- What is blocked and why it needs the user: REOPENED for remediation commit
  `84c0275`; the previously authorized Mac was unreachable at both its documented
  IP and hostname, so exact-remediation native execution needs the host online.
- Exact action/resource/decision needed: Make the documented Mac reachable by SSH
  and provide its current address only if it changed again.
- What was implemented instead, and what remains unproven: Linux full/race/build,
  boundary and four-target cross-build gates pass at `84c0275`; exact native macOS
  runtime behavior for the remediation remains unproven. Exact `bdd6e33` native
  evidence remains valid historical coverage. The Darwin fork-accounting limitation
  and live production runtime crash matrix remain separate gates.
- Safe to resume when: The Mac accepts the existing SSH identity at a known address.
- Logged: 2026-09-26
- REOPENED 2026-09-27 — complete-history bundle SHA-256
  `26f8df61c98588a360432449331522f028ed13ee9f01906abf40982a7caf6d96`
  was verified locally for exact commit
  `84c0275fa5eab599b2fa66cebd12d89f617eb484`. Transfer to
  `192.168.0.108` returned `No route to host`; `Simones-MBP.home` did not
  resolve. No remote file, directory or process was created. The local bundle was
  removed after the attempt.
