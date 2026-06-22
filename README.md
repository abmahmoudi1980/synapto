# Synapto — Telegram News Digest Assistant

A single-binary service that fetches new messages from a set of public Telegram channels you choose, summarizes and categorizes them with an AI provider, and delivers one grouped digest message to your Telegram chat on a fixed cadence (default every 10 minutes). A Svelte admin panel — embedded into the same Go binary — lets you curate channels and categories, tune the interval, and browse digest history.

> **Phase 1 scope:** single subscriber, single designated Telegram bot. Multi-subscriber / multi-bot support is out of scope for this phase. See [`specs/001-telegram-news-assistant/spec.md`](specs/001-telegram-news-assistant/spec.md) for the full specification.

## How it works

Every digest cycle runs the same loop:

1. **Fetch** new messages from each selected channel since the last successful cycle (per-channel cursor, no double-deliver).
2. **Deduplicate** items forwarded into multiple channels within the same window.
3. **Summarize + categorize** each item through a pluggable AI summarizer (`fake` or any OpenAI-compatible endpoint).
4. **Render** a single Telegram message grouped by category, respecting Telegram's size limits (split or top-N with a "more" marker when needed).
5. **Send** the digest from the designated bot to your chat. Cycles with no new items are silently suppressed.

If the AI provider is unavailable, the cycle degrades gracefully to raw headlines + categories instead of skipping. Failures (Telegram API errors, blocked bot, channel gone private, scheduling slips) are recorded and surfaced in the admin panel. Restart safety guarantees no window is double-delivered or skipped.

### Two ways to fetch channel posts

The read side is pluggable via `TELEGRAM_SOURCE`:

- **`longpoll`** (default) — the bot long-polls `getUpdates` for `channel_post` events. The bot must be a member of every channel you want to monitor. Real-time, no rate limits beyond Telegram's.
- **`preview`** — the service reads the public web preview at `t.me/s/<handle>` and walks paginated pages newest-to-oldest. **The bot does not need to be a member of the channel** — useful for public channels you don't administer. Unofficial surface; rate-limited to 1 req/handle/sec. Send side still uses the Bot API.

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
| `make env-track-a` | write `.runtime/track-a.env` (no creds) |
| `make env-track-b` | write `.runtime/track-b.env` (uses `TELEGRAM_BOT_TOKEN` + chat) |
| `make env-track-c` | write `.runtime/track-c.env` (uses `TELEGRAM_*` + `AI_*`) |
| `make run-track-a` | run Track A locally (fake Telegram + fake AI) |
| `make run-track-b` | run Track B locally (real Telegram + fake AI) |
| `make run-track-c` | run Track C locally (real Telegram + real AI) |
| `make docker-build` | build the production image (SPA + Go) |
| `make docker-up` | start the stack via `docker compose` |
| `make docker-down` | stop the stack |
| `make docker-logs` | tail the assistant container logs |

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
| `TELEGRAM_SOURCE` | `longpoll` | `longpoll` (bot must be a member) or `preview` (reads `t.me/s/<handle>`, no membership required) |
| `TELEGRAM_PREVIEW_BASE` | `https://t.me` | base URL for the preview source (override for tests / mirrors) |
| `TELEGRAM_USE_FAKE` | `false` | force the in-process fake Telegram client |
| `TELEGRAM_FAKE_SEED` | `./.runtime/source-messages.yaml` | seed file for the fake client |
| `TELEGRAM_FAKE_OUT` | `./.runtime/telegram-sent.jsonl` | where the fake client records sent messages |
| `ADMIN_LISTEN_ADDR` | `127.0.0.1:8080` | admin HTTP bind address |
| `DB_PATH` | `./assistant.db` | SQLite file path |
| `LOG_LEVEL` | `info` | slog level |
| `ASSISTANT_DEV` | `false` | dev flag |

> **Security:** the admin API has no auth layer in phase 1. Bind `ADMIN_LISTEN_ADDR` to a non-public interface (e.g. `127.0.0.1:8080`) or put it behind a reverse proxy / VPN. Credentials are read from env vars at startup, never stored in the DB in plaintext.

### Pure-local run (no Telegram, no AI)

The fastest path to a working service. The `run-track-a` target writes an env file with safe defaults and starts the binary:

```bash
make run-track-a
# open http://127.0.0.1:8080/
```

The first boot seeds the default category set (Politics, Technology, Business, Sports, World, Other) and the singleton settings row. The fake Telegram client reads from `.runtime/source-messages.yaml` and records sent digests to `.runtime/telegram-sent.jsonl`.

Want to run a real Telegram bot (Track B) or a real OpenAI-compatible provider (Track C)? See the [quickstart](specs/001-telegram-news-assistant/quickstart.md) for full instructions, or just `make env-track-b` / `make env-track-c` to write the env files, export the required credentials, and `make run-track-b` / `make run-track-c`.

## Docker deployment

The service is shipped as a single multi-stage image: stage 1 builds the SvelteKit SPA, stage 2 compiles a static Go binary that embeds the SPA via `//go:embed`, stage 3 is a minimal Alpine runtime that runs as a non-root user. The resulting image is self-contained — no sidecars, no external DB, no init system.

```bash
# 1. Write a populated env file (do not commit a populated copy).
cp deploy/assistant.env.example .runtime/assistant.env
$EDITOR .runtime/assistant.env   # set TELEGRAM_BOT_TOKEN, AI_API_KEY, etc.

# 2. Build the image and start the stack.
make docker-up

# 3. Tail the logs.
make docker-logs

# 4. Stop the stack (keeps the named volume with the SQLite DB).
make docker-down
```

Equivalent direct commands:

```bash
docker build -t synapto/assistant:latest .
docker compose up -d
docker compose logs -f assistant
```

### What the image contains

| Layer | Purpose |
| --- | --- |
| `node:20-alpine` (build) | `npm ci` + `npm run build` of the Svelte SPA → `frontend/build/` |
| `golang:1.23-alpine` (build) | `go build` with the SPA copied into the `//go:embed` directory, `CGO_ENABLED=0`, trimmed + stripped binary |
| `alpine:3.20` (runtime) | ca-certificates + tzdata + a non-root `assistant` user; the single binary + a healthcheck |

The image is reproducible (no bind mounts during build), runs as UID 10001, and exposes a single port (8080). The SQLite file lives on a named volume (`synapto-data`) so it survives `docker compose down` (but not `docker compose down -v`).

### Production checklist

- **Never bake credentials into the image.** Use `env_file` (see `.runtime/assistant.env.example` for the full list) and keep the populated file out of git. The provided `.gitignore` already covers `.runtime/`, `.env`, and `*.env.local`.
- **Put the admin panel behind a reverse proxy** (Caddy, nginx, or Traefik) that handles TLS and rate limiting, and set `ADMIN_PASSWORD` in the env file so the login page is enforced. Binding to `127.0.0.1:8080` and fronting it with a proxy on port 443 is the recommended topology.
- **Back up the named volume.** `sqlite3 /var/lib/docker/volumes/synapto-data/_data/assistant.db ".backup '/path/to/snap.db'"` is enough; the schema is small and the digest history is the only state worth keeping.
- **Watch `/api/health`.** The container's healthcheck pings it every 30s; external monitoring can do the same.
- **Tune `DIGEST_INTERVAL`** for the traffic you actually see. The default 10 minutes is fine for typical news channels; if you follow busy channels, raise `AI_MAX_CONCURRENCY` and lower the interval.

### One-shot smoke test

If you just want to confirm the image boots without a stack file:

```bash
make docker-build
make docker-run    # uses .runtime/assistant.env, binds 127.0.0.1:8080
```

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

- **Implemented now (v1.0):** the digest cycle, scheduler with restart safety and live interval reload, SQLite store, dedup, renderer with full MarkdownV2 escaping (header, footer, category heading `# ` → `\# `, summary text, channel handle, status), admin HTTP API with **session-cookie auth** when `ADMIN_PASSWORD` is set, embedded Svelte SPA, **real OpenAI- and Anthropic-compatible summarizers** (`backend/internal/ai/openai.go`, `backend/internal/ai/anthropic.go`), **fake Telegram client** for local dev, **HTTPPreview Telegram client** for reading public channels via `t.me/s/<handle>` without bot membership (`TELEGRAM_SOURCE=preview`), **op_events audit log** (`cycle.start`, `cycle.success`, `cycle.degraded`, `cycle.failed`, `cycle.skipped_no_items`, `telegram.send.failed`, `telegram.send.blocked`, `telegram.send.no_recipient`, `channel.inaccessible`, `channel.banned`, `settings.changed`, etc.), **graceful shutdown** that lets the in-flight cycle finish, a `tracks-a/b/c/p` validation matrix, and **env-to-settings sync at boot** so the AI fields in the panel always reflect the live env (operator-tunable fields are left as-is).
- **Pluggable by design:** the AI summarizer (`ai.Summarizer`) and Telegram client (`telegram.Client`) are behind interfaces, so the provider/model and the read source can change without touching the cycle. The fake implementations stay for tests and Track A.
- **Out of scope for v1:** multi-subscriber, multi-bot, OCR/ASR for media-only posts, horizontal scaling. The preview source cannot read private channels and does not auto-discover the subscriber chat id from `/start` (use long-poll for that, or set `TELEGRAM_SUBSCRIBER_CHAT` explicitly).

## Spec & design docs

- [Feature spec](specs/001-telegram-news-assistant/spec.md) — requirements, user stories, edge cases, success criteria
- [Implementation plan](specs/001-telegram-news-assistant/plan.md) — technical context, project structure, constitution check
- [Quickstart](specs/001-telegram-news-assistant/quickstart.md) — runnable end-to-end validation
- [Admin API contract](specs/001-telegram-news-assistant/contracts/admin-api.md)
- [Telegram render contract](specs/001-telegram-news-assistant/contracts/telegram-render.md)
- [AI summarizer contract](specs/001-telegram-news-assistant/contracts/ai-summarizer.md)
