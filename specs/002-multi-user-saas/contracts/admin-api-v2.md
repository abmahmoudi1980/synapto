# Contract: Admin HTTP API (v2 — Multi-User)

**Feature**: 002-multi-user-saas
**Version**: 2.0.0
**Base URL**: `http://<admin listen addr>` (default `http://localhost:8080`)
**Audience**: Svelte SPA, served from the same origin
**Format**: JSON over HTTP/1.1; UTF-8; `Content-Type: application/json; charset=utf-8`
**Auth**: Bearer JWT in the `Authorization: Bearer <token>` header. All endpoints under `/api/v2/*` (except `/api/v2/auth/*` and `/api/v2/health`) require a valid, non-revoked, non-expired token.

## Conventions

- All request and response bodies are JSON objects.
- All timestamps are ISO-8601 UTC strings (e.g. `"2026-06-23T07:30:00Z"`).
- All ids are UUIDv4 strings unless otherwise stated.
- `PATCH` endpoints accept partial updates; fields not present in the body are left unchanged. Fields explicitly set to `null` mean "clear this value" only for nullable fields (documented per endpoint).
- Errors use the standard error shape below; the HTTP status code is always meaningful (4xx for caller errors, 5xx for server errors).
- All endpoints return responses scoped to the authenticated user. A user can only read or modify their own resources; cross-user access returns `404 not_found` (we do not reveal whether the resource exists for another user — see `data-model.md` Validation rules).

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

### Authentication

#### `POST /api/v2/auth/telegram`

Verify a Telegram Login Widget payload and issue a session. The request body is the raw `application/x-www-form-urlencoded` body the widget posts, e.g. `id=42&first_name=Foo&auth_date=1718000000&hash=abc...`. The server computes the expected `hash` from the configured bot token, rejects the request if the signature is wrong or if `auth_date` is older than 5 minutes, consumes the nonce (replay protection), upserts the `users` row, issues a session, and returns the JWT.

**Request** (Content-Type: `application/x-www-form-urlencoded`):
```
id=42&first_name=Foo&last_name=Bar&username=baz&photo_url=https%3A%2F%2Ft.me%2Fi%2Fuserpic&auth_date=1718000000&hash=abc...
```

**Response 200**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-06-23T19:30:00Z",
  "user": {
    "id": "9c0a1f4e-...",
    "telegram_id": 42,
    "first_name": "Foo",
    "last_name": "Bar",
    "username": "baz",
    "photo_url": "https://t.me/i/userpic",
    "is_active": true
  }
}
```

The `token` is also set as a `Set-Cookie: synapto_session=<token>; HttpOnly; Secure; SameSite=Lax` cookie for browser convenience.

**Error codes**:
- `400 invalid_payload` — body is not a valid form-encoded payload.
- `401 hash_mismatch` — the signature does not match the bot token.
- `401 auth_date_stale` — `auth_date` is older than 5 minutes.
- `401 nonce_replayed` — the same payload was already accepted.

---

#### `POST /api/v2/auth/webapp`

Same as `auth/telegram`, but verifies a Mini App `initData` blob (the body is JSON `{initData: "..."}` where `initData` is the raw URL-encoded body the Mini App passed). Kept for parity; not the primary path.

**Request**:
```json
{ "initData": "id=42&first_name=Foo&auth_date=1718000000&hash=abc..." }
```

**Response 200**: same shape as `auth/telegram`.

**Error codes**: same as `auth/telegram`.

---

#### `POST /api/v2/auth/logout`

Revoke the current session. Requires the Authorization header. Sets the cookie to an expired value.

**Request**: empty body.

**Response 200**:
```json
{ "revoked": true }
```

**Error codes**:
- `401 unauthenticated` — no valid session.

---

#### `GET /api/v2/auth/status`

Return the current session's user and expiry. Used by the SPA on page load to decide whether to show the login page or the dashboard.

**Response 200** (authenticated):
```json
{
  "authenticated": true,
  "user": {
    "id": "9c0a1f4e-...",
    "telegram_id": 42,
    "first_name": "Foo",
    "last_name": "Bar",
    "username": "baz",
    "photo_url": "https://t.me/i/userpic",
    "is_active": true
  },
  "expires_at": "2026-06-23T19:30:00Z"
}
```

**Response 200** (unauthenticated):
```json
{ "authenticated": false }
```

---

### Me

#### `GET /api/v2/me`

Return the authenticated user's record and per-user settings.

**Response 200**:
```json
{
  "user": { ... },
  "settings": {
    "digest_interval_seconds": 600,
    "uncategorized_label": "Uncategorized",
    "delivery_mode": "per_post",
    "updated_at": "2026-06-23T07:30:00Z"
  }
}
```

---

#### `PATCH /api/v2/me/settings`

Update the authenticated user's settings. Partial updates are supported.

**Request**:
```json
{
  "digest_interval_seconds": 900,
  "uncategorized_label": "Other",
  "delivery_mode": "bundled"
}
```

**Response 200**: the updated settings object.

**Error codes**:
- `400 invalid_interval` — `digest_interval_seconds` not in [60, 86400].
- `400 invalid_name` / `name_too_long` — `uncategorized_label` is empty or longer than 40 characters.
- `400 invalid_delivery_mode` — `delivery_mode` is not 'bundled' or 'per_post'.

---

### Channels (watch-list)

#### `GET /api/v2/channels`

List the channels the authenticated user follows. Returned in handle order.

**Response 200**:
```json
{
  "channels": [
    {
      "id": "9c0a1f4e-...",
      "handle": "durov",
      "display_name": "Durov's Channel",
      "status": "active",
      "last_observed_at": "2026-06-23T07:20:14Z",
      "last_error": null,
      "filter": {
        "id": "f1-...",
        "filter_type": "ai_prompt",
        "filter_value": "Only forward crypto news related to Ethereum and ignore Bitcoin",
        "is_active": true
      }
    }
  ]
}
```

`filter` is the resolved effective filter (precedence: channel override → per-channel → user default). `null` when the user has no filter for this channel.

---

#### `POST /api/v2/channels/subscribe`

Add a public Telegram channel to the authenticated user's watch-list. The server calls Telegram `getChat` to validate existence; the row is not stored if the call fails.

**Request**:
```json
{ "handle": "durov" }
```

**Response 201**:
```json
{
  "channel": {
    "id": "9c0a1f4e-...",
    "handle": "durov",
    "display_name": "Durov's Channel",
    "status": "active",
    "filter": null
  }
}
```

**Error codes**:
- `400 invalid_handle` — handle is empty or fails the regex `^[A-Za-z][A-Za-z0-9_]{3,31}[A-Za-z0-9]$`.
- `400 channel_not_found_on_telegram` — Telegram's `getChat` returned 404.
- `400 bot_not_in_channel` — the bot is not a member of the channel.
- `503 telegram_unavailable` — Telegram's API is unreachable or returned 5xx.
- `409 duplicate_channel` — the user already has this channel in their watch-list.

---

#### `DELETE /api/v2/channels/{channelId}`

Remove the channel from the authenticated user's watch-list. The catalog row is kept (other users may still follow it).

**Response 204**: empty.

**Error codes**:
- `404 channel_not_found` — the user does not have this channel in their watch-list.

---

### Filters

#### `GET /api/v2/channels/{channelId}/filters`

List the authenticated user's filter rules for the given channel (the channel-specific rules only; the user's default filter is fetched via `GET /api/v2/filters?default=true`).

**Response 200**:
```json
{
  "filters": [
    {
      "id": "f1-...",
      "filter_type": "ai_prompt",
      "filter_value": "Only forward crypto news related to Ethereum and ignore Bitcoin",
      "is_active": true,
      "created_at": "2026-06-23T07:30:00Z",
      "updated_at": "2026-06-23T07:30:00Z"
    }
  ]
}
```

---

#### `POST /api/v2/filters/set`

Upsert a filter rule. If `channel_id` is `null`, the rule is the user's default filter (applies to all channels without a per-channel override). If `channel_id` is provided, the rule is per-channel.

**Request**:
```json
{
  "channel_id": "9c0a1f4e-...",
  "filter_type": "ai_prompt",
  "filter_value": "Only forward crypto news related to Ethereum and ignore Bitcoin"
}
```

`filter_type` is one of `keywords`, `ai_prompt`, `category`. `filter_value` is interpreted by `filter_type`:
- `keywords`: comma-separated list, 1–2000 characters.
- `ai_prompt`: free text, 1–4000 characters.
- `category`: a single category name; must match an existing `categories.name` row (case-insensitive).

**Response 200**:
```json
{
  "filter": {
    "id": "f1-...",
    "filter_type": "ai_prompt",
    "filter_value": "Only forward crypto news related to Ethereum and ignore Bitcoin",
    "is_active": true,
    "created_at": "2026-06-23T07:30:00Z",
    "updated_at": "2026-06-23T07:30:00Z"
  }
}
```

**Error codes**:
- `400 invalid_filter_type` — not one of `keywords`, `ai_prompt`, `category`.
- `400 invalid_keywords` — empty or >2000 characters.
- `400 invalid_prompt` — empty or >4000 characters.
- `400 invalid_category` — category name not in the global set.
- `404 channel_not_found` — `channel_id` provided but the user does not follow that channel.

---

#### `GET /api/v2/filters`

List the authenticated user's filters. By default returns only the channel-specific filters; pass `?default=true` to return the default filter.

**Query**:
- `default=true` — return the default filter (`channel_id IS NULL`).

**Response 200**:
```json
{ "filters": [ ... ] }
```

---

#### `PATCH /api/v2/filters/{filterId}`

Update a filter's `filter_value` and/or `is_active` flag. Cannot change the `filter_type` (delete and re-create instead).

**Request**:
```json
{
  "filter_value": "btc, eth, sol",
  "is_active": true
}
```

**Response 200**: the updated filter object.

**Error codes**:
- `404 filter_not_found` — the filter does not exist or belongs to a different user.

---

#### `DELETE /api/v2/filters/{filterId}`

Delete a filter rule. Takes effect on the next cycle; in-flight cycles may finish with the prior rule.

**Response 204**: empty.

**Error codes**:
- `404 filter_not_found` — the filter does not exist or belongs to a different user.

---

### Delivery history

#### `GET /api/v2/deliveries`

List the authenticated user's recent deliveries, newest first.

**Query**:
- `limit` (default 50, max 200)
- `offset` (default 0)
- `status` (optional) — filter by `sent`, `filtered_out`, `send_failed`, `no_match`

**Response 200**:
```json
{
  "deliveries": [
    {
      "id": "ud-...",
      "cycle_id": "c-...",
      "post": {
        "id": "p-...",
        "channel_handle": "durov",
        "channel_display_name": "Durov's Channel",
        "source_msg_id": 12345,
        "link": "https://t.me/durov/12345",
        "raw_text": "Ethereum is moving...",
        "media_kind": "text",
        "captured_at": "2026-06-23T07:25:00Z"
      },
      "status": "sent",
      "filter": { "id": "f1-...", "filter_type": "ai_prompt", "filter_value": "..." },
      "summary": "Ethereum price update",
      "confidence": 0.91,
      "telegram_msg_id": 6789,
      "sent_at": "2026-06-23T07:30:00Z",
      "send_error": null
    }
  ]
}
```

---

#### `GET /api/v2/deliveries/{id}`

Return one delivery record (full details including `raw_text` and `send_error`).

**Response 200**: same shape as a single entry in the list.

**Error codes**:
- `404 delivery_not_found` — the delivery does not exist or belongs to a different user.

---

### Categories (read-only)

#### `GET /api/v2/categories`

Return the global category set. Read-only for users in v2; the operator manages categories via a future admin endpoint.

**Response 200**:
```json
{
  "categories": [
    { "id": "cat-...", "name": "Politics", "ordering": 0, "is_default": true },
    { "id": "cat-...", "name": "Technology", "ordering": 1, "is_default": true }
  ]
}
```

---

### Health

#### `GET /api/v2/health`

Liveness probe. Public (no auth required).

**Response 200**:
```json
{ "status": "ok", "version": "2.0.0", "started_at": "2026-06-23T07:00:00Z" }
```

---

## Error codes (consolidated)

| Code | HTTP | Meaning |
|---|---|---|
| `400 invalid_body` | 400 | Request body is not valid JSON / form-encoded. |
| `400 invalid_handle` | 400 | Channel handle fails the regex. |
| `400 channel_not_found_on_telegram` | 400 | Telegram `getChat` returned 404. |
| `400 bot_not_in_channel` | 400 | Bot is not a member of the channel. |
| `400 invalid_filter_type` | 400 | Not one of `keywords`, `ai_prompt`, `category`. |
| `400 invalid_keywords` / `invalid_prompt` / `invalid_category` | 400 | Filter value failed validation for its type. |
| `400 invalid_interval` | 400 | `digest_interval_seconds` not in [60, 86400]. |
| `400 invalid_name` / `name_too_long` | 400 | `uncategorized_label` failed validation. |
| `400 invalid_delivery_mode` | 400 | Not 'bundled' or 'per_post'. |
| `401 unauthenticated` | 401 | Missing or invalid JWT. |
| `401 hash_mismatch` | 401 | Telegram signature does not match. |
| `401 auth_date_stale` | 401 | `auth_date` older than 5 minutes. |
| `401 nonce_replayed` | 401 | Same payload was already accepted. |
| `401 session_revoked` | 401 | `user_sessions` row's `revoked_at` is set. |
| `401 session_expired` | 401 | JWT `exp` is in the past. |
| `404 channel_not_found` | 404 | Resource does not exist or belongs to a different user. |
| `404 filter_not_found` | 404 | Same. |
| `404 delivery_not_found` | 404 | Same. |
| `409 duplicate_channel` | 409 | User already has this channel in their watch-list. |
| `503 telegram_unavailable` | 503 | Telegram's API is unreachable. |
| `503 ai_unavailable` | 503 | AI provider is unreachable. |
| `500 internal` | 500 | Unhandled error. |

## Versioning

This is the v2 contract. The v1 endpoints (`/api/channels`, `/api/auth/login`, etc.) are removed in `0004_multi_user.sql`; there is no `/api/v1/*` shim. The v2 contract is the only contract for this service from v2 onward.
