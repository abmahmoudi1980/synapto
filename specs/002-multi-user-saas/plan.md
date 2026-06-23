# Implementation Plan: Multi-User Telegram News Aggregator

**Branch**: `002-multi-user-saas` | **Date**: 2026-06-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-multi-user-saas/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Phase 2 of the Synapto service refactors the single-subscriber v1 into a multi-tenant SaaS. Any visitor can sign in with the Telegram-hosted web sign-in flow, manage their own personal watch-list of Telegram channels, and configure per-channel filters (keywords, natural-language prompt, or single category). The backend (Go) is extended with new repositories (`users`, `user_channels`, `user_filters`, `user_deliveries`, `user_sessions`, `auth_nonces`), a new `auth` package that verifies the Telegram sign-in payload and issues revocable session tokens, and a refactored digest cycle that fans out each source-channel post to every subscriber of that channel, evaluating the subscriber's effective filter and (when matching) producing a per-user tailored summary before sending the message to that subscriber's Telegram chat. The Svelte admin panel is rewritten to be per-user: login page with a Telegram sign-in button, per-user channels page, per-user filter editor, per-user delivery history, and per-user settings. The v1 single-admin password model and the v1 singleton `settings.telegram_subscriber_chat` field are removed (hard cut); v1's `posts` queue, `cycles` log, and `op_events` audit table are kept as shared global infrastructure. Persistence stays on the existing single-file embedded database; no external database service is introduced.

## Technical Context

**Language/Version**: Go 1.23+ (toolchain `go1.24.4`, as already pinned in `backend/go.mod`); TypeScript + Svelte 4 + SvelteKit 2 for the admin SPA. No new languages introduced.

**Primary Dependencies**:
- Backend additions: `github.com/golang-jwt/jwt/v5` (or equivalent) for HS256 session tokens with a `jti` claim tied to a `user_sessions` row (revocation table). The existing `github.com/go-telegram-bot-api/telegram-bot-api/v5`, `github.com/go-chi/chi/v5`, `modernc.org/sqlite`, `github.com/jmoiron/sqlx`, `github.com/google/uuid`, `github.com/caarlos0/env/v10`, `github.com/sashabaranov/go-openai` are reused unchanged.
- Frontend additions: a small Telegram sign-in widget loader (vanilla `<script>` tag from `telegram.org/js/telegram-widget.js`); the existing Svelte + Vite stack is unchanged.

**Storage**: Single-file embedded SQLite (same engine as v1, unchanged). One forward-only migration `0004_multi_user.sql` adds the new tables and trims the `settings` row to operator-only fields. No external database service is introduced; the existing volume path (`/data/assistant.db` in the container, `./assistant.db` in dev) is reused. The `schema_migrations` table tracks applied versions; the migration is wrapped in a single transaction (existing `applyOne` in `backend/internal/store/migrate.go:106`).

**Testing**: Go `testing` + `github.com/stretchr/testify` (already in use). New tests: (a) `backend/tests/auth_telegram_test.go` — table-driven verification of the Telegram sign-in HMAC for both the Login Widget and Mini App initData paths, (b) `backend/tests/cycle_fanout_test.go` — one source-channel post + three users with three different filter types, asserting each user receives the right message and the per-user delivery records are correct, (c) `backend/tests/jwt_revocation_test.go` — assert a revoked `user_sessions` row rejects subsequent requests. Existing tests are updated where they reference the v1 password auth path. Svelte component tests cover the login button + filter editor; Vitest is unchanged. An end-to-end smoke scenario is added to `quickstart.md` and runnable via `make quickstart-multi-user`.

**Target Platform**: Linux server (amd64 + arm64) as a single container or systemd service, identical to v1. The Windows build path is not a target (already noted as out of scope in v1).

**Project Type**: Web application — Go backend + Svelte SPA embedded in the same binary. The `Dockerfile` and `docker-compose.yml` need no infra-side change (SQLite stays); only the env file template (`deploy/assistant.env.example`) gains two new required keys (`TELEGRAM_BOT_USERNAME`, `JWT_SECRET`).

**Performance Goals**: A cycle with up to ~50 source messages across 10 channels and up to 1,000 subscribers must finish (fetch + global-summarize + per-user AI call + per-user send) in under 5 minutes on a 2-core VM. The single `Summarizer.ApplyUserFilter` call is the new bottleneck: 50 posts × 1,000 ai_prompt users = 50,000 AI calls. The cycle's per-user fan-out is bounded by the `perUserAIConcurrency` setting (default 16) to keep the AI provider from rate-limiting. Per-keyword filter matches are O(text-length) and not a bottleneck.

**Constraints**:
- All login payloads must be verified against the bot token; unsigned or stale payloads are rejected. Payload freshness is bounded to 5 minutes from `auth_date`. A nonce store (`auth_nonces`) prevents replay of a captured payload within the freshness window.
- The bot identity is single-tenant (one `TELEGRAM_BOT_TOKEN`). All users receive deliveries from the same bot. The Bot API limits reading channel history to messages posted after the bot joined; for the public-web preview source (`TELEGRAM_SOURCE=preview`) the bot does not need to be a member. The existing long-poll and preview source paths are reused.
- AI provider latency: each per-user `ApplyUserFilter` call is treated as a hard timeout (reusing the v1 `AI_PER_CALL_TIMEOUT`); on failure the cycle falls back to a degraded per-user delivery with a "best effort" note (no silent drop, per FR-016).
- Telegram rate limits (≈30 messages/sec global, ≈1/sec per chat) must be respected; the per-user send path uses the v1 per-post gap (1.5s/user) and a per-cycle cap (default 1,000 deliveries per cycle; the rest are queued for the next cycle).
- No external secret store: the bot token and AI key are read from env vars; the JWT signing secret is read from `JWT_SECRET` (required). Secrets are never written to the DB in plaintext; only refs.
- Per-user state is the per-user row's responsibility; cross-user reads or modifications are rejected at the repository layer (FR-026).

**Scale/Scope**: 1 operator, 1 bot, up to 1,000 active users (the SC-014 deployment target). Each user follows 1–50 channels, with up to ~100 new messages per 10-minute window in the common case. The single-bot topology is a deliberate trade-off (per the user's locked decision): simple to operate, lower per-user onboarding friction, but the per-cycle fan-out is O(posts × users), so the per-user AI call has the bounded concurrency above to keep the cycle from blowing past 5 minutes.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The project's `constitution.md` is still on its unratified template (no principles filled in), so the constitution-derived gate list is currently empty. The plan therefore relies on the **default Spec Kit quality bar** plus the spec's own success criteria, and applies the following gates by convention (reused from `specs/001-telegram-news-assistant/plan.md` so the two plans share a single quality bar):

- **G1 – No implementation details in the spec**: PASS (validated in `checklists/requirements.md`). The spec is user-focused; implementation choices (SQLite, Login Widget, single-bot, etc.) live in the spec's **Assumptions** section and are bound here.
- **G2 – All FRs are testable**: PASS (every FR-001…FR-027 has at least one acceptance scenario in the spec).
- **G3 – All SCs are measurable and tech-agnostic**: PASS (SC-001…SC-014 include numbers, percentages, or count-based outcomes; none name a technology, framework, or database).
- **G4 – Architecture fits the spec scope (multi-user, one bot, single binary)**: PASS — the locked decisions (single shared bot, single binary, embedded SPA) match the v1 deployment topology; only the auth and per-user state shapes change.
- **G5 – No premature abstraction**: PASS — only one new abstraction is introduced: the `auth.Verifier` interface that wraps Telegram's two signature paths (Login Widget and Mini App initData). The cycle, render, scheduler, and deduper stay concrete. The single `Summarizer` interface gains a second method (`ApplyUserFilter`) rather than splitting into a second interface; this keeps the three implementations (OpenAI, Anthropic, fake) in lockstep.
- **G6 – Edge cases are covered by FRs or explicit handling**: PASS — each edge case listed in the spec is anchored to a specific FR or cycle behavior (e.g. "bot blocked by user" → FR-023; "AI service unavailable" → FR-016; "Login replay" → FR-001's 5-minute freshness + a nonce-store table; "1,000 users on a single chatty channel" → SC-009 + a 1,000-delivery-per-cycle cap).

No violations to justify; the **Complexity Tracking** table is therefore not needed.

### Post-Phase 1 re-evaluation

After the design artifacts (`research.md`, `data-model.md`, `contracts/*`, `quickstart.md`) are produced, the same gates are re-checked:

- **G1**: PASS — the spec still has no tech-stack terms in its body. The plan pins the stack in **Technical Context** and **research.md** only.
- **G2**: PASS — every FR is now traceable to a contract field, a data-model field, or a quickstart validation step.
- **G3**: PASS — every SC is now traceable to a quickstart step (the **Mapping back to the spec** section in `quickstart.md`).
- **G4**: PASS — the single-binary + embedded SPA structure in **Project Structure** matches the v1 model and the user's locked decision.
- **G5**: PASS — only the two intentional abstractions (`auth.Verifier`, the `Summarizer.ApplyUserFilter` method) are introduced. The cycle, render, scheduler, and deduper remain concrete and testable.
- **G6**: PASS — each edge case is now anchored to a specific code-level mechanism: dedup → `posts.dedup_key`; AI outage → FR-016 fallback to "best effort" placeholder; login replay → `auth_nonces`; blocked bot → `user_deliveries.status='send_failed'` + a per-user "stop sending to this user" flag; per-user rate limit → per-cycle cap with the rest deferred to the next cycle.

All gates continue to pass. No new violations.

## Project Structure

### Documentation (this feature)

```text
specs/002-multi-user-saas/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── admin-api-v2.md
│   └── telegram-render-v2.md
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

The v1 layout is preserved. The v2 work **adds** new packages and **modifies** a small set of existing files. New and changed paths are tagged with `+` and `~` respectively; everything else is unchanged.

```text
backend/
├── cmd/
│   └── assistant/
│       └── main.go                       # ~ wire new repos; drop telegram_subscriber_chat; wire auth.Verifier
├── internal/
│   ├── config/
│   │   └── config.go                     # ~ drop TELEGRAM_SUBSCRIBER_CHAT, ADMIN_PASSWORD; add TELEGRAM_BOT_USERNAME, JWT_SECRET, AI_MAX_USER_FILTER_CONCURRENCY
│   ├── auth/                             # + NEW package: telegram HMAC verify, JWT issue/parse, middleware
│   │   ├── telegram.go                   # + LoginWidget + WebApp initData verification (HMAC-SHA-256)
│   │   ├── jwt.go                        # + HS256 issue/parse; checks user_sessions.revoked_at on every parse
│   │   ├── middleware.go                 # + chi-compatible middleware; sets X-Synapto-User + X-Synapto-Session
│   │   ├── nonce.go                     # + generate/consume nonces from auth_nonces
│   │   └── auth_test.go                  # + table-driven HMAC tests + revocation tests
│   ├── store/
│   │   ├── store.go                      # ~ add UserRepo, UserChannelRepo, UserFilterRepo, UserDeliveryRepo, UserSessionRepo, AuthNonceRepo; add UserSettings, DeliveryStatus types
│   │   ├── migrate.go                    # unchanged
│   │   └── migrations/
│   │       ├── 0001_init.sql             # unchanged
│   │       ├── 0002_posts_queue.sql      # unchanged
│   │       ├── 0003_per_post_delivery.sql # unchanged
│   │       └── 0004_multi_user.sql       # + NEW (see data-model.md)
│   ├── store/sqlite/
│   │   ├── store.go                      # unchanged
│   │   ├── users.go                      # + NEW
│   │   ├── user_channels.go              # + NEW
│   │   ├── user_filters.go               # + NEW
│   │   ├── user_deliveries.go            # + NEW
│   │   ├── user_sessions.go              # + NEW
│   │   ├── auth_nonces.go                # + NEW
│   │   ├── user_settings.go              # + NEW (CRUD on the per-user settings row)
│   │   ├── settings.go                   # ~ drop telegram_subscriber_chat; reduce to operator-only
│   │   ├── channels.go                   # ~ add telegram_id column + UNIQUE index on handle; still in the catalog
│   │   ├── adapters.go                   # ~ add the new concrete stores (UserStore, UserChannelStore, …)
│   │   └── posts.go                      # unchanged (post-queue stays global)
│   ├── telegram/
│   │   ├── client.go                     # unchanged
│   │   ├── real.go                       # ~ drop the onSubscriberChat callback; chat id is now learned from the Login Widget payload
│   │   ├── fake.go                       # unchanged
│   │   ├── preview.go                    # unchanged
│   │   ├── sender.go                     # unchanged
│   │   └── fetcher.go                    # unchanged
│   ├── ai/
│   │   ├── summarizer.go                 # ~ add ApplyUserFilter method to the Summarizer interface + UserFilterInput/Output types
│   │   ├── openai.go                     # ~ implement ApplyUserFilter (chat-completions call, JSON response shape)
│   │   ├── anthropic.go                  # ~ implement ApplyUserFilter (messages call, JSON response shape)
│   │   ├── fake.go                       # ~ implement ApplyUserFilter (test-time substring match against Prompt)
│   │   └── *_test.go                     # ~ extend for the new method
│   ├── digest/
│   │   ├── cycle.go                      # ~ rewrite: global fetch + global summarize (1×) + per-user fan-out (N×)
│   │   ├── per_post_cycle.go             # - DELETED (v1 per-post mode is replaced by per-user delivery)
│   │   ├── scheduler.go                  # unchanged
│   │   ├── render.go                     # ~ add PerUserRenderer (per-(post, user, filter) text builder)
│   │   ├── dedup.go                      # unchanged (cross-channel dedup at the post level still applies)
│   │   └── scheduler_test.go             # unchanged
│   └── adminapi/
│       ├── server.go                     # ~ drop the password auth middleware; mount the v2 auth middleware; route /api/v2/*
│       ├── auth.go                       # - DELETED (replaced by the auth.Verifier + middleware + /api/v2/auth/* handlers)
│       ├── v2/                           # + NEW package (or subdirectory)
│       │   ├── auth_handlers.go          # + /api/v2/auth/{telegram,webapp,logout,me}
│       │   ├── channels_handlers.go      # + /api/v2/channels (list, subscribe, delete, list-by-channel)
│       │   ├── filters_handlers.go       # + /api/v2/filters (set, list, delete, toggle)
│       │   ├── user_settings_handlers.go # + /api/v2/me/settings (GET, PATCH)
│       │   ├── deliveries_handlers.go    # + /api/v2/deliveries (per-user history)
│       │   └── v2_test.go                # + handler tests with httptest + a fake verifier
│       ├── channels.go                   # - DELETED (v1 admin API; v2 takes over)
│       ├── categories.go                 # ~ remains global; GET /api/v2/categories lists the global set (operator-curated, read-only for users in v2)
│       ├── settings.go                   # - DELETED (per-user settings move to v2)
│       ├── history.go                    # - DELETED (per-user history move to v2)
│       ├── posts.go                      # - DELETED
│       └── health.go                     # unchanged (liveness probe stays public)
└── tests/
    ├── cycle_fanout_test.go              # + NEW: one channel, three users, three filter types
    ├── jwt_revocation_test.go            # + NEW: revoked user_sessions row rejects next request
    ├── auth_telegram_test.go             # + NEW: HMAC verification for both Login Widget + WebApp paths
    └── digest_cycle_test.go              # ~ update to drive the v2 fan-out interface

frontend/
├── src/
│   ├── routes/
│   │   ├── +layout.svelte                # ~ show "Logged in as <first name> · Logout" when a session is present
│   │   ├── +page.svelte                  # ~ replace single-admin overview with per-user overview
│   │   ├── login/+page.svelte            # + NEW: the only public route; renders the Telegram sign-in button
│   │   ├── channels/+page.svelte         # ~ per-user channels list (subscribe / list / delete)
│   │   ├── channels/[id]/filters/+page.svelte # + NEW: per-user filter editor (keywords / ai_prompt / category)
│   │   ├── history/+page.svelte          # ~ per-user delivery history (per (post, cycle) row)
│   │   └── settings/+page.svelte         # ~ per-user settings (interval, uncategorized label, delivery format)
│   ├── lib/
│   │   ├── api.ts                        # ~ add v2 client; keep v1 removed
│   │   ├── auth.ts                       # + NEW: load the Telegram widget; pass credentials back to /api/v2/auth/telegram
│   │   └── components/
│   │       ├── TelegramLoginButton.svelte  # + NEW: wraps the official widget script
│   │       ├── ChannelList.svelte         # ~ per-user
│   │       ├── FilterEditor.svelte        # + NEW: 3-mode filter editor
│   │       └── DeliveryHistoryList.svelte # + NEW: per-user history
│   └── app.html                          # ~ add the Telegram widget loader
└── static/

deploy/
├── assistant.env.example                # ~ add TELEGRAM_BOT_USERNAME, JWT_SECRET; drop TELEGRAM_SUBSCRIBER_CHAT, ADMIN_PASSWORD

Makefile                                  # ~ add `make quickstart-multi-user` target that runs the v2 smoke
```

**Structure Decision**: The v1 single-binary + embedded SPA model is preserved. The only structural change is a new `internal/auth` package, a new `internal/adminapi/v2` subdirectory, and a new SQLite migration. No new services, no new processes, no new infrastructure. This matches the v1 deploy topology and the user's locked decision (single shared bot, single binary, embedded SQLite, no new infra).

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none)    | —          | —                                    |
