# Data Model: Multi-User Telegram News Aggregator

**Feature**: 002-multi-user-saas
**Date**: 2026-06-23
**Purpose**: Translate the **Key Entities** in `spec.md` into a concrete relational schema, repository interfaces, and state-transition rules. This is the contract the Go `internal/store` package implements on top of the existing v1 schema.

## Storage engine

- **Engine**: SQLite (WAL mode) via `modernc.org/sqlite`. Unchanged from v1.
- **File**: same path as v1 (`./assistant.db` in dev, `/data/assistant.db` in the container). The migration is forward-only and runs at startup via the existing `Migrate` function in `backend/internal/store/migrate.go:23`.
- **Concurrency model**: a single writer (the cycle goroutine) for the per-user fan-out write path; many readers (the admin API) for the per-user list/read paths. The Go store layer serializes writes with a single internal mutex (unchanged from v1).
- **Migrations**: plain SQL files in `backend/internal/store/migrations/`, embedded via `//go:embed` and applied in lexical order at startup. The new migration for this feature is `0004_multi_user.sql`.

## Schema (new + modified in migration `0004_multi_user.sql`)

The v1 tables `categories`, `cycles`, `digests`, `posts`, `op_events` are **kept unchanged**; they are the global infrastructure the per-user model sits on top of. The v1 table `channels` is **modified** in two ways (it becomes a global catalog with a new `telegram_id` column and a unique handle index). The v1 table `settings` is **modified** (the `telegram_subscriber_chat`, `digest_interval_seconds`, `uncategorized_label`, `delivery_mode` columns are removed; the bot token and AI fields stay; a new `system_default_filter` column is added). The v1 table `digest_items` is **dropped** (its rows are migrated to `user_deliveries` before the drop).

```sql
-- ─── USERS ───────────────────────────────────────────────────────────────
CREATE TABLE users (
  id               TEXT PRIMARY KEY,             -- UUID
  telegram_id      INTEGER NOT NULL UNIQUE,      -- numeric Telegram user id from login widget
  username         TEXT,                          -- @handle, nullable
  first_name       TEXT NOT NULL,
  last_name        TEXT,                          -- nullable
  photo_url        TEXT,                          -- nullable
  telegram_chat_id INTEGER NOT NULL UNIQUE,      -- chat id the bot uses to send; equals telegram_id for DMs
  is_active        INTEGER NOT NULL DEFAULT 1    -- 1 = active, 0 = deactivated
                  CHECK (is_active IN (0, 1)),
  last_login_at    TEXT,                          -- ISO-8601 UTC, nullable
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);
CREATE INDEX idx_users_telegram_id ON users(telegram_id);
CREATE INDEX idx_users_active     ON users(is_active);

-- ─── PER-USER SETTINGS ────────────────────────────────────────────────────
-- One row per user. Operator-tunable fields from v1's singleton `settings`
-- move here. Defaults match v1.
CREATE TABLE user_settings (
  user_id                 TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  digest_interval_seconds INTEGER NOT NULL DEFAULT 600
                          CHECK (digest_interval_seconds BETWEEN 60 AND 86400),
  uncategorized_label     TEXT    NOT NULL DEFAULT 'Uncategorized',
  delivery_mode           TEXT    NOT NULL DEFAULT 'per_post'
                          CHECK (delivery_mode IN ('bundled','per_post')),
  updated_at              TEXT    NOT NULL
);

-- ─── CHANNEL CATALOG (modified from v1) ───────────────────────────────────
-- v1's `channels` table becomes a global catalog of distinct Telegram
-- channels the service has ever seen. The per-user watch-list is now
-- `user_channels`. New column: telegram_id (Telegram's numeric channel id).
ALTER TABLE channels ADD COLUMN telegram_id INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_channels_handle ON channels(handle);

-- ─── USER WATCH-LIST ──────────────────────────────────────────────────────
CREATE TABLE user_channels (
  id               TEXT PRIMARY KEY,              -- UUID
  user_id          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id       TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  custom_filter_id TEXT REFERENCES user_filters(id) ON DELETE SET NULL,
  created_at       TEXT NOT NULL,
  UNIQUE (user_id, channel_id)
);
CREATE INDEX idx_user_channels_user    ON user_channels(user_id);
CREATE INDEX idx_user_channels_channel ON user_channels(channel_id);

-- ─── USER FILTERS ─────────────────────────────────────────────────────────
-- Per-user, per-channel (or per-user default when channel_id IS NULL).
--   filter_type   = 'keywords' | 'ai_prompt' | 'category'
--   filter_value  = for 'keywords': "btc,eth,sec" (comma-separated, OR)
--                   for 'ai_prompt': "Only forward crypto news related to
--                                     Ethereum and ignore Bitcoin"
--                   for 'category':  "Technology"  (single category name)
CREATE TABLE user_filters (
  id          TEXT PRIMARY KEY,                   -- UUID
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id  TEXT REFERENCES channels(id) ON DELETE CASCADE,  -- NULL = default for this user
  filter_type TEXT NOT NULL
              CHECK (filter_type IN ('keywords','ai_prompt','category')),
  filter_value TEXT NOT NULL,
  is_active   INTEGER NOT NULL DEFAULT 1
              CHECK (is_active IN (0, 1)),
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  UNIQUE (user_id, channel_id, filter_type)
);
CREATE INDEX idx_user_filters_user    ON user_filters(user_id);
CREATE INDEX idx_user_filters_channel ON user_filters(channel_id) WHERE channel_id IS NOT NULL;

-- ─── GLOBAL SETTINGS STRIP-DOWN (modified from v1) ────────────────────────
-- v1's `settings` row is reduced to operator-only knobs: the bot token
-- ref and AI provider credentials. Per-subscriber knobs move to
-- user_settings. A `system_default_filter` is added so the operator can
-- ship a catch-all filter that all new users inherit.
CREATE TABLE settings (
  id                      TEXT PRIMARY KEY CHECK (id = 'singleton'),
  telegram_bot_token_ref  TEXT NOT NULL,
  ai_provider             TEXT NOT NULL,
  ai_model                TEXT NOT NULL,
  ai_api_key_ref          TEXT NOT NULL,
  ai_base_url             TEXT NOT NULL,
  system_default_filter   TEXT,                   -- nullable; same format as user_filters.filter_value
  updated_at              TEXT NOT NULL
);

-- ─── PER-USER DELIVERY RECORDS ───────────────────────────────────────────
-- Replaces v1's `digest_items` table. One row per (user × post × cycle).
-- Cross-cycle retries update the same row in place.
CREATE TABLE user_deliveries (
  id              TEXT PRIMARY KEY,               -- UUID
  user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id         TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  cycle_id        TEXT NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
  status          TEXT NOT NULL                   -- 'sent' | 'filtered_out' | 'send_failed' | 'no_match'
                  CHECK (status IN ('sent','filtered_out','send_failed','no_match')),
  filter_id       TEXT REFERENCES user_filters(id) ON DELETE SET NULL,
  summary         TEXT,                           -- per-user tailored summary (may differ from posts.summary)
  confidence      REAL,                           -- AI's match confidence
  telegram_msg_id INTEGER,                        -- set when status='sent'
  send_error      TEXT,                           -- set when status='send_failed'
  sent_at         TEXT,                           -- set when status='sent'
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  UNIQUE (user_id, post_id, cycle_id)
);
CREATE INDEX idx_user_deliveries_user     ON user_deliveries(user_id);
CREATE INDEX idx_user_deliveries_post     ON user_deliveries(post_id);
CREATE INDEX idx_user_deliveries_status   ON user_deliveries(status);
CREATE INDEX idx_user_deliveries_cycle    ON user_deliveries(cycle_id);

-- ─── SESSIONS (revocable JWT tracking) ───────────────────────────────────
-- A row in this table is the source of truth for revocation; the JWT
-- carries the same `jti` (session id) as a claim.
CREATE TABLE user_sessions (
  id           TEXT PRIMARY KEY,                  -- UUID; matches JWT 'jti' claim
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  issued_at    TEXT NOT NULL,
  expires_at   TEXT NOT NULL,
  revoked_at   TEXT,                               -- nullable; non-null = revoked
  user_agent   TEXT,                               -- nullable; for the audit log
  ip           TEXT                                -- nullable
);
CREATE INDEX idx_user_sessions_user     ON user_sessions(user_id);
CREATE INDEX idx_user_sessions_expires  ON user_sessions(expires_at);

-- ─── TELEGRAM AUTH NONCE STORE ───────────────────────────────────────────
-- Protects against replay of a captured login payload. Each successful
-- /api/v2/auth/telegram consumes the nonce.
CREATE TABLE auth_nonces (
  nonce       TEXT PRIMARY KEY,
  created_at  TEXT NOT NULL,
  consumed_at TEXT                                 -- set after first successful use
);
CREATE INDEX idx_auth_nonces_created ON auth_nonces(created_at);
```

### v1 → v2 data migration (in `0004_multi_user.sql`, after the table creations)

The migration also runs the following forward-only data moves in the same transaction. They are best-effort: a failure aborts the migration and leaves the DB at the v1 schema.

```sql
-- 1. If v1's `settings.telegram_subscriber_chat` was set, create a
--    synthetic "v1 admin" user and grant it ownership of the v1 channels
--    and digest history. The synthetic user can be renamed by the
--    operator later; the display name is visually distinct.
INSERT INTO users (id, telegram_id, telegram_chat_id, first_name, is_active, created_at, updated_at)
SELECT 'v1-admin', telegram_subscriber_chat, telegram_subscriber_chat,
       'Imported (v1)', 1, datetime('now'), datetime('now')
FROM settings WHERE telegram_subscriber_chat != 0;

INSERT INTO user_settings (user_id) VALUES ('v1-admin')
  ON CONFLICT DO NOTHING;

INSERT INTO user_channels (id, user_id, channel_id, created_at)
SELECT 'uc-' || c.id, 'v1-admin', c.id, datetime('now')
FROM channels c
WHERE EXISTS (SELECT 1 FROM users WHERE id = 'v1-admin');

-- 2. Migrate v1 `digest_items` rows to `user_deliveries` (status='sent'
--    if the parent digest was 'ok', else status='send_failed').
INSERT INTO user_deliveries (id, user_id, post_id, cycle_id, status,
                              summary, confidence, telegram_msg_id, sent_at,
                              created_at, updated_at)
SELECT 'ud-' || di.id, 'v1-admin', di.post_id, di.cycle_id,
       CASE WHEN (SELECT d.send_status FROM digests d WHERE d.cycle_id = di.cycle_id) = 'ok'
            THEN 'sent' ELSE 'send_failed' END,
       di.summary, di.confidence,
       (SELECT d.telegram_msg_id FROM digests d WHERE d.cycle_id = di.cycle_id),
       (SELECT d.sent_at FROM digests d WHERE d.cycle_id = di.cycle_id),
       datetime('now'), datetime('now')
FROM digest_items di
WHERE di.post_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM users WHERE id = 'v1-admin');

-- 3. Drop the v1 admin-only fields from `settings`:
--    telegram_subscriber_chat, digest_interval_seconds, uncategorized_label,
--    delivery_mode. The remaining operator-only fields stay.
--    (Implemented via ALTER TABLE ... DROP COLUMN; the exact column list
--    is dropped in the same transaction.)
ALTER TABLE settings DROP COLUMN telegram_subscriber_chat;
ALTER TABLE settings DROP COLUMN digest_interval_seconds;
ALTER TABLE settings DROP COLUMN uncategorized_label;
ALTER TABLE settings DROP COLUMN delivery_mode;
ALTER TABLE settings ADD COLUMN system_default_filter TEXT;

-- 4. Drop the v1 `digest_items` table after the migration above.
DROP TABLE digest_items;
```

## Repository interfaces (Go)

All interfaces live in `internal/store`. The concrete implementation lives under `internal/store/sqlite`. The cycle and the v2 admin API depend only on the interfaces.

```go
// UserRepo persists the SaaS's user records.
type UserRepo interface {
    // Get returns one user by id, or ErrNotFound.
    Get(ctx context.Context, id string) (User, error)

    // GetByTelegramID returns one user by Telegram user id, or ErrNotFound.
    GetByTelegramID(ctx context.Context, telegramID int64) (User, error)

    // UpsertFromLogin inserts a new user (and a default user_settings row)
    // or updates the display fields of an existing user, keyed on
    // telegram_id. If a system_default_filter is configured, the new user
    // inherits it as a user_filters row with channel_id=NULL.
    UpsertFromLogin(ctx context.Context, in LoginPayload) (User, bool, error)  // (user, created, err)

    // SetActive toggles the is_active flag (operator action).
    SetActive(ctx context.Context, id string, active bool) error

    // List returns all active users (cycle's fan-out input).
    ListActive(ctx context.Context, limit, offset int) ([]User, error)
}

// UserSettingsRepo persists per-user operator-tunable fields.
type UserSettingsRepo interface {
    Get(ctx context.Context, userID string) (UserSettings, error)
    Update(ctx context.Context, userID string, u UserSettingsUpdate) (UserSettings, error)
}

// UserChannelRepo persists the per-user watch-list.
type UserChannelRepo interface {
    ListByUser(ctx context.Context, userID string) ([]UserChannel, error)
    ListByChannel(ctx context.Context, channelID string) ([]UserChannel, error)
    Subscribe(ctx context.Context, userID, channelID string) (UserChannel, error)
    Unsubscribe(ctx context.Context, userID, channelID, userChannelID string) error
}

// UserFilterRepo persists per-user filter rules.
type UserFilterRepo interface {
    ListByUser(ctx context.Context, userID string) ([]UserFilter, error)
    ListByChannel(ctx context.Context, userID, channelID string) ([]UserFilter, error)
    Get(ctx context.Context, id string) (UserFilter, error)

    // ResolveFor returns the effective filter for a (user, channel), or
    // nil if the user has no filter. Precedence: user_channels.custom_filter_id
    // → per-channel filter (is_active=1) → user default filter (channel_id IS
    // NULL, is_active=1). See research.md R5.
    ResolveFor(ctx context.Context, userID, channelID string) (*UserFilter, error)

    Set(ctx context.Context, f UserFilter) (UserFilter, error)
    Delete(ctx context.Context, id string) error
    SetActive(ctx context.Context, id string, active bool) error
}

// UserDeliveryRepo persists per-(user × post × cycle) delivery records.
type UserDeliveryRepo interface {
    Insert(ctx context.Context, d UserDelivery) error
    ListByUser(ctx context.Context, userID string, limit, offset int) ([]UserDelivery, error)
    ListByCycle(ctx context.Context, cycleID string) ([]UserDelivery, error)
    ListUnsentByUser(ctx context.Context, userID string, limit int) ([]UserDelivery, error)
    MarkSent(ctx context.Context, id string, telegramMsgID int64) error
    MarkSendFailed(ctx context.Context, id string, errMsg string) error
}

// UserSessionRepo persists the source-of-truth for JWT revocation.
type UserSessionRepo interface {
    Create(ctx context.Context, s UserSession) error
    Get(ctx context.Context, jti string) (UserSession, error)
    Revoke(ctx context.Context, jti string) error
    PurgeExpired(ctx context.Context) (int, error)
}

// AuthNonceRepo persists login nonces to prevent replay.
type AuthNonceRepo interface {
    Create(ctx context.Context, nonce string) error
    Consume(ctx context.Context, nonce string) error  // returns ErrNonceReplayed if already consumed
    PurgeExpired(ctx context.Context, olderThan time.Time) (int, error)
}
```

### Repository interfaces kept from v1 (unchanged)

The v1 `ChannelRepo`, `CategoryRepo`, `SettingsRepo`, `CycleRepo`, `DigestRepo`, `CursorRepo`, `HealthRepo`, `PostRepo` interfaces all stay. The only modification is to `ChannelRepo`: the `Channel` struct gains a `TelegramID int64` field (and the new SQLite index), and `Add(ctx, handle, displayName, telegramID)` gains the new argument.

## Entity reference (aligned with `spec.md`)

| Spec entity | DB rows | Notes |
|---|---|---|
| **User** | `users` | One row per Telegram user that has logged in. Keyed by `telegram_id` (immutable). |
| **User Session** | `user_sessions` | One row per successful login. The JWT's `jti` claim is the row id. `revoked_at` is the source of truth for revocation. |
| **User Settings** | `user_settings` | One row per user. Carries the per-user digest cadence, uncategorized label, and delivery format. |
| **Telegram Channel (catalog)** | `channels` | One row per distinct Telegram channel the service has ever seen. Shared across all users. The v1 row is repurposed: a new `telegram_id` column is added, and `UNIQUE (handle)` is enforced. |
| **User Channel (watch-list)** | `user_channels` | Many-to-many join between users and the channel catalog. The user's personal subscription. |
| **User Filter** | `user_filters` | Per-user, per-channel (or default). Precedence: `user_channels.custom_filter_id` → per-channel → default. |
| **Source Message** | `posts` | One row per unique (channel_id, source_msg_id). Lifecycle state is per-user below. |
| **Cycle** | `cycles` | One row per scheduled execution. Unchanged from v1. |
| **User Delivery** | `user_deliveries` | One row per (user × post × cycle). The per-user delivery history. Replaces v1's `digest_items`. |
| **Operational Event** | `op_events` | Ring-buffered audit log. Unchanged from v1; new event kinds for the v2 lifecycle. |
| **System Settings** | `settings` | Operator-only configuration: bot token ref, AI provider creds, system default filter. Per-subscriber knobs have moved to `user_settings`. |

## State transitions

### User lifecycle

```
            (no row)
                │
                │  /api/v2/auth/telegram (first time, valid hash)
                │  → UpsertFromLogin creates a user + user_settings + (optional) inherited filter
                ▼
            active (is_active = 1)
                │                  ▲
                │  operator action │  operator action
                ▼                  │
          inactive                │
          (is_active = 0)         │
                │                  │
                │  operator action │
                ▼                  │
              (deleted)  ◄─────────┘
                (operator triggers deletion via
                 DELETE /api/v2/admin/users/{id}
                 → CASCADE deletes user_settings, user_channels,
                   user_filters, user_sessions, user_deliveries;
                   channels catalog rows are NOT deleted)
```

### Filter lifecycle

```
            Add (user)                              SetActive=false (user)
   (none) ─────────────────► active ─────────────────────────► inactive
                               ▲                                       │
                               │  SetActive=true (user)                │  Delete (user)
                               └────────────────────────────────────── ┘
                                                                            │
                                                                            ▼
                                                                         (deleted)
```

- The user can have at most one default filter (channel_id IS NULL) and at most one filter per (channel, type).
- Inactive filters are skipped by `ResolveFor` and do not affect delivery.
- The `is_active` toggle is preserved across updates; deleting and re-adding the filter is the only way to re-enable a `Delete`d filter.

### User delivery lifecycle

```
                 (per post, per subscriber, per cycle)
                                 │
                                 │  Insert
                                 ▼
                  (one of: sent, filtered_out, no_match, send_failed)
                                 │
       ┌────────────────────┬────┴────┬─────────────────────┐
       │                    │         │                     │
   SendMessage           AI:match   AI:no-match        Send:error
       │                  =false                          │
       ▼                    │                             ▼
  status='sent'              ▼                       status='send_failed'
  + telegram_msg_id    status='no_match'           + send_error
  + sent_at            OR status='filtered_out'    (next cycle may retry)
                       (keyword filter rejected)
```

- A row's status is set once per (user, post, cycle). Cross-cycle retries update the existing row in place (or insert a new one for a different cycle_id).
- `no_match` is distinct from `filtered_out`: `no_match` is set when the filter model produced a hard "no" (e.g. ai_prompt match=false); `filtered_out` is set when the filter model was not invoked (e.g. a media-only post with no caption against a keyword filter — see research.md R6).

### Channel lifecycle (unchanged from v1)

The v1 lifecycle is kept: `active → inaccessible → banned → (none)`. The same op-event kinds apply (`channel.inaccessible`, `channel.banned`, `channel.fetch.ok`).

### Cycle lifecycle (unchanged from v1)

`pending → succeeded | degraded | failed | skipped_no_items`. The cycle's `output_items` column now means "per-user deliveries sent" (was "posts bundled" in v1).

## Validation rules (enforced in the store layer)

- **Telegram user id**: positive int64. Unique across all users. Set at first login and never changed.
- **Telegram handle** (for channel subscribe): must match `^[A-Za-z][A-Za-z0-9_]{3,31}[A-Za-z0-9]$`; leading `@` is stripped before storage and comparison. A handle already in the catalog reuses the existing `channels` row (the catalog is shared).
- **User filter value**:
  - For `keywords`: 1–2000 characters, must contain at least one non-whitespace, non-comma character; whitespace and commas are the separators.
  - For `ai_prompt`: 1–4000 characters, free text.
  - For `category`: must match an existing `categories.name` row (case-insensitive).
- **User settings interval**: 60 ≤ value ≤ 86400.
- **Cycle idempotency**: `user_deliveries.UNIQUE (user_id, post_id, cycle_id)` makes it safe to retry any step of the cycle without producing duplicates; the cycle uses `INSERT ... ON CONFLICT DO NOTHING` for `user_deliveries` so a partial replay is safe.
- **JWT revocation**: every parse looks up `user_sessions.jti = ? AND revoked_at IS NULL AND expires_at > now`. A revoked or expired `jti` returns 401 to the client.
- **Cross-user access**: every repo method that takes a `userID` argument scopes the SELECT/WHERE on that id. A handler that tries to read or modify another user's rows gets `ErrNotFound` (not `ErrForbidden` — we don't reveal whether the resource exists for another user).

## Initial seed data

- The new `user_sessions`, `user_deliveries`, `user_filters`, `user_channels`, `user_settings`, `auth_nonces` tables are empty.
- A synthetic v1 admin user is created by the migration if v1's `settings.telegram_subscriber_chat` was set (see "v1 → v2 data migration" above). Otherwise the migration is a no-op for users.
- The `settings` row is reseeded with the existing operator fields (bot token ref, AI provider creds) plus the new `system_default_filter = NULL`.
- The shipped category set (Politics, Technology, Business, Sports, World, Other) is kept from v1 and is global — every user sees the same set.

## Operational event kinds (audit log)

The v1 kinds are kept unchanged. New kinds added by v2:

| Kind | When |
|---|---|
| `user.created` | A new user row was created by `UpsertFromLogin`. |
| `user.login` | An existing user logged in (display name updated). |
| `user.logout` | A user revoked their session. |
| `user.inactive` | Operator deactivated a user. |
| `channel.subscribed` | A user added a channel to their watch-list. |
| `channel.unsubscribed` | A user removed a channel from their watch-list. |
| `filter.set` | A user created or updated a filter rule. |
| `filter.deleted` | A user deleted a filter rule. |
| `delivery.sent` | A per-user delivery was sent to Telegram (status='sent'). |
| `delivery.filtered_out` | A per-user delivery was filtered out by a keyword/category rule. |
| `delivery.no_match` | A per-user delivery was rejected by the AI (status='no_match'). |
| `delivery.send_failed` | A per-user send to Telegram failed. |
| `delivery.cycle_overrun` | A per-user send was deferred because the cycle hit the time cap. |
| `auth.nonce_replayed` | A login was rejected because the nonce had already been consumed. |
| `auth.session_revoked` | A JWT was rejected because the underlying `user_sessions` row was revoked or expired. |

## Migration policy

- Migrations are append-only SQL files in `backend/internal/store/migrations/`, named `NNNN_description.sql`. The v2 migration is `0004_multi_user.sql`.
- The store layer records applied migrations in a `schema_migrations` table (unchanged from v1).
- A failed migration aborts startup with the exact error; the DB is not touched.
- The `Makefile` adds a `make backup-before-migration` target that copies the SQLite file before applying `0004`. A `BACKUP_BEFORE_MIGRATIONS=1` env var gates the auto-backup; default off in dev, on in production.
