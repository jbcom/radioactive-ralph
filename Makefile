.PHONY: help build test lint vuln clean install-tools release-snapshot docs-api docs-build docs-check test-linux test-linux-race test-linux-adapters test-linux-agent wsl-rootfs wsl-rootfs-lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.Date=$(DATE)

# Docker image for local Linux testing (matches CI Go version).
GO_LINUX_IMAGE ?= golang:1.26.6

# Workspace root — the directory containing go.mod.
WORKSPACE_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the radioactive_ralph binary into ./dist/
	go build -ldflags "$(LDFLAGS)" -o dist/radioactive_ralph ./cmd/radioactive_ralph

test: ## Run go test with race + coverage
	go test -race -coverprofile=coverage.out -covermode=atomic ./...

lint: ## Run golangci-lint
	golangci-lint run

vuln: ## Run govulncheck
	govulncheck ./...

install-tools: ## Install dev tools (golangci-lint, govulncheck, goreleaser)
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/goreleaser/goreleaser/v2@latest

release-snapshot: ## GoReleaser dry-run into ./dist/
	goreleaser release --snapshot --clean

docs-api: ## Regenerate Go API docs into ./docs/api
	bash scripts/generate-api-docs.sh

docs-build: ## Build the Sphinx docs site into ./docs/_build/html
	python3 -m tox -e docs

docs-check: docs-build ## Validate docs references and build the Sphinx site

clean: ## Remove build artifacts
	rm -rf dist/ coverage.out

## Linux testing via Docker (run from any platform with Docker)
## Usage: make test-linux                    # full suite
##        make test-linux-race               # race detector
##        make test-linux-adapters            # adapters package only
##        make test-linux-agent               # agent package only
##        make test-linux PKG=./internal/orch # specific package
##
## --init matters, not just hygiene: without it, PID 1 inside the container
## is the go test binary itself, which does not reap zombie descendants the
## way a real init process does. internal/agent's process-group reaping
## tests (TestKillReapsGrandchildProcess and four siblings) fail under a
## bare `docker run` for exactly this reason -- confirmed directly by
## running the identical command against unmodified upstream main and
## seeing the same five failures, then confirming --init (Docker's built-in
## tini) fixes all five with no other change. Not a code bug; a container
## PID-1 gap in this convenience target.
test-linux: ## Run full test suite on Linux via Docker
	docker run --rm --init -v "$(WORKSPACE_ROOT):/workspace" -w /workspace $(GO_LINUX_IMAGE) \
		sh -c 'apt-get update -qq && apt-get install -y -qq unzip > /dev/null 2>&1 && curl -fsSL https://bun.sh/install | bash && export BUN_INSTALL=$${HOME}/.bun && export PATH=$$BUN_INSTALL/bin:$$PATH && go test -timeout 10m ./...'

test-linux-race: ## Run full test suite with race detector on Linux via Docker
	docker run --rm --init -v "$(WORKSPACE_ROOT):/workspace" -w /workspace $(GO_LINUX_IMAGE) \
		sh -c 'apt-get update -qq && apt-get install -y -qq unzip > /dev/null 2>&1 && curl -fsSL https://bun.sh/install | bash && export BUN_INSTALL=$${HOME}/.bun && export PATH=$$BUN_INSTALL/bin:$$PATH && go test -race -timeout 20m ./...'

test-linux-adapters: ## Run adapters tests on Linux via Docker
	docker run --rm --init -v "$(WORKSPACE_ROOT):/workspace" -w /workspace $(GO_LINUX_IMAGE) \
		sh -c 'apt-get update -qq && apt-get install -y -qq unzip > /dev/null 2>&1 && curl -fsSL https://bun.sh/install | bash && export BUN_INSTALL=$${HOME}/.bun && export PATH=$$BUN_INSTALL/bin:$$PATH && go test -race -timeout 5m ./internal/adapters/...'

test-linux-agent: ## Run agent tests on Linux via Docker
	docker run --rm --init -v "$(WORKSPACE_ROOT):/workspace" -w /workspace $(GO_LINUX_IMAGE) go test -race -timeout 5m ./internal/agent/...

wsl-rootfs-lint: ## Lint packaging/wsl/Dockerfile with hadolint (mise-pinned)
	mise exec -- hadolint packaging/wsl/Dockerfile

wsl-rootfs: wsl-rootfs-lint ## Build packaging/wsl/rootfs.tar.gz (requires Docker)
	./packaging/wsl/build-rootfs.sh
