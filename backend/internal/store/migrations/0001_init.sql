-- 0001_init.sql
-- Full initial schema for the Telegram News Digest Assistant.
-- See specs/001-telegram-news-assistant/data-model.md for the entity reference
-- and state-transition rules.

-- Channels the subscriber wants monitored
CREATE TABLE channels (
  id               TEXT PRIMARY KEY,            -- UUID
  handle           TEXT NOT NULL UNIQUE,        -- without leading "@"
  display_name     TEXT NOT NULL,
  status           TEXT NOT NULL,               -- 'active' | 'inaccessible' | 'banned'
  last_seen_msg_id INTEGER NOT NULL DEFAULT 0,  -- cursor
  last_observed_at TEXT,                        -- ISO-8601 UTC, nullable
  last_error       TEXT,                        -- nullable
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  CHECK (status IN ('active','inaccessible','banned'))
);
CREATE INDEX idx_channels_status ON channels(status);

-- Categories used to group digest items
CREATE TABLE categories (
  id         TEXT PRIMARY KEY,                 -- UUID
  name       TEXT NOT NULL UNIQUE,
  ordering   INTEGER NOT NULL DEFAULT 0,
  is_default INTEGER NOT NULL,                 -- 0 custom, 1 default
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (is_default IN (0, 1))
);
CREATE INDEX idx_categories_ordering ON categories(ordering, name);

-- A single operational settings row (id = 'singleton')
CREATE TABLE settings (
  id                       TEXT PRIMARY KEY CHECK (id = 'singleton'),
  digest_interval_seconds  INTEGER NOT NULL,
  telegram_bot_token_ref   TEXT NOT NULL,
  telegram_subscriber_chat INTEGER NOT NULL,
  ai_provider              TEXT NOT NULL,
  ai_model                 TEXT NOT NULL,
  ai_api_key_ref           TEXT NOT NULL,
  ai_base_url              TEXT NOT NULL,
  uncategorized_label      TEXT NOT NULL DEFAULT 'Uncategorized',
  updated_at               TEXT NOT NULL
);

-- One row per cycle execution
CREATE TABLE cycles (
  id              TEXT PRIMARY KEY,            -- UUID
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

-- A delivered digest (one row per cycle that produced a message)
CREATE TABLE digests (
  id             TEXT PRIMARY KEY,             -- UUID
  cycle_id       TEXT NOT NULL UNIQUE REFERENCES cycles(id) ON DELETE CASCADE,
  rendered_text  TEXT NOT NULL,
  degraded       INTEGER NOT NULL DEFAULT 0,
  telegram_msg_id INTEGER,                     -- nullable
  sent_at        TEXT NOT NULL,
  send_status    TEXT NOT NULL,                -- 'ok' | 'failed' | 'blocked'
  CHECK (degraded IN (0, 1)),
  CHECK (send_status IN ('ok','failed','blocked'))
);
CREATE INDEX idx_digests_sent_at ON digests(sent_at DESC);

-- One row per source message observed during a cycle
CREATE TABLE digest_items (
  id            TEXT PRIMARY KEY,              -- UUID
  cycle_id      TEXT NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
  channel_id    TEXT NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
  category_id   TEXT REFERENCES categories(id) ON DELETE SET NULL,
  source_msg_id INTEGER NOT NULL,
  dedup_key     TEXT NOT NULL,
  raw_text      TEXT NOT NULL,
  media_kind    TEXT NOT NULL,
  summary       TEXT NOT NULL,
  confidence    REAL,                          -- nullable
  ordering      INTEGER NOT NULL DEFAULT 0,
  UNIQUE (cycle_id, channel_id, source_msg_id)
);
CREATE INDEX idx_items_cycle ON digest_items(cycle_id, ordering);
CREATE INDEX idx_items_channel ON digest_items(channel_id);
CREATE INDEX idx_items_dedup ON digest_items(dedup_key);

-- A short audit log of operational events (ring-buffered by the app)
CREATE TABLE op_events (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  occurred_at TEXT NOT NULL,
  level       TEXT NOT NULL,                   -- 'info' | 'warn' | 'error'
  kind        TEXT NOT NULL,
  cycle_id    TEXT,                            -- nullable
  message     TEXT NOT NULL,
  context     TEXT                             -- JSON blob, nullable
);
CREATE INDEX idx_op_events_at ON op_events(occurred_at DESC);
