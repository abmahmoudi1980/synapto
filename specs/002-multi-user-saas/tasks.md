---
description: "Task list for the multi-user Telegram news aggregator refactor (002-multi-user-saas)"
---

# Tasks: Multi-User Telegram News Aggregator

**Input**: Design documents from `/specs/002-multi-user-saas/`
- `plan.md` (required) — tech stack, libraries, project structure
- `spec.md` (required) — five user stories (P1, P1, P1, P2, P3)
- `research.md` — R1…R10 design decisions
- `data-model.md` — schema, repository interfaces, validation rules
- `contracts/admin-api-v2.md` — HTTP API contract
- `contracts/telegram-render-v2.md` — per-user message format
- `quickstart.md` — Tracks A/B/C validation scenarios

**Tests**: Test tasks are included — the spec and quickstart both rely on end-to-end validation tracks, and the plan explicitly calls out `tests/cycle_fanout_test.go`, `tests/jwt_revocation_test.go`, and `tests/auth_telegram_test.go` as required artifacts. Tests are written first per phase 3+ of this list.

**Organization**: Tasks are grouped by user story. Each story is independently testable once the Foundational phase is complete.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3, US4, US5)
- Include exact file paths in descriptions

## Path Conventions

- Backend: `backend/cmd/`, `backend/internal/`, `backend/tests/`
- Frontend: `frontend/src/`
- Migrations: `backend/internal/store/migrations/`
- The v1 layout is preserved; the v2 work **adds** new packages and **modifies** a small set of existing files. New and changed paths are tagged `+ NEW` and `~ MODIFIED` respectively in the task descriptions.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, v1 cleanup, environment configuration, and the migration runner changes that every later phase depends on.

- [ ] T001 Create `internal/auth` package skeleton with `telegram.go`, `jwt.go`, `middleware.go`, `nonce.go` files in `backend/internal/auth/`
- [ ] T002 Create `internal/adminapi/v2` subdirectory with `auth_handlers.go`, `channels_handlers.go`, `filters_handlers.go`, `user_settings_handlers.go`, `deliveries_handlers.go` files in `backend/internal/adminapi/v2/`
- [ ] T003 [P] Drop `ADMIN_PASSWORD` and `TELEGRAM_SUBSCRIBER_CHAT` from `Config` struct in `backend/internal/config/config.go`; add `TELEGRAM_BOT_USERNAME`, `JWT_SECRET`, `PER_CYCLE_DELIVERY_CAP`, `AI_MAX_USER_FILTER_CONCURRENCY`, `MAX_CYCLE_DURATION`
- [ ] T004 [P] Add `golang-jwt/jwt/v5` (or equivalent) to `backend/go.mod`; run `go mod tidy`
- [ ] T005 [P] Add `BACKUP_BEFORE_MIGRATIONS=1` env-gated auto-backup to `cmd/assistant/main.go`; write `<DB_PATH>.pre-<migration_version>` next to the original
- [ ] T006 [P] Update `Makefile` with `backup-before-migration`, `quickstart-multi-user`, `run-track-multi-user`, `loadtest-multi-user` targets
- [ ] T007 [P] Update `deploy/assistant.env.example` with `TELEGRAM_BOT_USERNAME` and `JWT_SECRET`; remove `ADMIN_PASSWORD` and `TELEGRAM_SUBSCRIBER_CHAT`

**Checkpoint**: setup skeleton in place; no user story work has started yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Database migration, repository interfaces, auth package, JWT middleware, and the SPA login page. **CRITICAL**: no user story work can begin until this phase is complete.

- [ ] T008 Write `backend/internal/store/migrations/0004_multi_user.sql` per `data-model.md` "Schema" — adds `users`, `user_sessions`, `user_settings`, `user_channels`, `user_filters`, `user_deliveries`, `auth_nonces`; modifies `channels` (adds `telegram_id` + UNIQUE index on `handle`); modifies `settings` (drops v1 columns, adds `system_default_filter`); drops `digest_items`; includes the v1→v2 data migration that creates the synthetic `v1-admin` user
- [ ] T009 [P] Add entity types `User`, `UserSettings`, `UserChannel`, `UserFilter`, `UserDelivery`, `UserSession`, `AuthNonce` and `DeliveryStatus` enum to `backend/internal/store/store.go`; add the `User`, `UserSettings`, `UserChannel`, `UserFilter`, `UserDelivery`, `UserSession`, `AuthNonce`, `AuthNonceRepo` interfaces per `data-model.md` "Repository interfaces (Go)"; modify `Channel` struct to add `TelegramID int64` field
- [ ] T010 [P] Implement `UserRepo` on top of SQLite in `backend/internal/store/sqlite/users.go` (`Get`, `GetByTelegramID`, `UpsertFromLogin`, `SetActive`, `ListActive`); cover the system-default-filter inheritance in `UpsertFromLogin`
- [ ] T011 [P] Implement `UserSettingsRepo` in `backend/internal/store/sqlite/user_settings.go` (`Get`, `Update` with `UserSettingsUpdate` partial-update struct)
- [ ] T012 [P] Implement `UserChannelRepo` in `backend/internal/store/sqlite/user_channels.go` (`ListByUser`, `ListByChannel`, `Subscribe`, `Unsubscribe`); add the synthetic-v1-admin backfill path
- [ ] T013 [P] Implement `UserFilterRepo` in `backend/internal/store/sqlite/user_filters.go` (`ListByUser`, `ListByChannel`, `Get`, `ResolveFor` per research.md R5 precedence, `Set`, `Delete`, `SetActive`)
- [ ] T014 [P] Implement `UserDeliveryRepo` in `backend/internal/store/sqlite/user_deliveries.go` (`Insert`, `ListByUser`, `ListByCycle`, `ListUnsentByUser`, `MarkSent`, `MarkSendFailed`)
- [ ] T015 [P] Implement `UserSessionRepo` in `backend/internal/store/sqlite/user_sessions.go` (`Create`, `Get`, `Revoke`, `PurgeExpired`)
- [ ] T016 [P] Implement `AuthNonceRepo` in `backend/internal/store/sqlite/auth_nonces.go` (`Create`, `Consume` returning `ErrNonceReplayed`, `PurgeExpired`); expose `ErrNonceReplayed` in `store.go` sentinels
- [ ] T017 [P] Modify `ChannelRepo` SQLite implementation in `backend/internal/store/sqlite/channels.go` — add `telegram_id` to the `Add` signature, add the `telegram_id` column to the row struct, add the `idx_channels_handle` UNIQUE index
- [ ] T018 [P] Modify `SettingsRepo` SQLite implementation in `backend/internal/store/sqlite/settings.go` — drop the v1 admin-only fields from the row struct, add `SystemDefaultFilter` field, add a `SyncAISettings` no-op for the dropped fields
- [ ] T019 [P] Add thin adapter wrappers `UserStore`, `UserSettingsStore`, `UserChannelStore`, `UserFilterStore`, `UserDeliveryStore`, `UserSessionStore`, `AuthNonceStore` to `backend/internal/store/sqlite/adapters.go` mirroring the v1 pattern
- [ ] T020 Implement Telegram Login Widget + Mini App initData HMAC verification in `backend/internal/auth/telegram.go` per `research.md` R1 — single `verifyTelegramPayload(botToken, rawQuery, secretPrefix)` function, two exported wrappers `VerifyTelegramLogin` and `VerifyWebAppInitData`, 5-minute `auth_date` freshness check, constant-time HMAC compare
- [ ] T021 Implement HS256 JWT issue/parse in `backend/internal/auth/jwt.go` — `Issue(userID, telegramID, sessionID, ttl)` returns the signed token; `Parse(token, secret)` returns claims + checks `user_sessions` revocation on every parse (so a revoked `jti` is rejected immediately)
- [ ] T022 Implement chi-compatible auth middleware in `backend/internal/auth/middleware.go` — parses the `Authorization: Bearer` header, calls `Parse`, sets `X-Synapto-User` and `X-Synapto-Session` on the request, returns 401 with the structured error shape on any failure
- [ ] T023 [P] Implement nonce generation and consumption in `backend/internal/auth/nonce.go` — `NewNonce()` returns a 128-bit random hex string; `Consume(nonce)` calls `AuthNonceRepo.Consume` and returns `ErrNonceReplayed` if the row is already consumed
- [ ] T024 Drop the v1 password auth flow — delete `backend/internal/adminapi/auth.go`; remove `AdminPassword` field from `adminapi.Deps` in `backend/internal/adminapi/server.go`; remove the password-related fields from `settingsJSON` in `backend/internal/adminapi/settings.go`
- [ ] T025 Modify `backend/internal/adminapi/server.go` — remove the v1 route registrations (`/api/channels`, `/api/categories`, `/api/settings`, `/api/cycles`, `/api/posts`); mount the v2 router under `/api/v2`; mount the new auth middleware on `/api/v2/*` (except `/api/v2/auth/*` and `/api/v2/health`)
- [ ] T026 [P] Add `lib/auth.ts` to `frontend/src/lib/` — loads the Telegram widget script, calls `Telegram.Login.auth(...)`, posts the resulting payload to `/api/v2/auth/telegram`, stores the returned JWT in a cookie and in memory
- [ ] T027 [P] Create the new login page at `frontend/src/routes/login/+page.svelte` — renders a `TelegramLoginButton` component and handles the callback from the widget
- [ ] T028 [P] Create `TelegramLoginButton.svelte` in `frontend/src/lib/components/` — wraps the official widget script with the configured `TELEGRAM_BOT_USERNAME`; emits a `login` event with the verified payload
- [ ] T029 [P] Update `frontend/src/routes/+layout.svelte` — when a session JWT is present, show "Logged in as <first name> · Logout"; when not, redirect to `/login`
- [ ] T030 [P] Add `src/app.html` to load the Telegram widget loader from `telegram.org/js/telegram-widget.js` (asynchronously, with `data-telegram-login="<TELEGRAM_BOT_USERNAME>"`)

**Checkpoint**: Migration applies cleanly on a fresh DB; a v1 DB upgrades to v2 with the synthetic v1 admin user created; `/api/v2/auth/telegram` accepts a valid payload and issues a JWT; the SPA login page is reachable and renders the Telegram widget.

---

## Phase 3: User Story 1 - Log In with Telegram and Reach the Dashboard (Priority: P1) 🎯 MVP

**Goal**: A new visitor can log in with Telegram and reach an empty, personalized dashboard. This is the entry point of the SaaS model.

**Independent Test**: Click the Telegram login button, complete the confirmation, and verify the user lands on a dashboard showing their first name and an empty channel list — with no other story implemented, the visitor can still log in and see "no channels yet."

### Tests for User Story 1

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T031 [P] [US1] Write `backend/internal/auth/auth_test.go` table-driven HMAC verification tests covering both Login Widget (`secret = SHA-256(bot_token)`) and Mini App initData (`secret = HMAC-SHA-256("WebAppData", SHA-256(bot_token))`) per `research.md` R1; assert 5-minute `auth_date` rejection; assert `auth_date` exactly 5min+1s is rejected
- [ ] T032 [P] [US1] Write `backend/internal/auth/jwt_test.go` — assert a freshly-issued token parses to the right `sub`/`tid`/`jti`; assert a revoked `user_sessions` row causes `Parse` to return an error on the next call
- [ ] T033 [P] [US1] Write `backend/internal/auth/middleware_test.go` — assert missing `Authorization` header returns 401; assert invalid signature returns 401; assert valid token sets `X-Synapto-User` correctly; assert revoked session returns 401
- [ ] T034 [P] [US1] Write `backend/tests/auth_telegram_test.go` — full end-to-end: `POST /api/v2/auth/telegram` with a valid payload returns 200 + JWT + user JSON; with a tampered `hash` returns 401 `hash_mismatch`; with `auth_date` 6 minutes ago returns 401 `auth_date_stale`; replaying the same payload returns 401 `nonce_replayed`
- [ ] T035 [P] [US1] Write `backend/internal/store/sqlite/users_test.go` — assert `UpsertFromLogin` is idempotent on `telegram_id`; assert the synthetic v1 admin user is created on first login if `settings.system_default_filter` is set; assert the system default filter is inherited as a `user_filters` row with `channel_id = NULL`
- [ ] T036 [P] [US1] Write frontend component test in `frontend/src/routes/login/+page.test.ts` — assert the TelegramLoginButton renders with the configured bot handle; assert the post-callback handler stores the JWT and redirects to `/`

### Implementation for User Story 1

- [ ] T037 [P] [US1] Implement `POST /api/v2/auth/telegram` handler in `backend/internal/adminapi/v2/auth_handlers.go` — parses `application/x-www-form-urlencoded` body, calls `auth.VerifyTelegramLogin`, consumes a nonce, calls `UserRepo.UpsertFromLogin`, issues a JWT via `auth.Issue`, sets the session cookie, records `user.created` or `user.login` op event, returns `{token, expires_at, user}`
- [ ] T038 [P] [US1] Implement `POST /api/v2/auth/webapp` handler in `backend/internal/adminapi/v2/auth_handlers.go` — same as T037 but calls `auth.VerifyWebAppInitData` on a JSON `{initData: "..."}` body
- [ ] T039 [US1] Implement `POST /api/v2/auth/logout` handler in `backend/internal/adminapi/v2/auth_handlers.go` — reads the session id from the middleware-set `X-Synapto-Session` header, calls `UserSessionRepo.Revoke`, clears the `synapto_session` cookie, records `user.logout` op event
- [ ] T040 [US1] Implement `GET /api/v2/auth/status` handler in `backend/internal/adminapi/v2/auth_handlers.go` — returns `{authenticated: true, user, expires_at}` when a valid session is present, `{authenticated: false}` otherwise (no error path)
- [ ] T041 [US1] Implement `GET /api/v2/me` handler in `backend/internal/adminapi/v2/user_settings_handlers.go` — reads `X-Synapto-User`, calls `UserRepo.Get` and `UserSettingsRepo.Get`, returns `{user, settings}`; creates default `user_settings` row on first call if missing
- [ ] T042 [P] [US1] Add `lib/api.ts` v2 client in `frontend/src/lib/api.ts` — typed wrapper for `/api/v2/auth/*` and `/api/v2/me`; reads the JWT from the cookie or memory; surfaces structured errors to the SPA
- [ ] T043 [P] [US1] Replace `frontend/src/routes/+page.svelte` with a per-user overview — fetch `/api/v2/me` on mount; render first name + photo; render "Add a channel to start" empty state for the channels list
- [ ] T044 [US1] Update `frontend/src/routes/+layout.svelte` — call `/api/v2/auth/status` on mount; redirect to `/login` when unauthenticated; redirect to `/` when authenticated and on `/login`

**Checkpoint**: User Story 1 is fully functional. A new visitor can log in via Telegram, land on a personalized dashboard, and log out. The cycle does not run yet (no channels are subscribed); the SPA is otherwise complete.

---

## Phase 4: User Story 2 - Subscribe to a Telegram Channel (Priority: P1)

**Goal**: A logged-in user can add a public Telegram channel to their watch-list, see it in their personal channel list, and remove it. The watch-list is private to the user.

**Independent Test**: Log in, submit the handle of a public Telegram channel, verify the channel appears in the user's list, then delete it and verify the list returns to empty.

### Tests for User Story 2

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T045 [P] [US2] Write `backend/internal/store/sqlite/user_channels_test.go` — assert `Subscribe` rejects duplicate `(user_id, channel_id)` with `ErrDuplicateChannel`; assert the second user's `Subscribe` reuses the existing `channels` row (no duplicate catalog row); assert `Unsubscribe` does not delete the catalog row when other users still follow it
- [ ] T046 [P] [US2] Write `backend/internal/adminapi/v2/channels_handlers_test.go` — assert `POST /api/v2/channels/subscribe` with a valid handle returns 201 and a `channel` JSON; assert with a handle that fails the regex returns 400 `invalid_handle`; assert with a handle that 404s on `getChat` returns 400 `channel_not_found_on_telegram`; assert `GET /api/v2/channels` returns only the authenticated user's channels; assert `DELETE /api/v2/channels/{id}` returns 204 and removes the row for the authenticated user only
- [ ] T047 [P] [US2] Write `backend/tests/isolation_test.go` — assert user A cannot delete user B's user_channels row (gets 404); assert user A's `GET /api/v2/channels` does not include user B's subscriptions

### Implementation for User Story 2

- [ ] T048 [P] [US2] Implement `POST /api/v2/channels/subscribe` handler in `backend/internal/adminapi/v2/channels_handlers.go` — validates the handle regex, calls `telegram.Fetcher.ValidateHandle` (reusing the v1 helper at `internal/telegram/fetcher.go:21`), creates the `channels` catalog row if missing (`ChannelRepo.Add` with the new `telegram_id` arg), creates the `user_channels` row, records `channel.subscribed` op event, returns the `channel` JSON
- [ ] T049 [P] [US2] Implement `GET /api/v2/channels` handler in `backend/internal/adminapi/v2/channels_handlers.go` — calls `UserChannelRepo.ListByUser` for the authenticated user; for each entry resolves the effective filter via `UserFilterRepo.ResolveFor` (returns nil for US2 — the filter is always nil until US3); returns `{channels: [{...channel, filter: null|...}]}`
- [ ] T050 [P] [US2] Implement `DELETE /api/v2/channels/{channelId}` handler in `backend/internal/adminapi/v2/channels_handlers.go` — calls `UserChannelRepo.Unsubscribe` with the user-scoped check; returns 204 on success, 404 if the user does not have this channel; records `channel.unsubscribed` op event
- [ ] T051 [P] [US2] Update `frontend/src/routes/channels/+page.svelte` — fetch `/api/v2/channels` on mount; render the list with handle, display name, status, and an empty-state message; render a form with handle input + "Add" button
- [ ] T052 [US2] Add a per-user channel delete button in `frontend/src/routes/channels/+page.svelte` — confirm dialog, call `DELETE /api/v2/channels/{id}`, refresh the list on success

**Checkpoint**: User Story 2 is fully functional. A logged-in user can add a channel, see it in their list, and remove it. The cycle still does not run (no filter is set, but with no filter, every post would match — the cycle is gated on US3 to avoid delivering unfiltered content).

---

## Phase 5: User Story 3 - Set a Custom AI-Powered Filter on a Subscribed Channel (Priority: P1)

**Goal**: A logged-in user can configure a per-channel filter (keywords, natural-language prompt, or single category) and see the rule reflected in their channel list and filter editor.

**Independent Test**: Subscribe to a known channel, set a keyword filter, observe that matching messages are delivered and non-matching messages are not. With the per-user fan-out wired in US4, a full end-to-end is possible; in isolation, US3 is testable by inspecting the `user_filters` rows after a `POST /api/v2/filters/set`.

### Tests for User Story 3

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T053 [P] [US3] Write `backend/internal/store/sqlite/user_filters_test.go` — assert `ResolveFor` returns `custom_filter_id` first when set on `user_channels`; falls back to per-channel `is_active=1` filter; falls back to default (`channel_id IS NULL`); returns nil when none exist; assert `Set` rejects empty keywords; rejects an empty ai_prompt; rejects a category name not in the global set
- [ ] T054 [P] [US3] Write `backend/internal/adminapi/v2/filters_handlers_test.go` — assert `POST /api/v2/filters/set` with `channel_id` and `filter_type=keywords` creates a per-channel filter; with `channel_id=null` and `filter_type=ai_prompt` creates a default filter; with a malformed `filter_type` returns 400; assert `GET /api/v2/channels/{id}/filters` returns the channel-specific filters; assert `GET /api/v2/filters?default=true` returns the default filter; assert `PATCH /api/v2/filters/{id}` updates `filter_value` and `is_active`; assert `DELETE /api/v2/filters/{id}` removes the row
- [ ] T055 [P] [US3] Write `backend/internal/ai/summarizer_apply_test.go` — assert `fake.go` `ApplyUserFilter` returns `match=false` when the prompt substring is absent from the post; returns `match=true` with a tailored summary when present; assert `openai.go` and `anthropic.go` produce the right shape (mocked transport); assert `ErrUnavailable` from the AI is treated as a degraded fallback by the cycle (deferred to US4 test)
- [ ] T056 [P] [US3] Write `backend/internal/digest/textmatch_test.go` — assert `MatchKeywords` does case-insensitive substring match against text + captions; returns false for media-only posts with no caption; returns true for posts whose caption contains the keyword

### Implementation for User Story 3

- [ ] T057 [P] [US3] Add `UserFilterInput`, `UserFilterOutput` types and `ApplyUserFilter` method to the `Summarizer` interface in `backend/internal/ai/summarizer.go` per `research.md` R1 of `plan.md`'s design — the new method takes `(ctx, UserFilterInput) (UserFilterOutput, error)` where `UserFilterInput` is `{Post Input, GlobalSummary string, Prompt string}` and `UserFilterOutput` is `{Match bool, Summary string, Confidence float64}`
- [ ] T058 [P] [US3] Implement `ApplyUserFilter` in `backend/internal/ai/fake.go` — substring match `Prompt` against `Post.Text` + `Post.Captions`; on match, return `{true, GlobalSummary, 0.9}`; on no match, return `{false, "", 0}`
- [ ] T059 [P] [US3] Implement `ApplyUserFilter` in `backend/internal/ai/openai.go` — chat-completions call with a system prompt of the form `"You are a personal news assistant. The user wants only messages matching this rule: <prompt>. Given the post below, decide if it matches. If yes, also produce a one-line summary tailored to what the user cares about. If no, return match=false and summary=''. Respond in JSON: {match, summary, confidence}."`; parse the JSON response; map provider errors to the v1 sentinel set
- [ ] T060 [P] [US3] Implement `ApplyUserFilter` in `backend/internal/ai/anthropic.go` — Messages API call with the same system prompt and JSON output shape; reuse the v1 Anthropic client's transport
- [ ] T061 [P] [US3] Implement `POST /api/v2/filters/set` handler in `backend/internal/adminapi/v2/filters_handlers.go` — validates `filter_type` ∈ {`keywords`, `ai_prompt`, `category`}; for `keywords` enforces 1–2000 chars and at least one non-whitespace, non-comma char; for `ai_prompt` enforces 1–4000 chars; for `category` verifies the name exists in `categories` (case-insensitive); when `channel_id` is non-null, verifies the user follows that channel; calls `UserFilterRepo.Set`; returns the filter JSON
- [ ] T062 [P] [US3] Implement `GET /api/v2/channels/{channelId}/filters` handler — calls `UserFilterRepo.ListByChannel`; returns `{filters: [...]}`
- [ ] T063 [P] [US3] Implement `GET /api/v2/filters` handler with `?default=true` query — calls `UserFilterRepo.ListByUser` filtered by `channel_id IS NULL`; returns the default filter
- [ ] T064 [P] [US3] Implement `PATCH /api/v2/filters/{filterId}` handler — only allows updating `filter_value` and `is_active`; rejects changes to `filter_type` (return 400 `cannot_change_type`); calls `UserFilterRepo.Set`; returns the updated filter
- [ ] T065 [P] [US3] Implement `DELETE /api/v2/filters/{filterId}` handler — calls `UserFilterRepo.Delete`; returns 204; records `filter.deleted` op event
- [ ] T066 [P] [US3] Add `MatchKeywords(text string, captions []string, keywords []string) bool` helper in `backend/internal/digest/textmatch.go` — case-insensitive substring match against text first, then each caption
- [ ] T067 [P] [US3] Create `FilterEditor.svelte` in `frontend/src/lib/components/` — three-mode editor (keywords / ai_prompt / category) with a textarea, an active toggle, and a save button; renders the current filter value when editing
- [ ] T068 [US3] Create `frontend/src/routes/channels/[id]/filters/+page.svelte` — on mount, fetch `/api/v2/channels/{id}/filters` and `/api/v2/categories`; render the filter editor and the category dropdown; show the resolved effective filter (per `data-model.md` precedence) at the top
- [ ] T069 [P] [US3] Add a "Manage filters" link per channel in `frontend/src/routes/channels/+page.svelte` — clicking navigates to `/channels/{id}/filters`

**Checkpoint**: User Story 3 is fully functional. A logged-in user can set, edit, and delete filters for a channel they follow. Filters are persisted in `user_filters` and resolved correctly per the precedence rule. The cycle does not yet use the filter for delivery — that's US4.

---

## Phase 6: User Story 4 - Receive a Filtered Message in Their Telegram Chat (Priority: P2)

**Goal**: A logged-in user receives a Telegram message from the service's bot when a source-channel post matches their filter. The per-user fan-out is the headline new capability.

**Independent Test**: Set up a source channel that posts a known message, configure a user with a permissive filter on that channel, and verify the user receives a Telegram message in their private chat within one delivery cycle.

### Tests for User Story 4

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T070 [P] [US4] Write `backend/tests/cycle_fanout_test.go` — one source-channel post + three users with three different filter types (keywords, ai_prompt, category), assert each user receives the right message and the per-user `user_deliveries` rows are correct (`sent` for matching, `no_match` for non-matching, `filtered_out` for media-only with no caption against a keyword filter)
- [ ] T071 [P] [US4] Write `backend/tests/cycle_overrun_test.go` — assert the cycle stops after `MAX_CYCLE_DURATION` and marks unfinished deliveries as `send_failed` with `send_error='cycle_overrun'`; assert the next cycle picks them up
- [ ] T072 [P] [US4] Write `backend/tests/cycle_blocked_test.go` — assert that when `telegram.Real.SendMessage` returns `ErrBlocked` for user A, user A's `user_deliveries` row is `send_failed` with `send_error='blocked'` and a "stop sending" flag is set on the user; user B in the same cycle is unaffected
- [ ] T073 [P] [US4] Write `backend/tests/cycle_ai_degraded_test.go` — assert that when `Summarizer.ApplyUserFilter` returns `ErrUnavailable`, the cycle falls back to a degraded per-user delivery (global summary + `[best effort — AI unavailable]` prefix) with `status='sent'` and `send_error='ai_degraded'`
- [ ] T074 [P] [US4] Write `backend/internal/digest/render_test.go` golden-file test for `PerUserRender` — assert the per-message format is `summary\n\n— display (@handle)\nlink`; assert the bundled format produces the v1 category-grouped header/footer; assert MarkdownV2 escaping
- [ ] T075 [P] [US4] Write `backend/internal/adminapi/v2/deliveries_handlers_test.go` — assert `GET /api/v2/deliveries` returns the authenticated user's history; with `?status=send_failed` filters correctly; with `?limit=200&offset=0` paginates; assert `GET /api/v2/deliveries/{id}` returns 404 for another user's delivery

### Implementation for User Story 4

- [ ] T076 [US4] Modify `Summarizer` interface in `backend/internal/ai/summarizer.go` — add `ApplyUserFilter(ctx, UserFilterInput) (UserFilterOutput, error)` method (depends on T057)
- [ ] T077 [US4] Add `ListReceivedForFanout` method to `PostRepo` in `backend/internal/store/store.go` and implement in `backend/internal/store/sqlite/posts.go` — returns posts with `status='received'`, oldest first, capped at `limit` (default 500)
- [ ] T078 [US4] Add `ListUnsentByUser` method to `UserDeliveryRepo` in `backend/internal/store/store.go` and implement in `backend/internal/store/sqlite/user_deliveries.go` — returns `user_deliveries` rows for the user where `status='send_failed'` and `send_error='cycle_overrun'`, oldest first
- [ ] T079 [US4] Rewrite the cycle in `backend/internal/digest/cycle.go` per `research.md` R3 + R4 — the new flow is (1) fetch from each active channel and upsert posts, (2) global summarize each `received` post (concurrency 8, unchanged), (3) per-user fan-out: for each post, list `user_channels` for that channel, for each subscriber call `UserFilterRepo.ResolveFor` and `Summarizer.ApplyUserFilter` (concurrency 16), (4) per-user send with `perPostSendGap` and `PER_CYCLE_DELIVERY_CAP`, (5) record `user_deliveries` per outcome
- [ ] T080 [P] [US4] Add `PerUserRender(post store.Post, f *store.UserFilter, summary string) string` to `backend/internal/digest/render.go` — produces the per-message body per `contracts/telegram-render-v2.md` "Per-user single-message format"; escapes MarkdownV2
- [ ] T081 [P] [US4] Add `RenderUserBundle(...)` to `backend/internal/digest/render.go` — produces the bundled-mode text for one user for one cycle, respecting Telegram's 4096-char cap and splitting deterministically per the contract
- [ ] T082 [US4] Modify the v1 cycle's per-post send path — remove the v1 `per_post_cycle.go` (delete the file) and inline the per-user send loop in the rewritten `cycle.go`; the per-post gap is now per-(user, post) not per-post
- [ ] T083 [P] [US4] Modify the v1 Telegram real client's `onSubscriberChat` callback in `backend/internal/telegram/real.go` — remove the callback; the chat id is now learned from the Login Widget payload and stored on the `users` row
- [ ] T084 [P] [US4] Add the new per-user op-event kinds to the cycle's audit logging: `user.created`, `user.login`, `user.logout`, `channel.subscribed`, `channel.unsubscribed`, `filter.set`, `filter.deleted`, `delivery.sent`, `delivery.filtered_out`, `delivery.no_match`, `delivery.send_failed`, `delivery.cycle_overrun`, `auth.nonce_replayed`, `auth.session_revoked`
- [ ] T085 [P] [US4] Implement `GET /api/v2/deliveries` handler in `backend/internal/adminapi/v2/deliveries_handlers.go` — calls `UserDeliveryRepo.ListByUser` with `limit`/`offset`; joins each row with its `posts` row for `channel_handle`, `channel_display_name`, `link`, etc.; filters by `status` when present
- [ ] T086 [P] [US4] Implement `GET /api/v2/deliveries/{id}` handler in `backend/internal/adminapi/v2/deliveries_handlers.go` — calls `UserDeliveryRepo.Get`-equivalent and joins with `posts` and `user_filters`; returns 404 if the delivery belongs to a different user
- [ ] T087 [P] [US4] Create `DeliveryHistoryList.svelte` in `frontend/src/lib/components/` — renders a list of deliveries with channel, summary, sent_at, status badge
- [ ] T088 [US4] Create `frontend/src/routes/history/+page.svelte` — fetch `/api/v2/deliveries?limit=50` on mount; render the DeliveryHistoryList; support a status filter dropdown; support pagination via `?offset=`
- [ ] T089 [P] [US4] Add a per-user "stop sending" flag (call it `is_sending_paused`) to the `users` row via migration 0005; cycle checks this flag before sending; `POST /api/v2/admin/users/{id}/unpause-sending` (operator endpoint, future iteration) clears it

**Checkpoint**: User Story 4 is fully functional. The end-to-end multi-user loop works: a user subscribes to a channel, sets a filter, the cycle fetches the channel's posts, the AI evaluates each post per the user's filter, matching posts are sent to the user's Telegram chat, the user's history reflects the delivery.

---

## Phase 7: User Story 5 - Manage Personal Settings (Priority: P3)

**Goal**: A logged-in user can change the cadence at which they receive deliveries, customize the uncategorized label, and pick between bundled and per-post delivery formats.

**Independent Test**: Change the digest interval in the user's settings and observe the new cadence take effect on the next cycle.

### Tests for User Story 5

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [ ] T090 [P] [US5] Write `backend/internal/store/sqlite/user_settings_test.go` — assert `Get` returns a freshly-seeded default row for a new user; assert `Update` rejects an interval outside [60, 86400] with `ErrInvalidInterval`; rejects an empty `uncategorized_label`; rejects an invalid `delivery_mode`
- [ ] T091 [P] [US5] Write `backend/internal/adminapi/v2/user_settings_handlers_test.go` — assert `PATCH /api/v2/me/settings` with a valid body updates the row and returns the new settings; with an out-of-range interval returns 400; with an empty uncategorized label returns 400; with a malformed delivery_mode returns 400
- [ ] T092 [P] [US5] Write `backend/tests/cycle_per_user_interval_test.go` — assert that two users with different `digest_interval_seconds` see different cycle cadences (cycle per user, NOT per service; the v1 single-scheduler is replaced by a per-user scheduler — see implementation T093)

### Implementation for User Story 5

- [ ] T093 [US5] Replace the v1 single-scheduler fan-out in `backend/internal/digest/cycle.go` with a per-user scheduler — instead of one cycle ticking every `DigestIntervalSeconds`, the service iterates `UserRepo.ListActive` and fires one cycle per user when the user's per-user interval has elapsed; the global fetch and global summarize steps are still shared (one call per channel per cycle), but the per-user fan-out respects each user's cadence
- [ ] T094 [P] [US5] Add `Settings` field to `UserSettingsUpdate` struct in `backend/internal/store/store.go` — adds `DigestIntervalSeconds *int`, `UncategorizedLabel *string`, `DeliveryMode *DeliveryMode`; all partial-update fields
- [ ] T095 [P] [US5] Implement `PATCH /api/v2/me/settings` handler in `backend/internal/adminapi/v2/user_settings_handlers.go` — validates the input (`DigestIntervalSeconds` ∈ [60, 86400], `UncategorizedLabel` non-empty, length ≤ 40, `DeliveryMode` ∈ {`bundled`, `per_post`}), calls `UserSettingsRepo.Update`, returns the updated settings
- [ ] T096 [P] [US5] Update `frontend/src/routes/settings/+page.svelte` — fetch `/api/v2/me/settings` on mount; render a form with the three fields; submit calls `PATCH /api/v2/me/settings`; show a success toast on 200
- [ ] T097 [P] [US5] Add `me` API client methods to `frontend/src/lib/api.ts` for `GET /api/v2/me` and `PATCH /api/v2/me/settings`

**Checkpoint**: User Story 5 is fully functional. A logged-in user can change their per-user interval, uncategorized label, and delivery format from the settings page, and the changes take effect on the next cycle.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Documentation, end-to-end validation, and security hardening that affect every user story.

- [ ] T098 [P] Run `make quickstart-multi-user` end-to-end per `quickstart.md` Track A; document the run in `specs/002-multi-user-saas/quickstart-validation.md`
- [ ] T099 [P] Run the 1,000-user load test from `quickstart.md` "Mapping back to the spec" — assert zero dropped deliveries, p95 cycle duration < 60s, no `cycle_overrun` rows; record results in `specs/002-multi-user-saas/loadtest-results.md`
- [ ] T100 [P] Update `README.md` — replace the v1 single-admin sections with multi-user sections; add the `TELEGRAM_BOT_USERNAME` and `JWT_SECRET` env vars; add the `make quickstart-multi-user`, `make backup-before-migration`, `make loadtest-multi-user` targets
- [ ] T101 [P] Update `backend/internal/config/config.go` `Validate` — assert `JWT_SECRET` is non-empty (32+ chars); assert `TELEGRAM_BOT_USERNAME` is non-empty and starts with a letter; return a fatal error at startup if either is missing
- [ ] T102 [P] Add operator-only endpoint `GET /api/v2/admin/users` (paginated) and `POST /api/v2/admin/users/{id}/pause-sending` / `unpause-sending` in a new `backend/internal/adminapi/v2/admin_handlers.go`; gate behind a new `OPERATOR_TELEGRAM_ID` env var (the user whose `telegram_id` matches gets operator privileges)
- [ ] T103 [P] Add an "ops" dashboard page in `frontend/src/routes/admin/+page.svelte` — visible only to the operator; shows recent op_events, per-user cycle counts, and per-channel fetch health
- [ ] T104 [P] Add the `BACKUP_BEFORE_MIGRATIONS=1` auto-backup path to `cmd/assistant/main.go` — when the env var is set, before applying the migration, copy the SQLite file to `<DB_PATH>.pre-<migration_version>` next to the original
- [ ] T105 [P] Add unit tests in `backend/internal/auth/nonce_test.go` for nonce creation and consumption
- [ ] T106 [P] Add `golangci-lint` rules to catch common v1 leftovers — the v1 `AdminPassword`, `telegram_subscriber_chat`, `digest_items` references should fail the lint
- [ ] T107 [P] Add a `cybersecurity-self-test.md` documenting the cross-user access tests run in `tests/isolation_test.go` (T047) and the JWT revocation tests (T032) — operators can rerun these as a smoke after a security change
- [ ] T108 [P] Add Sentry-style structured-logging correlation for the v2 cycle's per-user fan-out — every per-user delivery record includes the `cycle_id`, the `user_id`, the `post_id`, and the `filter_id` in log lines
- [ ] T109 Update `AGENTS.md` if the plan changes (the file already points to the new plan from Phase 2)
- [ ] T110 [P] Final verification: `make test-backend` (Go tests) + `make test-frontend` (Vitest) + `make lint` (golangci-lint + ESLint) all green

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately.
- **Foundational (Phase 2)**: Depends on Setup (Phase 1). **BLOCKS all user stories.** Every user story needs the migration applied, the auth package, and the JWT middleware.
- **User Stories (Phases 3-7)**: All depend on Foundational (Phase 2) completion. Stories are sequenced by priority (P1 → P1 → P1 → P2 → P3) but the work is parallelizable once each story's prerequisites are met (US1 needs the auth package; US2 needs the SPA layout from US1; US3 needs the channels page from US2; US4 needs the filter editor from US3; US5 needs the me endpoint from US1).
- **Polish (Phase 8)**: Depends on all desired user stories being complete.

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) — no dependencies on other stories. **MVP scope.**
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) — depends on the SPA layout from US1 for the channels page route, but is independently testable via the v2 API + cURL.
- **User Story 3 (P1)**: Can start after Foundational (Phase 2) — depends on the channels API from US2 to verify the user follows a channel before accepting a filter, but is independently testable via the v2 API + cURL.
- **User Story 4 (P2)**: Can start after Foundational (Phase 2) — depends on US1, US2, US3 for the full data path, but the cycle rewrite can land as a single PR that integrates all three.
- **User Story 5 (P3)**: Can start after Foundational (Phase 2) — depends on the `me` endpoint from US1 for the form, but is independently testable via the v2 API.

### Within Each User Story

- Tests MUST be written and FAIL before implementation (TDD).
- Models before services before handlers.
- Backend handlers before frontend components.
- Backend integration before frontend integration.
- Story complete before moving to the next priority.

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel (T002-T007 are independent files).
- All Foundational repository tasks marked [P] can run in parallel (T010-T019 are different files in `internal/store/sqlite/`).
- The auth package tasks (T020-T023) can run in parallel with each other but depend on T001 (the package skeleton).
- The SPA login page tasks (T026-T030) can run in parallel with the backend auth tasks.
- Within each user story, all test tasks marked [P] can run in parallel.
- Different user stories can be worked on in parallel by different team members once the Foundational phase is complete.

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together:
Task: "T031 [P] [US1] Write backend/internal/auth/auth_test.go table-driven HMAC verification tests"
Task: "T032 [P] [US1] Write backend/internal/auth/jwt_test.go"
Task: "T033 [P] [US1] Write backend/internal/auth/middleware_test.go"
Task: "T034 [P] [US1] Write backend/tests/auth_telegram_test.go"
Task: "T035 [P] [US1] Write backend/internal/store/sqlite/users_test.go"
Task: "T036 [P] [US1] Write frontend/src/routes/login/+page.test.ts"

# Launch all handler implementations together:
Task: "T037 [P] [US1] Implement POST /api/v2/auth/telegram handler in backend/internal/adminapi/v2/auth_handlers.go"
Task: "T038 [P] [US1] Implement POST /api/v2/auth/webapp handler in backend/internal/adminapi/v2/auth_handlers.go"
Task: "T039 [US1] Implement POST /api/v2/auth/logout handler"
Task: "T040 [US1] Implement GET /api/v2/auth/status handler"
Task: "T041 [US1] Implement GET /api/v2/me handler"
```

---

## Parallel Example: User Story 4

```bash
# Launch all cycle tests together:
Task: "T070 [P] [US4] Write backend/tests/cycle_fanout_test.go"
Task: "T071 [P] [US4] Write backend/tests/cycle_overrun_test.go"
Task: "T072 [P] [US4] Write backend/tests/cycle_blocked_test.go"
Task: "T073 [P] [US4] Write backend/tests/cycle_ai_degraded_test.go"

# Launch all render and repo additions together:
Task: "T077 [US4] Add ListReceivedForFanout to PostRepo"
Task: "T078 [US4] Add ListUnsentByUser to UserDeliveryRepo"
Task: "T080 [P] [US4] Add PerUserRender to backend/internal/digest/render.go"
Task: "T081 [P] [US4] Add RenderUserBundle to backend/internal/digest/render.go"
Task: "T085 [P] [US4] Implement GET /api/v2/deliveries handler"
Task: "T086 [P] [US4] Implement GET /api/v2/deliveries/{id} handler"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup (T001-T007)
2. Complete Phase 2: Foundational (T008-T030) — **CRITICAL — blocks all stories**
3. Complete Phase 3: User Story 1 (T031-T044)
4. **STOP and VALIDATE**: Test User Story 1 independently via Track A from `quickstart.md`
5. Deploy/demo if ready — the user can log in and see an empty dashboard, but cannot yet subscribe to channels, set filters, or receive deliveries.

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!) — user can log in
3. Add User Story 2 → Test independently → Deploy/Demo — user can subscribe to channels
4. Add User Story 3 → Test independently → Deploy/Demo — user can set filters (but the cycle still does not run for them)
5. Add User Story 4 → Test independently → Deploy/Demo — **the headline new capability**: users can receive filtered messages
6. Add User Story 5 → Test independently → Deploy/Demo — users can tune per-user settings
7. Each story adds value without breaking previous stories.

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together (T001-T030).
2. Once Foundational is done:
   - Developer A: User Story 1 (T031-T044) — login flow
   - Developer B: User Story 2 (T045-T052) — channels subscribe, in parallel with A
   - Developer C: User Story 3 (T053-T069) — filters, after B lands T045 (the user_channels repo)
3. After US1-US3 are merged, one developer takes US4 (T070-T089) — the cycle rewrite is large but self-contained.
4. A different developer takes US5 (T090-T097) — the per-user scheduler change is small and additive.
5. Polish (T098-T110) is a final cross-cutting pass.

---

## Notes

- [P] tasks = different files, no dependencies.
- [Story] label maps each task to the user story it serves.
- Each user story is independently completable and testable.
- Verify tests fail before implementing (TDD discipline).
- Commit after each task or logical group.
- Stop at any checkpoint to validate a story independently.
- The v1 `digest_items` table is dropped in T008; the cycle must not reference it after US4 lands.
- The cycle rewrite (T079, T082) is the highest-risk task in the project — review carefully and run the load test (T099) before declaring US4 done.
