# Research and validation evidence

<!-- vigil-tier: evidence -->

This directory stores dated implementation evidence and independent reviews by
the stage that produced them. Historical contents remain records of what was
observed at the time; later work adds dated sections rather than rewriting past
results to look current. It is the `evidence` tier: read it to support a claim
about what was observed, never as a substitute for a specification.

Every document here carries `<!-- vigil-tier: evidence -->`, and `make docs-check`
enforces that marker, so evidence cannot be filed as live guidance by accident.
Because a review record quotes the repository as the reviewer saw it, a path,
commit or line number inside one may no longer resolve after a later
restructuring; use [`../START-HERE.md`](../START-HERE.md) to find the current
path rather than editing the record.

## Layout

```text
stage-1/                 harness discovery and Stage 1 settings evidence
stage-2/                 bounded transport results
stage-3/                 Linux lifecycle and policy results
stage-3.5/               native macOS runtime results
stage-4/                 specification validation
stage-5/
  foundation-results.md  shared Stage 5 planning/control foundation
  5.1/                   execution-qualification results and review
  5.2/                   repository/execution results, review and probes
  5.3/                   recovery/control results, review and probes
  5.4/                   quality/acceptance results, review and probes
  5.5/                   workflow/planning results and user review material
  5.6/                   delivery/finalization results and review
  5.7/                   autonomous qualification results and review
stage-6/                  terminal interface inventory, parity register and feature list
  results.md             Stage 6's required deliverable; skeleton created by 6.1
  6.1-review.md          Stage 6.1 independent review record
```

Stage 5 review probes use the name `review-probes/` and the `.go.txt` suffix so
they cannot accidentally compile as part of the normal suite. Future Stage 5.x
evidence belongs under `stage-5/5.x/` using `results.md` and, when applicable,
`astra-review.md`, `blockers.md` and `review-probes/`.

Stage 6 differs deliberately. `stage-6/results.md` is not a per-sub-stage record:
it is the stage's **required deliverable**, a complete feature list of the
terminal interface that Stage 7 documents from and Stage 8 tests against, so it
is accumulated across 6.2–6.9 rather than written per slice. Per-sub-stage
evidence lands as dated sections in it, and the sub-stage's own review record
lives beside it as `6.N-review.md`. The parity register in the same document is
the authoritative classification of every product capability; the
machine-readable form in the source code is what the mechanical check reads, and
the document and the table are kept identical by test.

The authoritative description of each evidence file remains the main
[documentation index](../README.md#evidence-and-reviews).
