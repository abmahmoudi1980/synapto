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
RUNTIME_DIR = .runtime
TRACK_A_ENV = $(RUNTIME_DIR)/track-a.env
TRACK_B_ENV = $(RUNTIME_DIR)/track-b.env
TRACK_C_ENV = $(RUNTIME_DIR)/track-c.env

# Tools
GO = go
NPM = npm

.PHONY: all deps build build-frontend build-backend run test test-backend \
        test-frontend lint lint-go lint-fe fmt fmt-go fmt-fe clean help \
        env-track-a env-track-b env-track-c run-track-a run-track-b run-track-c \
        docker-build docker-run docker-up docker-down docker-logs docker-push

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

# Track A: pure-local validation. No Telegram, no AI.
# See specs/001-telegram-news-assistant/quickstart.md Track A.
env-track-a:
	@mkdir -p $(RUNTIME_DIR)
	@printf '%s\n' \
		'ASSISTANT_AI_PROVIDER=fake' \
		'DIGEST_INTERVAL=1m' \
		'ADMIN_LISTEN_ADDR=127.0.0.1:8080' \
		'DB_PATH=./.runtime/assistant.db' \
		'TELEGRAM_FAKE_OUT=./.runtime/telegram-sent.jsonl' \
		'TELEGRAM_FAKE_SEED=./.runtime/source-messages.yaml' \
		'LOG_LEVEL=info' > $(TRACK_A_ENV)
	@echo "Wrote $(TRACK_A_ENV)"

# Track B: real Telegram bot + fake AI. Operator supplies the token.
env-track-b: env-track-a
	@TELEGRAM_BOT_TOKEN=$${TELEGRAM_BOT_TOKEN:-} ; \
	TELEGRAM_SUBSCRIBER_CHAT=$${TELEGRAM_SUBSCRIBER_CHAT:-} ; \
	{ printf '%s\n' \
		'ASSISTANT_AI_PROVIDER=fake' \
		'DIGEST_INTERVAL=1m' \
		'ADMIN_LISTEN_ADDR=127.0.0.1:8080' \
		'DB_PATH=./.runtime/assistant.db' \
		"TELEGRAM_BOT_TOKEN=$$TELEGRAM_BOT_TOKEN" \
		"TELEGRAM_SUBSCRIBER_CHAT=$$TELEGRAM_SUBSCRIBER_CHAT" \
		'LOG_LEVEL=info' ; } > $(TRACK_B_ENV)
	@echo "Wrote $(TRACK_B_ENV) (export TELEGRAM_BOT_TOKEN + TELEGRAM_SUBSCRIBER_CHAT first)"

# Track C: real Telegram + real AI. Operator supplies all credentials.
env-track-c: env-track-a
	@TELEGRAM_BOT_TOKEN=$${TELEGRAM_BOT_TOKEN:-} ; \
	TELEGRAM_SUBSCRIBER_CHAT=$${TELEGRAM_SUBSCRIBER_CHAT:-} ; \
	AI_BASE_URL=$${AI_BASE_URL:-https://api.openai.com/v1} ; \
	AI_MODEL=$${AI_MODEL:-gpt-4o-mini} ; \
	AI_API_KEY=$${AI_API_KEY:-} ; \
	{ printf '%s\n' \
		'ASSISTANT_AI_PROVIDER=openai' \
		'DIGEST_INTERVAL=10m' \
		'ADMIN_LISTEN_ADDR=127.0.0.1:8080' \
		'DB_PATH=./.runtime/assistant.db' \
		"TELEGRAM_BOT_TOKEN=$$TELEGRAM_BOT_TOKEN" \
		"TELEGRAM_SUBSCRIBER_CHAT=$$TELEGRAM_SUBSCRIBER_CHAT" \
		"AI_BASE_URL=$$AI_BASE_URL" \
		"AI_MODEL=$$AI_MODEL" \
		"AI_API_KEY=$$AI_API_KEY" \
		'LOG_LEVEL=info' ; } > $(TRACK_C_ENV)
	@echo "Wrote $(TRACK_C_ENV) (export TELEGRAM_* + AI_* first)"

# run-track-a/b/c: write the env file if missing, source it, run the binary.
run-track-a: env-track-a build
	set -a; . ./$(TRACK_A_ENV); set +a; ./$(BIN)

run-track-b: env-track-b build
	set -a; . ./$(TRACK_B_ENV); set +a; ./$(BIN)

run-track-c: env-track-c build
	set -a; . ./$(TRACK_C_ENV); set +a; ./$(BIN)

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
	@echo "  env-track-a    - write .runtime/track-a.env (no creds)"
	@echo "  env-track-b    - write .runtime/track-b.env (uses TELEGRAM_BOT_TOKEN + chat)"
	@echo "  env-track-c    - write .runtime/track-c.env (uses TELEGRAM_* + AI_*)"
	@echo "  run-track-a    - run Track A locally (fake Telegram + fake AI)"
	@echo "  run-track-b    - run Track B locally (real Telegram + fake AI)"
	@echo "  run-track-c    - run Track C locally (real Telegram + real AI)"
	@echo "  docker-build   - build the production image (Dockerfile)"
	@echo "  docker-run     - run the image with a one-shot docker run (no compose)"
	@echo "  docker-up      - start the stack via docker compose"
	@echo "  docker-down    - stop the stack via docker compose"
	@echo "  docker-logs    - tail the assistant container logs"
	@echo "  docker-push    - push the image to the registry"
	@echo ""
	@echo "Validation tracks are documented in specs/001-telegram-news-assistant/quickstart.md"

# ---- Docker ----------------------------------------------------------------
# Build a single production image containing the SPA + Go binary. Tag is
# overridable with IMAGE / VERSION on the command line:
#   make docker-build VERSION=$(git describe --tags --always)
IMAGE ?= synapto/assistant
VERSION ?= 0.1.0-dev
TAG ?= $(IMAGE):$(VERSION)
LATEST ?= $(IMAGE):latest

docker-build:
	docker build -t $(TAG) -t $(LATEST) --build-arg VERSION=$(VERSION) .

# One-shot run: bind 127.0.0.1:8080 on the host, persist the SQLite DB on
# a named volume, source the env file. Useful for quick smoke tests; the
# docker-compose stack is the recommended path for a real deployment.
docker-run:
	@if [ ! -f .runtime/assistant.env ]; then \
		if [ -f deploy/assistant.env.example ]; then \
			echo ".runtime/assistant.env not found. Copying deploy/assistant.env.example to get started."; \
			mkdir -p .runtime; \
			cp deploy/assistant.env.example .runtime/assistant.env; \
		else \
			echo ".runtime/assistant.env not found and no deploy/assistant.env.example template available."; \
			exit 1; \
		fi; \
	fi
	docker run --rm -it \
		--name synapto-assistant \
		-p 127.0.0.1:8080:8080 \
		--env-file .runtime/assistant.env \
		-v synapto-data:/data \
		$(TAG)

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f assistant

docker-push:
	docker push $(TAG)
	docker push $(LATEST)
