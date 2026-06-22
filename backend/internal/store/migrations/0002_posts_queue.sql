-- 0002_posts_queue.sql
-- Persistent per-post queue. Replaces the per-cycle dedup model so the
-- service can recover from mid-cycle crashes and retry failed sends
-- across cycles. Each unique Telegram channel post becomes one row,
-- independent of any cycle, with its own status lifecycle.
--
-- See specs/001-telegram-news-assistant/data-model.md for the
-- posts-queue entity reference and the migration that introduces
-- PostRepo.

CREATE TABLE posts (
  id              TEXT PRIMARY KEY,                       -- UUID
  channel_id      TEXT NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
  source_msg_id   INTEGER NOT NULL,                       -- Telegram message id
  dedup_key       TEXT NOT NULL,                          -- sha256(normalized_text) or media signature
  link            TEXT NOT NULL,                          -- "https://t.me/<handle>/<source_msg_id>"
  raw_text        TEXT NOT NULL,                          -- may be empty for media-only posts
  media_kind      TEXT NOT NULL,                          -- 'text' | 'image' | 'video' | 'voice' | 'other'
  captured_at     TEXT NOT NULL,                          -- ISO-8601 UTC, when we first saw it
  status          TEXT NOT NULL,                          -- 'received' | 'summarized' | 'included_in_digest' | 'sent' | 'send_failed' | 'filtered_out' | 'dead'
  category_id     TEXT REFERENCES categories(id) ON DELETE SET NULL,
  summary         TEXT,                                   -- nullable; populated after AI
  confidence      REAL,                                   -- nullable
  attempts        INTEGER NOT NULL DEFAULT 0,             -- send-attempt count
  last_attempt_at TEXT,                                   -- nullable
  sent_at         TEXT,                                   -- nullable
  telegram_msg_id INTEGER,                                -- nullable; last successful send's msg id
  send_error      TEXT,                                   -- nullable
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  CHECK (status IN ('received','summarized','included_in_digest','sent','send_failed','filtered_out','dead')),
  UNIQUE (channel_id, source_msg_id)
);
CREATE INDEX idx_posts_status          ON posts(status);
CREATE INDEX idx_posts_status_captured ON posts(status, captured_at DESC);
CREATE INDEX idx_posts_dedup           ON posts(dedup_key);
CREATE INDEX idx_posts_channel         ON posts(channel_id, source_msg_id DESC);

-- Link digest_items to the new persistent post table. UNIQUE(post_id)
-- enforces the one-post-one-digest-row invariant at the DB level.
ALTER TABLE digest_items ADD COLUMN post_id TEXT REFERENCES posts(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX idx_items_post ON digest_items(post_id);
CREATE INDEX idx_items_post_id ON digest_items(post_id);

-- Backfill: one post per existing digest_item, status='sent' (or
-- 'send_failed') inferred from the parent digest row. This preserves
-- history: existing digests render identically because the new code
-- reads posts.summary instead of digest_items.summary.
INSERT INTO posts (
  id, channel_id, source_msg_id, dedup_key, link, raw_text, media_kind, captured_at,
  status, category_id, summary, confidence, attempts, last_attempt_at, sent_at,
  telegram_msg_id, send_error, created_at, updated_at
)
SELECT
  'post-' || di.cycle_id || '-' || di.channel_id || '-' || di.source_msg_id,
  di.channel_id, di.source_msg_id, di.dedup_key,
  'https://t.me/' || (SELECT handle FROM channels WHERE id = di.channel_id) || '/' || di.source_msg_id,
  di.raw_text, di.media_kind,
  COALESCE((SELECT started_at FROM cycles WHERE id = di.cycle_id), datetime('now')),
  CASE WHEN (SELECT d.send_status FROM digests d WHERE d.cycle_id = di.cycle_id) = 'ok'
       THEN 'sent' ELSE 'send_failed' END,
  di.category_id, di.summary, di.confidence,
  1,
  (SELECT d.sent_at FROM digests d WHERE d.cycle_id = di.cycle_id),
  (SELECT d.sent_at FROM digests d WHERE d.cycle_id = di.cycle_id),
  (SELECT d.telegram_msg_id FROM digests d WHERE d.cycle_id = di.cycle_id),
  NULL,
  COALESCE((SELECT started_at FROM cycles WHERE id = di.cycle_id), datetime('now')),
  COALESCE((SELECT started_at FROM cycles WHERE id = di.cycle_id), datetime('now'))
FROM digest_items di;

UPDATE digest_items
SET post_id = 'post-' || cycle_id || '-' || channel_id || '-' || source_msg_id
WHERE post_id IS NULL;
