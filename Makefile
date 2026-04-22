.DEFAULT_GOAL := help

# ---- Configuration ---------------------------------------------------------

BINARY       := orchestrator
VERSION      := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS      := -X main.Version=$(VERSION) -s -w
GOFLAGS      := -trimpath

FRONTEND_DIR := web
FRONTEND_SRC := $(FRONTEND_DIR)/src
FRONTEND_OUT := $(FRONTEND_DIR)/dist
EMBED_DIR    := cmd/orchestrator/web-dist

GO_PKGS      := ./cmd/... ./internal/...

# ---- User-facing targets ---------------------------------------------------

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Orchestrator — MicroVM orchestrator for AI agents\n\nUsage: make <target>\n\nTargets:\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: all
all: build build-agent ## Build host binary and guest agent

.PHONY: build
build: $(EMBED_DIR) ## Build the host binary (embeds frontend)
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/orchestrator

.PHONY: build-agent
build-agent: ## Build the guest agent (static linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/agent ./cmd/agent

.PHONY: build-frontend
build-frontend: ## Build the web dashboard bundle
	cd $(FRONTEND_DIR) && npm install --silent && npm run build

$(EMBED_DIR): build-frontend
	rm -rf $(EMBED_DIR)
	cp -r $(FRONTEND_OUT) $(EMBED_DIR)

.PHONY: test
test: ## Run all Go tests
	go test $(GOFLAGS) $(GO_PKGS)

.PHONY: test-race
test-race: ## Run tests with the race detector
	go test -race $(GO_PKGS)

.PHONY: vet
vet: ## Run go vet
	go vet $(GO_PKGS)

.PHONY: fmt
fmt: ## Format Go source
	gofmt -s -w $(shell find . -name '*.go' -not -path './web/*' -not -path './bin/*')

.PHONY: lint
lint: vet ## Run linters (currently go vet; add staticcheck in CI)

.PHONY: install-hooks
install-hooks: ## Install dev git hooks (pre-commit: fmt + vet)
	@mkdir -p .git/hooks
	@printf '#!/usr/bin/env bash\nset -e\nmake fmt vet\n' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "pre-commit hook installed"

.PHONY: install
install: build ## Install the orchestrator binary to /usr/local/bin
	install -Dm755 bin/$(BINARY) /usr/local/bin/$(BINARY)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/ $(FRONTEND_OUT) $(EMBED_DIR)

.PHONY: deps
deps: ## Tidy Go modules
	go mod tidy

# ---- Infrastructure --------------------------------------------------------

.PHONY: install-firecracker
install-firecracker: ## Download and install Firecracker binaries (requires sudo)
	sudo scripts/install-firecracker.sh

.PHONY: rootfs
rootfs: ## Build the base guest rootfs (requires sudo, ~10 min)
	sudo scripts/build-rootfs.sh

.PHONY: demo
demo: build build-agent ## Run a quick demo task end-to-end
	sudo bin/$(BINARY) task run --prompt "echo 'hello from orchestrator'; uname -a; date > /root/output/hello.txt" --runtime shell --ram 512 --vcpus 1

# ---- Release ---------------------------------------------------------------

.PHONY: release
release: ## Build release artifacts for the current platform
	mkdir -p dist
	tar -czf dist/$(BINARY)-$(VERSION)-linux-amd64.tar.gz -C bin $(BINARY) agent
	@echo "Release artifact: dist/$(BINARY)-$(VERSION)-linux-amd64.tar.gz"
