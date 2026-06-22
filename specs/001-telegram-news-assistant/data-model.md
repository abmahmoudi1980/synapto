# Data Model: Telegram News Digest Assistant

**Feature**: 001-telegram-news-assistant
**Date**: 2026-06-21 (post-queue added 2026-06-22)
**Purpose**: Translate the Key Entities in `spec.md` into a concrete relational schema, repository interfaces, and state-transition rules. This is the contract the Go `internal/store` package implements.

## Storage engine

- **Engine**: SQLite (WAL mode) via `modernc.org/sqlite`.
- **File**: a single file at the path given by `DB_PATH` (default `./assistant.db`).
- **Concurrency model**: a single writer (the cycle goroutine), many readers (the admin API). The Go store layer serializes writes with a single internal mutex to keep the WAL simple.
- **Migrations**: plain SQL files in `backend/migrations/`, embedded via `//go:embed` and applied in lexical order at startup. One initial migration: `0001_init.sql`.

## Schema (initial migration `0001_init.sql`)

All tables use `INTEGER PRIMARY KEY` (SQLite rowid) for the internal id, plus a string `id` (UUIDv4) for external references. Timestamps are stored as ISO-8601 UTC strings to keep the schema greppable.

```sql
-- Channels the subscriber wants monitored
CREATE TABLE channels (
  id              TEXT PRIMARY KEY,           -- UUID
  handle          TEXT NOT NULL UNIQUE,       -- e.g. "durov" or "@durov"; stored without leading "@"
  display_name    TEXT NOT NULL,              -- last known title from Telegram
  status          TEXT NOT NULL,              -- 'active' | 'inaccessible' | 'banned'
  last_seen_msg_id INTEGER NOT NULL DEFAULT 0, -- cursor: max Telegram message id seen
  last_observed_at TEXT,                      -- ISO-8601 UTC, nullable
  last_error      TEXT,                       -- nullable
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL
);
CREATE INDEX idx_channels_status ON channels(status);

-- Categories used to group digest items
CREATE TABLE categories (
  id           TEXT PRIMARY KEY,    -- UUID
  name         TEXT NOT NULL UNIQUE,
  ordering     INTEGER NOT NULL DEFAULT 0,
  is_default   INTEGER NOT NULL    -- 0 custom, 1 default (defaults cannot be deleted, only renamed)
                  CHECK (is_default IN (0, 1)),
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
CREATE INDEX idx_categories_ordering ON categories(ordering, name);

-- A single operational settings row (id = 'singleton')
CREATE TABLE settings (
  id                       TEXT PRIMARY KEY CHECK (id = 'singleton'),
  digest_interval_seconds  INTEGER NOT NULL,    -- default 600
  telegram_bot_token_ref   TEXT NOT NULL,       -- secret reference; raw value lives in env
  telegram_subscriber_chat INTEGER NOT NULL,    -- chat id of the subscriber
  ai_provider              TEXT NOT NULL,       -- 'openai' | 'anthropic' | 'fake' | future
  ai_model                 TEXT NOT NULL,       -- e.g. 'gpt-4o-mini', 'minimax-m3'
  ai_api_key_ref           TEXT NOT NULL,       -- secret reference
  ai_base_url              TEXT NOT NULL,       -- e.g. 'https://api.openai.com/v1'
  uncategorized_label      TEXT NOT NULL DEFAULT 'Uncategorized',
  updated_at               TEXT NOT NULL
);
-- The four AI fields (provider, model, base_url, api_key_ref) are
-- re-synced from the env file on every boot via store.SyncAISettings,
-- called from main after the initial seed. The operator-tunable
-- fields (digest_interval_seconds, telegram_subscriber_chat,
-- uncategorized_label) are NOT touched by the sync, so admin edits
-- persist across restarts.

-- One row per cycle execution
CREATE TABLE cycles (
  id              TEXT PRIMARY KEY,            -- UUID; same as cycle_id in logs
  window_start    TEXT NOT NULL,               -- ISO-8601 UTC
  window_end      TEXT NOT NULL,               -- ISO-8601 UTC
  status          TEXT NOT NULL,               -- 'pending' | 'succeeded' | 'failed' | 'degraded' | 'skipped_no_items'
  input_msg_count INTEGER NOT NULL DEFAULT 0,
  output_items    INTEGER NOT NULL DEFAULT 0,
  error           TEXT,                        -- nullable
  started_at      TEXT NOT NULL,
  finished_at     TEXT,                        -- nullable
  CHECK (status IN ('pending','succeeded','failed','degraded','skipped_no_items'))
);
CREATE INDEX idx_cycles_window_end ON cycles(window_end DESC);
CREATE INDEX idx_cycles_status ON cycles(status);

-- A delivered digest (one row per cycle, present iff a message was actually delivered)
CREATE TABLE digests (
  id             TEXT PRIMARY KEY,            -- UUID
  cycle_id       TEXT NOT NULL UNIQUE REFERENCES cycles(id) ON DELETE CASCADE,
  rendered_text  TEXT NOT NULL,               -- the exact message text sent to Telegram
  degraded       INTEGER NOT NULL DEFAULT 0   -- 1 if the cycle fell back to raw headlines
                 CHECK (degraded IN (0, 1)),
  telegram_msg_id INTEGER,                     -- the id Telegram assigned to the sent message; nullable
  sent_at        TEXT NOT NULL,
  send_status    TEXT NOT NULL                 -- 'ok' | 'failed' | 'blocked'
                 CHECK (send_status IN ('ok','failed','blocked'))
);
CREATE INDEX idx_digests_sent_at ON digests(sent_at DESC);

-- One row per source message observed during a cycle
CREATE TABLE digest_items (
  id              TEXT PRIMARY KEY,
  cycle_id        TEXT NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
  channel_id      TEXT NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
  category_id     TEXT REFERENCES categories(id) ON DELETE SET NULL, -- nullable when no category matches
  source_msg_id   INTEGER NOT NULL,            -- Telegram message id
  post_id         TEXT REFERENCES posts(id) ON DELETE CASCADE,  -- back-reference to the persistent post (post-queue)
  dedup_key       TEXT NOT NULL,               -- sha256(normalized_text) | sha256(media_signature)
  raw_text        TEXT NOT NULL,               -- captured text, may be empty for media-only
  media_kind      TEXT NOT NULL,               -- 'text' | 'image' | 'video' | 'voice' | 'other'
  summary         TEXT NOT NULL,               -- the summary that went into the digest
  confidence      REAL,                         -- 0..1 from AI, nullable
  ordering        INTEGER NOT NULL DEFAULT 0,  -- per-category order
  UNIQUE (cycle_id, channel_id, source_msg_id),
  UNIQUE (post_id)
);
CREATE INDEX idx_items_cycle ON digest_items(cycle_id, ordering);
CREATE INDEX idx_items_channel ON digest_items(channel_id);
CREATE INDEX idx_items_dedup ON digest_items(dedup_key);
CREATE INDEX idx_items_post_id ON digest_items(post_id);

-- Persistent per-post queue. One row per unique (channel_id,
-- source_msg_id); the cycle reads from here for summarize and send
-- steps. Status drives the lifecycle. See "Post queue" below.
CREATE TABLE posts (
  id              TEXT PRIMARY KEY,                  -- UUID
  channel_id      TEXT NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
  source_msg_id   INTEGER NOT NULL,                  -- Telegram message id
  dedup_key       TEXT NOT NULL,
  link            TEXT NOT NULL,                     -- https://t.me/<handle>/<source_msg_id>
  raw_text        TEXT NOT NULL,
  media_kind      TEXT NOT NULL,                     -- 'text' | 'image' | 'video' | 'voice' | 'other'
  captured_at     TEXT NOT NULL,                     -- ISO-8601 UTC
  status          TEXT NOT NULL,                     -- see post-queue state diagram
  category_id     TEXT REFERENCES categories(id) ON DELETE SET NULL,
  summary         TEXT,                              -- nullable until AI runs
  confidence      REAL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT,                              -- nullable
  sent_at         TEXT,                              -- nullable
  telegram_msg_id INTEGER,                           -- nullable
  send_error      TEXT,                              -- nullable
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  CHECK (status IN ('received','summarized','included_in_digest','sent','send_failed','filtered_out','dead')),
  UNIQUE (channel_id, source_msg_id)
);
CREATE INDEX idx_posts_status          ON posts(status);
CREATE INDEX idx_posts_status_captured ON posts(status, captured_at DESC);
CREATE INDEX idx_posts_dedup           ON posts(dedup_key);
CREATE INDEX idx_posts_channel         ON posts(channel_id, source_msg_id DESC);

-- A short audit log of operational events (last N rows, ring-buffered)
CREATE TABLE op_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  occurred_at TEXT NOT NULL,
  level       TEXT NOT NULL,                   -- 'info' | 'warn' | 'error'
  kind        TEXT NOT NULL,                   -- e.g. 'cycle.start', 'cycle.success', 'telegram.send.failed', 'post.received', 'post.sent'
  cycle_id    TEXT,                            -- nullable
  message     TEXT NOT NULL,
  context     TEXT                             -- JSON blob, nullable
);
CREATE INDEX idx_op_events_at ON op_events(occurred_at DESC);
```

## Repository interfaces (Go)

All interfaces live in `internal/store`. One concrete implementation (`internal/store/sqlite`) implements them. The cycle and the admin API depend only on the interfaces, not on the concrete type.

```go
type ChannelRepo interface {
    List(ctx context.Context) ([]Channel, error)
    Get(ctx context.Context, id string) (Channel, error)
    GetByHandle(ctx context.Context, handle string) (Channel, error)
    Add(ctx context.Context, handle, displayName string) (Channel, error)     // rejects duplicates, validates handle
    Remove(ctx context.Context, id string) error
    UpdateStatus(ctx context.Context, id string, status ChannelStatus, errMsg string) error
    AdvanceCursor(ctx context.Context, id string, lastSeenMsgID int64, observedAt time.Time) error
}

type CategoryRepo interface {
    List(ctx context.Context) ([]Category, error)
    Add(ctx context.Context, name string) (Category, error)
    Rename(ctx context.Context, id, newName string) (Category, error)
    Remove(ctx context.Context, id string) error                                // refuses to remove defaults
    EnsureDefaults(ctx context.Context, defaults []string) error                -- called once at first boot
}

type SettingsRepo interface {
    Get(ctx context.Context) (Settings, error)                                  -- returns seeded defaults if no row
    Update(ctx context.Context, s Settings) (Settings, error)                    -- partial updates via pointer fields
}

type CycleRepo interface {
    Create(ctx context.Context, c Cycle) error
    Finish(ctx context.Context, id string, status CycleStatus, inputCount, outputItems int, errMsg string) error
    LastSuccessfulWindowEnd(ctx context.Context) (time.Time, bool, error)       -- used by scheduler on restart
    List(ctx context.Context, limit, offset int) ([]Cycle, error)
    Get(ctx context.Context, id string) (Cycle, error)
}

type DigestRepo interface {
    Create(ctx context.Context, d Digest) error
    UpdateSendResult(ctx context.Context, id string, telegramMsgID int64, status SendStatus) error
    ListByCycle(ctx context.Context, cycleID string) ([]DigestItem, error)
    ListRecent(ctx context.Context, limit int) ([]DigestListEntry, error)         -- joined with cycles for the history view
    GetByCycle(ctx context.Context, cycleID string) (Digest, error)
}

type CursorRepo interface {                                                       -- thin convenience on top of channels
    Get(ctx context.Context, channelID string) (lastSeenMsgID int64, err error)
    Advance(ctx context.Context, channelID string, toMsgID int64) error
}

// PostRepo persists one row per unique channel post. The cycle uses it
// to (1) dedupe fetches across cycles via Upsert, (2) drive the
// summarize step from ListReceived, (3) drive the send step from
// ListUnsent, and (4) record per-post send outcomes. See "Post queue"
// below.
type PostRepo interface {
    Upsert(ctx context.Context, p Post) (Post, bool, error)
    Get(ctx context.Context, id string) (Post, error)
    GetByChannelMsg(ctx context.Context, channelID string, sourceMsgID int64) (Post, error)
    ListReceived(ctx context.Context, limit int) ([]Post, error)
    ListUnsent(ctx context.Context, cutoff time.Time, limit int) ([]Post, error)
    ListByStatus(ctx context.Context, status PostStatus, limit int) ([]Post, error)
    ListAll(ctx context.Context, limit int) ([]Post, error)
    MarkSummarized(ctx context.Context, id string, categoryID, summary string, confidence float64) error
    MarkIncluded(ctx context.Context, postIDs []string) error
    MarkSent(ctx context.Context, id string, telegramMsgID int64) error
    MarkSendFailed(ctx context.Context, id string, errMsg string) error
    MarkFiltered(ctx context.Context, id string) error
}

type HealthRepo interface {
    Snapshot(ctx context.Context) (Health, error)                                  -- last success, last failure, per-channel status
    RecordEvent(ctx context.Context, e OpEvent) error                              -- also writes to op_events
    RecentEvents(ctx context.Context, limit int) ([]OpEvent, error)
}
```

## Entity reference (aligned with `spec.md`)

| Spec entity | DB rows | Notes |
|---|---|---|
| **Subscriber** | implicit (one row in `settings` with `telegram_subscriber_chat`) | single-subscriber phase 1 |
| **Source Channel** | `channels` | `status`, `last_seen_msg_id`, `last_observed_at`, `last_error` capture the lifecycle |
| **Persistent Post** | `posts` | one row per unique `(channel_id, source_msg_id)`; carries source message + lifecycle + per-send outcome |
| **Source Message** | `posts.raw_text` + `posts.source_msg_id` + `posts.dedup_key` + `posts.media_kind` + `posts.link` | captured at fetch time, never re-fetched; persists across cycles |
| **Digest Item** | `digest_items` (with `post_id` FK back to `posts`) | the per-cycle summary text, category, ordering, and the post it came from |
| **Category** | `categories` | `is_default` flag protects shipped categories from deletion |
| **Digest Cycle** | `cycles` | one row per scheduled execution, including skipped / failed ones |
| **Digest Record** | `digests` (1:1 with successful or degraded cycles) | the exact text that was sent to Telegram |
| **Operator Configuration** | `settings` (single row) | credentials are referenced by name, not stored |
| **Operational Health Snapshot** | derived view over `cycles` + `channels` + `op_events` + `posts` | computed on read by `HealthRepo.Snapshot` |

## State transitions

### Channel lifecycle

```
            Add (admin)
   (none) ─────────────► active ─────► inaccessible ─────► banned
                            ▲                │                  │
                            └──── re-Add ────┘                  │
                                                               │
                                          Remove (admin) ◄─────┘
```

- `Add` validates the handle (must match `^[A-Za-z][A-Za-z0-9_]{3,31}[A-Za-z0-9]$` after stripping a leading `@`), calls the Telegram `getChat` method to confirm the channel exists, and stores the row in `active`.
- A cycle that fails to read a channel (channel went private, bot kicked) marks the channel `inaccessible` with `last_error` populated.
- Repeated inaccessible state (configurable threshold, default 3 consecutive cycles) promotes the channel to `banned`, which is shown in the admin panel with a clear badge and is excluded from future fetches.
- `Remove` deletes the channel; the foreign key on `digest_items.channel_id` is `ON DELETE RESTRICT` so removal of a channel with history is refused with a clear error.

### Cycle lifecycle

```
   scheduled tick
        │
        ▼
    pending ──► succeeded          (delivered digest, no degradation)
        │  ──► degraded            (delivered digest with raw headlines; AI failed)
        │  ──► skipped_no_items    (no new items in any channel)
        │  ──► failed              (could not deliver; failure recorded)
```

- A cycle is created `pending` before fetches start.
- The cycle is finished exactly once, with one of the four terminal states.
- **`failed` is used both for "send to Telegram failed" and for "no recipient configured"** (the cycle has items to send but `telegram_subscriber_chat` is 0 in the DB and `TELEGRAM_SUBSCRIBER_CHAT` is 0 in the env, or no chat id was auto-discovered from `/start`). The two cases are distinguished by the op_event kind (`telegram.send.failed` vs `telegram.send.no_recipient`).
- The scheduler reads `LastSuccessfulWindowEnd` on startup to compute the first post-restart window (FR-016, SC-008). If the last cycle's window is still "open" (i.e. less than `digest_interval_seconds` has passed since its end), the scheduler waits the remainder; if the window is already "overdue", the next cycle starts immediately with `window_start = last_window_end`.

### Digest delivery status

`digests.send_status` reflects what actually happened on the wire:

- `ok` — Telegram accepted the message; `telegram_msg_id` is populated with the id Telegram assigned.
- `failed` — the `sendMessage` call returned an error (network, 4xx, 5xx). The exact reason is recorded in an op event with `kind='telegram.send.failed'`. The cycle's `status` is then also `failed`. A subsequent retry happens on the next cycle if new items arrive.
- `blocked` — Telegram returned a "bot was blocked by the user" / "Forbidden" response. Same handling as `failed`, but the op event kind is `telegram.send.blocked`. The cycle backs off (does not retry the same window).

If no recipient is configured when the cycle has items to send, the digest is still created (so the operator can see the rendered text in the history view) but `send_status='failed'`, `telegram_msg_id=NULL`, `sent_at=NULL`, and an op event of `kind='telegram.send.no_recipient'` is recorded. The system does not mark un-sent digests as `ok`.

### Category lifecycle

```
   shipped defaults (EnsureDefaults, run once)
        │
        ▼
   default ◄──── rename (admin) ──── default
        │
        │  add (admin)
        ▼
   custom ──── rename (admin) ───► custom
        │
        │  remove (admin)
        ▼
   (deleted)   -- only for custom; defaults refuse Remove
```

- `EnsureDefaults` is idempotent: at first boot it inserts the shipped set (Politics, Technology, Business, Sports, World, Other) marked `is_default = 1`; on subsequent boots it is a no-op.
- `Remove` of a default category returns an error surfaced in the admin panel: "Cannot remove a built-in category; rename it instead."

### Post lifecycle (added in migration `0002_posts_queue.sql`)

Each unique `(channel_id, source_msg_id)` is one row in `posts`. The status field drives the cycle. The cycle's summarize step pulls `received` rows; the send step pulls `summarized` / `send_failed` / `included_in_digest` rows. After a successful Telegram ack the row is `sent` and is excluded from future bundles automatically.

```
   (no row) -- fetch step: Upsert
        │
        ▼
   received -- AI step: MarkSummarized ─► summarized
        │                                  │
        │ -- AI rejected (ErrInvalidInput)  │ -- MarkIncluded (bundle) ─► included_in_digest
        ▼                                  │                              │
   filtered_out                            │                              │
                                           │           ┌── send OK ◄──────┤
                                           │           ▼                  │
                                           │         sent                 │
                                           │                              │
                                           │           ┌── send failed ◄──┤
                                           │           ▼                  │
                                           │      send_failed ────────────┘  (auto-retry on next cycle)
                                           │
                                           └── MarkFiltered (only valid from 'received')
```

- **received → summarized** on a successful AI call (`MarkSummarized`).
- **received → filtered_out** when the AI returns `ErrInvalidInput` (`MarkFiltered`).
- **summarized → included_in_digest** when the cycle bundles the post into a digest row that has not been sent yet (`MarkIncluded`). The unique constraint on `digest_items.post_id` makes this the last stop before send: at most one digest row may reference a given post.
- **included_in_digest → sent** on a successful Telegram send (`MarkSent`); the row's `telegram_msg_id` and `sent_at` are populated, `attempts` increments.
- **included_in_digest → send_failed** on a Telegram error or blocked-subscriber response (`MarkSendFailed`). The next cycle re-bundles the post via `ListUnsent` and retries (auto-retry; no operator action).
- A post in `send_failed` that is later moved to `sent` clears its `send_error` column (set to NULL by `MarkSent`).
- A post in `send_failed` after `attempts` consecutive cycles is never auto-marked `dead` in phase 1. (Future: a max-attempts threshold + `MarkDead`.)

## Validation rules (enforced in the store layer)

- **Channel handle**: must match `^[A-Za-z][A-Za-z0-9_]{3,31}[A-Za-z0-9]$`; leading `@` is stripped before storage and comparison.
- **Channel existence**: `Add` calls the Telegram `getChat` API; failures (404, bot not in channel) are surfaced as admin-panel errors and the row is not stored.
- **Category name**: trimmed, non-empty, length 1–40, unique case-insensitively, not equal to `uncategorized_label` from settings.
- **Digest interval**: integer seconds, 60 ≤ value ≤ 86400 (one minute to one day). Values outside this range are rejected with a clear error.
- **Credentials**: settings stores `*_ref` strings (e.g. `env:TELEGRAM_BOT_TOKEN`); the store layer never accepts raw secret values. The main process resolves the refs at startup and at update time and refuses to start if any ref is unresolvable.
- **Cycle idempotency**: the unique constraint on `(cycle_id, channel_id, source_msg_id)` and the unique key on `digests.cycle_id` make it safe to retry any step of the cycle without producing duplicates; the cycle uses `INSERT ... ON CONFLICT DO NOTHING` for `digest_items` so a partial replay is safe.

## Initial seed data

- `categories` rows for the shipped defaults.
- `settings` row with sensible defaults (`digest_interval_seconds = 600`, `uncategorized_label = 'Uncategorized'`, `ai_provider = 'fake'`, `ai_model = 'gpt-4o-mini'`, `ai_base_url = 'https://api.openai.com/v1'`, `telegram_subscriber_chat = 0`, `ai_api_key_ref = 'env:AI_API_KEY'` when no AI env is set so a fresh boot is safe and self-testing).
- After the seed, the AI fields (`ai_provider`, `ai_model`, `ai_base_url`, `ai_api_key_ref`) are re-synced from the live env on every boot, so the panel reflects the running configuration. Operator-tunable fields are left as-is.
- No `channels` rows, no `cycles` rows, no `digests` rows.

## Operational event kinds (audit log)

`op_events.kind` is a free-form string. The set produced by the system in v1.x is:

| Kind | When |
|---|---|
| `cycle.start` | A new cycle is created and fetches begin. |
| `cycle.fetched` | Fetches completed; carries `received` count. |
| `cycle.summarized` | AI summarization completed; carries `items` and `degraded`. |
| `cycle.success` | Cycle completed with `status='succeeded'`. |
| `cycle.degraded` | Cycle completed with `status='degraded'`. |
| `cycle.skipped_no_items` | Cycle completed with `status='skipped_no_items'`. |
| `cycle.failed` | Cycle completed with `status='failed'`. |
| `channel.fetch.ok` | A channel was fetched without error. |
| `channel.inaccessible` | A channel's Telegram getChat/fetch failed; the channel is marked `inaccessible`. |
| `channel.banned` | A channel was promoted from `inaccessible` after repeated failures. |
| `post.received` | A new post row was created (`Upsert` returned `created=true`). |
| `post.sent` | A post row was marked `sent` after a successful Telegram send. |
| `post.send_failed` | A post row was marked `send_failed` after a Telegram send error. |
| `telegram.send.failed` | The send side returned an error (network / API 4xx or 5xx). |
| `telegram.send.blocked` | Telegram returned a "bot blocked by the user" response. |
| `telegram.send.no_recipient` | The cycle had items to send but no subscriber chat id was configured. Distinct from `telegram.send.failed` so the operator can tell the two apart in the events view. |
| `settings.changed` | The operator-tunable settings row was PATCHed. |

## Migration policy

- Migrations are append-only SQL files in `backend/migrations/`, named `NNNN_description.sql`.
- The store layer records applied migrations in a `schema_migrations` table (`version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL`).
- A failed migration aborts startup with the exact error; the DB is not touched.
- Migration `0002_posts_queue.sql` introduces the persistent `posts` table, the `digest_items.post_id` FK + UNIQUE, and a backfill from existing `digest_items` rows. The backfill creates one post per existing item with status `sent` (or `send_failed` if the parent digest's `send_status` was not `ok`) so the live history renders identically.
