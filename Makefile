GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
unexport GOROOT
export GOENV := off
export GOPROXY := https://proxy.golang.org,direct
export GOSUMDB := sum.golang.org
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/go-build

.PHONY: build hello dashboard fmt check check-race tidy cross-build
tidy:
	$(GO) mod tidy
build:
	CGO_ENABLED=0 $(GO) build -o bin/vigil ./cmd/vigil
hello:
	$(GO) run ./cmd/vigil hello
dashboard:
	$(GO) run ./cmd/vigil dashboard
fmt:
	$(GO) fmt ./...
check:
	$(GO) vet ./...
	$(GO) test ./...
check-race:
	$(GO) test -race ./internal/harness ./internal/spike ./internal/store ./internal/artifacts ./internal/core ./internal/coordinator ./internal/boundary
cross-build:
	@set -e; for os in linux darwin; do for arch in amd64 arm64; do \
		echo "Building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/vigil-$$os-$$arch ./cmd/vigil; \
	done; done
