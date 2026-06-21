# Synapto — Telegram News Digest Assistant
# Single-binary build: Svelte SPA → embedded into Go binary via //go:embed.
#
# NOTE: This Makefile targets GNU make. On Windows use Git Bash's make,
# `choco install make`, or run the underlying commands directly.

# Keep Go from auto-downloading a newer toolchain (modernc.org/sqlite pins).
GOTOOLCHAIN = local
export GOTOOLCHAIN

# Paths
BACKEND_DIR = backend
FRONTEND_DIR = frontend
BIN_DIR = bin
BIN = $(BIN_DIR)/assistant

# Tools
GO = go
NPM = npm

.PHONY: all deps build build-frontend build-backend run test test-backend \
        test-frontend lint lint-go lint-fe fmt fmt-go fmt-fe clean help

all: build

deps:
	$(GO) -C $(BACKEND_DIR) mod download
	cd $(FRONTEND_DIR) && $(NPM) ci || $(NPM) install

build: build-frontend build-backend

build-frontend:
	cd $(FRONTEND_DIR) && $(NPM) run build

build-backend: copy-spa
	$(GO) -C $(BACKEND_DIR) build -o ../$(BIN) ./cmd/assistant

# copy-spa: stage the built SPA into the //go:embed directory before go build.
# Keeps .gitkeep so the embed directive always has at least one file.
copy-spa:
	@find $(BACKEND_DIR)/internal/adminapi/spa -mindepth 1 -name '.gitkeep' -prune -o -exec rm -rf {} +
	@touch $(BACKEND_DIR)/internal/adminapi/spa/.gitkeep
	@if [ -d $(FRONTEND_DIR)/build ]; then cp -r $(FRONTEND_DIR)/build/. $(BACKEND_DIR)/internal/adminapi/spa/; fi

run: build
	./$(BIN)

test: test-backend test-frontend

test-backend:
	$(GO) -C $(BACKEND_DIR) test ./...

test-frontend:
	cd $(FRONTEND_DIR) && $(NPM) test

lint: lint-go lint-fe

lint-go:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run $(BACKEND_DIR)/...; \
	else \
		$(GO) -C $(BACKEND_DIR) vet ./...; \
	fi

lint-fe:
	cd $(FRONTEND_DIR) && $(NPM) run lint

fmt: fmt-go fmt-fe

fmt-go:
	$(GO) -C $(BACKEND_DIR) fmt ./...

fmt-fe:
	cd $(FRONTEND_DIR) && $(NPM) run format

clean:
	-rm -rf $(BIN_DIR)
	-rm -rf $(FRONTEND_DIR)/build
	-$(GO) -C $(BACKEND_DIR) clean -testcache

help:
	@echo "Synapto Makefile targets:"
	@echo "  deps           - install backend + frontend dependencies"
	@echo "  build          - build SPA then Go binary (default)"
	@echo "  run            - build and run the assistant"
	@echo "  test           - run backend + frontend tests"
	@echo "  lint           - run Go + frontend linters"
	@echo "  fmt            - format Go + frontend code in place"
	@echo "  clean          - remove build artifacts"
