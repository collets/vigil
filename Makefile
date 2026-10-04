GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
unexport GOROOT
export GOENV := off
export GOPROXY := https://proxy.golang.org,direct
export GOSUMDB := sum.golang.org
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/go-build

.PHONY: build build-boundary hello dashboard fmt check check-race docs-check manifest tidy cross-build build-scenario scenario scenario-live scenario-clean scenario-guard-check
tidy:
	$(GO) mod tidy
build:
	CGO_ENABLED=0 $(GO) build -o bin/vigil ./cmd/vigil
build-boundary:
	CGO_ENABLED=0 GOOS=linux $(GO) build -o bin/vigil-guardian ./cmd/vigil-guardian
	CGO_ENABLED=0 GOOS=linux $(GO) build -o bin/vigil-worker ./cmd/vigil-worker
	CGO_ENABLED=0 GOOS=linux $(GO) build -o bin/vigil-relay ./cmd/vigil-relay
hello:
	$(GO) run ./cmd/vigil hello
dashboard:
	$(GO) run ./cmd/vigil dashboard "$(PROJECT)"
fmt:
	$(GO) fmt ./...
check:
	$(GO) vet ./...
	$(GO) test ./...
check-race:
	$(GO) test -race ./internal/harness ./internal/spike ./internal/store ./internal/artifacts ./internal/core ./internal/coordinator ./internal/boundary ./internal/checkpoint ./internal/supervisor ./internal/checks ./internal/review ./internal/quality ./internal/tui ./internal/cli ./internal/tools ./internal/mcp ./internal/workspace ./internal/scenario ./cmd/vigil-scenario
# `docs-check` runs the whole documentation gate, which includes
# TestGeneratedManifestsAreCurrent: it fails when a committed generated manifest
# (docs/research/stage-6/6.3-manifest.md) differs from what the generator
# produces, so a stale description of the checker cannot be committed. Run
# `make manifest` and commit the result whenever you change the checker, its
# configuration row, or the manifest template.
docs-check:
	$(GO) test ./internal/doccheck -count=1 -v
manifest:
	$(GO) test ./internal/doccheck -count=1 -run TestGeneratedManifestsAreCurrent -update
cross-build:
	@set -e; for os in linux darwin; do for arch in amd64 arm64; do \
		echo "Building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/vigil-$$os-$$arch ./cmd/vigil; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/vigil-scenario-$$os-$$arch ./cmd/vigil-scenario; \
	done; done

# Stage 5.7 autonomous end-to-end qualification. `make scenario` drives the real
# production binary as a subprocess against a disposable fixture. It contacts no
# model provider, no hosting service and no real remote. It does make one bounded
# metadata GET against the loopback route when one is configured on the host, to
# record that route's identity; it never issues a prompt, never follows a redirect
# and never leaves the loopback interface. The opt-in targets below add bounded
# live capability and are never part of `make check`.
# The scenario root must live OUTSIDE the checkout: the walkthrough creates and
# removes its own disposable tree, and it refuses any root inside a repository so
# it can never touch an operator's working copy.
SCENARIO_ROOT ?= /tmp/vigil-stage-5.7-scenario
# Exported so the guard and the recipes read one environment variable. The guard
# uses `$$SCENARIO_ROOT` rather than a make substitution precisely so a value
# containing a quote or a shell metacharacter cannot be interpolated into the
# recipe text and executed before the guard has checked anything.
export SCENARIO_ROOT
# Exported for the same reason as SCENARIO_ROOT: a value containing a quote or a
# shell metacharacter must never be interpolated into the recipe text.
SCENARIO_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
export SCENARIO_COMMIT

build-scenario:
	CGO_ENABLED=0 $(GO) build -o bin/vigil-scenario ./cmd/vigil-scenario
	CGO_ENABLED=0 $(GO) build -o bin/vigil ./cmd/vigil

# The disposable root is created outside any checkout, and the scenario binary
# refuses a root that already holds content. Before any `rm -rf`, the root must
# resolve to the exact documented disposable directory.
#
# The value is passed through the environment rather than interpolated into the
# recipe text, so a `'` in SCENARIO_ROOT cannot break out of the guard and run
# before any check happens. Every use is `--`-guarded and quoted.
#
# Resolution uses `cd`/`pwd -P` on the parent directory rather than `realpath -m`,
# because macOS's realpath has no `-m` and an earlier version of this guard failed
# closed on every macOS host. Both the candidate root and the documented constant
# go through the same resolution, so a platform where /tmp is a symlink to
# /private/tmp compares like with like instead of refusing the correct path.
define scenario_root_guard
	@root="$$SCENARIO_ROOT"; \
	case "$$root" in *..*) \
		echo "refusing to touch SCENARIO_ROOT=$$root: it contains a traversal component" >&2; exit 1 ;; \
	esac; \
	resolve() { \
		dir=`dirname -- "$$1"`; \
		base=`basename -- "$$1"`; \
		parent=`cd "$$dir" 2>/dev/null && pwd -P` || return 1; \
		printf '%s/%s\n' "$$parent" "$$base"; \
	}; \
	resolved=`resolve "$$root"` || resolved=""; \
	documented=`resolve "/tmp/vigil-stage-5.7-scenario"` || documented=""; \
	if [ -z "$$resolved" ] || [ "$$resolved" != "$$documented" ]; then \
		echo "refusing to touch SCENARIO_ROOT=$$root: it resolves to '$$resolved', not the documented disposable /tmp/vigil-stage-5.7-scenario" >&2; \
		exit 1; \
	fi
endef

scenario: build-scenario
	@$(call scenario_root_guard)
	@rm -rf -- "$$SCENARIO_ROOT"
	@mkdir -p -- "$$SCENARIO_ROOT"
	./bin/vigil-scenario --binary bin/vigil --root "$$SCENARIO_ROOT" \
		--source-commit "$$SCENARIO_COMMIT" --verbose

# The same offline walkthrough, with the bounded local-inference capability
# permitted. Be precise about what this does: the opt-in lets the run *probe* the
# existing loopback llama route with a metadata read and record its identity. It
# does not yet start a live model turn, because no stage of the walkthrough drives
# a live turn — the report records the turn count, and wiring a real turn is a
# recorded pending item. It never falls back to a metered endpoint and never
# enables production dispatch.
scenario-live: build-scenario
	@$(call scenario_root_guard)
	@mkdir -p -- "$$SCENARIO_ROOT"
	./bin/vigil-scenario --binary bin/vigil --root "$$SCENARIO_ROOT" \
		--source-commit "$$SCENARIO_COMMIT" --allow-local-inference --verbose

# Removes only the agent-owned disposable scenario root, and only under the same
# guard as the run itself. The guard is also exercised automatically by
# `make scenario-guard-check`.
scenario-clean:
	@$(call scenario_root_guard)
	rm -rf -- "$$SCENARIO_ROOT"

# Exercises the scenario-root guard against a battery of paths that must all be
# refused, and against the one path that must be allowed. It creates only its own
# sentinel directory under /tmp and removes it. This is what keeps the guard from
# silently regressing into something that removes the wrong tree.
scenario-guard-check:
	@status=0; \
	probe() { \
		if $(MAKE) --no-print-directory scenario-clean SCENARIO_ROOT="$$1" >/dev/null 2>&1; then \
			echo "FAIL: guard allowed SCENARIO_ROOT=$$1"; status=1; \
		else \
			echo "ok: refused SCENARIO_ROOT=$$1"; \
		fi; \
	}; \
	sentinel=/tmp/vigil-scenario-guard-sentinel; \
	rm -rf "$$sentinel"; mkdir -p "$$sentinel"; touch "$$sentinel/sentinel"; \
	ln -sfn "$$sentinel" /tmp/vigil-scenario-guard-link; \
	probe /tmp/vigil-scenario-guard-sentinel; \
	probe /tmp/vigil-scenario-guard-sentinel/../vigil-scenario-guard-sentinel; \
	probe /tmp/vigil-scenario-guard-sentinel/nested; \
	probe /tmp/vigil-scenario-guard-link; \
	probe /tmp/vigil-scenario-guard-sentinel/; \
	probe /etc; \
	probe relative/path; \
	probe ""; \
	# A documented-looking path that is one character off must be refused too, so \
	# the comparison cannot be a prefix or basename match. \
	probe /tmp/vigil-stage-5.7-scenario-extra; \
	probe /tmp/vigil-stage-5.7-scenari; \
	# The documented root's own parent must not be removable through the root. \
	probe /tmp; \
	rm -rf "$$sentinel" /tmp/vigil-scenario-guard-link; \
	if [ -e "$$sentinel" ]; then echo "FAIL: the guard removed the sentinel"; status=1; else echo "ok: sentinel survived"; fi; \
	exit $$status
