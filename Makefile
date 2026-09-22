GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
unexport GOROOT
export GOENV := off
export GOPROXY := https://proxy.golang.org,direct
export GOSUMDB := sum.golang.org
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/go-build

.PHONY: build build-boundary hello dashboard fmt check check-race tidy cross-build
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
	$(GO) test -race ./internal/harness ./internal/spike ./internal/store ./internal/artifacts ./internal/core ./internal/coordinator ./internal/boundary ./internal/checkpoint ./internal/supervisor ./internal/checks ./internal/review ./internal/quality ./internal/tui ./internal/workspace
cross-build:
	@set -e; for os in linux darwin; do for arch in amd64 arm64; do \
		echo "Building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/vigil-$$os-$$arch ./cmd/vigil; \
	done; done
