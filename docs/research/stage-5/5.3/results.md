# Stage 5.3 — recovery, checkpoints and execution controls

Independent follow-up, 2026-09-22: **Stage 5.3 offline implementation is accepted at `a182152`; R1–R10 are closed.** Independent retained probes, Linux full/race/build/boundary-build and four cross-builds pass. See the [acceptance and validation limits](astra-review.md#independent-follow-up-acceptance-of-a182152). Live interrupted-session qualification remains open and production dispatch remains disabled.

Implementation started from accepted Stage 5.2 baseline `bf09f4d`. The initial implementation ends at `5617679`; review remediation currently ends at `a182152`. The implementation handoff below is supplemented by the independent offline acceptance above; neither establishes live interrupted-session qualification. Production dispatch remains disabled.

## Independent-review remediation

| Finding | Implemented correction | Permanent evidence |
| --- | --- | --- |
| R1 stop/dispatch race | Stop atomically fences every new effect and outcome commit, cancels a same-controller dispatcher, and waits boundedly until all non-containment effects retire; an older run cannot overwrite stop | `TestStopFencesRunBlockedInCreate`, stop replay/uncertainty tests |
| R2 missing destructive authority | Clear/restore require a live owner, full-set claims, fencing generations and reserved endpoint ticket for the checkpoint run, plus a current terminal `contained_stopped` writer observation on initial call and replay | `TestClearRequiresCoreOwnershipEvidenceAndLiveAuthority`, coordinator reservation tests |
| R3 caller-declared ownership | Caller-owned paths are rejected. Clear derives paths from the persisted validated result, matches its exact repository fingerprint, and binds the baseline to immutable pre-attempt bytes/index/HEAD | ownership and post-result concurrent-edit tests |
| R4 pathname escape | Worktree, Git index and loose-object traversal/apply use held directory descriptors with `O_NOFOLLOW`; Git index replacement holds `index.lock`; final expected state is rechecked at the descriptor boundary | deterministic parent replacement and outside-sentinel test |
| R5 missing staged objects | Every staged object is reconstructed from private bytes, object-format hashed, descriptor-relatively installed and verified before the raw index is exposed | prune/restore test plus SHA-1, SHA-256 and conflict-stage object tests |
| R6 unsafe infrastructure retry | All repair/infrastructure/fresh follow-ups require and transactionally recheck a terminal, independently contained source writer | unresolved-writer infrastructure regression |
| R7 unusable exact resume | Exact eligibility binds the current verified checkpoint; migration 008 stores an immutable generation recovery baseline; preparation and dispatch recheck workspace and native-history identity | partial-edit exact resume/reload and changed-history dispatch tests |
| R8 clear cannot replay after mutation | New clear authority still requires the exact validated-result fingerprint; replay first resolves the immutable command receipt, then reconciles the current repository only against the exact captured/desired states in its persisted action journal. Unrelated paths, substituted actions/digests and drift remain rejected | post-apply, post-progress, completed-receipt, unrelated-edit and multi-repository replay tests |
| R9 absent parent treated as conflict | Descriptor traversal distinguishes a missing intermediate beneath the verified root from symlink, non-directory or root replacement; comparison models it as an absent leaf and authorized apply recreates parents through held descriptors | tracked-directory clear and nested-parent restore tests plus retained symlink/root replacement negatives |
| R10 repeated exact snapshot collision | Forward migration 009 removes only global digest uniqueness; generation identity remains the immutable primary authority and equal workspace content is valid across generations | two unchanged-workspace exact resumes and populated v8-to-v9 migration preservation/immutability tests |

## Delivered behavior

- `project pause` durably disables later dispatch. `project continue` is revisioned and refuses unresolved writers, checkpoint recovery and exhausted cumulative ledgers.
- Stop and foreground shutdown persist project/plan/request/run/control/effect intent before interrupt or termination. Interrupt and termination have independent bounds; repeated commands return the same receipt and never repeat an uncertain external effect. Writer and inference observations remain separate. A failed containment observation stays uncertain and retains coordinator quarantine.
- Checkpoint sets are private, content-addressed and all-repository atomic. A set contains the exact HEAD/ref and HEAD bytes, a Git bundle, original index bytes, parsed index entries and staged blobs, tracked worktree bytes/deletions, scoped nonignored untracked bytes, modes, symlink targets, exclusions and physical repository identity.
- Save, clear and restore are separate commands. Clear accepts only paths from a validated execution result, verifies the complete captured/baseline sets before any mutation, refuses branch/HEAD drift and ambiguous mixed index changes, then applies per-path compare-and-swap operations. A repeated command reconciles only its immutable receipt and exact persisted action journal, accepting captured or already-desired states while rejecting unrelated edits. Restore is bound to target, baseline and a separately verified destination checkpoint, journals every path, and uses three-way classification. Divergence is preserved as conflict.
- Recovery classifies exact native continuation from fresh-context reconstruction. Exact resume requires matching native home, durable session, immutable profile, workspace identity, transport generation, qualified history class, no automatic queued work and independently contained writers. It creates a new generation of the same run, submits no replacement prompt and retains the remaining wall and task allowance. Missing, corrupt, unsupported or mismatched history cannot become exact resume.
- Fresh context is an explicitly new run/attempt. Its artifact includes the task criteria, current repository fingerprints, prior run/writer/submission uncertainty, checks/findings counts, history observation and cumulative budget. The selected checkpoint ID is immutable recovery authority and is re-verified before preparation.
- Repair, infrastructure and continuation attempts have distinct durable kinds and limits. Infrastructure retry requires proof of no delivered prompt and no normalized tool events. All attempts reuse the task ledger; crash gaps remain charged/unknown. Exhaustion creates immutable evidence plus a pending supervisor request and blocks progression without accepting the task.
- Native completion still stops at `checking`; it never creates acceptance.

## Preservation matrix

| State | Captured representation | Verification / recovery behavior |
| --- | --- | --- |
| HEAD and branch | Commit OID, symbolic ref, raw HEAD bytes and readable Git bundle | Bundle heads must contain the recorded commit; clear refuses ref/OID changes and restore never rewrites unrelated refs |
| Index | Exact raw index blob plus path/mode/OID/stage entries and staged object bytes | Raw bytes and every blob digest are verified; SHA-1/SHA-256 and conflict-stage objects are installed and verified before an index is exposed; clear restores the whole index only when every changed path is result-proven |
| Mixed staged/unstaged file | Staged blob in index record; current worktree bytes in path record | Permanent test proves both distinct byte sequences survive capture and exact-baseline round-trip |
| Tracked modification/deletion | Bounded bytes/mode or explicit `deleted` entry | Compare-and-swap checks captured destination before clear/restore |
| Scoped untracked file/binary | Exact bytes, size, mode and `untracked` source | Only nonignored paths admitted by the immutable task scope are captured |
| Executable bit | Path and index modes | Round-trip test proves executable mode restoration |
| Symlink | Link target bytes and symlink kind | Restore recreates the link itself; parent traversal is descriptor-relative and no-follow |
| Ignored/excluded data | Not captured unless covered by the immutable preservation contract | Caller substitution of exclusions or untracked scope is rejected before intent persistence |
| Nested participating repositories | One repository manifest per immutable participant; parent excludes enrolled child boundary | Whole set verifies before mutation; corruption in child prevents clearing parent |
| Unsupported/special entry | Capture or apply fails closed | No cleanup is authorized and incomplete evidence remains inspectable |

Checkpoint publication writes blobs and manifests to private state, fsyncs them, verifies digests and Git readability, then commits database verification. Failure before that point cannot create a verified set. Historical migration digests remain unchanged; migrations 005–009 only extend the installed schema. Migration 008 adds immutable generation-scoped recovery workspace authority; forward migration 009 preserves populated rows while permitting the same content digest for distinct generations.

## Clear and restore failure matrix

| Scenario | Result |
| --- | --- |
| Repository B capture/write failure | No set is published; repository A is not cleared; incomplete failure evidence is retained |
| Blob or manifest corruption | Full-set verification fails before clear/restore intent or filesystem mutation |
| Concurrent manual edit after capture | Preflight compare-and-swap rejects the operation; manual bytes remain unchanged |
| Manual edit after validated result but before capture | Exact result fingerprint mismatch rejects clear; path names alone confer no ownership |
| Missing coordinator claim or unresolved source writer | Clear/restore reject before creating or replaying a destructive operation |
| Parent replaced by symlink after preflight | Descriptor-relative apply rejects at the intended apply boundary; outside target is unchanged |
| Unowned index entry mixed with an owned change | Broad raw-index clear is refused as ambiguous |
| Interruption after a clear apply | Per-path states identify applied versus prepared work; replay reconciles only the recorded desired/current states |
| Interruption after clear progress persistence or completed receipt replay | The identical command returns to the same journal/receipt; no new authority or operation is created |
| Tracked file beneath a deleted directory | Missing intermediate directories compare as an absent leaf and are recreated descriptor-relatively only during authorized apply |
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
| Stop while runtime create is blocked | Persisted stop cancels/contains and retires the in-flight effect; no start/native-create/submit can begin afterward |
| Stop/foreground shutdown with failed termination | Control/effect stay uncertain; no replay; continue is refused |
| Controller/owner death | Existing coordinator quarantine and live-owner/fence tests reject dispatch; PID/container absence does not release authority |
| Persistence/event write failure | Already-journaled execution is boundedly contained; no result or `checking` transition is written |
| Native history missing/corrupt/unsupported/mismatched | Exact resume disabled; fresh reconstruction is offered only with contained writer and re-verified checkpoint |
| Exact history and identity match | New generation of the same run resumes the durable native session; `native_create` and submit are never called |
| A later exact resume sees identical workspace content | A distinct immutable generation snapshot may reuse the content digest; cumulative limits and native identity checks remain unchanged |
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

- remediation-focused store/checkpoint/supervisor tests, including R1–R10, post-apply/post-progress replay and populated migration upgrade; pass;
- `make check` (`go vet ./...` and `go test ./...`); pass outside the restricted sandbox, which does not permit the boundary test's loopback listener;
- `make check-race`; pass, now including `internal/checkpoint` in the standard target;
- `make build`, `make build-boundary`; pass;
- `make cross-build`; pass for Linux/macOS amd64/arm64.

Native macOS 26.6.2 arm64, Go 1.27.1:

- The first exact bundle at `7c227a8` reached checkpoint save and exposed `/tmp` versus `/private/tmp` alias handling in replay validation. It failed rather than mutating a repository.
- `5617679` normalizes a supplied root through current physical/Git identity, checks it against the immutable participant and canonicalizes command authority. It also permanently rejects preservation-scope substitution.
- Exact bundle SHA-256 `c890d1e3de294fabf77330e91617d011a3107f5f6d6d97cebde4525b29e48074` passed native `make check` (`internal/checkpoint` 14.574 seconds, `internal/supervisor` 43.780 seconds), `make check-race` (`internal/supervisor` 64.427 seconds), and `make build`.
- Two repetitions of the high-risk checkpoint round-trip, nested partial restore, scope substitution, stop idempotency, exact no-prompt resume and independent-writer-safety tests passed.
- Remediation commit `a0afb05` was transferred as exact bundle SHA-256 `553df7b6db5cf7e4707eaa0d052a08099dbb964f2f4f3c4456fc958f622f0dc2`. Native `make check`, `make check-race` (including checkpoint), and `make build` passed. Two focused repetitions passed for blocked-create stop, partial-work exact resume, parent replacement, pruned staged-object restore, and SHA-1/SHA-256/conflict-stage object recovery.
- Follow-up commit `a182152` was transferred as exact bundle SHA-256 `799d46aa24427b4f557cbe5de0265dfa15afad233668599652b891983d8fe50b`. Native `make check`, `make check-race` (including checkpoint), and `make build` passed. Two focused race repetitions passed for clear post-apply/post-progress/receipt replay, unrelated-edit rejection, multi-repository reconciliation, absent-parent clear/restore, populated v8-to-v9 upgrade and repeated unchanged-workspace exact resume.
- All isolated Mac checkouts and bundles were removed. The normal Mac checkout was not accessed or changed.

No Docker flag, model, provider, credential, Codex subscription turn, llama.cpp turn, push, publish, purchase or production dispatch was used.

## Independently reviewed commits

| Commit | Checkpoint | Main evidence |
| --- | --- | --- |
| `b0e1c16` | Durable controls | Pause/continue, stop/shutdown intent, request retirement, uncertain-stop replay refusal |
| `0bc3b92` | Checkpoint codec | Mixed staged/unstaged/untracked/binary/deletion/mode/symlink capture, multi-repository atomic publish, corruption refusal |
| `2df4ecd` | Clear/restore | Per-path journals, scoped CAS clear, destination-bound three-way restore and partial-failure recovery |
| `7c227a8` | Recovery/budgets | Exact versus fresh identity, retry kinds, shared ledgers, exhaustion evidence and recovery CLI |
| `5617679` | Native portability/scope | Physical-root alias normalization and immutable preservation-scope enforcement |
| `9d095fd` | Supervisor review remediation | Stop/effect retirement fence, contained follow-ups, checkpoint-bound exact generation baseline and dispatch-time native-history revalidation |
| `a0afb05` | Checkpoint review remediation | Live recovery reservation, result-derived ownership, descriptor-relative apply/index lock/object recovery and checkpoint race coverage |
| `a182152` | Follow-up review remediation | Journal-bound clear replay, absent-parent recovery and forward migration for repeated equal-content exact resumes |

Independent acceptance covered baseline `bf09f4d`, the initial implementation through `5617679`, both findings sections in `astra-review.md`, and remediation commits `9d095fd`, `a0afb05` and `a182152`. It reran the retained R1–R10 probes and permanent store/checkpoint/supervisor regressions, including migration 009 populated upgrade, while preserving migrations 001–008 and the Stage 5.2 invariants. Future changes to these contracts must retain that review surface.

## Remaining gates and limitations

- Stage 5.3 has independent offline acceptance at `a182152`; live qualification remains separate and incomplete.
- Exact resume was exercised with the synthetic history/resume driver. Interrupted/corrupt/long native history through real Codex and Hermes transports remains unqualified.
- Production dispatch remains disabled. Safe contained Codex subscription routing, trusted provider-idle proof, a shared Mac/WSL capacity authority and the complete live recovery matrix are still open.
- The CLI provides synthetic history inspection only. A production history inspector must independently report its supported recovery class and exact durable identities.
- Checkpoint preservation supports regular files and symlinks in the declared scope. Unsupported special types fail closed. Ignored files and secrets are excluded unless a future explicit preservation policy covers them.
- Ambiguous real recovery still requires the user. No real checkout clear/restore authority was inferred.
- Stage 5.4 must consume the pending supervisor/exhaustion records, run actual checks and fresh review, invalidate stale evidence and own acceptance. UI rendering remains Stage 5.5.
