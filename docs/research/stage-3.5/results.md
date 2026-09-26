# Stage 3.5 — macOS runtime qualification

Completed 2026-09-20 as a bounded investigation. Adapter/runtime evidence is established for the versions below; strict production editing remains **unsupported**. This stage did not implement the scheduler or a production containment boundary. [Sanitized machine-readable evidence](results.json) includes all passing and failing cases, process ancestry and native policy probes.

## Environment and access

The user enabled account-scoped Remote Login and authorized continued setup over SSH. A Git bundle transferred committed Vigil source at `1fb5de6` directly from WSL into `~/development/vigil`; Linux caches, native homes and credential files were not transferred. Fresh fixtures/native homes were prepared on the Mac. Node 24 is selected by the repository's `.nvmrc`; the user's zsh auto-switch hook was configured separately.

| Component | Observed version/configuration |
| --- | --- |
| OS/CPU/filesystem | macOS 26.6.2 (25G83), arm64, APFS; case and Unicode aliases observed |
| Go / embedded SQLite | 1.27.1 / 3.53.4 |
| Node / npm | 24.21.0 / 11.19.0, managed by nvm |
| Codex | 0.155.1, existing Mac ChatGPT sign-in, `gpt-6-astra` |
| Hermes | 0.21.3, source `6a627e6eb38e28ac421d5ad8df3f676e49d0c287` |
| Hermes Python | uv-managed CPython 3.11.16; locked core dependencies in `~/.hermes/hermes-agent/venv` |
| Inference | `qwen3.8-27b-local`, 131072 context tokens, one advertised slot |

**Inference runs in native Windows, not Linux or macOS.** The tested route was Mac loopback port 8080 → encrypted reverse SSH tunnel → WSL loopback port 8080 → Windows llama.cpp. WSL's shared localhost reached the server. The existing key was supplied via encrypted SSH stdin into the test process environment; it was not written to the Mac's shell profile or a new credential file. This route proves connectivity, not physical Mac-local inference, OS-wide egress control, or cross-host capacity arbitration. Experiments were operator-sequenced; other clients were not automatically coordinated.

## Runtime and lifecycle results

Sixteen model-backed turns were dispatched across fourteen fresh experiments, with no retries or automatic replay. The provider-error test was one separate submission rejected by a loopback relay before model inference. Eleven model-backed cases passed; three retained explicit failures.

| Case | Codex | Hermes |
| --- | --- | --- |
| Editing + independent file/diff/structured-result verification | Passed | Passed |
| Completed-history resume, exact durable identity and recall | Passed | Passed |
| Unknown native identity rejected, no unsolicited resumed turn during bounded wait | Passed | Passed |
| Normal output interruption | Passed | Passed |
| Foreground heartbeat interruption | Passed; two lines then stable | Passed; two lines then stable |
| Abrupt transport loss | Unknown outcome retained; **writer survived** | Unknown outcome retained; **writer survived** |
| Narrow native approval allow/deny | Passed; stale/duplicate replies rejected | Deterministic installed-module probe passed |
| Model-triggered native clarification | **Not demonstrated**; completed without a native request | Passed; answered blue, stale/duplicate replies rejected |
| Controlled provider rejection | Not run | Failed outcome, no acceptance/replay; one intercepted inference request |

The Codex clarification failure is specific to this profile/stimulus; it does not prove the protocol lacks input support. Do not enable a profile capability solely from schemas or model prose. Interrupted/corrupt native-history resume, long-history pagination and broad auxiliary/egress coverage remain unqualified.

Both abrupt-loss heartbeat scripts continued for their full 150 lines / approximately 30-second self-limit. Process evidence observed Codex's Python writer reparented to PID 1 in a process group separate from the native harness. Hermes left a reparented shell and its Python writer in another group. Both were gone after the self-limit. **This differs from the Linux Codex fixture**, which stopped writing after transport closure. Never generalize Linux cleanup evidence to macOS; never equate transport closure or native idle with writer quiescence.

`make check`, `make check-race`, `make build`, the SQLite hello command and dashboard open/quit all passed natively. No Go adapter changes were required by these bounded checks. The new diagnostic scripts passed syntax checks, and the filesystem/lock prototype ran on both hosts. An older Linux Git version exposed an unsupported `--path-format` flag in the first prototype; resolving `--git-common-dir` output against each repository fixed it, with both hosts rechecked.

## Policy and filesystem findings

Native Git policy probes reproduced the Linux result using disposable repositories and local bare destinations only:

- Codex `command/exec` with explicit `workspaceWrite` blocked direct commit (128) and Python ref write (1), but allowed local bare push (0). HEAD stayed unchanged and the alternate local ref was absent; the bare destination ref was created.
- Hermes manual approval mode allowed direct commit and local push without an approval request. The Python-wrapped commit requested approval and was denied. This is not a complete application commit/push gate.
- Installed Hermes once/deny, clarification answer/cancel, expiry and native interrupt passed deterministic request-module tests; late replies were rejected. These are module-path evidence, not every model/tool path.

The disposable filesystem prototype established symlink identity, ancestor overlap, distinction from similarly prefixed siblings, and shared common-Git identity between linked worktrees. Case variants and composed/decomposed Unicode names referred to the same inode while `Path.resolve()` returned unequal strings. Comparing existing ancestor filesystem identities detected those overlaps. Production ownership must also address replacement races, missing paths and destructive-transition revalidation.

Two-process advisory locking rejected a concurrent owner through both direct and symlink paths. Killing the owner released the OS lock, but a separate persisted unknown-writer claim survived. This demonstrates the primitives required for crash quarantine; the production coordinator and its transactional dispatch guard are still Stage 5 work.

An isolated `sandbox-exec` primitive probe allowed a fixture worktree write and denied a protected `.git` write, a read of a synthetic secret marker, and a connection to a fixture loopback listener. The probe used an **allow-default profile with three deny rules**, not a complete worker policy. It establishes useful primitives, not host-wide secret isolation, protected-parent replacement resistance, provider-only egress or descendant cleanup. VM/container or equivalent complete boundary selection and qualification remain Stage 5 D; no VM/container runtime was installed.

## Reproduction and cleanup

The Mac checkout is `~/development/vigil`. Hermes's interpreter is `~/.hermes/hermes-agent/venv/bin/python`; load nvm explicitly in noninteractive SSH sessions and include that Python environment on PATH. Re-establish the temporary reverse SSH tunnel from WSL only after checking the Windows service, and resolve the key into the experiment environment without putting it in command arguments or manifests.

```sh
make check check-race build
python scripts/spike/platform_probe.py
python scripts/spike/qualification_suite.py --live \
  codex:edit hermes:edit codex:resume hermes:resume \
  codex:interrupt hermes:interrupt codex:child hermes:child \
  codex:loss hermes:loss codex:approval-allow codex:approval-deny \
  hermes:clarify codex:clarify
```

The live suite caps requested model turns at sixteen, prepares a new fixture per case, records process names/ancestry without arguments or environments, and retains failure evidence. A nonzero result is expected for the documented unsupported guarantees. Run the native policy probes and `route_probe.py --live --error` separately with fresh preparations as in Stage 3. Process sampling can miss short-lived descendants and is not a containment proof.

Cleanup verified zero recorded native PIDs remaining, no `runner.lock`, and no copied Codex auth files. The self-limiting writers exited. The inference tunnel and temporary keep-awake processes were stopped; source/dependencies and ignored private evidence remain on the Mac. Existing Remote Login and the user's authentication remain available for future work. Nothing was pushed to GitHub.

Next: implement Stage 5 A–C (persisted commands, policy/readiness and coordination/recovery). Stage 5 D gates strict editing/delivery on both platforms; the two macOS abrupt-loss failures and incomplete native permission coverage are mandatory acceptance tests.
