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
make check          # vet + full suite + the documentation gate
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
than from an assumed landing: on this run the loss landed with
`submission_state=not_attempted` and `run_state=prepared`, so the report says
exactly that. Where a kill lands is a race against the synthetic driver's speed,
so the description is derived from the values each run observes and never claims a
boundary it did not see — including the `writing` state, which it describes as
in-flight and possibly already delivered rather than as "no effect". The
uncertain-outcome branch, where a resubmission is an unproven repeat and must be
refused, is carried by the accepted Stage 5.2 crash matrix and is not claimed
here.

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

All R01–R71 are classified: 49 `automated`, 14 carried forward, 8 gates.

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
