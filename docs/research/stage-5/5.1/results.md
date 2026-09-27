# Stage 5.1 execution qualification results

<!-- vigil-tier: evidence -->

Review remediation update, 2026-09-21: follow-up [independent validation](astra-review.md#follow-up-review--r1r3-remediation) confirms R1–R3 are resolved, including a fresh Mac/OrbStack mount regression. The additional P2 finding R4 is now remediated: class-specific requirements use structured class/name keys, and a retained synthetic artifact named `live:provider_idle` is rejected as provider-idle proof. Independent R4 review passed: the retained-artifact spoofing regression, uncached qualification tests and Linux routine/race/build checks all pass. R1–R4 are closed; this does not close live production gates. Production dispatch remains disabled.

Date: 2026-09-20–21. Starting revision: `930ed37`. Status: offline qualification, admission and lifecycle contracts implemented; production dispatch remains disabled. Live Codex containment and the joint Stage 5.2 launch/restart matrix are still required.

## Astra R1–R4 remediation

- **R1, nested-parent replacement:** every ordinary ancestor of a protected nested path is now a self-bind mount. Implementation ancestors stay writable, but Linux mountpoint semantics prevent a worker from renaming or unlinking the ancestor and recreating the approved instruction or nested-repository pathname. The planner seals the derived guard list, effective-mount validation requires it, and review-role guards are read-only. Permanent WSL Docker and OrbStack tests attempt replacement of `docs`, a deeper `docs/policy` parent and an enrolled `child` repository while also proving ordinary files beneath those guards remain writable.
- **R2, never-started inference:** `EndpointRelease` now receives the trusted expected inference-route set and `LifecycleEvidence` must contain exactly one startup-boundary observation for every route. Each known route state requires an independent evidence digest. `ProviderState=not_started` is valid only with proven prompt non-delivery and proof that every expected route remained before inference-capable startup. Missing routes, create/resume auxiliary traffic, cancellation or transport loss retain quarantine until all inference routes are independently idle. Tests cover the unsafe non-delivery reproduction, an incomplete route set, auxiliary traffic, unknown provider state, independently observed idle, and a genuinely pre-start release.
- **R3, referenced evidence:** every qualification observation now carries an artifact ID and content digest. Admission and every eligibility query resolve the ID through the private artifact repository, require `available` plus `durable` retention and the qualification-evidence kind, reopen the blob without following symlinks, and verify size and SHA-256 bytes. Absent evidence is rejected before recording; disappearance or corruption after admission changes eligibility to `unsupported`. Tests cover absent artifacts, post-admission removal/corruption, wrong digest/kind/retention, incomplete claim-specific evidence, and valid retained evidence after database reopen.
- **R4, evidence class/name collision:** plain evidence-name lookup and class-specific lookup now use separate maps, with class-specific claims keyed by the structured `{class, name}` pair. A synthetic observation named `live:provider_idle` cannot satisfy the required live `provider_idle` observation. The permanent negative regression publishes valid durable artifacts before proving record admission fails, while the valid live-provider and missing-artifact paths continue to pass.

## Result

The core can now answer `supported`, `unsupported` or `unverified` for one exact execution combination. The input identity binds harness source/version and executable, architecture-specific image, guardian/worker/relay, persisted profile revision and digest, native profile/tool/instruction configuration, mount plan, runtime/platform, provider/model/reasoning setting, credential route, relay topology, physical endpoint and capacity authority. A change to any field produces a new digest and cannot reuse old evidence.

Project schema v2 stores bounded immutable qualification records with observation times, admitted roles/layouts/recovery classes and separate metadata, synthetic and live artifact references. Records cannot be updated or deleted. Referenced durable artifact bytes are verified both before admission and during eligibility, so missing, corrupt, stale or incomplete records fail closed. `profile.put` remains a user configuration command and cannot write trusted records; a user-authored capability/evidence ID does not create eligibility. The migration runner now verifies every historical digest, upgrades a populated v1 database in one transaction, rejects gaps/newer history, and rolls back an injected v2 failure without losing v1 data.

`core.Engine.ExecutionEligibility` first verifies that the requested profile is the latest persisted revision and matches its digest, harness, version, model, provider, endpoint and role. It then consults the exact trusted record. Stage 5.2 must invoke this query again immediately before an external effect. Readiness remains false and names the pending production launch/recovery integration.

The qualification record keeps three independent claims:

- `boundary_execution`: exact image/profile/platform containment passed.
- `provider_idle`: provider-specific idle was independently observed; a closed relay connection is insufficient.
- `production_launch_recovery`: the Stage 5.2 create/start/attach/submit/result/restart matrix passed for named recovery classes.

No record in this implementation is being advertised as production-supported. The API and tests use synthetic passing records to prove selection and invalidation behavior; historical probe documents remain evidence inputs rather than silently promoted database authority.

## Images, authentication and routes

The new `scripts/boundary/build-codex.py` validates the exact `@openai/codex` 0.155.1 Linux architecture package, builds the boundary binaries for the engine architecture, copies the guardian and worker into an image without credentials, verifies the executable inside the finished Linux image, and writes a private manifest. `config/boundary/codex.toml` fixes `gpt-5.6-luna`, low reasoning effort, manual approvals, workspace-write sandboxing, disabled web search and disabled multi-agent operation. Runtime tests use a disposable private `CODEX_HOME`; they do not mount the user's full home.

| Host/runtime | Image | Verified without inference |
| --- | --- | --- |
| WSL2 Linux amd64, Docker 29.8.0 | Codex `sha256:d95cd60ca0c9f49e0c71e8d21830ddfcd0a9cd22911dac4177bbc25278fff2bc` | Codex 0.155.1, Git 2.52.0, ripgrep 15.2.0, Luna/low config, `multi_agent=false`, private home, no auth file in image |
| macOS 26.6.2 arm64, OrbStack engine 29.4.0 | Codex `sha256:1992835ce1644d77007645215913eeebdf51ad61db58d0f1f032fcd84f15316d` | Same pinned CLI/profile/private-home assertions under Linux arm64 |
| WSL2 Linux amd64, Docker 29.8.0 | Hermes `sha256:863305d7a6fe32172f86ce512e3656560777ec09d92b472cf3eec1b30ae9f199` | Existing pinned Hermes 0.21.3/source `6a627e6e…`, current containment and Git probes |
| macOS 26.6.2 arm64, OrbStack engine 29.4.0 | Hermes `sha256:51a4c7a804d86ee15d6b1fd8e13f2af4f865cfc9f8ae84e95c575fd52d6b05c3` | Existing pinned Hermes 0.21.3/source `6a627e6e…`, current containment and Git probes |

The exact executable inputs were re-read from the immutable images after validation:

| Architecture | Codex | ripgrep | guardian | worker |
| --- | --- | --- | --- | --- |
| amd64 | `0753dfe1d8b87a52436deb13eb1c549661ef4c84fee2c5aa688385eebeccb761` | `e62198eb19b136b88c330af83647b5a962cb99b6b1f066758568f12de1974849` | `be06cfe81b712c8cc732bdba5039ea8ea1fb17cabe03b015d505299cc420bbf0` | `a43fbd29fc25e9cdaad2167ac4c39d9cef773de46b63c8da204e65299ff9192b` |
| arm64 | `298d3d73d0bbc1367e58a370df5b6216fe30ce0a92e8b6b0afb0377a958dc335` | `e36d0eb52e70696bdf1781392722e05a21bb91d3b7b762ef5ec20e5df2ec687b` | `14a82475069147eb3a0f720b397fec22048476ead9199a3b146a39b9c9bc4763` | `53d2c24ff46ef5d4c933a805c35569c47b47c27c1d63dd05233b368cbe707376` |

The shared Dockerfile digest is `13917a9ddcf061c0a3909e24f9eb6785f2cc214c3a6dcf024c47b5940aff0e47`; the qualification profile digest is `390103be096615a56b10bc46e8859bd0a07f6ad9cd5f9060149ea29c65bfd820`. Full private manifests were generated beneath `.cache/boundary/`; qualification relies on the immutable image and input identities recorded here rather than the cache path.

Official OpenAI documentation distinguishes ChatGPT sign-in, which accesses Codex through the subscription, from API-key sign-in, which uses metered API billing. It also lists Luna and low reasoning effort as supported. See [authentication](https://learn.chatgpt.com/docs/auth) and the [GPT-5.6 Luna model page](https://developers.openai.com/api/docs/models/gpt-5.6-luna). The installed CLI reported `Logged in using ChatGPT`; a metadata-only app-server probe confirmed account type `chatgpt` and advertised `gpt-5.6-luna`. It submitted no turn and recorded no usage.

This establishes the authorized no-extra-charge login path but exposes a technical incompatibility: the contained worker's current relay accepts a scoped OpenAI-compatible API key, while Codex subscription access uses ChatGPT authentication and its service route. Exposing unrestricted container egress or substituting `OPENAI_API_KEY` would violate the boundary or spending constraint. Live contained Codex qualification is therefore deferred until a scoped ChatGPT-compatible credential/network broker is implemented and tested. Effective low effort must be verified on the created thread at that time; a config file alone is metadata.

The existing Windows llama.cpp credential sources were present on WSL, authenticated metadata was reachable, and `qwen3.8-27b-local` was advertised. No credential value or endpoint value was printed or persisted. No new live Hermes turn was needed: the unchanged image/harness/model combinations retain the one bounded ordinary-layout turn per platform recorded in [the foundation results](../foundation-results.md). Those turns prove native completion and boundary cleanup, but relay counters and connection closure do not prove llama.cpp inference idle.

## Qualification matrix

| Combination | Metadata | Synthetic enforcement | Live task | Provider-idle proof | Stage 5.2 recovery | Current status |
| --- | --- | --- | --- | --- | --- | --- |
| WSL / Hermes 0.21.3 / qwen3.8 local / amd64 image / host-socket relay | Pass | Pass, refreshed | One unchanged bounded edit | Unverified | Missing | `unverified` |
| Mac / Hermes 0.21.3 / qwen3.8 local / arm64 image / VM relay | Pass | Pass, refreshed | One unchanged bounded edit | Unverified; tunnel now closed | Missing; cross-host shared authority absent | `unverified` |
| WSL / Codex 0.155.1 / Luna low / amd64 image / ChatGPT subscription | Pass; account and model advertised | Image/private-home and generic boundary pass | Not run: safe ChatGPT route absent | Unverified | Missing | `unverified` |
| Mac / Codex 0.155.1 / Luna low / arm64 image / ChatGPT subscription | Image metadata pass | Image/private-home and generic boundary pass | Not run: safe ChatGPT route absent | Unverified | Missing | `unverified` |

Historical Astra turns do not qualify Luna. No paid API fallback, model upgrade, automatic replay, publication or real-checkout mutation occurred.

## Repository and mount qualification

`PlanCheckoutWithRepositories` now admits an ordinary root plus explicitly enrolled ordinary nested Git roots. Discovery alone is not enrollment. It protects top-level and nested `.git`, `.gitmodules`, `AGENTS.md`, `AGENTS.override.md`, `CLAUDE.md`, `GEMINI.md`, `.agents`, `.codex` and `.hermes` paths and revalidates every filesystem/common-Git identity and the metadata seal before producing mounts. Every ordinary ancestor of a nested protected path is self-mounted to prevent worker rename/replacement without blocking ordinary implementation writes below it. The implementation role receives the admitted checkout and ancestor guards writable with protected paths over-mounted read-only; the review role receives all of them read-only.

`ValidateMountsForRole` is the Stage 5.2 post-create/pre-start contract. It compares effective sources by filesystem identity, requires the expected read/write mode, rejects duplicate destinations, missing protected or guarded mounts, and any unplanned mount that shadows `/work`. Tests on WSL and OrbStack used real Git to prove ordinary and explicitly enrolled nested files remain writable for implementation while staging, commit/config/ref/raw metadata writes, instruction writes, nested-parent replacement, new hard links and local push into protected Git fail. A separate real container proved the reviewer can read and cannot write.

The supported layout is one ordinary root with zero or more explicitly enrolled ordinary nested roots on one filesystem, without hard links, symlinks or special files. Linked worktrees, external common-Git directories, submodule Git files, nested filesystems, unregistered nested repositories, more than 100,000 entries and hostile concurrent host mutation remain unsupported. Managed worktrees remain deferred as planned.

## Lifecycle and capacity contract

`LifecycleEvidence` records the run generation and runtime resource separately from worker process state, containment emptiness, relay transport, native submission delivery, each expected inference route's startup boundary, and provider state. The trusted caller supplies the exact qualified route set; missing, extra or duplicate observations fail validation. `EndpointRelease` permits release only after the worker is stopped, the boundary is empty, submission is not uncertain, and the provider is independently idle or every expected route is proven never to have crossed inference-capable startup. Prompt non-delivery alone cannot establish `not_started`. Known route-boundary and provider `idle`/`active` observations require independent evidence digests. This prevents a stopped container or closed HTTP connection from being treated as stopped inference.

`ValidateEndpointAuthorities` rejects two independent WSL/Mac capacity owners for the same physical endpoint. A cross-host route becomes eligible only when both hosts name the same implemented shared authority. Current production support is `single_host`; the prior Mac tunnel is test orchestration and remains ineligible for concurrent WSL/Mac dispatch.

The existing controller-crash suite was rerun on both engines. Pre-start, running-writer and observed-result fixtures retain workspace/endpoint quarantine; lease loss empties the namespace. Under the corrected contract, only durable proof that a run never reached inference-capable startup can support `not_started`; fixtures that crossed native create/resume require independent provider-idle evidence even when no task prompt was delivered.

## Validation

Commands and results:

- Baseline and final Linux: `make check`; pass. Final `make check-race`, `make build`, and `make cross-build`; pass for Linux/macOS amd64/arm64.
- Focused: `go test` for boundary qualification, lifecycle, checkout, core eligibility and store migration; pass. Tests mutate every `QualificationInputs` field and prove missing/corrupt/stale evidence remains ineligible.
- WSL Docker: `VIGIL_TEST_DOCKER=1 VIGIL_HERMES_IMAGE=sha256:863305… make check`; all packages pass, boundary package 62.393 seconds. Focused Codex image and final nested/reviewer tests also pass.
- Native Mac isolated copy: `make check check-race build`; pass with Go 1.27.1 on macOS 26.6.2 arm64.
- OrbStack: full `VIGIL_TEST_DOCKER=1 ... make check`; pass, boundary package 65.003 seconds. Focused Codex arm64 private-home and final nested/reviewer tests pass.
- Metadata only: `vigil spike --manifest ... --harness codex`; ChatGPT account and Luna advertised, `submitted=false`. Local llama `/models` metadata reachable and selected model advertised.

R1–R4 remediation validation on the final source:

- Focused artifact, qualification, lifecycle, checkout and core regressions: pass, including all four Astra reproductions.
- Linux `make check` and `make check-race build cross-build`: pass.
- WSL Docker `TestDockerCheckoutGitProtection` with Hermes image `sha256:863305d7…f199`: pass in 0.67 seconds; nested and deep parent replacement plus enrolled-repository replacement were denied while guarded work-file writes succeeded.
- Fresh native Mac disposable copy: `make check build`; pass.
- OrbStack `TestDockerCheckoutGitProtection` with Hermes arm64 image `sha256:51a4c7a8…b05c3`: pass in 0.81 seconds with the same actual mount plan and assertions.
- No inference flag was enabled. No probe container or Mac fixture remained after cleanup.

No live inference flag was set during this increment. No containers remained from the `--rm`/test-cleanup probes. The agent-owned Mac source and npm-extraction directories were temporary; no normal Mac checkout, global harness profile or host security setting was changed.

## Exact Stage 5.2 integration requirements

Production dispatch must remain disabled until Stage 5.2 supplies and tests all of the following:

1. Persist stable run/generation/container/native identities and preparation intent before Docker create or native session creation. Owned Docker resources need durable labels/names discoverable after an uncertain create response.
2. Immediately before the first external effect, resolve the latest profile and call `Engine.ExecutionEligibility` with exact runtime/image/config/mount/topology inputs. Production requires `boundary_execution`, `provider_idle`, and `production_launch_recovery` plus the exact recovery classes exercised below.
3. Hold every enrolled workspace/common-Git claim before endpoint FIFO/slot acquisition. Validate endpoint routes together so WSL and Mac cannot independently own the Windows llama capacity.
4. Revalidate the checkout and common-Git identities before create, inspect the effective mounts after create with `ValidateMountsForRole`, and refuse start on any mismatch.
5. Journal Docker create/start/attach, native create/resume, prompt submit write/ack, terminal result, artifact publication and outcome commit separately. `starting` must precede submission. `uncertain` submission is never automatically replayed.
6. On shutdown/restart, resolve the exact qualified inference-route set and populate one `LifecycleEvidence` startup-boundary observation per route from the durable journal, together with independent runtime and provider observations. Retain workspace and endpoint quarantine until `EndpointRelease` passes; a missing route, prompt non-delivery, relay close or stopped container alone is insufficient.
7. Inject crashes before and after create, start, attach, native create, submit write/ack, result persistence and outcome commit. Reopen from the durable database, discover owned containers/sessions, prove no duplicate prompt, and preserve quarantine under ambiguous observations and persistence failure.
8. After that matrix and one bounded live run through each advertised combination pass, record a new exact trusted qualification containing the named production recovery classes. Only then may Stage 5.2 replace the unconditional dispatcher denial for that combination. All others remain disabled.

## Security-sensitive follow-up for Astra review

A subsequent Astra review should concentrate on:

- qualification record canonicalization, artifact resolution/revalidation, latest-record precedence, corruption behavior and the trusted-only write boundary;
- schema v2 upgrade atomicity and the immutable-table triggers;
- completeness of every field in `QualificationInputs`, especially credential mode, topology, mount plan and capacity authority;
- nested repository/instruction admission, complete ancestor guards, filesystem-identity mount comparison and reviewer root read-only behavior;
- lifecycle evidence transitions, particularly inference-capable startup proof, `proven_not_delivered`, provider-idle provenance and endpoint release;
- Codex image supply-chain inputs, package-version/architecture checks, credential-free layers and the future scoped ChatGPT broker;
- Stage 5.2's eventual ordering of journal writes, mount inspection, eligibility recheck and external effects.

The most important remaining risk is the absence of a safe contained ChatGPT subscription route and provider-specific inference-idle proof. The most important integration risk is Stage 5.2 crash recovery around native submission. Both remain explicit gates rather than inferred successes.
