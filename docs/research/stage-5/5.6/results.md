# Stage 5.6 implementation evidence

<!-- vigil-tier: evidence -->

Status: implementation in progress on `task/5.6-delivery-finalization` from
`54784ecb8987877f0cb4079daf43defd14371b6b`. This is not independent
acceptance or production-delivery qualification.

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
reconciliation-only, with no automatic second push.

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
- On the current uncommitted candidate, `make docs-check`, `git diff --check`
  and escalated `make check` pass. The full check ran pinned `go vet ./...`
  and `go test ./...`; it did not exercise a real hosted repository or model.

## Remaining Stage 5.6 work

The exact candidate still needs adversarial review, native macOS validation,
and a decision on the default in-project Git-ignored archive view required by
R65/R66. No real publication is authorized.
