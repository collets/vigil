GO := $(if $(wildcard .tools/go/bin/go),$(CURDIR)/.tools/go/bin/go,go)
unexport GOROOT
export GOENV := off
export GOPROXY := https://proxy.golang.org,direct
export GOSUMDB := sum.golang.org
export GOPATH := $(CURDIR)/.cache/gopath
export GOCACHE := $(CURDIR)/.cache/go-build

.PHONY: build hello dashboard fmt check tidy cross-build
tidy:
	$(GO) mod tidy
build:
	CGO_ENABLED=0 $(GO) build -o bin/agent-control ./cmd/agent-control
hello:
	$(GO) run ./cmd/agent-control hello
dashboard:
	$(GO) run ./cmd/agent-control dashboard
fmt:
	$(GO) fmt ./...
check:
	$(GO) vet ./...
	$(GO) test ./...
cross-build:
	@set -e; for os in linux darwin; do for arch in amd64 arm64; do \
		echo "Building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -o dist/agent-control-$$os-$$arch ./cmd/agent-control; \
	done; done
