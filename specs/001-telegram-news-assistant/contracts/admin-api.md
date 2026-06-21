# Contract: Admin HTTP API

**Feature**: 001-telegram-news-assistant
**Version**: 0.1.0
**Base URL**: `http://<admin listen addr>` (default `http://localhost:8080`)
**Audience**: Svelte admin panel (same-origin; cookies, sessions, or auth headers are out of scope for phase 1)
**Format**: JSON over HTTP/1.1; UTF-8; `Content-Type: application/json; charset=utf-8`

## Conventions

- All request and response bodies are JSON objects.
- All timestamps are ISO-8601 UTC strings (e.g. `"2026-06-21T07:30:00Z"`).
- All ids are UUIDv4 strings unless otherwise stated.
- All `PATCH` endpoints accept partial updates; fields not present in the body are left unchanged. Fields explicitly set to `null` mean "clear this value" only for nullable fields (documented per endpoint).
- Errors use the standard error shape below; the HTTP status code is always meaningful (4xx for caller errors, 5xx for server errors).

### Error shape

```json
{
  "error": {
    "code": "channel_not_found",
    "message": "Channel 9c0… not found.",
    "field": "id"
  }
}
```

`field` is optional and is set when the error refers to a specific request field. The `code` is a stable machine-readable string (see per-endpoint "Error codes").

## Endpoints

### Channels

#### `GET /api/channels`

List all channels the subscriber has selected, in handle order.

**Response 200**:
```json
{
  "channels": [
    {
      "id": "9c0a1f4e-…",
      "handle": "durov",
      "display_name": "Durov's Channel",
      "status": "active",
      "last_observed_at": "2026-06-21T07:20:14Z",
      "last_error": null
    }
  ]
}
```

#### `POST /api/channels`

Add a new channel. The server calls Telegram `getChat` to validate existence and bot membership; the row is not stored if that call fails.

**Request**:
```json
{ "handle": "durov" }
```

**Response 201**:
```json
{
  "channel": { /* same shape as in GET /api/channels */ }
}
```

**Error codes**: `invalid_handle`, `channel_not_found_on_telegram`, `bot_not_in_channel`, `duplicate_channel`, `telegram_unavailable`.

#### `DELETE /api/channels/{id}`

Remove a channel. Refuses if the channel has digest history; in that case the caller should rename it to a "do not monitor" state instead (a follow-up feature, not in phase 1).

**Response 204** (no body).

**Error codes**: `channel_not_found`, `channel_has_history`.

---

### Categories

#### `GET /api/categories`

List categories in `ordering ASC, name ASC` order.

**Response 200**:
```json
{
  "categories": [
    { "id": "…", "name": "Technology", "ordering": 0, "is_default": true },
    { "id": "…", "name": "Politics",   "ordering": 1, "is_default": true }
  ]
}
```

#### `POST /api/categories`

Add a custom category.

**Request**:
```json
{ "name": "AI & ML" }
```

**Response 201**: the new category object (same shape as in the list).

**Error codes**: `invalid_name`, `duplicate_name`, `name_too_long`.

#### `PATCH /api/categories/{id}`

Rename a category. Refuses to set `is_default = false`; defaults are forever defaults.

**Request**:
```json
{ "name": "AI / ML" }
```

**Response 200**: the updated category object.

**Error codes**: `category_not_found`, `invalid_name`, `duplicate_name`, `name_too_long`.

#### `DELETE /api/categories/{id}`

Remove a custom category. Refuses to remove a default; the admin panel surfaces a clear error and offers rename as the only mutating action.

**Response 204** (no body).

**Error codes**: `category_not_found`, `cannot_remove_default`, `category_has_items`.

---

### Settings (operator)

#### `GET /api/settings`

Read the operator settings.

**Response 200**:
```json
{
  "settings": {
    "digest_interval_seconds": 600,
    "telegram_bot_token_ref":   "env:TELEGRAM_BOT_TOKEN",
    "telegram_subscriber_chat": 123456789,
    "telegram_bot_reachable":   true,
    "ai_provider":              "openai",
    "ai_model":                 "gpt-4o-mini",
    "ai_base_url":              "https://api.openai.com/v1",
    "ai_api_key_ref":           "env:AI_API_KEY",
    "ai_reachable":             true,
    "uncategorized_label":      "Uncategorized",
    "updated_at":               "2026-06-20T11:14:02Z"
  }
}
```

`telegram_bot_reachable` and `ai_reachable` are derived live (last health probe), not persisted.

#### `PATCH /api/settings`

Partial update. `digest_interval_seconds` and `telegram_subscriber_chat` are settable. Credentials (`*_ref` fields) are read-only via this endpoint; rotation is a startup-time operation (re-launch the process with a new env var). `uncategorized_label` is settable.

**Request**:
```json
{
  "digest_interval_seconds": 300,
  "telegram_subscriber_chat": 123456789,
  "uncategorized_label": "Other"
}
```

**Response 200**: the updated settings object.

**Error codes**: `invalid_interval` (out of [60, 86400]), `invalid_chat_id`, `name_too_long`.

#### `POST /api/settings/test-telegram`

Probe the configured bot token by calling `getMe`.

**Response 200**:
```json
{ "ok": true, "bot": { "id": 1234567890, "username": "MyDigestBot", "first_name": "MyDigestBot" } }
```

**Error codes**: `telegram_unavailable`, `invalid_token`.

#### `POST /api/settings/test-ai`

Probe the configured AI provider by issuing a 1-token request.

**Response 200**:
```json
{ "ok": true, "model": "gpt-4o-mini", "latency_ms": 412 }
```

**Error codes**: `ai_unavailable`, `invalid_credentials`.

---

### Health & operational snapshot

#### `GET /api/health`

Liveness and summary of the last cycle.

**Response 200**:
```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 7321,
  "last_successful_cycle_at": "2026-06-21T07:20:00Z",
  "last_failure_at":           null,
  "last_failure_reason":       null,
  "scheduler_state":           "idle",
  "db_ok":                     true
}
```

`scheduler_state` is one of `idle`, `running`, `restarting`. `status` is `ok` when `db_ok` is true and the last cycle (if any) was not `failed`.

---

### Cycles & history

#### `GET /api/cycles?limit=20&offset=0`

List recent cycles in reverse chronological order.

**Response 200**:
```json
{
  "cycles": [
    {
      "id": "…",
      "window_start": "2026-06-21T07:10:00Z",
      "window_end":   "2026-06-21T07:20:00Z",
      "status":       "succeeded",
      "input_msg_count": 18,
      "output_items": 18,
      "degraded": false,
      "started_at": "2026-06-21T07:20:00Z",
      "finished_at":"2026-06-21T07:20:09Z"
    }
  ],
  "total": 142
}
```

#### `GET /api/cycles/{id}`

Get a single cycle, its digest (if any), and the list of digest items grouped by category.

**Response 200**:
```json
{
  "cycle":   { /* same shape as in GET /api/cycles */ },
  "digest":  {
    "id": "…",
    "rendered_text": "📰 News digest — 2026-06-21 07:20\n\n# Technology\n• …\n\n# Politics\n• …",
    "degraded": false,
    "telegram_msg_id": 4711,
    "sent_at":  "2026-06-21T07:20:09Z",
    "send_status": "ok"
  },
  "items_by_category": [
    {
      "category": { "id": "…", "name": "Technology", "ordering": 0, "is_default": true },
      "items": [
        {
          "id": "…",
          "channel": { "id": "…", "handle": "durov", "display_name": "Durov's Channel" },
          "source_msg_id": 12345,
          "media_kind": "text",
          "summary": "Telegram announced a new privacy feature for groups.",
          "confidence": 0.87
        }
      ]
    }
  ]
}
```

**Error codes**: `cycle_not_found`, `digest_not_available` (when the cycle had no items and produced no digest).

---

### Operational events

#### `GET /api/events?limit=50`

Return the most recent operational events (newest first), drawn from the `op_events` ring buffer.

**Response 200**:
```json
{
  "events": [
    {
      "id": 142,
      "occurred_at": "2026-06-21T07:20:09Z",
      "level": "info",
      "kind": "cycle.success",
      "cycle_id": "…",
      "message": "Cycle … delivered 18 items in 9.2s",
      "context": { "input_count": 18, "output_count": 18, "degraded": false }
    }
  ]
}
```

---

## CORS, cookies, and auth (phase 1)

- The admin API is served from the **same origin** as the SPA (the Go binary serves both). No CORS is configured; no CORS preflight is expected.
- There is no auth layer in phase 1. The admin `LISTEN_ADDR` should be bound to a non-public interface (e.g. `127.0.0.1:8080`) or placed behind a reverse-proxy / VPN by the operator. This is documented in the `README` and in the spec's `Assumptions`.

## Versioning

- The API is prefixed `/api/` and currently has no version segment. A future breaking change will move to `/api/v2/`; the v1 routes will be removed after a deprecation window.
