# Stage 5.6 implementation evidence

<!-- vigil-tier: evidence -->

Status: **accepted at `b0a085b`, 2026-09-29, for the autonomous scope only.**
Eight independent antagonist reviews have run. `9777de0`, `ebf7f0f`, `0368227`
and `7b758f8` were each rejected and remediated on this branch; `d5906ed`,
`3ec4065` and `fc2f909` received conditional verdicts, all remediated; and the
eighth returned an **accepted** verdict on the implementation with no P0, P1 or
P2, with 7/7 fix-reverts caught by revert-sensitive tests. A ninth review of the
post-acceptance delta confirmed that verdict and found one defect in the
remediation itself (a heading inserted mid-list recategorised two completed
remediations as unfixed residuals), which is fixed. Exact-commit native macOS
gates pass at `f2d740c`, having found and fixed one platform defect the Linux
suite could not.

**This acceptance does not qualify the product for delivery.** It means the
paths are implemented, independently reviewed and validated. Nothing here was
exercised by a human against a real remote or a real hosting provider; the
delivery, attestation, retention-expiry and narrative-review operations are
Stage 8's, and no evidence for them exists. Nothing here is
production-delivery qualification.

The seventh review confirmed the reconciliation invariants hold under adversarial
probing and found no P0 or P1. Its two blocking items were that the resumed-claim
fix shipped with a test that skipped on every run, and that the fix silently
escalated `uncertain` to `executing`; both are resolved by recording the
pre-claim state durably rather than inferring it. It also found documentation
obligations under `AGENTS.md` and three unpinned defence-in-depth paths, all
recorded below.

## Native macOS validation

Native macOS validation of the candidate on an agent-owned detached worktree
(never the user's checkout) found a genuine platform defect that the Linux
suite could not: `archive-export` refused every legitimate destination. The
parent walk rejected **any** symbolic link in the ancestor chain, but macOS
resolves its platform roots through `/private` — `/var` and `/tmp` are both
symbolic links — so a real operator directory beneath them was refused. The
check now requires the directory the operator **named** to be a real directory,
and no longer rejects ancestors above it, which is the operating system's own
layout rather than an operator redirection; `os.OpenRoot` confines every
subsequent operation to the resolved parent. A permanent Linux-runnable test
reproduces the macOS shape (`<root>/var/private -> <real>`, with a real operator
directory beneath it), asserts the export succeeds there, and still asserts
that an alias in the *named* parent is refused. Reverting the fix fails that
test. This is exactly the class of defect the native gate exists to find.

Exact-commit native macOS gates pass at `b42afe1`, `9ef3a98` and the final
candidate `f2d740c`, run on macOS 26.6.2 arm64 with the pinned Go 1.27.1
(Homebrew) and Apple Git 2.54.0, in an agent-owned detached worktree under
`/tmp`:

| Gate | Result |
| --- | --- |
| `make check` (vet + full suite) | pass — all packages `ok` |
| `make check-race` (native darwin/arm64) | pass — every package `ok`, `internal/quality` 198.4s |
| `make build` | pass |
| `make build-boundary` | pass |
| `make docs-check` | pass |
| `make cross-build` | pass — linux and darwin, amd64 and arm64 |
| `git diff --check` | clean |
| native CLI smoke | `hello` OK; all eleven Stage 5.6 commands present |

The worktree was then removed and the user's normal checkout verified
unchanged (`main` at `19ebc42`, clean). Permission changes during cleanup were
confined to the identified agent worktree, and the transferred bundles and path
markers were deleted. The Mac has no GitHub SSH credential, so each exact
commit was transferred as a git bundle and fetched into the existing
repository; object identity and the SHA were preserved and verified before
validation. The validated candidate is
`f2d740ccabee70fae83a77b010514d7b5f0a6168`, which includes the reconciliation
closure assertions and every documentation correction made after acceptance was
granted.

Not exercised natively: no live model turn, no credentialed or real hosted
remote, and no production dispatch. Those gates remain closed.

## Second independent review remediation

The review of `ebf7f0f` rejected it on four points. This remediation closes
them:

- **P1.1 (reconcile could record a false net-zero effect):** the first attempt
  at this fix claimed the operation before observing it, which the second review
  showed is insufficient — the claim blocks a *new* executor but not one already
  past its state check, and the reviewer reproduced a landed push recorded as
  `reconciled`/`failed` using a blocking server-side `pre-receive` hook. The
  root cause is not a race that more locking can fix: no durable state
  distinguishes an executor mid-effect from one that crashed mid-effect, so a
  point-in-time observation can never *prove* non-occurrence. The design was
  therefore changed rather than re-plumbed:

  - `delivery-reconcile` now closes automatically **only on positive proof**
    that the approved effect happened. There is no automatic "no effect"
    closure, for any kind.
  - A **push** whose destination still holds the approved predecessor — the
    common lost-response case — is resolved by re-attempting the identical
    non-force `OID:ref` push. That is idempotent by construction: a no-op if the
    first push landed, the delivery if it did not, and a Git non-fast-forward
    rejection if another writer moved the destination. The destination is
    re-observed immediately before the effect, and the outcome is re-observed
    after. No other kind is retried; reconciliation never moves a local ref or
    POSTs a draft.
  - The residual case (a ref or remote moved by a third party, or a hosting
    listing without the exact draft) stays open with its approved operands, and
    closes only through the new `delivery-close-unobserved`, which records an
    explicit **human attestation** — a decision of record, never a system proof.

  The claim is retained (it still prevents a new effect from starting during
  the decision, and is released when blocked) but it is no longer load-bearing
  for correctness. Regression tests cover: an untouched commit where reconcile
  must refuse to close, a blocked push not overwriting a third-party ref, the
  idempotent retry delivering the approved head, the attestation being required
  and labelled, and a refusal to repeat a closed operation.
- **P2.1 (`finalization-run` could never succeed):** the live adapter reported
  only harness/model/provider, so the full-identity check added for B2 could
  never pass — a dead CLI command hidden by a bespoke test fixture. The adapter
  is now constructed from the *persisted profile's own* route identity
  (`Engine.ProfileRoute`) and additionally requires the prepared manifest's
  secret environment name to equal the profile's credential reference.
  `planning-run` and `tool-qualify` take the same route from the profile
  (`tool-qualify` gained required `--profile-id`/`--profile-revision`).
- **P2.2 (`.vigil` exclusion invalidated existing acceptances):** making
  `.vigil` always-excluded changed the exclusion set of every stored
  `Baseline.Exclusions`, so an acceptance recorded by the previous candidate no
  longer satisfied its own exact-fingerprint comparison — a stricter check that
  silently bricked accepted work. Comparisons now normalize the *stored*
  exclusion set through `workspace.NormalizeExclusions` before comparing
  (delivery, archive collection and archive publication), so an
  application-owned forced exclusion alone can never invalidate an otherwise
  unchanged acceptance, while any real content change is still refused. A
  regression test covers a legacy manifest and the tampered-content case.
- **P2.3 (view failure masked advanced durable state):** the in-repository
  `.vigil` view was materialized *after* the durable transition, so a blocked
  view (for example an operator-owned `.vigil` file) reported command failure
  for state that had already advanced, and repeating the command could never
  repair the view. A blocked view is now a presentation-only `view_warning` on
  the returned record; the command succeeds and repeating it re-verifies the
  receipt and retries the view. This also makes the "reported success/failure
  matches durable state" property hold for `archive-build`, `archive-narrative`
  and the draft-success path.

Also addressed from the same review:

- **P3.1 (B1 had no test at all):** the blocker that started this review cycle
  — a real plan being completed by a live model turn — had no regression test,
  so it could silently return. A new test seeds a genuinely real plan (no
  disposable-fixture marker, `core` acceptance actor) and asserts that both the
  fixture narrative path and the live run path refuse it, that no provider turn
  is ever dispatched, and that the plan stays `finalization_pending` with its
  task still `ready`. A second engine seeds the inverse case (fixture
  acceptance actor, marker-less repository) to prove the two gates —
  acceptance provenance and repository marker — are independent.
- **P3.2:** the reconciliation closure transaction asserts
  `RowsAffected == 1` on the claim, the delivery journal, the attested closure
  and both `state='observed'` transitions, so a lost update is never treated as
  a successful closure. `releaseClaim` deliberately does not assert it: a
  release whose claim is already gone is a legitimate no-op, and forcing the
  count would turn a concurrent close into a spurious error. (The claim itself
  is retained but is no longer the correctness mechanism, so the earlier
  review's observation that it was untested is resolved differently: the
  net-zero closure it protected no longer exists.) The two `state='observed'`
  assertions and the added `AND state='reconciled'` guard on the main
  `state='observed'` transition are themselves **unpinned** — reverting them
  leaves the suite green, because the closure runs as one single-connection
  `BEGIN IMMEDIATE` transaction and the pre-existing re-read at
  `delivery_cancel.go:254` already proves the claim is held inside it, so a lost
  claim cannot interleave. They are recorded in
  [Accepted residuals](#accepted-residuals) below rather than claimed as
  covered.
- **P1.1 follow-ups (from the third review of the redesign):** the redesign
  itself was confirmed correct — the blocking-`pre-receive` repro now records
  `observed`/`succeeded` — but three defects on the same path were found and
  fixed:
  - A committed human attestation and an interrupted reconciliation claim both
    read as `operations.state='reconciled'`, so `delivery-reconcile` could
    re-enter an already-closed operation and re-attempt a push on it, leaving
    the attestation false. Resumability is now gated on the delivery journal not
    being terminal, so a committed closure is never reopened.
  - When the destination advanced to the approved head *between* the
    reconciler's first observation and its pre-push re-observation, the error
    told the operator to attest — producing exactly the false "no effect" record
    the change exists to prevent. The approved head is now recognised as proof
    of the effect, and the message directs the operator to re-run reconcile
    rather than attest.
  - The attestation was recorded only in one command receipt, so
    `delivery-status` showed a `reconciled`/`failed` operation with no
    provenance. `DeliveryStatus` now reads the attestation back, and says so
    explicitly when a closure has none.

  Regression tests cover each: a reconcile attempt on an attested closure is
  refused and the remote stays untouched; a head landing mid-reconciliation is
  observed rather than attested; and the attestation provenance survives in
  `delivery-status`. The mid-reconciliation test uses a `git` shim that lands
  the approved head on the reconciler's *second* `ls-remote`, so it opens the
  real window rather than a pre-reconciled state; reverting the fix makes it
  fail.

- **P1 (the fourth review's live defect):** the terminal-journal gate could
  not distinguish an attested closure from an interrupted claim when the
  operation had **no delivery journal row** — which `CloseUnobservedDelivery`
  explicitly tolerates, and which is exactly the state the normal two-transaction
  crash window in `ExecutePush` (`operation.start` committed, the journal
  `INSERT` not) produces. Because the push branch is the only reconciliation
  branch that reaches an external effect, reconcile could push to an operation
  a human had attested never took effect, falsifying the attestation and never
  recording the landing. The closure is now a **first-class durable fact**:
  forward-only project migration 018 adds `operations.closure_kind`, written
  atomically with the attested state change. Reconciliation refuses to resume
  any operation carrying that marker, and the push branch additionally requires
  a delivery journal before it will attempt anything. `delivery-status` keys
  its attestation read-back on the marker rather than on `delivery_state`, so a
  journal-less closure is still labelled. A regression test drives the real
  crash window and asserts the remote stays untouched across two reconciles.

- **Fifth review (conditional verdict on `d5906ed`):** it confirmed the P1
  above is closed robustly, including under a 12-iteration race between
  `delivery-reconcile` and `delivery-close-unobserved`, and that the closure
  marker is tamper-proof (`CHECK` rejects any other value, no backfill, no
  writer can clear it). It raised two P2s and several smaller findings, all
  remediated here:
  - The push-journal requirement was load-bearing but entirely unpinned —
    removing it left the full suite green, and on an **unattested** crash
    window reconcile would then push while the closure failed on the missing
    journal, leaving a landed-but-unrecorded effect in a state no command could
    close. A regression test now drives exactly that window and asserts both
    that the remote is untouched and that the documented remedy works.
  - A blocked reconcile downgraded an `executing` operation to `uncertain`.
    Since `executing` is the resumable effect state and `uncertain` is
    observation-only, a read-only `delivery-reconcile` permanently removed the
    push the operator had been told to re-run. The claim now records the state
    it was taken from (forward-only project migration 019,
    `operations.claimed_from_state`), and a blocked or failed reconciliation
    restores exactly that value. This removes the guessing in both directions:
    the sixth review's first attempt at this still failed on a *resumed* claim,
    and the seventh review showed that attempt also escalated an `uncertain`
    operation into a re-executable one. A permanent test drives each direction
    from a real claim, and a revert of the restoration fails one of them.
  - A failed closure transaction could strand a held claim; it is now released
    on that path too, and the release never clears a closure marker.
  - `DeliveryStatus` swallowed a `closure_kind` read error, which is the exact
    provenance-hiding failure the change exists to prevent; the error is
    returned instead.
  - The claim release now carries the same `closure_kind IS NULL` guard used
    everywhere else, matching the marker as append-only.
  - Documentation corrected: the attested closure is described accurately for a
    journal-less delivery, and the L3 note records that `reconciled` is written
    by both `delivery-reconcile` and `delivery-close-unobserved`.

  The same review noted that the permanent tests model a push that has already
  completed rather than one caught mid-flight by a blocking server-side
  `pre-receive` hook. That specific interleaving is coverage, not a defect —
  reconciliation closes only on positive proof — and is recorded in
  [Accepted residuals](#accepted-residuals) below rather than claimed as covered.
  It also noted a set of defence-in-depth guards that no test can currently
  reach; those are consolidated into the same section, which is the single place
  that records what is deliberately unpinned.

- **P3.4:** the dry-run retention receipt is identified by its recorded command
  kind and human actor (via the `command_applied` event), not by the JSON shape
  of its result.
- **P3.5:** `delivery-status` reports the approved comparison operands
  (`target_ref`, `approved_predecessor`, `approved_tree`/`approved_head`, or
  `project`/`head`/`base`) so a diverged observation is resolvable.

## Accepted residuals

These are deliberate, disclosed boundaries of the accepted slice, not open
defects, and the single place where anything deliberately unpinned is recorded.
The severity labels are those of the review that raised each item.

- **P3.3** (first review): archive publication holds the project write lock
  across the repository fingerprint re-verification (git subprocesses plus a
  worktree walk). The verification is required; a two-phase prepare/compare would
  avoid holding the single writer for its duration.
- **P4.1** (first review): `.vigil` view files are bounded per plan (64
  revisions, 1 MiB each) but are never pruned; the authoritative artifact copy
  remains the store.
- **P4.2** (first review): the B3 regression test drives `isolatedRemoteGit`
  directly rather than through `ExecutePush`; the production push path routes
  through it (`delivery_push.go`), but the test asserts the helper's isolation,
  not the call site.
- **P4.3** (first review): `ensureLocalGitIgnore` appends to the repository's
  shared `.git/info/exclude`; the append is idempotent and marked, but the write
  is not surfaced in command output.
- Because `.vigil` is always excluded, a user who tracks their own file or
  directory named `.vigil` has it silently omitted from every acceptance
  fingerprint. This is the recorded R65/R66 decision and its trade-off.
- An operation in `reconciled` with neither `closure_kind` nor
  `claimed_from_state` can be acted on by no command. It is unreachable through
  the code — every claim records the state it was taken from — and could only
  arise from a claim held at the instant of the 018→019 upgrade on a
  pre-release database. It fails closed and remains operator-visible.

### Deliberately unpinned guards

Each of the following is retained as defence-in-depth, is revert-checked, and
leaves the suite green when removed, so none is claimed as covered. They cannot
be reached because the reconciliation closure runs as a single
`BEGIN IMMEDIATE` transaction on a single connection, and the pre-existing
re-read at `delivery_cancel.go:254` already proves the claim is held inside it:

- the `closure_kind IS NULL` guard on the claim release, so a release can never
  reopen an attested closure;
- the release performed when the closure transaction itself fails;
- the `failed`-journal and `succeeded`-journal refusals in the resumed-claim
  path;
- the closure-kind read error in `DeliveryStatus`;
- the `RowsAffected == 1` assertions on both `state='observed'` transitions, and
  the `AND state='reconciled'` guard on the main one, added when the P3.2 claim
  was reconciled against the code;
- `releaseClaim`'s *deliberate omission* of that assertion, since a release whose
  claim is already gone is a legitimate no-op.

## First independent review remediation

The independent antagonist review of `9777de0` rejected that commit with four
blockers, three high and several medium/low findings. The remediation on this
branch closes all of them:

- **B1/B2 (finalization dispatch authority):** `finalization-run` now requires
  a current plan acceptance whose actor is `fixture_core` plus the
  disposable-fixture marker on every enrolled repository, and the recorded
  narrative actor is always `fixture`. A live model turn can no longer
  complete a real (non-fixture) plan. The provider identity binding covers
  the full profile: harness, model, provider, version, endpoint and
  credential reference.
- **B3 (repo-local Git configuration):** all push/ls-remote transport now
  runs in a fresh temporary bare repository with the approved URL passed
  directly, so repository-local `receivepack`, `insteadOf` rewrites, hooks,
  refspecs and credential helpers cannot retarget or execute. A regression
  test proves a managed-repository URL rewrite cannot retarget the push.
- **B4 (unverifiable URL archived as fact):** hosting URLs must now match the
  exact project path plus the request number (`OWNER/REPO/pull/N` or
  `OWNER/REPO/-/merge_requests/N`), not merely the same host. An adversarial
  same-host wrong-project response is rejected by a permanent test.
- **H1:** commit reconciliation refuses to move a newly checked-out plan
  branch (the user's HEAD branch) with a dedicated regression test.
- **H2 (no exit from non-terminal operations):** `delivery-status` inspects
  any delivery operation; `delivery-cancel` closes a never-started prepared
  operation; `delivery-reconcile` closes an executing/uncertain operation
  only from a fresh observation proving the approved end state or the still
  unchanged approved prior state, and the duplicate-draft race loser is
  auto-cancelled before any POST. Genuinely diverged observations (a moved
  ref or remote) stay observably open for manual resolution — that is a
  human decision, not a stuck state: the operator resolves the underlying
  ref and reconciles. Reconciled/observed/failed states no longer pin
  transcript retention.
- **H3 (grant-free irreversible retention expiry):** `retention-inspect`
  now persists the exact candidate set as a durable human dry-run receipt,
  and `retention-expire` runs through a human `retention.expire` envelope
  with an expected-revision check and consumes that receipt unchanged; a
  missing, stale, non-inspection or already-consumed receipt refuses the
  expiry. `archive-build` and `archive-export` remain core-actor creation
  paths: they create or copy records and delete nothing.
- **M1:** a hosting creation response that made the exact request non-draft
  (GitLab with `FF_DISABLE_IMPLICIT_DRAFT`) is detected, journaled as an
  uncertain external side effect with its exact external ID and URL, and
  requires manual remediation instead of being silently discarded.
- **M2/M4:** archives are bounded to 64 revisions, one MiB and ten
  finalization attempts per plan; the plan-services ledger is reconciled
  with the plan's current service limit before reserving an attempt.
- **M3:** accepted repository fingerprints are re-verified inside the
  publication transaction, not only during collection.
- **M5:** `finalization-quarantine` conservatively accounts for an overdue
  unconfirmed attempt (run `unknown`, full cap charged, execution fence
  held, task back to `ready`) so a crashed attempt no longer bricks the plan.
- **M6:** destination base branches are bounded to 255 bytes and validated
  with `git check-ref-format --branch`.
- **M7 (in-repository archive view, R65/R66):** the user chose the
  in-repository `.vigil` view over the interim private-state view. The
  `.vigil` directory is now always excluded from repository fingerprints
  (like `.git`), never checkpointed, and never enters approved commit paths;
  the view is materialized into `.vigil/plans/PLAN_ID/archive/` of every
  accepted repository and kept locally Git-ignored via the repository-local
  `.git/info/exclude`. A regression test proves the view write leaves the
  accepted fingerprint unchanged and the view is ignored.
- **M9:** a pending request whose deadline has passed no longer pins
  transcript retention.
- **L1:** export URL validation now agrees with the archive collector
  (https or credential-free loopback only).
- **L2:** the finalization task no longer claims `accepted` without
  acceptance evidence: it is `ready` while awaiting the runner, `running`
  while an attempt is active, and ends `stopped` after the verified
  narrative, with no acceptance row and no dispatch eligibility.
- **L3:** `operations.state='reconciled'` is now written by both
  `delivery-reconcile` (the transient claim, which is releasable) and
  `delivery-close-unobserved` (the durable attested closure, marked by
  `operations.closure_kind`), and `archives.state='factual_ready'` is written by
  `archive-build` (the first narrative attempt flips it to `narrative_pending`);
  no schema value remains unwritten.
- **L4:** hosting HTTP clients never use an ambient proxy, so named
  credentials cannot leak through `HTTPS_PROXY`.
- **M8 (handoff documentation):** the remaining live-delivery inputs are
  recorded in [pending decisions](../../../process/pending-decisions.md), and
  the design decisions below are recorded in the session audit.

## Local factual archive and retention slice

The first slice persists a factual manifest only after current plan/task
acceptances and enrolled repository fingerprints are verified. It includes the
persisted specification text and applied source revision references, plan/task
definitions, the exact config snapshot/revision, accepted repository and quality
scope identities, acceptance and quality artifact references, run usage
observations, delivery and operation observations, and recovery state. The manifest
is a durable content-addressed artifact; optional narrative is a separate
artifact and failure leaves the finalization task visible. Synthetic narrative
completion is restricted to disposable fixture repositories and fixture
acceptance. The export copies verified artifacts to a new private directory
with portable relative paths. Transcript expiry is project-configurable, 30
days by default, and requires completed plans with no unresolved operations or
pending requests. Typed non-run artifact references conservatively prevent
expiry; shared available blob digests prevent byte deletion. Native harness
histories are untouched.

The initial archive/retention slice added no schema migration; the `archives`
and `artifacts` tables from project migration 001 remain its storage contract.
The draft-request slice adds forward-only project migration 017, a partial
unique index preventing concurrent live/uncertain draft delivery records for
one exact plan/repository/remote/head/base identity. Production narrative
dispatch remains disabled.

The exact-tree commit/push slice previews blobs
with no filters in a temporary object database and index, binds the task
acceptance/scope, repository revision, exact paths, parent, tree and plan ref
to a separate human approval operation, then journals the commit object and
uses compare-and-swap for the non-checked-out plan ref. Fixture tests cover
unchanged user HEAD/index/worktree, changed accepted bytes rejection and
reconciliation after a ref update. During this work, a repository reader bug
was found: equal JSON values for included/dirty paths collided in a map used
for field decoding. The reader now decodes each field independently.

A separate push intent binds enrolled remote identity and URL, one local plan
ref/head, exact destination ref and observed prior remote OID. Effect start
requires its own human grant and rechecks remote configuration. The Git push
uses a single explicit `OID:ref` refspec without force, tags, hooks or
submodules; a post-push exact remote observation determines success. The fake
bare-remote tests verify only the plan ref appeared, approval replay, and
remote-URL changes blocked before grant consumption. An uncertain push is
reconciliation-only: the executor never re-pushes on its own, and only
`delivery-reconcile` may re-attempt the identical non-force `OID:ref` push when
the destination still holds the approved predecessor.

GitHub PR and GitLab MR draft adapters now share a bounded hosting interface.
They use official endpoints for real operations, credential-free loopback
servers only for marked disposable fixtures, exact project/head/base filtering,
five-page/100-item bounds, strict draft/head/operation-marker verification and
single-POST semantics. A successful request appends its URL as a new factual
archive revision. Focused fake-server tests passed for one GitHub and one
GitLab draft, including exact revision 2 and no duplicate POST on replay. A
separate adversarial fake-server matrix passed for both providers: duplicate
exact candidates, HTTP 429, five full pages, non-draft creation response and
a POST that succeeded remotely but lost its response before a subsequent
exact listing reconciled it.

The finalization runner now uses an explicitly selected eligible profile,
reserves a bounded attempt from the existing plan-services ledger, records a
durable `runs` row with role `finalization`, and accepts only closed narrative
JSON that cites the plan and every accepted task. An idle-observed failure
charges elapsed time and leaves the plan pending; a new command retries only
finalization. An unconfirmed provider is recorded `unknown` with the full
reserved cap charged as unknown and is never replayed. A local Hermes adapter
reuses the Stage 5.5 contained qualification route under an explicit
`--live-local` flag. No live model turn has been run in this Stage 5.6 work.

## Validation log

- Focused local Linux gate during implementation:
  `env -u GOROOT GOCACHE=/tmp/vigil-stage56-go-cache .tools/go/bin/go test ./internal/artifacts ./internal/core ./internal/cli ./internal/policy`
  — passed after the retention slice. The archive tests cover factual-before-
  narrative persistence, receipt replay, corrupt evidence, changed accepted
  trees, fixture citations and portable/symlink-safe export. Retention tests
  use a fake clock and cover unfinished plans, unresolved operations, the
  30-day boundary, receipt replay and shared durable blobs.
- `make docs-check` passed locally after the CLI/doc index update.
- Initial sandboxed `make check` could not bind the test suite's loopback
  `httptest` listener (`socket: operation not permitted`); the identical
  command passed when re-run with loopback permission. It ran `go vet ./...`
  and `go test ./...` using the pinned Go toolchain.
- `git diff --check` passed locally. Independent antagonist review and native
  macOS validation remain pending for the final candidate.
- After migration 017, the first full check exposed two old downgrade test
  fixtures that dropped the preceding indexes but not the new partial index.
  Their downgrade setup was updated; the focused migration tests passed.
- On the remediated candidate, `go vet ./...`, the full `go test ./...`,
  `make docs-check` and `git diff --check` pass locally on Linux with the
  pinned Go 1.27.1 toolchain, including the new regression tests for the
  review findings (URL rewrite isolation, wrong-project URL rejection,
  checked-out reconciliation refusal, crash quarantine, task lifecycle,
  delivery cancel/reconcile for commit/push/draft, retention dry-run
  receipt consumption, fingerprint-neutral `.vigil` view and always-excluded
  view paths). No real hosted repository, remote or model was exercised.
- The second review's fix set adds permanent regression tests for: a landed
  effect observed by reconciliation, a legacy stored exclusion set still
  matching an unchanged repository (and a tampered one still failing), a
  blocked `.vigil` view producing a warning rather than a command failure, and
  user `.vigil` content being left untouched. The second review's P1.1 is
  closed by the redesign above, with tests for the refusal to prove
  non-occurrence, the idempotent push retry, a blocked push not overwriting a
  third-party ref, and the explicit human attestation. The reviewer
  independently confirmed `ebf7f0f` passes vet, the full test suite and
  `docs-check`; the fixes above are verified by the same local Linux gates.
  Native macOS validation remains unverified for this candidate.

## Remaining Stage 5.6 work

Three validation cases the plan requires explicitly and that the suite did not
originally carry are now permanent tests: a revoked and an expired approval
both stop the effect before any ref movement, push or POST, and leave the
operation `prepared` with no delivery journal; a destination base that moves
after draft approval blocks the request before any POST, and the single-use
approval cannot be re-driven into a second attempt; and an explicitly
authorized draft delivery is permitted while the narrative is still pending,
appending its URL as a new factual archive revision that is itself still
awaiting its narrative, with the accepted plan left `finalization_pending`.

No real publication is authorized; every production dispatch, real-approval and
real-delivery gate remains closed. The final independent acceptance review of
the current tip, and the user decisions recorded in
[pending decisions](../../../process/pending-decisions.md), remain outstanding.
