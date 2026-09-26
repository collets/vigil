# Stage 5.1 independent validation

Latest follow-up, 2026-09-21: **R1–R4 resolved; no remaining findings from this review.** The R4 fix and its retained-artifact regression passed independent validation. Production/live qualification gates remain open. Earlier findings and reproduction source below are historical review evidence.

Date: 2026-09-21. Reviewed Sol's uncommitted implementation over `930ed37`, preserving the user's earlier planning/configuration edits. Verdict: **changes requested**. Production dispatch remains disabled; the findings below must be addressed before treating the new contracts as qualified.

## Findings

### R1 — P1: writable ancestors bypass nested instruction protection

Location: `internal/boundary/checkout.go`, `MountArgsForRole` (lines 263–271 at review time), and newly admitted nested protected paths.

The planner bind-mounts `/work` writable and over-mounts individual protected descendants read-only. This prevents writing the mounted file, but does not prevent renaming an ordinary ancestor directory inside `/work`. A worker can move `docs` to `old-docs`, recreate `docs`, and write a new `docs/AGENTS.md`. The original file remains protected at its moved path, while the approved instruction pathname now contains worker-authored content. This is a worker action, not the excluded hostile-host race.

Reproduced with the existing immutable Hermes amd64 image, non-root UID, read-only container root, no network, no capabilities and no-new-privileges. The following commands returned exit 0:

```sh
mv /work/docs /work/old-docs
mkdir /work/docs
echo replacement > /work/docs/AGENTS.md
```

The host fixture's original instruction pathname contained `replacement`, while `old-docs/AGENTS.md` retained the original bytes. A second reproduction uses `PlanCheckout` and its actual `MountArgs`, not hand-written alternative mounts. Existing `TestDockerCheckoutGitProtection` checks direct nested writes and top-level mount replacement, but misses rename/replacement of nested ancestors.

Required correction: protect every relevant ancestor against replacement, or fail closed on those layouts until a qualified mount arrangement exists. Add worker-side rename/recreate tests for nested instructions and enrolled nested Git ancestors, on both supported engines. Do not mark nested-layout requirements complete merely by reverting to rejection; keep the remaining support work explicit.

### R2 — P1: prompt non-delivery is insufficient proof that inference never started

Location: `internal/boundary/lifecycle.go`, `LifecycleEvidence.Validate` / `EndpointRelease` (lines 48–78).

`ProviderState=not_started` needs no provider evidence; it is accepted when `SubmissionState=proven_not_delivered`. With a stopped worker and empty boundary, `EndpointRelease` returns true. The existing test explicitly endorses that path. However, the core specification requires reserving inference capacity before native create/resume because those operations may initiate auxiliary inference before the application prompt is submitted. Stopping the worker does not prove that such a provider request has ended.

Reproduction: start from the lifecycle test fixture, set `SubmissionState` to `proven_not_delivered`, `ProviderState` to `not_started`, and clear `ProviderEvidenceDigest`; release is allowed. This demonstrates the contract's missing proof requirement; no real inference was started for this review.

Required correction: independently bind a trusted never-started observation to the whole run generation and all possible inference routes, or prove the run never crossed an inference-capable startup boundary. Otherwise require independent provider-idle evidence and retain quarantine. Test create/resume auxiliary traffic with no delivered task prompt, cancellation/transport loss, and a genuinely pre-start case that can safely release.

### R3 — P2: qualification claims do not verify referenced evidence artifacts

Location: `internal/boundary/qualification.go`, `RecordTrustedQualification` and `QueryEligibility`.

Evidence digests are checked for SHA-256 syntax, and the enclosing record is hashed and verified. No code resolves the evidence digests to retained artifacts, verifies their contents, or detects their disappearance/corruption. A new empty project database accepts `testQualificationRecord()` and reports production claims `supported` even though none of its evidence artifacts were ever published. The “corrupt evidence” test modifies the enclosing record, not a referenced evidence artifact.

The recorder comment delegates manifest verification to a trusted caller, but that verifier is not implemented; only tests call the recorder. This is not a demonstrated user-profile privilege escalation. It is an incomplete evidence-admission/freshness contract, contrary to the completed checkpoint and results claim that missing/corrupt evidence fails closed.

Required correction: implement a trusted verifier with resolvable, retained evidence references and fail-closed integrity checks before issuing/consuming supported eligibility. If verification is deliberately delegated, enforce that boundary in the API and leave the verifier/integrity work explicitly incomplete until implemented. Test absent artifacts, corruption after admission, incomplete claim-specific evidence and retained valid evidence after reopen.

## Validation scope

- Linux `make check`, `make check-race`, `make build`: passed. The initial sandboxed check could not bind the synthetic HTTP listener; rerunning with local socket access passed.
- `make cross-build`: passed for Linux/macOS amd64/arm64.
- Existing Docker suite: passed with `VIGIL_TEST_DOCKER=1`, the recorded Hermes amd64 image and Codex image `sha256:d95cd60ca0c9f49e0c71e8d21830ddfcd0a9cd22911dac4177bbc25278fff2bc`; boundary package completed in 58.585 seconds. Live inference was not enabled.
- The existing passing suite does not invalidate these findings: focused negative review cases exercise missing conditions.
- Native Mac tests were not independently rerun in this review. Sol's recorded Mac results remain prior evidence; the new ancestor-protection fix will require a Mac regression run.
- No paid/model calls, real-checkout failure experiments, hosting operations or host configuration changes were performed. Reproduction containers and repositories are disposable.

## Remaining planned gates, separate from defects

The safe ChatGPT subscription broker, live contained Codex/Luna turn, provider-specific idle qualification and Stage 5.2 persisted launch/submission/result crash matrix remain unfinished as Sol documented. These are not newly discovered regressions and should not be presented as completed Stage 5.1 qualification.

Recommended next action: repair R1–R3 and add lasting negative regression tests, then rerun focused checks and affected platform probes. Stage 5.2 offline journal development can proceed independently, but must not consume these contracts as production-qualified until corrected. No production implementation was changed by this review.

## Reproducible negative regression cases

All three tests below failed as expected on the reviewed implementation: instruction pathname replaced, supported qualification with no published evidence artifacts, and endpoint release without independent never-started proof. They were temporarily compiled inside `internal/boundary` to reuse its fixture helpers, then removed after review. The source is retained here so the fixing agent can install them as lasting regression tests with the corrected contract. No failing test file is left in the working package.

Save the following as `internal/boundary/astra_review_tmp_test.go` and run only `TestAstraReview` with the explicit Docker/Hermes image settings from this report:

```sh
GOENV=off GOPATH="$PWD/.cache/gopath" GOCACHE="$PWD/.cache/go-build" \
VIGIL_TEST_DOCKER=1 \
VIGIL_HERMES_IMAGE=sha256:863305d7a6fe32172f86ce512e3656560777ec09d92b472cf3eec1b30ae9f199 \
.tools/go/bin/go test ./internal/boundary -run "^TestAstraReview" -count=1
```

```go
package boundary

import (
 "context"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "testing"
)

func TestAstraReviewNestedParentReplacement(t *testing.T) {
 image := hermesImage(t)
 root := checkoutFixture(t)
 if err := os.Mkdir(filepath.Join(root, "docs"), 0700); err != nil { t.Fatal(err) }
 if err := os.WriteFile(filepath.Join(root, "docs", "AGENTS.md"), []byte("original"), 0600); err != nil { t.Fatal(err) }
 plan, err := PlanCheckout(context.Background(), root)
 if err != nil { t.Fatal(err) }
 mounts, err := plan.MountArgs(context.Background())
 if err != nil { t.Fatal(err) }
 args := []string{"run", "--rm", "--pull=never", "--label=vigil.probe=astra-review", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--entrypoint=/bin/sh"}
 args = append(args, mounts...)
 args = append(args, image, "-c", "set -eu; mv /work/docs /work/old-docs; mkdir /work/docs; echo replacement > /work/docs/AGENTS.md")
 if b, err := exec.Command("docker", args...).CombinedOutput(); err != nil { t.Fatalf("probe: %v %s", err, b) }
 b, err := os.ReadFile(filepath.Join(root,"docs","AGENTS.md"))
 if err != nil { t.Fatal(err) }
 if string(b) != "original" { t.Fatalf("protected instruction pathname replaced: %q", b) }
}

func TestAstraReviewMissingEvidenceMustDeny(t *testing.T) {
 db := qualificationDB(t)
 record := testQualificationRecord()
 if _, err := RecordTrustedQualification(context.Background(), db, record); err != nil { return }
 result, err := QueryEligibility(context.Background(), db, productionRequest(record.Inputs))
 if err != nil { t.Fatal(err) }
 if result.Status == "supported" { t.Fatal("supported despite no evidence artifacts ever being published") }
}

func TestAstraReviewNonDeliveryNeedsIndependentNoInferenceProof(t *testing.T) {
 evidence := lifecycleEvidence()
 evidence.SubmissionState = "proven_not_delivered"
 evidence.ProviderState = "not_started"
 evidence.ProviderEvidenceDigest = ""
 decision, err := EndpointRelease(evidence)
 if err == nil && decision.Release { t.Fatal("endpoint released from prompt non-delivery without proof that create/resume auxiliary inference never started") }
}
```

## Follow-up review — R1–R3 remediation

Date: 2026-09-21. Independently inspected the corrected code and permanent regressions. R1 is resolved by sealed self-bind guards for every ordinary protected ancestor; R2 now requires exact expected inference routes and independent pre-start observations; R3 resolves retained artifact IDs and verifies durable bytes both at admission and consumption. The provider/journal observation producers remain future integration work, as documented; these helpers do not make arbitrary observations trustworthy by themselves.

Validation performed on the current uncommitted source:

- Linux `make check check-race build cross-build`: all passed.
- WSL/Docker `TestDockerCheckoutGitProtection`, uncached: passed in 0.69 seconds, including shallow/deep/nested-repository parent replacement and allowed work writes.
- Mac/OrbStack: copied the current source to an agent-owned disposable `/tmp/vigil-astra-rereview.*` directory and ran the same uncached Docker regression; passed (test 0.71 seconds, package 1.234 seconds). Removed the temporary source directory afterward. The normal Mac checkout was untouched.
- The first direct Go command inherited an incompatible `GOROOT`; clearing it, as the Makefile already does, resolved that environment error.
- No inference or paid calls, hosting operations or host configuration changes were performed. Production dispatch remains disabled.

### R4 — P2: synthetic evidence can satisfy the live provider-idle requirement

Location: `internal/boundary/qualification.go`, lines 244–245 and 258 at review time.

`validateRecord` inserts both the unqualified evidence name and `class + ":" + name` into the same `names` map. A synthetic observation whose name is `live:provider_idle` therefore sets exactly the key used to require live provider-idle evidence. Retaining a separate ordinary live bounded-task observation satisfies the generic live-class requirement, so the record is accepted and `QueryEligibility` reports `supported` without any live provider-idle observation.

The regression below reproduced this using the existing fixtures and correctly retained durable artifacts. It fails with: `synthetic evidence named live:provider_idle satisfied the live provider-idle claim`. This is a trusted-record validation error, not a demonstrated model-facing grant escalation. It nevertheless defeats an explicit qualification gate and must be corrected before supported records are consumed for production.

Required correction: use distinct lookup structures for plain names and class/name pairs, or a structured key such as `{class, name}` for class-specific requirements. Validate the actual `Class` and `Name` fields. Add a permanent negative test for this collision while preserving valid live-provider evidence and missing-artifact behavior. Audit other prefixed claim lookups for the same ambiguity.

The temporary reproduction was removed from the package after running; its source is retained here:

```go
package boundary

import (
 "context"
 "testing"
)

func TestAstraReviewEvidenceClassCannotBeSpoofedByName(t *testing.T) {
 db := qualificationDB(t)
 record := testQualificationRecord()
 for n := range record.Evidence {
  if record.Evidence[n].Name == "provider_idle" {
   record.Evidence[n].Class = "synthetic"
   record.Evidence[n].Name = "live:provider_idle"
  }
 }
 retainQualificationEvidence(t, db, &record)
 if _, err := RecordTrustedQualification(context.Background(), db, record); err != nil { return }
 result, err := QueryEligibility(context.Background(), db, productionRequest(record.Inputs))
 if err != nil { t.Fatal(err) }
 if result.Status == "supported" { t.Fatal("synthetic evidence named live:provider_idle satisfied the live provider-idle claim") }
}
```

Run the test after placing it temporarily in `internal/boundary/astra_claim_review_tmp_test.go`:

```sh
env -u GOROOT GOENV=off GOPATH="$PWD/.cache/gopath" GOCACHE="$PWD/.cache/go-build" \
.tools/go/bin/go test ./internal/boundary -run '^TestAstraReviewEvidenceClassCannotBeSpoofedByName$' -count=1 -v
```

Next action: fix R4 with the focused regression and routine checks. The mount fix needs no repeat platform probe unless changed again. Stage 5.2 offline work can continue; live Codex, independent provider-idle qualification and the persisted production crash matrix remain separate pending gates.

## R4 closure — independent validation

Date: 2026-09-21. Verdict: **R4 resolved**. `validateRecord` now uses separate plain-name and structured `{class, name}` maps. The provider-idle claim checks the exact live/provider_idle pair; a synthetic observation with a prefixed name cannot occupy that key. Recovery lookup uses only exact plain names, so class-qualified aliases cannot satisfy it either. Duplicate-name detection remains independent of pass/fail value.

The permanent `TestQualificationEvidenceClassCannotBeSpoofedByName` retains valid artifacts and asserts rejection for the specific missing-live-proof reason. This prevents an unrelated absent-artifact error from masking the regression. Uncached qualification tests passed, including valid records, exact-input invalidation, missing/corrupt retained evidence, reopen, claim completeness and the R4 reproduction. Linux `make check check-race build` also passed. No implementation change was made during this review; only review/handoff documentation was updated.

No further Docker/Mac probes were necessary for this platform-independent lookup change; the prior independent WSL and OrbStack ancestor-guard regressions remain the relevant mount evidence. No model calls, paid usage or publication occurred.

All four review findings are closed. This accepts their corrections, not complete live Stage 5.1 production qualification: safe contained Codex subscription routing, provider-idle evidence and the joint Stage 5.2 persisted crash matrix remain pending. Proceed with Stage 5.2 offline implementation while preserving those gates. At review time the implementation/planning changes were still uncommitted over `930ed37`; do not describe them as a committed baseline until a commit is actually made.
