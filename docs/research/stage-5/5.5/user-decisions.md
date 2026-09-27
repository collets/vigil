# Stage 5.5 user decision packet

Status: review packet for implementation commit `20ca4d0`; no choice in this
document is itself an approval, grant or authorization.

Resolution update, 2026-09-27: the user selected autonomous fixture exercise of
all proposal outcomes and terminal flows rather than an attended application of
the example proposal. Commit `bdd6e33` completes the offline A–D mechanics and
leaves independent review as the remaining offline acceptance step. The option
descriptions below are retained as the dated decision record; statements that B
or native Hermes/macOS qualification were incomplete describe the earlier
`20ca4d0` review point and are superseded by the current plan/results documents.

User response, 2026-09-26: select 1A, use local llama while it is the available
safe planning route with a five-minute first-run cap, select 3A, select 4B, and
select 5A. Online research and bounded Codex use within the existing ChatGPT
included quota are authorized. Metered API use, extra-credit consumption,
purchases and paid fallback remain unauthorized. The real `cardtracker` checkout
must not be changed or committed; use an agent-owned disposable copy.

This packet turns the Stage 5.5 partial checkpoints and blocker log into choices
that can be reviewed without reading the implementation. Fixture evidence proves
mechanics only. A real specification, proposal, criterion, manual outcome or live
run is authorized only when the user explicitly identifies that exact item.

## What to read

Read these in order:

1. This packet for the decisions and recommendations.
2. [Stage 5.5 plan](../../../stage-5/5.5-workflow-and-planning.md) for the normative
   checklist. The unchecked B, C and D items are the remaining boundary.
3. [Stage 5.5 results](results.md) for what was actually implemented and
   which offline tests passed.
4. [Stage 5.5 blockers](blockers.md) for the exact user/runtime gates.
5. [Stage 5 CLI](../../../stage-5-cli.md#markdown-specifications-and-fixture-planning-proposals)
   for the current import/proposal commands and
   [dashboard controls](../../../stage-5-cli.md#dashboard) for the current UI surface.
6. [Pending decisions](../../../pending-decisions.md) only if checking prior spending,
   fixture, recovery or live-runtime authorizations. Those confirmed choices do
   not need to be granted again.

The implementation can be inspected at `4dbb444` (checkpoint A) and `20ca4d0`
(B–D offline work). Stage 5.5 is not accepted.

## Current boundary in plain language

Complete offline:

- durable ranked plan queue, explicit continue/advance and stable task selection;
- responsive TUI queue/pause/continue/advance and focused permission decisions;
- bounded private Markdown import and immutable specification revisions;
- validation and atomic human application of a closed proposal revision;
- bounded role/session handlers and a transport that reuses those handlers;
- fixture end-to-end, restart, hostile-input, PTY, full, race and cross-build tests.

Not complete:

- TUI actions for owned-run stop/recovery, task acceptance, manual Pass and native
  clarification;
- a live planner that obtains a proposal through an eligible profile while opening,
  charging and closing the 30-minute cumulative plan-services ledger;
- native Codex/Hermes tool-session qualification;
- native macOS execution;
- independent Stage 5.5 acceptance review.

There is **no real proposal to approve now**. `proposal-create` is fixture-only and
accepts an already-produced closed proposal. It does not invoke a model. The next
session must not present fixture output as a live planner proposal.

## Decision 1 — finish the terminal workflow or retain the CLI boundary

### Option 1A — finish the TUI workflow (recommended)

Add typed TUI actions for stop/recovery and the distinct 5.4 human actions. Stop is
available only while a foreground controller owns the run boundary. After restart,
the TUI shows authoritative recovery state and offers only commands the existing
recovery authority permits. It does not adopt a writer or invent a runtime driver.

Consequences:

- completes the intended Stage 5.5 foreground workflow;
- requires more implementation and adversarial PTY/restart tests;
- preserves the existing supervisor, recovery and acceptance state machines.

### Option 1B — retain exact CLI commands

Keep the TUI as implemented and use existing owner-aware CLI commands for
stop/recovery and exact commands for acceptance/manual/clarification actions.

Consequences:

- smallest and most conservative surface;
- checkpoint B remains partial and Stage 5.5 cannot be described as fully meeting
  its current plan without amending that plan;
- no runtime adoption or containment behavior changes.

### Option 1C — add runtime adoption after restart (not recommended)

This would make the TUI attach to or adopt an existing runtime after process loss.
It expands containment and ownership semantics and conflicts with the current
fail-closed design unless separately specified and qualified. Do not infer this
choice from a request for a convenient stop button.

Decision requested: choose `1A` or `1B`. `1C` requires a new design decision.

## Decision 2 — planning implementation and live qualification route

The design allowance is 30 minutes cumulative active time per plan for planning,
plan acceptance and finalization together. A planning run must use an explicit
current profile revision, persist its budget effect before inference, charge crash
gaps conservatively, and never enable production dispatch.

### Option 2A — existing local llama route (recommended)

Implement the missing planner lifecycle against the already selected local route,
first with a deterministic fake provider and then with one attended bounded local
qualification. Local llama inference and necessary online implementation research
are authorized. No paid API, subscription change, credential exposure,
unrestricted worker egress or paid fallback is authorized.

Recommended first live cap: 5 minutes active within the cumulative 30-minute
plan-services allowance. A shorter run preserves budget for acceptance/finalization.

### Option 2B — included Codex subscription route

Use the previously authorized included ChatGPT subscription quota through an
effective ChatGPT login, never a metered API key. Inspect remaining included usage
when the client exposes it and stop at the included limit instead of purchasing or
consuming additional credits. Contained use still needs a supported scoped route
that does not expose the account home or grant unrestricted worker egress. The
current evidence says that contained route is not yet available, so local llama is
the active planning route while Codex qualification remains conditional.

### Option 2C — defer live planning

Keep closed proposal import/validation only. This avoids all live model use but
leaves checkpoint C partial and does not prove model-produced proposal quality or
provider-idle accounting.

Paid API fallback is not an option under the current policy.

Decision requested: choose `2A`, `2B` or `2C`, and state the active-time cap if
choosing a live route. Choosing `2A` or `2B` authorizes implementation of the
budgeted path; the actual live call still occurs only after the route identity and
no-additional-charge condition are displayed.

## Decision 3 — specification used for the human approval demonstration

This decision is needed only after Decision 2 produces an inspectable proposal.

### Option 3A — dedicated qualification specification (recommended first)

Use a small credential-free Markdown file in an agent-owned disposable fixture.
The user chooses or approves its exact content. This proves a real human decision
without turning Stage 5.5 qualification into authorization for product work.

### Option 3B — an actual product-work specification

Use a user-selected `.md` file inside the enrolled project root. The import is
private and immutable. After planning, the user must inspect the exact imported
content, proposal revision, tasks, criteria, scopes, profiles, checks and limits,
answer each clarification, and then approve or reject that exact revision.

This option authorizes plan application only. It does not authorize execution,
spending beyond the selected bound, acceptance, commit, push, publication or merge.

### Option 3C — defer the human demonstration

Leave B1 open for Stage 5.7. Stage 5.5 remains unaccepted.

Decision requested: choose `3A`, `3B` or `3C`. For `3B`, provide the project ID and
repository-relative Markdown path. Do not provide secrets or credentials.

## Decision 4 — native model-tool qualification coverage

### Option 4A — qualify Hermes/local first (recommended)

Exercise the injected project/role/run/session/generation authority through the
local harness, including a stale-generation rejection. This minimizes external
dependencies and does not enable production dispatch.

### Option 4B — qualify both Hermes and Codex

Run the same matrix for both harnesses. Codex remains conditional on a scoped,
no-additional-charge route. This is the strongest evidence but may remain partially
blocked even after Hermes passes.

### Option 4C — defer all native qualification

Retain fixture handler/transport evidence only. Checkpoint D and production
dispatch remain incomplete.

Decision requested: choose `4A`, `4B` or `4C`.

## Decision 5 — native macOS validation

### Option 5A — run on the documented macOS host

At the exact implementation commit, run focused core/TUI/tool tests, the PTY test,
migration upgrade tests, `make check`, `make check-race`, `make build`, and
`git diff --check`. This is native execution evidence; the existing Darwin
cross-build is not a substitute.

### Option 5B — defer to Stage 5.7

Keep B4 open and clearly report Linux-only runtime validation.

Decision requested: choose `5A` or `5B`. For `5A`, identify when the existing host
is available; do not weaken host security or copy credentials into the repository.

## Independent review

After the chosen implementation and qualification work, an independent reviewer
must examine the exact commit range and report findings. The implementing agent's
read-only subreview and the user's product choices do not constitute Stage 5.5
acceptance. The review should explicitly verify:

- one active plan/context, pause and restart truth;
- no second UI or transport authorization path;
- exact proposal/profile/spec/budget revision binding and atomic application;
- role/session/generation isolation and all contract caps;
- no delivery authority and production dispatch still disabled;
- every partial/deferred option is accurately reflected in status documents.

## Copy-ready response

The user can reply with only the applicable lines:

```text
Decision 1: 1A | 1B
Decision 2: 2A | 2B | 2C
Planning active-time cap: <duration; recommended 5 minutes for the first run>
Decision 3: 3A | 3B | 3C
If 3B: project ID and repository-relative Markdown path: <value>
Decision 4: 4A | 4B | 4C
Decision 5: 5A | 5B
```

No response grants commit, push, publication, merge, paid fallback, production
dispatch or blanket approval of a future proposal.
