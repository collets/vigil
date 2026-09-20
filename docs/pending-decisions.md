# Decisions to revisit with the user

Updated 2026-09-20 while continuing Stage 5.

The user authorized continued autonomous local development and tests while away, and conditional Stage 6 planning/implementation after Stage 5, provided no additional spending or dangerous changes are made to either computer. Record questions here rather than waiting for an immediate answer.

- **Paid inference:** do not make paid API calls, buy credits, change subscriptions, or switch to a paid model/provider. Existing Codex ChatGPT-profile live qualification is deferred unless its cost implications are established within the user's constraint. Synthetic tests, local image builds, existing local llama.cpp inference and read-only authentication research may continue. No credential values belong in this document.
- **Production setup:** real project/base/branch/check/manual-prerequisite definitions and exact delivery destination remain explicit user choices. Use disposable repositories, synthetic providers and local bare remotes for implementation tests. Do not publish or push the implementation merely because a local commit is ready.
- **Unsupported checkout layouts:** the first admission planner rejects nested Git/instruction paths, linked worktrees/submodules, symlinks, hard links, special files and nested filesystems. These are visible limitations to expand through qualification, not accepted-requirement deletions or permission to rewrite the user's checkout.
- **Stage 6 scope:** the current roadmap defines Stages 1–5 only. Finish the agreed Stage 5 milestone before treating a new stage as active; propose a concrete Stage 6 from the remaining product requirements and evidence. The user's conditional authorization does not waive Stage 5's unfinished acceptance gates.

No user action currently blocks the remaining local implementation and synthetic tests. The temporary Mac-to-WSL model tunnel from the completed qualification experiment is closed. OrbStack and Docker Desktop were already started by the user; host security settings, user checkouts and services are not to be reconfigured destructively.
