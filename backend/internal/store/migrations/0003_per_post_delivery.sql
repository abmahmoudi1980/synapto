-- 0003_per_post_delivery.sql
-- Per-post delivery mode: each post becomes its own Telegram message.
-- Replaces the bundled-digest model as the default for v2; the bundled
-- path is kept (and switchable via /api/settings) for comparison.
--
-- See specs/001-telegram-news-assistant/data-model.md (v2 amendment)
-- and contracts/telegram-render.md (per-post format).

ALTER TABLE settings ADD COLUMN delivery_mode TEXT NOT NULL DEFAULT 'bundled';
UPDATE settings SET delivery_mode = 'per_post';
