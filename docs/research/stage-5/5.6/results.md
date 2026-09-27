# Stage 5.6 implementation evidence

<!-- vigil-tier: evidence -->

Status: implementation in progress on `task/5.6-delivery-finalization` from
`54784ecb8987877f0cb4079daf43defd14371b6b`. This is not independent
acceptance or production-delivery qualification.

## Local factual archive and retention slice

The first slice persists a factual manifest only after current plan/task
acceptances and enrolled repository fingerprints are verified. It includes the
persisted plan/task definitions, acceptance and quality artifact references,
run usage observations, delivery observations and recovery state. The manifest
is a durable content-addressed artifact; optional narrative is a separate
artifact and failure leaves the finalization task visible. Synthetic narrative
completion is restricted to disposable fixture repositories and fixture
acceptance. The export copies verified artifacts to a new private directory
with portable relative paths. Transcript expiry is project-configurable, 30
days by default, and requires completed plans with no unresolved operations or
pending requests. Typed non-run artifact references conservatively prevent
expiry; shared available blob digests prevent byte deletion. Native harness
histories are untouched.

No schema migration was added by this slice. The `archives` and `artifacts`
tables from project migration 001 remain the storage contract. No commit,
push, hosted request or production narrative path is enabled yet.

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

## Remaining Stage 5.6 work

Exact-tree commit preparation/effect reconciliation, authorized exact-ref push,
GitHub/GitLab fake hosting adapters and draft-request reconciliation are not
implemented. Factual archive completeness and retention race cases require
adversarial review before acceptance. No real publication is authorized.
