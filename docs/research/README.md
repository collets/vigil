# Research and validation evidence

This directory stores dated implementation evidence and independent reviews by
the stage that produced them. Historical contents remain records of what was
observed at the time; later work adds dated sections rather than rewriting past
results to look current.

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
```

Stage 5 review probes use the name `review-probes/` and the `.go.txt` suffix so
they cannot accidentally compile as part of the normal suite. Future Stage 5.x
evidence belongs under `stage-5/5.x/` using `results.md` and, when applicable,
`astra-review.md`, `blockers.md` and `review-probes/`.

The authoritative description of each evidence file remains the main
[documentation index](../README.md#evidence-and-reviews).
