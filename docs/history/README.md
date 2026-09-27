# Superseded records

<!-- vigil-tier: history -->

These documents record alternatives that were explored, plans for stages that are
complete, and mechanism questions that later documents resolved. They are worth
reading whenever you want to know *why* a decision was made, or to recover an
option that was rejected. They are not the authority for current behavior: for
that, read the `core/` documents each one points to.

| Document | Superseded by | Retained for |
| --- | --- | --- |
| [`adapter-spike.md`](adapter-spike.md) | [`../core/harness-capabilities.md`](../core/harness-capabilities.md), [`../plans/stage-5/`](../plans/stage-5/) | The Stage 1 adapter contract and the reproducible profile preparation it defined |
| [`stage-2-plan.md`](stage-2-plan.md) | [`../core/core-spec.md`](../core/core-spec.md) | The minimum transport and controlled-execution sequence, and its explicit non-goals |
| [`stage-3-plan.md`](stage-3-plan.md) | [`../core/core-spec.md`](../core/core-spec.md) | The lifecycle and policy validation plan, and the limits it refused to claim |
| [`stage-3.5-macos.md`](stage-3.5-macos.md) | [`../research/stage-3.5/results.md`](../research/stage-3.5/results.md) | The macOS environment and runtime qualification plan as written before the host was used |
| [`stage-4-plan.md`](stage-4-plan.md) | [`../core/core-spec.md`](../core/core-spec.md) | How the specification stage was sequenced and bounded |
| [`autonomy-presets.md`](autonomy-presets.md) | [`../core/core-spec.md`](../core/core-spec.md) | The two-axis autonomy proposal and the alternatives behind the accepted defaults |
| [`supervision-options.md`](supervision-options.md) | [`../core/core-spec.md`](../core/core-spec.md) | Supervisor and workflow-composition options considered before the contracts were fixed |
| [`discovery-notes.md`](discovery-notes.md) | [`../core/requirements.md`](../core/requirements.md) | The chronological discovery history, including interpretations and superseded readings |

Two conventions apply to this folder:

- A document's own status line stays as written. It is a dated record.
- Paths, commit identifiers and line numbers quoted inside a *review record*
  under [`../research/`](../research/) are left exactly as the reviewer saw
  them, even after the file they name has moved. Use this folder and
  [`../START-HERE.md`](../START-HERE.md) to find the current path.
