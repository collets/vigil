# Stage 5.3 — recovery, checkpoints and execution controls

Implementation completed 2026-09-21 from accepted Stage 5.2 baseline `bf09f4d`. The implementation commits are `b0e1c16`, `0bc3b92`, `2df4ecd`, `7c227a8` and `5617679`. This is an implementation handoff, not independent acceptance or live interrupted-session qualification. Production dispatch remains disabled.

## Delivered behavior

- `project pause` durably disables later dispatch. `project continue` is revisioned and refuses unresolved writers, checkpoint recovery and exhausted cumulative ledgers.
- Stop and foreground shutdown persist project/plan/request/run/control/effect intent before interrupt or termination. Interrupt and termination have independent bounds; repeated commands return the same receipt and never repeat an uncertain external effect. Writer and inference observations remain separate. A failed containment observation stays uncertain and retains coordinator quarantine.
- Checkpoint sets are private, content-addressed and all-repository atomic. A set contains the exact HEAD/ref and HEAD bytes, a Git bundle, original index bytes, parsed index entries and staged blobs, tracked worktree bytes/deletions, scoped nonignored untracked bytes, modes, symlink targets, exclusions and physical repository identity.
- Save, clear and restore are separate commands. Clear accepts only paths from a validated execution result, verifies the complete captured/baseline sets before any mutation, refuses branch/HEAD drift and ambiguous mixed index changes, then applies per-path compare-and-swap operations. Restore is bound to target, baseline and a separately verified destination checkpoint, journals every path, and uses three-way classification. Divergence is preserved as conflict.
- Recovery classifies exact native continuation from fresh-context reconstruction. Exact resume requires matching native home, durable session, immutable profile, workspace identity, transport generation, qualified history class, no automatic queued work and independently contained writers. It creates a new generation of the same run, submits no replacement prompt and retains the remaining wall and task allowance. Missing, corrupt, unsupported or mismatched history cannot become exact resume.
- Fresh context is an explicitly new run/attempt. Its artifact includes the task criteria, current repository fingerprints, prior run/writer/submission uncertainty, checks/findings counts, history observation and cumulative budget. The selected checkpoint ID is immutable recovery authority and is re-verified before preparation.
- Repair, infrastructure and continuation attempts have distinct durable kinds and limits. Infrastructure retry requires proof of no delivered prompt and no normalized tool events. All attempts reuse the task ledger; crash gaps remain charged/unknown. Exhaustion creates immutable evidence plus a pending supervisor request and blocks progression without accepting the task.
- Native completion still stops at `checking`; it never creates acceptance.

## Preservation matrix

| State | Captured representation | Verification / recovery behavior |
| --- | --- | --- |
| HEAD and branch | Commit OID, symbolic ref, raw HEAD bytes and readable Git bundle | Bundle heads must contain the recorded commit; clear refuses ref/OID changes and restore never rewrites unrelated refs |
| Index | Exact raw index blob plus path/mode/OID/stage entries and staged object bytes | Raw bytes and every blob digest are verified; clear restores the whole index only when every changed entry is agent-owned |
| Mixed staged/unstaged file | Staged blob in index record; current worktree bytes in path record | Permanent test proves both distinct byte sequences survive capture and exact-baseline round-trip |
| Tracked modification/deletion | Bounded bytes/mode or explicit `deleted` entry | Compare-and-swap checks captured destination before clear/restore |
| Scoped untracked file/binary | Exact bytes, size, mode and `untracked` source | Only nonignored paths admitted by the immutable task scope are captured |
| Executable bit | Path and index modes | Round-trip test proves executable mode restoration |
| Symlink | Link target bytes and symlink kind | Restore recreates the link itself; parent traversal is descriptor-relative and no-follow |
| Ignored/excluded data | Not captured unless covered by the immutable preservation contract | Caller substitution of exclusions or untracked scope is rejected before intent persistence |
| Nested participating repositories | One repository manifest per immutable participant; parent excludes enrolled child boundary | Whole set verifies before mutation; corruption in child prevents clearing parent |
| Unsupported/special entry | Capture or apply fails closed | No cleanup is authorized and incomplete evidence remains inspectable |

Checkpoint publication writes blobs and manifests to private state, fsyncs them, verifies digests and Git readability, then commits database verification. Failure before that point cannot create a verified set. Historical migration digests remain unchanged; migrations 005–007 only extend the installed schema.

## Clear and restore failure matrix

| Scenario | Result |
| --- | --- |
| Repository B capture/write failure | No set is published; repository A is not cleared; incomplete failure evidence is retained |
| Blob or manifest corruption | Full-set verification fails before clear/restore intent or filesystem mutation |
| Concurrent manual edit after capture | Preflight compare-and-swap rejects the operation; manual bytes remain unchanged |
| Parent replaced by symlink after preflight | Descriptor-relative apply rejects at the intended apply boundary; outside target is unchanged |
| Unowned index entry mixed with an owned change | Broad raw-index clear is refused as ambiguous |
| Interruption after a clear apply | Per-path states identify applied versus prepared work; replay reconciles only the recorded desired/current states |
| Exact-baseline restore | Index/worktree bytes, deletion, mode and symlink state round-trip |
| Divergent destination | Three-way restore records conflicts and preserves destination bytes plus both verified recovery copies |
| Interruption after repository A restore | Target and destination checkpoint sets remain verified; retry resumes per-path progress and restores repository B |
| Stale target/baseline/destination authority | Repository-set identity, destination snapshot and project revision checks reject the operation |

All destructive cases ran only in `t.TempDir` repositories carrying fixture state. The real Vigil checkout was never enrolled, cleared or restored.

## Execution and recovery matrix

| Condition | Allowed result |
| --- | --- |
| Pause repeated with the same command | Same durable receipt; no new dispatch |
| Stop with pending request | Request is cancelled before interrupt; a late answer cannot authorize work |
| Stop repeated after observed containment | Same receipt; interrupt and stop counts remain one |
| Stop/foreground shutdown with failed termination | Control/effect stay uncertain; no replay; continue is refused |
| Controller/owner death | Existing coordinator quarantine and live-owner/fence tests reject dispatch; PID/container absence does not release authority |
| Persistence/event write failure | Already-journaled execution is boundedly contained; no result or `checking` transition is written |
| Native history missing/corrupt/unsupported/mismatched | Exact resume disabled; fresh reconstruction is offered only with contained writer and re-verified checkpoint |
| Exact history and identity match | New generation of the same run resumes the durable native session; `native_create` and submit are never called |
| Replacement prompt supplied to exact resume | Rejected before dispatch |
| Fresh reconstruction | New continuation run linked to the explicit choice and checkpoint; warning states that this is not native resume |
| Delivered prompt proposed as infrastructure retry | Rejected before attempt creation |
| Repair versus infrastructure allowance | Counted independently; neither counter resets the other or the task ledger |
| Cumulative budget exhausted | No new attempt; immutable exhaustion evidence, blocked task and pending bounded-supervisor request |
| Proven human wait | Active charge is excluded; wall deadline and lease remain enforced; returning active restores cumulative remainder |
| Native terminal result | Task moves only to `checking`, never `accepted` |

## Supported CLI commands

The implemented command surface is deliberately explicit:

```text
vigil project pause PROJECT_ID --command-id ID --expected-revision N
vigil project continue PROJECT_ID --command-id ID --expected-revision N
vigil project execution-stop PROJECT_ID RUN_ID --command-id ID --synthetic-fixture --repository REPO --path PATH
vigil project checkpoint-save PROJECT_ID RUN_ID --command-id ID --expected-revision N
vigil project checkpoint-clear PROJECT_ID CAPTURED BASELINE --command-id ID --expected-revision N
vigil project checkpoint-restore PROJECT_ID TARGET BASELINE DESTINATION --command-id ID --expected-revision N
vigil project execution-recovery-choose PROJECT_ID RUN_ID --command-id ID --expected-revision N --mode exact_resume|fresh_context|remain_blocked --synthetic-fixture
vigil project execution-resume-prepare PROJECT_ID CHOICE_ID --command-id ID --expected-revision N
vigil project execution-followup-prepare PROJECT_ID SOURCE_RUN_ID --command-id ID --expected-revision N --kind repair|infrastructure|fresh_context [--choice-id ID]
```

`execution-start` remains the only fixture dispatcher. A new/fresh attempt requires `--prompt`; an exact resume rejects `--prompt`. Production history inspection and production runtime drivers are not exposed by this CLI.

Operational recovery order is: pause; stop and independently prove writer safety; save and verify the complete set; inspect the native history and explicitly choose exact resume, fresh context or continued blocking. Clear is optional and only valid for result-proven agent paths. Restore requires a newly saved destination checkpoint and explicit target/baseline/destination IDs. On any conflict or partial failure, keep both recovery copies and inspect the per-path journal before retrying.

## Validation evidence

Linux x86_64, Go 1.27.1:

- focused `go test ./internal/store ./internal/checkpoint ./internal/supervisor ./internal/cli`; pass;
- `make check` (`go vet ./...` and `go test ./...`); pass outside the restricted sandbox, which does not permit the boundary test's loopback listener;
- `make check-race`; pass, including `internal/supervisor` in 32.510 seconds on the final implementation;
- `make build`, `make build-boundary`; pass;
- `make cross-build`; pass for Linux/macOS amd64/arm64.

Native macOS 26.6.2 arm64, Go 1.27.1:

- The first exact bundle at `7c227a8` reached checkpoint save and exposed `/tmp` versus `/private/tmp` alias handling in replay validation. It failed rather than mutating a repository.
- `5617679` normalizes a supplied root through current physical/Git identity, checks it against the immutable participant and canonicalizes command authority. It also permanently rejects preservation-scope substitution.
- Exact bundle SHA-256 `c890d1e3de294fabf77330e91617d011a3107f5f6d6d97cebde4525b29e48074` passed native `make check` (`internal/checkpoint` 14.574 seconds, `internal/supervisor` 43.780 seconds), `make check-race` (`internal/supervisor` 64.427 seconds), and `make build`.
- Two repetitions of the high-risk checkpoint round-trip, nested partial restore, scope substitution, stop idempotency, exact no-prompt resume and independent-writer-safety tests passed.
- Both isolated Mac checkouts and bundles were removed after making downloaded read-only module caches owner-writable. The normal Mac checkout was not accessed or changed.

No Docker flag, model, provider, credential, Codex subscription turn, llama.cpp turn, push, publish, purchase or production dispatch was used.

## Commits for independent review

| Commit | Checkpoint | Main evidence |
| --- | --- | --- |
| `b0e1c16` | Durable controls | Pause/continue, stop/shutdown intent, request retirement, uncertain-stop replay refusal |
| `0bc3b92` | Checkpoint codec | Mixed staged/unstaged/untracked/binary/deletion/mode/symlink capture, multi-repository atomic publish, corruption refusal |
| `2df4ecd` | Clear/restore | Per-path journals, scoped CAS clear, destination-bound three-way restore and partial-failure recovery |
| `7c227a8` | Recovery/budgets | Exact versus fresh identity, retry kinds, shared ledgers, exhaustion evidence and recovery CLI |
| `5617679` | Native portability/scope | Physical-root alias normalization and immutable preservation-scope enforcement |

Independent review should diff `bf09f4d..5617679`, inspect migrations 005–007 without changing earlier migration bytes, and concentrate on `internal/checkpoint`, `internal/supervisor/control.go`, `recovery.go`, `followup.go`, `budget.go`, CLI authority construction, and their failure tests. Stage 5.2 invariants remain part of the review surface.

## Remaining gates and limitations

- Stage 5.3 has not received independent acceptance. This document must not be read as self-approval.
- Exact resume was exercised with the synthetic history/resume driver. Interrupted/corrupt/long native history through real Codex and Hermes transports remains unqualified.
- Production dispatch remains disabled. Safe contained Codex subscription routing, trusted provider-idle proof, a shared Mac/WSL capacity authority and the complete live recovery matrix are still open.
- The CLI provides synthetic history inspection only. A production history inspector must independently report its supported recovery class and exact durable identities.
- Checkpoint preservation supports regular files and symlinks in the declared scope. Unsupported special types fail closed. Ignored files and secrets are excluded unless a future explicit preservation policy covers them.
- Ambiguous real recovery still requires the user. No real checkout clear/restore authority was inferred.
- Stage 5.4 must consume the pending supervisor/exhaustion records, run actual checks and fresh review, invalidate stale evidence and own acceptance. UI rendering remains Stage 5.5.
