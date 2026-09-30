# Stage 5.7 autonomous qualification evidence

<!-- vigil-tier: evidence -->

Status: **implemented, under independent review; not accepted.** The walkthrough
runs and produces a complete, honest record, but it found a blocking product
defect (5.7-F1) and the stage cannot be accepted while the delivery path is
unreachable. This slice does **not** claim the milestone is demonstrated.

## What this is

Stage 5.7 rehearses the autonomous part of the [accepted
milestone](../../../core/mvp-acceptance.md) against a disposable repository, using
the production application path. The runner is `cmd/vigil-scenario`
(`make scenario`); the harness, the disposable fixture, the evidence matrix and
the four capability gates are in `internal/scenario/`.

The runner drives the real `bin/vigil` binary as **separate operating-system
processes** — not `internal/spike`, not hand-arranged database rows — so it
exercises the actual command surface, the real per-invocation open/close cycle,
and the real receipt and revision checks.

## How to reproduce

```sh
make check          # vet + the full test suite
make docs-check     # the documentation consistency gate (a separate target)
make scenario       # the offline autonomous walkthrough, into /tmp/vigil-stage-5.7-scenario
```

`make scenario` exits **zero** when the walkthrough completes. That is
deliberate: the gaps it records are the deliverable, not a failure of the tool.
A nonzero exit means a stage *aborted*, which is a different and louder outcome,
and the report says `aborted: true` in that case.

`make scenario-guard-check` exercises the `SCENARIO_ROOT` guard against a battery
of paths that must all be refused — a sibling directory, a `..` traversal, a
nested subdirectory, a symlink, a trailing slash, `/etc`, a relative path and an
empty value — using a sentinel directory it creates and removes itself. Every
scenario target refuses a root that does not resolve to the documented
`/tmp/vigil-stage-5.7-scenario`.

`make scenario` writes `report.json` into its disposable root and prints the
milestone, recovery and requirement tables. `make scenario-clean` removes that
root; the runner itself never removes anything it did not create, and refuses a
root that already has content in it.

The run is bounded **before it starts** (`Bounds` in `internal/scenario/driver.go`):
at most 220 recorded steps, 120 s per step, 4 MiB of output per step, 4 live
model turns, and 45 minutes of wall clock. Every bound is enforced, not advisory.

## The milestone table

| Step | Result | Evidence |
| --- | --- | --- |
| 1 Configure profiles | automated | `project init`, explicit policy/check definitions, harness profile, single-host endpoint |
| 2 Load specification | automated | `spec-import` then `spec-show` read the Markdown back byte-identical |
| 3 Produce and approve plan | automated | `proposal-create` → approval item → `proposal-apply`; production eligibility stayed false |
| 4 Execute tasks sequentially | **pending Stage 8** | the full persisted execution lifecycle ran, but through the labelled `--synthetic-fixture` driver, not a live harness. The plan forbids calling an unavailable route a pass |
| 5 Review finding and repair | automated | a fresh distinct reviewer session found the injected defect as blocking; a bounded repair fixed it; fresh check and a distinct fresh review then passed |
| 6 Checks, findings, summaries | automated | check, fresh review, manual outcome, human decision, atomic acceptance, plus the plan's own `--plan-wide` gates |
| 7 Task and plan human review | **pending Stage 8** | a real functional Pass and real acceptance are human decisions; fixture actors prove mechanics only |
| 8 Draft pull/merge request | **unmet** | blocked by finding 5.7-F1 below; a real draft also needs an authorized destination |

## Finding 5.7-F1 — the delivery path is unreachable (blocking)

Preparing the plan branch is required for execution, and it leaves the checkout on
the plan branch. The commit path refuses to move a ref the user currently has
checked out. Returning the checkout to the base branch — which no production
command does — invalidates the accepted task fingerprint, because acceptance
binds the whole baseline including the head ref.

The two refusals are mutually exclusive, so commit, and therefore push and draft
delivery, cannot be reached from the documented workflow:

```
observation 1: commit-prepare refused while HEAD is on the plan branch
observation 2: commit-prepare refused again after an explicit base-branch return:
               "repository no longer matches accepted task fingerprint"
```

The rehearsal did **not** work around this and did not report it as a pass. The
three affected recovery rows and the draft milestone are recorded as `unmet`,
because the cause is a product defect in the Stage 5.2/5.6 delivery path and not
a human gate. Fixing it belongs to those slices; the check is that the
`commitParent` guard and the acceptance fingerprint stop contradicting each other
for a repository the application itself prepared.

## Boundary and recovery cases

Twenty cases, all decided: eleven fully automated, two carried from accepted
predecessor records, three `unmet` under 5.7-F1, and four **partial**.

A partial row is its own class, not a flavour of automated. It records that a
narrower observation happened through the production path while the full property
named by the case did not, and it is listed in the gap list, because a
partially observed case is a gap. `RequireComplete` refuses a partial row that
does not state what was not observed. The four are:

| Case | Observed here | Not observed here |
| --- | --- | --- |
| `endpoint-alias-queues-behind-one-capacity` | two URL spellings are aliases of one physical endpoint with one capacity; a second physical ID claiming the same URL is refused | a second waiter queueing behind the slot (needs two concurrent controllers) |
| `stale-owner-fenced` | reconciling an owner never observed dead is refused; claims carry fencing generations | fencing a real stale owner (needs a controller killed while holding a claim) |
| `mixed-user-and-agent-work-preserved` | a commit naming an out-of-scope path is refused and unrelated work is byte-identical afterwards | clearing and restoring a mixed user/agent change set |
| `partial-multi-repository-restore-is-visible` | a restore naming three nonexistent identities is refused before touching any repository | a genuinely partial multi-repository restore |

The remaining eleven were observed in full. `controller-kill-leaves-unknown-outcome`
is among them, and its detail is written from the observed durable state rather
than from an assumed landing. Where a kill lands is a race against the synthetic
driver's speed, and **both landings occur**: across repeated runs on this host the
loss landed with `submission_state=not_attempted`/`run_state=prepared` on some runs
and with `submission_state=writing`/`run_state=starting` on others. Each detail is
derived from the values that run observed, and the recorded run below is one
sample of a racy observation rather than a stable result.

The in-flight `writing` landing is the one worth reading carefully. It does **not**
mean no effect occurred: the run had journaled that it was about to submit and had
recorded no outcome, so the prompt may already have been delivered. The report
says exactly that. The genuinely uncertain branch, where a resubmission would be an
unproven repeat, is carried by the accepted Stage 5.2 crash matrix and is not
claimed here.

**What the harness asserts about replay safety is deliberately narrow**, because
it is checked against a racy landing. `verifiesReplayBoundary` is written from what
production's own `Inspect` actually guarantees:

- `uncertain` is the one state production withholds `start` for, so that is the
  only hard check — a resubmission would be an unproven repeat.
- `writing` is *pending reconciliation*, not already unsafe to resubmit. Production
  offers both `start` and `reconcile` for a `writing` run, so requiring `start` to
  be absent would assert a property the product does not have. The check that
  cannot false-positive is the narrower one: `reconcile` must be offered on a run
  the inspection would let a caller submit to — the only run where an unproven
  replay is possible. A run offering no submit path has nothing to replay and
  nothing left to reconcile, so it is exempt. Both conditions are read off the
  observed command list rather than off the run state, so the reason can never
  contradict the list printed beside it.
- a resolved state resolves the outcome, so offering `start` is the safe path.
- an absent or unrecognised state establishes nothing and is never reported as
  resolving an outcome. `decodeIsReadable` requires `submission_state` for this
  reason: a safety reason derived from an absent value would claim an outcome
  nobody observed, and would do so silently if the field were ever renamed in
  production.

The refusal to resubmit an unproven generation is enforced in production in
`Runner.submit` and `Runner.Reconcile`, deterministically and independently of
where a kill lands; the accepted Stage 5.2 crash matrix covers it. Asserting it
from a racy kill landing instead was a defect an earlier revision of this harness
shipped, and it aborted the qualification on a majority of runs while reporting a
replay-safety defect in the product that does not exist. The check is now also
non-fatal: a landing that does not satisfy it is recorded as a `partial` case
rather than asserted, so it appears in the gap list instead of only in the
limitations text.

**A limit of the `writing` landing: the walkthrough demonstrates no replay-safety
property on its own.** In the sampled runs the kill never landed in `uncertain`,
which is the only state the hard check covers, so the qualification shows that the
reopened inspection is readable and accurately described — not that the product
refuses an unproven replay. That property is carried by the accepted Stage 5.2
crash matrix, and it is stated here rather than left to be inferred from a case
name.

`TestReplayBoundaryAgreesWithProductionInspect` pins the correspondence by
transcribing production's `Inspect` switch and requiring the harness to accept every
combination the product can emit. That test exists because a check which only
exercises the harness's own rule cannot catch the harness asserting something the
product does not do.

Be precise about what it is worth. The transcription is a second hand-written copy
of production's switch, so by itself it cannot detect production changing
underneath it — its value is that a reviewer reading the harness check has the
product's rule beside it in executable form, and that is what caught the defect.
`TestProductionTranscriptionMatchesSource` is what makes the copy safe: it parses
production's `switch` arms — condition and command list, in order — out of the
source and fails if any differs from the transcription, and it fails loudly rather
than skipping when the source is unreadable. It is a drift guard on a copy, not an
integration test against a running product. An earlier version of it was named as
a drift check but only inspected two text shapes, and was shown to miss five of
six realistic production mutations; the parser form catches them, which was
verified by mutating `reconcile.go` and confirming each mutation fails the test.

`TestSchemaEnumerationsMatchMigrations` does the same for the run states and
submission states the harness enumerates: it reads the `CHECK` constraints out of
the migrations and fails if the enumeration and the schema disagree in either
direction. It also handles the shape these migrations actually use to widen a
constraint — SQLite cannot `ALTER TABLE ... ADD CONSTRAINT`, so a widening can
only ship as `CREATE TABLE runs_v<N> (…wider…)` followed by
`ALTER TABLE runs_v<N> RENAME TO runs` — and it ignores a versioned table that is
never renamed onto the name, so an abandoned rebuild does not report states the
product cannot hold. Both directions were verified by mutating a migration.

Also observed in full: pause refusing five
distinct dispatch commands and `continue` restoring them; a bounded stop
preserving the artifact byte-identical; an exact resume refused with missing
native history and a fresh-context choice producing a distinct attempt; a real
`SIGKILL`ed controller leaving a durable, inspectable record; overlapping projects
refused and disjoint siblings both readable in one state directory; a stale
revision refused, a deny decision resolving its request, and a second decision
refused; acceptance refused with a manual Pass and an accept decision but no
check or review evidence; a denied authority creating no consumable grant; and
retention expiry gated behind an unchanged dry-run receipt.

Two cases are carried from accepted predecessor records rather than re-derived:
repair exhaustion (this run consumed one of two allowed repairs, so it did not
reach exhaustion) and the charged-time exclusion for a resource wait (forcing a
live wait needs a second controller holding a slot).

## The injected defect is real

The fixture commits the wrong greeting, so its own check fails — asserted before
anything else runs. The implementation attempt then writes the specified greeting
**with a trailing space**. The deterministic check compares the trimmed line and
passes; the fresh reviewer, whose document is built by reading the file, finds the
real formatting defect. `reviewerDocument()` aborts rather than return a finding
if the artifact carries no trailing whitespace, so a fabricated reviewer result
is not producible: the defect and the finding are derived from the same bytes, but
the finding is only emitted when the bytes are actually defective.

The bounded repair removed the defect, and the run required **fresh** check and
review evidence afterwards rather than reusing the prior result.

## Requirements

All R01–R71 are classified: 48 `automated`, **1 `partial`**, 14 carried forward, 8
gates.

R41 is the partial one, and it is partial rather than automated because of the
evidence, not the disclosure. R41 requires delivery to *end* at
merge/pull-request creation. This run's delivery stopped earlier — at the
draft-request stage, because 5.7-F1 blocked commit and push — so the boundary R41
names was never exercised. The row records the one fact the run did observe (no
merge command, endpoint or transport exists anywhere in the product command
surface) alongside an explicit statement of what was not observed, and it appears
in the report's `requirement_gaps` list.

Requirement gaps are reported under their own key rather than folded into the
milestone/recovery gap list, because a requirement is a different kind of claim
from a case and conflating them would let a reader mistake one for the other. Each
section's printed total names every class it holds, including `undecided` for rows
an aborted run never reached, so the line's own arithmetic reconciles
(8 + 20 + 71). A class the renderer does not know about at all would render as an
explicit `UNPRINTED=` contradiction rather than a summary that quietly does not
add up.

**The citation check verifies resolvability only.** Each carried citation is
confirmed to name a record that is readable in this checkout; an absent record
would be demoted to a gate. It does *not* verify that the record's content covers
the requirement, because those records are narrative documents keyed by
slice-local finding IDs rather than by requirement identifiers. An earlier
revision required the identifier to appear in the text, which rejected all 14
substantively valid citations behind a false blocker — a second false claim in
place of the first. The limitation is stated here rather than left implicit, and
an independent reviewer spot-checked the citations substantively instead.

R09 is a gate on its own merits: Jev was considered as an optional aid to
model/task selection and no integration or evidence of benefit was ever recorded,
so nothing supports it. Its originally proposed citation did not cover it either.

## Capability gates

Four independent opt-ins, all off by default. Two are refusable by no
configuration and are recorded as owned Stage 8 gates with their exact blockers:

- `codex_live` — refused: no supported contained ChatGPT-auth route exists.
- `remote_delivery` — refused: no authorized destination.

`docker` is a plain opt-in. `local_inference` is granted only when the inherited
loopback route answers a metadata probe; the probe reads presence and route
identity only, never prints or persists a key value, never follows a redirect and
never contacts a non-loopback host.

**The live model turn is not yet driven.** The gate, the route probe and the turn
budget are implemented and tested, but no stage calls the live execution path, so
`make scenario-live` records a turn count of zero. That is stated in the report's
own notes and in the target's comment rather than implied otherwise, and it is
recorded as pending work.

## What was not verified

- Any real harness turn. Zero model turns ran; every execution used the labelled
  synthetic fixture driver.
- Native macOS validation and the cross-build matrix.
- `scenario-clean` and the `--keep=false` removal path, though the guard both share
  is exercised by `make scenario-guard-check`.
- The Docker opt-in, which this environment did not exercise.

## Pending Stage 8 inputs

1. A qualified contained Codex route — the single hardest blocker, and a user
   decision.
2. A real bounded local Hermes turn through the prepared route.
3. Real task and plan human acceptance, including a real functional Pass.
4. A real push and draft request against an authorized destination, plus the
   `delivery-cancel` / `delivery-reconcile` / `retention-expire` human commands.
5. Human narrative review of the factual archive.

See [pending decisions](../../../process/pending-decisions.md) for each with its
exact missing input.
