GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
unexport GOROOT
export GOENV := off
export GOPROXY := https://proxy.golang.org,direct
export GOSUMDB := sum.golang.org
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/go-build

.PHONY: build build-boundary hello dashboard fmt check check-race docs-check tidy cross-build build-scenario scenario scenario-live scenario-clean
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
	$(GO) test -race ./internal/harness ./internal/spike ./internal/store ./internal/artifacts ./internal/core ./internal/coordinator ./internal/boundary ./internal/checkpoint ./internal/supervisor ./internal/checks ./internal/review ./internal/quality ./internal/tui ./internal/cli ./internal/tools ./internal/mcp ./internal/workspace
docs-check:
	$(GO) test ./internal/doccheck -count=1 -v
cross-build:
	@set -e; for os in linux darwin; do for arch in amd64 arm64; do \
		echo "Building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/vigil-$$os-$$arch ./cmd/vigil; \
	done; done

# Stage 5.7 autonomous end-to-end qualification. `make scenario` drives the real
# production binary as a subprocess against a disposable fixture and is fully
# offline: it contacts no model provider, no hosting service and no real remote.
# The opt-in targets below add bounded live capability and are never part of
# `make check`.
# The scenario root must live OUTSIDE the checkout: the walkthrough creates and
# removes its own disposable tree, and it refuses any root inside a repository so
# it can never touch an operator's working copy.
SCENARIO_ROOT ?= /tmp/vigil-stage-5.7-scenario
SCENARIO_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)

build-scenario:
	CGO_ENABLED=0 $(GO) build -o bin/vigil-scenario ./cmd/vigil-scenario
	CGO_ENABLED=0 $(GO) build -o bin/vigil ./cmd/vigil

# The disposable root is created outside any checkout, and the scenario binary
# refuses a root that already holds content. Before any `rm -rf`, the root is
# resolved to a physical path and required to be the exact documented disposable
# directory. A glob or a `..` suffix cannot satisfy this: the check compares the
# resolved path, so `/tmp/vigil-stage-5.7-scenario/../../etc` is rejected.
define scenario_root_guard
	root='$(1)'; \
	case "$$root" in *..*) echo "refusing to touch SCENARIO_ROOT=$$root: it contains a traversal component" >&2; exit 1 ;; esac; \
	resolved=`cd "$$root" 2>/dev/null && pwd -P || echo "$$root"`; \
	if [ "$$resolved" != "/tmp/vigil-stage-5.7-scenario" ]; then \
		echo "refusing to touch SCENARIO_ROOT=$$root: it resolves to '$$resolved', not the documented disposable /tmp/vigil-stage-5.7-scenario" >&2; \
		exit 1; \
	fi
endef

scenario: build-scenario
	@$(call scenario_root_guard,$(SCENARIO_ROOT))
	@rm -rf $(SCENARIO_ROOT)
	@mkdir -p $(SCENARIO_ROOT)
	./bin/vigil-scenario --binary bin/vigil --root $(SCENARIO_ROOT) \
		--source-commit $(SCENARIO_COMMIT) --verbose

# The same offline walkthrough, with the bounded local-inference capability
# permitted. Be precise about what this does: the opt-in lets the run *probe* the
# existing loopback llama route with a metadata read and record its identity. It
# does not yet start a live model turn, because no stage of the walkthrough drives
# a live turn — the report records the turn count, and wiring a real turn is a
# recorded pending item. It never falls back to a metered endpoint and never
# enables production dispatch.
scenario-live: build-scenario
	@$(call scenario_root_guard,$(SCENARIO_ROOT))
	@mkdir -p $(SCENARIO_ROOT)
	./bin/vigil-scenario --binary bin/vigil --root $(SCENARIO_ROOT) \
		--source-commit $(SCENARIO_COMMIT) --allow-local-inference --verbose

# Removes only the agent-owned disposable scenario root, and only under the same
# guard as the run itself.
scenario-clean:
	@$(call scenario_root_guard,$(SCENARIO_ROOT))
	rm -rf $(SCENARIO_ROOT)
