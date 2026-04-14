.PHONY: help build test lint vuln clean install-tools release-snapshot

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.Date=$(DATE)

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the ralph binary into ./dist/
	go build -ldflags "$(LDFLAGS)" -o dist/ralph ./cmd/ralph

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

clean: ## Remove build artifacts
	rm -rf dist/ coverage.out
