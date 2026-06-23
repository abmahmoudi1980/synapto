# Quickstart: Multi-User Telegram News Aggregator

**Feature**: 002-multi-user-saas
**Date**: 2026-06-23
**Purpose**: A runnable, end-to-end validation of the v2 feature. Use this guide to prove that a fresh deployment supports the spec's five user stories, the four success criteria buckets, and the documented edge cases.

## Prerequisites

- **Go 1.23+** (`go version`)
- **Node 20+** and **npm** (`node --version && npm --version`)
- **make** (GNU make — on Windows use Git Bash's `make`, `choco install make`, or run the underlying commands directly)
- A POSIX shell for the Makefile's `cp`/`mkdir` (Git Bash or WSL on Windows)
- A POSIX shell environment for the Bash scripts (Git Bash, WSL, or `bash` from MSYS2)
- For Track A (no real Telegram / AI): no external accounts needed.
- For Track B (real Telegram): a `@BotFather` token, the bot added to the test channel, and your own `telegram_id` (from `@userinfobot` or a debug print of an inbound `Message`).
- For Track C (real AI): an OpenAI-compatible API key with at least $1 of credit.

## Build the binary

```bash
# From the repo root.
make build
# Equivalent: cd frontend && npm ci && npm run build && cd ../backend && go build -o ../bin/assistant ./cmd/assistant
```

The build embeds the SPA into the Go binary via `//go:embed`. The resulting `bin/assistant` is the single artifact you deploy.

## Run the migrations

The binary runs `0004_multi_user.sql` at startup, automatically. The migration is forward-only; running it twice is a no-op (the `schema_migrations` table tracks applied versions). For a v1 → v2 upgrade on an existing database, see "Upgrade from v1" below.

## Configuration

All config is read from environment variables. The new v2 keys are:

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `JWT_SECRET` | yes (v2) | — | HMAC key for the session JWT. Missing at boot is fatal. Use 32+ random bytes (`openssl rand -hex 32`). |
| `TELEGRAM_BOT_USERNAME` | yes (v2) | — | The `@handle` of the bot (e.g. `synapto_bot`). The Login Widget needs to know which bot to point at. |
| `PER_CYCLE_DELIVERY_CAP` | no | `1000` | Maximum number of per-user deliveries attempted in a single cycle. |
| `AI_MAX_USER_FILTER_CONCURRENCY` | no | `16` | Maximum number of in-flight per-user AI calls. |
| `MAX_CYCLE_DURATION` | no | `5m` | Hard cap on a single cycle's wall-clock time. |

The v1 keys `ADMIN_PASSWORD` and `TELEGRAM_SUBSCRIBER_CHAT` are removed in v2.

A minimal local env file (`.runtime/track-multi-user.env`):

```bash
TELEGRAM_BOT_TOKEN=                                # leave empty for Track A
TELEGRAM_BOT_USERNAME=synapto_bot
TELEGRAM_USE_FAKE=true
TELEGRAM_SOURCE=longpoll
ASSISTANT_AI_PROVIDER=fake                         # use 'openai' for Track C
AI_BASE_URL=https://api.openai.com/v1
AI_MODEL=gpt-4o-mini
AI_API_KEY=                                        # required when ASSISTANT_AI_PROVIDER=openai
JWT_SECRET=                                        # 32+ random bytes
DB_PATH=./assistant.db
ADMIN_LISTEN_ADDR=127.0.0.1:8080
LOG_LEVEL=info
```

## Upgrade from v1

The migration is destructive at the schema level (drops `digest_items`, drops v1 columns on `settings`) and conservative at the data level (the v1 admin user is reconstructed as a synthetic `users` row named `Imported (v1)`). Before applying, snapshot the SQLite file:

```bash
make backup-before-migration
# Equivalent: cp assistant.db assistant.db.pre-0004
```

The auto-backup is gated by `BACKUP_BEFORE_MIGRATIONS=1` (default off in dev, on in production deployments). With the env var set, the binary writes `<DB_PATH>.pre-0004` next to the original before applying the migration.

If you realize you need to revert to v1 after applying 0004, restore the backup and run the v1 binary:

```bash
cp assistant.db.pre-0004 assistant.db
# Run the v1 binary against the restored file.
```

## Track A — Multi-User Pure-Local (≈10 min)

Validates the full v2 loop with the fake Telegram and fake AI clients. No external accounts needed.

### 1. Start the service

```bash
# Use the existing Track A env file; add the v2 keys.
cp .runtime/track-a.env .runtime/track-multi-user.env
echo "TELEGRAM_BOT_USERNAME=synapto_bot" >> .runtime/track-multi-user.env
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .runtime/track-multi-user.env
make run-track-multi-user
# Equivalent: env $(cat .runtime/track-multi-user.env | xargs) ./bin/assistant
```

The service listens on `http://127.0.0.1:8080`. The fake Telegram client reads from `.runtime/source-messages.yaml` and records sent messages to `.runtime/telegram-sent.jsonl`.

### 2. Open the login page

Visit `http://127.0.0.1:8080/`. The dashboard shows a "Continue with Telegram" button.

### 3. Complete the fake login

The fake Telegram client ships with a stub that auto-verifies any payload signed with the configured bot token. For Track A, you do not have a real bot token; the fake client in `internal/telegram/fake.go` accepts any well-formed payload. Open the developer console and submit a valid payload via the SPA's stub helper:

```javascript
// In the browser console on http://127.0.0.1:8080/login
await fetch('/api/v2/auth/telegram', {
  method: 'POST',
  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  body: new URLSearchParams({
    id: '11111',
    first_name: 'Alice',
    last_name: 'Test',
    username: 'alice_test',
    auth_date: String(Math.floor(Date.now() / 1000)),
    hash: 'fake-hash',  // the fake Telegram client accepts this
  }),
});
```

The server returns a JWT; the SPA stores it and redirects to the dashboard. **This step validates User Story 1.**

### 4. Subscribe to a channel

In the dashboard, type `durov` in the channel input and click "Add". The server validates the handle via the fake Telegram client (which always succeeds for known seeded handles), creates the catalog row, and adds it to your watch-list. **This validates User Story 2.**

### 5. Set a filter

Open the channel's filter editor. Choose "AI Prompt" and enter `"Only forward crypto news"`. Save. **This validates User Story 3.**

### 6. Trigger a cycle and observe the per-user delivery

Wait for the next cycle (default 10 minutes) or restart the service with `DIGEST_INTERVAL=30s` for a faster loop. After one cycle:

- Open `http://127.0.0.1:8080/history`. You see a delivery record per matched post. The `summary` field is the per-user tailored text (matching the filter). **This validates User Story 4.**
- For a chatty channel with no matching posts, the cycle is silent (no "no items" message). **This validates SC-002.**

### 7. Change your settings and observe the new cadence

In the settings page, set the digest interval to 60 seconds. Wait one cycle at the new cadence. **This validates User Story 5 and SC-011.**

### 8. Log out and back in

Click "Log out". The page returns to the login page. The `Set-Cookie` is cleared. Log back in with the same payload; the server returns the same `user.id` (matched on `telegram_id`). **This validates FR-001 / FR-003.**

## Track B — Real Telegram, Fake AI (≈20 min)

Same as Track A, but with a real bot token. The Login Widget is real; the AI is the fake client (so per-user AI filter calls always return a deterministic match based on substring).

### 1. Create the bot

Talk to `@BotFather`, create a new bot, copy the token to `TELEGRAM_BOT_TOKEN`. Set `TELEGRAM_BOT_USERNAME` to the bot's `@handle` (without the `@`).

### 2. Configure the real Telegram client

```bash
TELEGRAM_USE_FAKE=false
TELEGRAM_BOT_TOKEN=<from BotFather>
TELEGRAM_BOT_USERNAME=<bot handle>
```

Start the service. Open `http://127.0.0.1:8080/login`. The "Continue with Telegram" button is now the real widget; clicking it opens the Telegram-hosted popup, you confirm, and the SPA posts the payload to `/api/v2/auth/telegram`.

### 3. Add the bot to a test channel

Create a public Telegram channel, add the bot as an administrator, post a few messages. The cycle picks them up via the long-poll `getUpdates` path.

### 4. Subscribe to the test channel from the dashboard

Type the channel's `@handle`. The server validates via the real `getChat` call. The channel appears in your watch-list. Subsequent cycles fetch the new posts and deliver to you (per your filter).

## Track C — Real Telegram, Real AI (≈30 min)

Same as Track B, but with the real OpenAI-compatible AI provider.

```bash
ASSISTANT_AI_PROVIDER=openai
AI_BASE_URL=https://api.openai.com/v1
AI_MODEL=gpt-4o-mini
AI_API_KEY=<your key>
```

The per-user AI filter call hits the real provider. To validate the `ai_prompt` filter path:

1. Subscribe to a channel that posts a mix of crypto and non-crypto news.
2. Set your filter to `"Only forward crypto news"`.
3. Run a few cycles. Verify that crypto posts are delivered with a tailored summary, and non-crypto posts are dropped (`status='no_match'` in your history view).

To validate degraded mode:

1. Set `AI_API_KEY=` (empty) without restarting — actually, you need to restart, but a simpler way is to set `ASSISTANT_AI_PROVIDER=fake` to a fake that always errors. **Easier:** temporarily set `AI_BASE_URL=http://127.0.0.1:1` (an unreachable port) and restart.
2. Run a cycle. The cycle records `status='sent'` with `send_error='ai_degraded'` and the delivered message has the `[best effort — AI unavailable]` prefix. **This validates FR-016 and SC-007.**

## Mapping back to the spec

The tracks above exercise the following spec requirements end-to-end:

| Track | User stories | Spec requirements | Success criteria |
|---|---|---|---|
| A | US1, US2, US3, US4 (partial), US5 | FR-001…FR-013, FR-019, FR-021, FR-022, FR-025, FR-026, FR-027 | SC-001, SC-002, SC-003, SC-005, SC-006, SC-010, SC-011, SC-012, SC-013 |
| B | All of A + US4 (full), real bot blocked path | FR-014, FR-015, FR-022, FR-023 | SC-005, SC-008, SC-013 |
| C | All of B + degraded mode | FR-016, FR-017, FR-018 | SC-004, SC-007, SC-009 |

A dedicated **load test** for SC-009 (1,000 active users on one chatty channel) is a separate runnable:

```bash
make loadtest-multi-user
# Equivalent: go test ./tests -run TestLoadTest1000Users -v
```

The load test seeds 1,000 synthetic users, 1 channel posting 1 message per second, and runs 24 hours of simulated cycles. The test asserts: zero dropped deliveries, p95 cycle duration < 60s, no `cycle_overrun` rows.

## Troubleshooting

- **`/api/v2/auth/telegram` returns `hash_mismatch`**: the bot token configured in the env does not match the token used to sign the Login Widget payload. Verify `TELEGRAM_BOT_TOKEN` is the token for the bot whose handle is in `TELEGRAM_BOT_USERNAME`.
- **`/api/v2/auth/telegram` returns `auth_date_stale`**: the Login Widget was opened more than 5 minutes ago. Refresh the page.
- **The cycle is silent for a chatty channel**: your filter is rejecting every post. Open `http://127.0.0.1:8080/deliveries?status=no_match` to see the rejections; relax the filter to receive more.
- **The dashboard shows "no items" for a channel with new posts**: the per-user fan-out is deferring deliveries because the cycle hit the cap. Check `op_events` for `kind='delivery.cycle_overrun'`. The next cycle picks them up automatically.
- **A user reports they never receive messages from a channel**: the cycle has marked the user as "stop sending" because they blocked the bot. An operator can clear the flag via the future admin endpoint `POST /api/v2/admin/users/{id}/unblock-flag`.
