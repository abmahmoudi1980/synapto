# Synapto — Telegram News Digest Assistant

A single-binary service that fetches new messages from a set of public Telegram channels you choose, summarizes and categorizes them with an AI provider, and delivers one grouped digest message to your Telegram chat on a fixed cadence (default every 10 minutes). A Svelte admin panel — embedded into the same Go binary — lets you curate channels and categories, tune the interval, and browse digest history.

> **Phase 1 scope:** single subscriber, single designated Telegram bot. Multi-subscriber / multi-bot support is out of scope for this phase. See [`specs/001-telegram-news-assistant/spec.md`](specs/001-telegram-news-assistant/spec.md) for the full specification.

## How it works

Every digest cycle runs the same loop:

1. **Fetch** new messages from each selected channel since the last successful cycle (per-channel cursor, no backfill).
2. **Deduplicate** items forwarded into multiple channels within the same window.
3. **Summarize + categorize** each item through a pluggable AI summarizer (`fake` or any OpenAI-compatible endpoint).
4. **Render** a single Telegram message grouped by category, respecting Telegram's size limits (split or top-N with a "more" marker when needed).
5. **Send** the digest from the designated bot to your chat. Cycles with no new items are silently suppressed.

If the AI provider is unavailable, the cycle degrades gracefully to raw headlines + categories instead of skipping. Failures (Telegram API errors, blocked bot, channel gone private, scheduling slips) are recorded and surfaced in the admin panel. Restart safety guarantees no window is double-delivered or skipped.

## Stack

- **Backend:** Go 1.23+, `go-chi/chi` (admin HTTP), `jmoiron/sqlx` + `modernc.org/sqlite` (pure-Go, no CGo), `google/uuid`, `caarlos0/env`, `log/slog`.
- **Frontend:** SvelteKit 2 + Vite 5 + TypeScript, `@sveltejs/adapter-static` — built to static files and embedded into the Go binary via `//go:embed`.
- **Storage:** embedded SQLite (single file, zero-ops). Schema is portable to PostgreSQL later if needed.
- **Deploy:** one binary, one DB file, one process. Linux amd64/arm64 as a container or systemd service.

## Project structure

```text
backend/
  cmd/assistant/main.go        # entrypoint: config → store → AI → telegram → admin API → scheduler
  internal/
    config/                    # env-based config
    store/                     # migrations + sqlite repository (channels, categories, cycles, digests, health)
    telegram/                  # bot API client, cycle fetcher, sender (real + in-process fake)
    ai/                        # Summarizer interface + OpenAI-compatible impl + fake
    digest/                    # the cycle: fetch → dedup → summarize → render → send, plus scheduler
    adminapi/                  # chi handlers + embedded SPA
    logging/                   # slog setup
frontend/
  src/routes/                  # overview, channels, categories, history, settings pages
  src/lib/                     # typed admin API client + components
specs/001-telegram-news-assistant/   # spec, plan, research, data model, contracts, quickstart
Makefile                       # build glue: SPA build → go:embed → go build
```

## Prerequisites

- **Go 1.23+** (`go version`)
- **Node 20+** and **npm** (`node --version && npm --version`)
- **make** (GNU make — on Windows use Git Bash's `make`, `choco install make`, or run the underlying commands directly)
- A POSIX shell for the Makefile's `cp`/`mkdir` (Git Bash or WSL on Windows)
- A Telegram bot token + a public channel the bot can read (only for real-Telegram runs)
- An OpenAI-compatible API key (only for real-AI runs)

## Build & run

```bash
make deps      # go mod download + npm ci (falls back to npm install)
make build     # build the Svelte SPA, then `go build` the embedded binary → bin/assistant
make run       # build and run
```

Other targets:

| Target | What it does |
| --- | --- |
| `make test` | backend + frontend tests |
| `make test-backend` / `make test-frontend` | each suite separately |
| `make lint` | `golangci-lint` (or `go vet`) + frontend ESLint/Prettier |
| `make fmt` | format Go + frontend in place |
| `make clean` | remove `bin/`, `frontend/build`, test cache |

## Configuration

All config is read from environment variables (see [`backend/internal/config/config.go`](backend/internal/config/config.go)). A fresh checkout boots with defaults using the `fake` AI provider and the `fake` Telegram client — no credentials required.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ASSISTANT_AI_PROVIDER` | `fake` | `fake` or `openai` |
| `AI_BASE_URL` | `https://api.openai.com/v1` | OpenAI-compatible base URL |
| `AI_MODEL` | `gpt-4o-mini` | model name |
| `AI_API_KEY` | _(empty)_ | required when provider is `openai` |
| `AI_PER_CALL_TIMEOUT` | `8s` | per-summarizer-call timeout |
| `AI_MAX_CONCURRENCY` | `8` | max in-flight summarizer calls |
| `DIGEST_INTERVAL` | `10m` | cycle cadence (min 1m, max 24h) |
| `TELEGRAM_BOT_TOKEN` | _(empty)_ | bot token from `@BotFather` |
| `TELEGRAM_SUBSCRIBER_CHAT` | `0` | your chat id to deliver digests to |
| `TELEGRAM_USE_FAKE` | `false` | force the in-process fake Telegram client |
| `TELEGRAM_FAKE_SEED` | `./.runtime/source-messages.yaml` | seed file for the fake client |
| `TELEGRAM_FAKE_OUT` | `./.runtime/telegram-sent.jsonl` | where the fake client records sent messages |
| `ADMIN_LISTEN_ADDR` | `127.0.0.1:8080` | admin HTTP bind address |
| `DB_PATH` | `./assistant.db` | SQLite file path |
| `LOG_LEVEL` | `info` | slog level |
| `ASSISTANT_DEV` | `false` | dev flag |

> **Security:** the admin API has no auth layer in phase 1. Bind `ADMIN_LISTEN_ADDR` to a non-public interface (e.g. `127.0.0.1:8080`) or put it behind a reverse proxy / VPN. Credentials are read from env vars at startup, never stored in the DB in plaintext.

### Pure-local run (no Telegram, no AI)

```bash
mkdir -p .runtime
cat > .runtime/assistant.env <<'EOF'
ASSISTANT_AI_PROVIDER=fake
DIGEST_INTERVAL=10s
ADMIN_LISTEN_ADDR=127.0.0.1:8080
DB_PATH=./.runtime/assistant.db
TELEGRAM_FAKE_OUT=./.runtime/telegram-sent.jsonl
EOF

set -a; source .runtime/assistant.env; set +a
./bin/assistant
```

Open <http://127.0.0.1:8080/> for the admin panel. Add a channel, seed `.runtime/source-messages.yaml`, restart, and watch a cycle fire every 10 seconds.

## Admin HTTP API

Served from the same origin as the SPA (no CORS, no auth in phase 1). JSON over HTTP/1.1, UTF-8, UUIDv4 ids, ISO-8601 UTC timestamps. Full contract: [`specs/001-telegram-news-assistant/contracts/admin-api.md`](specs/001-telegram-news-assistant/contracts/admin-api.md).

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/health` | liveness + last cycle summary |
| `GET` / `POST` | `/api/channels` | list / add a selected channel (validates via Telegram `getChat`) |
| `DELETE` | `/api/channels/{id}` | remove a channel |
| `GET` / `POST` | `/api/categories` | list / add a category |
| `PATCH` / `DELETE` | `/api/categories/{id}` | rename / remove a custom category (defaults can't be removed) |
| `GET` / `PATCH` | `/api/settings` | read / update interval, subscriber chat, uncategorized label |
| `POST` | `/api/settings/test-telegram` | probe the bot token via `getMe` |
| `POST` | `/api/settings/test-ai` | probe the AI provider with a 1-token request |
| `GET` | `/api/cycles?limit=&offset=` | list recent cycles (reverse chronological) |
| `GET` | `/api/cycles/{id}` | one cycle + its digest + items grouped by category |
| `GET` | `/api/events?limit=` | recent operational events (successes, failures, degradations) |

Errors use a stable shape: `{ "error": { "code": "...", "message": "...", "field": "..." } }` with a meaningful HTTP status.

## Default categories

The service ships with: **Politics, Technology, Business, Sports, World, Other.** The subscriber can add, rename, and remove custom categories from the admin panel; defaults can be renamed but not removed. Items that don't match any category are grouped under the configurable `uncategorized_label` (default `Uncategorized`).

## Testing

```bash
make test          # backend (go test ./...) + frontend (vitest)
make lint          # golangci-lint or go vet + eslint/prettier
```

Backend tests use an in-memory fake Telegram client and an interface-driven fake AI summarizer for end-to-end cycle tests, plus golden-file tests for the digest renderer. Frontend component tests use `@testing-library/svelte` + Vitest.

## Validation tracks

The [quickstart](specs/001-telegram-news-assistant/quickstart.md) describes three end-to-end validation tracks:

- **Track A — Pure-local (≈5 min):** fake AI + fake Telegram, validates the cycle, store, renderer, admin API, and SPA.
- **Track B — Real Telegram + fake AI (≈20 min):** real bot + public channel, validates the send/read paths.
- **Track C — Real Telegram + real AI (≈30 min):** full end-to-end with an OpenAI-compatible provider, including degraded-mode simulation.

## Status & scope

- **Implemented now:** the digest cycle, scheduler with restart safety, SQLite store, dedup, renderer with size-limit handling, admin HTTP API, embedded Svelte SPA, fake AI summarizer, fake Telegram client.
- **Pluggable by design:** the AI summarizer (`ai.Summarizer`) and Telegram client (`telegram.Client`) are behind interfaces, so the provider/model and bot library can change without touching the cycle. The real OpenAI-compatible summarizer and the real Telegram Bot API client are wired as the non-fake paths.
- **Out of scope for phase 1:** multi-subscriber, multi-bot, OCR/ASR for media-only posts, horizontal scaling, auth on the admin panel.

## Spec & design docs

- [Feature spec](specs/001-telegram-news-assistant/spec.md) — requirements, user stories, edge cases, success criteria
- [Implementation plan](specs/001-telegram-news-assistant/plan.md) — technical context, project structure, constitution check
- [Quickstart](specs/001-telegram-news-assistant/quickstart.md) — runnable end-to-end validation
- [Admin API contract](specs/001-telegram-news-assistant/contracts/admin-api.md)
- [Telegram render contract](specs/001-telegram-news-assistant/contracts/telegram-render.md)
- [AI summarizer contract](specs/001-telegram-news-assistant/contracts/ai-summarizer.md)
