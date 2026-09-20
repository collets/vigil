# Stage 3.5 — macOS environment and runtime qualification

Completed **after Stage 3 Linux work and Stage 4 specification**, on 2026-09-20, through user-authorized SSH access. See [results and qualification limits](research/stage-3.5-results.md) and [sanitized evidence](research/stage-3.5-results.json). The original plan below is retained; completion means bounded runtime investigation, not strict production containment qualification.

## Setup together

1. Choose execution access. Prefer opening this repository in the user's coding-agent environment on the Mac and running the documented commands locally. SSH is optional; if chosen, use an existing scoped account/key and discuss the host/address and access setup then. Never paste credentials into chat or expose SSH publicly for this task.
2. Record macOS version, architecture, filesystem/case behavior, Go and Git versions. Obtain the committed Vigil revision. Install missing development/harness dependencies only after inspecting existing installations; preserve global profiles. Use the same pinned Codex/Hermes versions or explicitly refresh protocol evidence for differences.
3. Confirm existing Codex authentication on the Mac. Decide where local inference runs. In this setup the existing server runs on native Windows, reachable through WSL shared localhost; a reverse SSH tunnel provides Mac loopback access. The current spike accepts a loopback URL; a tunnel does not make inference physically Mac-local or provide cross-host capacity arbitration.
4. Reprepare profiles/fixtures **on the Mac**. Do not copy Linux launch manifests, native homes, auth files or absolute-path evidence as executable inputs. Confirm terminal shell, sandbox availability and process-group behavior.

## Qualification sequence

- Run formatting/check/race/build checks and draft-schema integrity tests.
- Run one controlled editing fixture through each available harness.
- Run resume/unknown-handle tests, output interruption, heartbeat-child interruption and forced transport loss. Record PID/process ancestry with macOS tools, file stability and native idle separately.
- Exercise narrow native approvals, clarification, cancel/expiry/stale answers and error/partial-result handling. Preserve native/runtime-version differences; no silent compatibility fallback.
- Repeat commit/ref-write/local-bare-push policy probes, with no hosting actions. Investigate whether a stronger worker boundary can enforce read-only Git metadata, host-secret isolation, restricted egress and descendant termination.
- Verify canonical path overlap for symlinks, common Git directories, case-insensitive/normalization aliases and paths with spaces. Prototype advisory owner locks and crash quarantine on the actual filesystem.
- Save sanitized evidence and update each capability's **platform-qualified** status. Fix platform bugs with focused regressions and rerun only affected experiments.

## Exit and possible limitations

Mac support is qualified only for the tested harness versions and execution boundary. If required containment is unavailable, retain strict dispatch as unsupported; do not weaken project gates to make the demo run. Some protocol capabilities may pass while production execution remains gated. Missing local inference or authentication blocks only the affected experiments, not unrelated schema/UI work.

Next action: follow Stage 5 A–C, carrying both macOS abrupt-loss writer failures into the Stage 5 D containment gate. Live Codex clarification was attempted but not demonstrated; interrupted/corrupt native-history resume and broader egress/descendant guarantees remain unqualified. No additional Mac setup is required for the completed checks; re-establish the temporary tunnel for future Hermes experiments.
