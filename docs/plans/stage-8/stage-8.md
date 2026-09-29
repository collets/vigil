# Stage 8 — Human review of the product

<!-- vigil-tier: plan -->

Status: **scope defined, not planned and not started.** Established 2026-09-29
by explicit user scope revision.

This document is a **scope** description: outcome, boundaries and deliverable.
Work checkpoints are [8.1's](#81-human-review-plan) job.

**This is the human stage.** Everything here exists because a person has to be in
the loop. It is the first stage that cannot be completed by an agent alone, and
that is by design: it is the stage where a human decides whether the product is
acceptable, using the interface built in Stage 6 and the documentation built in
Stage 7.

## Why this stage exists

Every stage before this one has been autonomous, and that has a cost: the
product has never been used by a person. There is no evidence that the
workflow works, that the interface is usable, that the decisions are
comprehensible, or that the safety model is understood. `make check` passing
says none of those things.

The delivery, finalization, recovery, approval and retention paths were all
implemented and independently reviewed against **fakes** — local bare remotes,
fake hosting servers, fixture actors. Fake-hosting success is not a milestone.
Somebody has to do the real thing.

This stage absorbs all human-gated work deferred from 5.6, 5.7 and 5.1/5.2.

## Outcome

At the end of Stage 8:

1. **A human has completed the review walkthrough**, and the result is a durable
   record either way: the steps they performed, what they observed, and the
   findings they reported. A partially completed walkthrough recorded honestly
   is a valid outcome. A walkthrough reported as passed when it did not happen is
   not an outcome at all.
2. **Every step that requires a human is documented and reachable**: what to do,
   which command, which interface feature to use, what to expect, and how to
   report a finding.
3. **Findings are captured in bulk, in a form a remediation agent can consume
   without a human re-explaining context.** This is the single most important
   deliverable. A list of 60 individually reported observations costs more than
   it returns; one structured file with a stable schema, per-step verdicts, and
   free-text detail is what lets the findings actually get fixed.
4. The live qualification gates deferred since 5.1/5.2 are either **closed or
   explicitly recorded as open with their blocker**. "Open with a reason" is an
   acceptable ending. Silence is not.
5. The milestone status is updated honestly — demonstrated, or not demonstrated
   with the exact missing steps named.

## The human review guide

8.1 produces the guide itself. The requirements for it, which this document
sets, are:

- **Complete**: it covers every human-gated step across the product — the
  deferred 5.6 operations (real push, real draft, delivery attestation,
  retention expiry, the `delivery-cancel` / `delivery-reconcile` /
  `retention-expire` commands, human narrative review), the deferred 5.7 steps
  (real harness runs, real spec/plan/task decisions, `Cannot verify` manual
  outcomes, live platform qualification), and the Stage 6 interface review.
- **Actionable per step**: what the human does, the exact command or the exact
  interface feature, what they should observe, and what a *failure* looks like —
  not only the happy path.
- **Safe by construction**: each step states its blast radius, whether it
  mutates anything outside a disposable scope, and what cleanup is required.
  Steps that would touch a real remote or a real hosting provider are marked
  individually and are requested as explicit per-operation authorization, never
  under a blanket approval.
- **Independently reportable**: the human can stop after any step and still have
  left a usable record.
- **Re-runnable**: a step that has already passed says so, with the evidence, and
  a re-run does not require replaying the ones before it. Replaying an entire
  demonstration to recover one failed delivery step is forbidden.

## Bulk finding report

The report format is designed in 8.1 and must support:

- a **stable, versioned schema**, so a remediation agent can parse it without
  guessing and so future runs stay comparable;
- **per-step verdict** — passed, failed, blocked, not-run — with blocked and
  not-run distinguished from failed, because a blocker is not a product defect;
- **one entry per finding** with the step it came from, severity, what was
  expected, what happened, and reproduction detail;
- **machine-readable output** (JSON alongside any human-readable rendering);
- an explicit **unverified** marker, so anything the human did not actually check
  can never be read as a pass.

## Boundaries

- **In scope:** the walkthrough, the guide, the report format, live
  qualification, and the honest milestone status.
- **Out of scope:** fixing findings. Stage 8 finds and records. Remediation is
  separate work routed to the owning slice, on the ordinary branch and review
  workflow.
- **Out of scope:** enabling production dispatch as a way of making a step
  easier. If a gate is closed, the step is blocked, not bypassed.
- **Out of scope:** any real merge, publication or delivery beyond the single
  explicitly authorized draft. Never merge.
- **Standing:** the real Codex route remains blocked until the user decides it.
  See the prerequisite below.

## Prerequisites and known blockers

- **The live Codex route is blocked for a specific technical reason**, recorded
  in [pending decisions](../../process/pending-decisions.md). The contained relay
  accepts scoped OpenAI-compatible API credentials, while the authorized
  ChatGPT subscription sign-in cannot be converted into such a credential
  without extracting login credentials or exposing the Codex account home —
  neither of which is authorized. This is a **user decision**, and no step in
  this stage may work around it. The Codex-dependent walkthrough steps are
  blocked until it is decided; the remaining steps are not.
- **The delivery destination is undecided.** A real push or draft needs a named
  destination project and remote (never the Vigil development repository), the
  provider's `api_base`, a `credential_ref` naming an environment variable whose
  value only the trusted application holds, the draft content source, and
  explicit scoped authorization. Requested per operation, in 8.1.
- **Spending**: included subscription usage is authorized within its limit; a
  metered route, paid fallback or purchase is not, and is never substituted
  silently.

## Sub-stages

| Sub-stage | Purpose | Autonomous |
| --- | --- | --- |
| [8.1](#81-human-review-plan) | Design the guide and report format, request per-operation authorization, plan the sub-stages | partly — the planning is autonomous, the authorization requests need the human |
| 8.2 … | Execute the walkthrough with the human; record results | no — this is the human stage proper |

### 8.1 Human review plan

8.1 produces the guide, the report schema, and the sub-stage decomposition, and
puts the exact authorization requests in front of the human.

Its deliverables:

1. **The walkthrough guide**, complete and step-by-step, to the requirements
   above.
2. **The bulk finding report schema** and a worked example, plus the tool that
   captures and validates a report.
3. **The authorization requests**, one per operation needing them, each naming
   the exact destination, scope and blast radius — never a blanket request.
4. **The sub-stage decomposition** 8.2 … 8.y, sequenced so the independent steps
   run before the blocked ones, with the plan for each.
5. **The blocker register**: every known blocker, its owner, and what decision
   unblocks it.

## Handoff

An interrupted Stage 8 must record exactly which steps were performed, their
verdicts, what is in flight, what authorization is held and for what, and what
the human said about stopping. **Never replay the demonstration** to recover one
failed step. A resumed session reads the report and continues from the recorded
state.

Fresh-context prompt: "Execute Vigil Stage 8 using this document. Start at 8.1
unless a later sub-stage plan is named. Read the existing report first and
continue from the recorded verdicts. Never mark a step passed that was not
actually performed."
