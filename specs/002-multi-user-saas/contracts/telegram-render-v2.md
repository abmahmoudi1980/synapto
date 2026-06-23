# Contract: Telegram Render (v2 — Per-User)

**Feature**: 002-multi-user-saas
**Version**: 2.0.0
**Purpose**: Define the per-message text the digest cycle sends to a single user for one matched post. The v2 cycle is per-user fan-out: each (post × subscriber) match produces one Telegram message.

## Channel-agnostic format

Every per-user delivery is a single Telegram message. The format is the same regardless of whether the user is using `bundled` or `per_post` delivery mode:

- `bundled`: the v1 bundled digest is replaced by a per-user bundled digest. One Telegram message per cycle per user, containing all matched posts grouped by category (when category filters or category assignments are involved) or in captured-at order (when no category is involved). The per-user bundled digest is rendered by walking the per-user delivery records of the cycle and producing the v1 bundled text, with the user-tailored summaries in place of the global summaries.
- `per_post` (the default): one Telegram message per matched post. The format is the **per-user single-message format** below.

Both modes use the same per-message body shape; the bundled mode just concatenates them inside a single Telegram message (with the same MarkdownV2 escaping the v1 renderer used).

## Per-user single-message format

The cycle calls `PerUserRender(post, filter, summary)` to produce the text. The function takes:

- `post` — the source-message record (channel handle, link, media kind).
- `filter` — the resolved effective filter (may be nil; the rendering is the same — no filter annotation).
- `summary` — the per-user tailored summary from the AI call. If the filter is `keywords` or `category` (no AI per-user call), this is the global summary from the standard summarization step.

The output is MarkdownV2 text. The character set is the same subset Telegram MarkdownV2 supports; the v1 escape rules apply (see `backend/internal/digest/render.go` for the full list of escape characters).

### Layout

```
{summary}

— {channel_display_name} (@{channel_handle})
{link}
```

When the source message is media-only with no caption, `{summary}` is replaced with a media-kind marker (e.g. `[Image]` for `media_kind = image`), matching the v1 behavior. The cycle never sends an empty `{summary}`.

### Example

User A is subscribed to `@durov` with an ai_prompt filter of `"Only forward crypto news"`. A new post arrives: `"Ethereum is moving up 3% today"`. The AI returns `{match: true, summary: "ETH up 3% on volume"}`. The delivered message is:

```
ETH up 3% on volume

— Durov's Channel (@durov)
https://t.me/durov/12345
```

### Example (no filter)

User B is subscribed to `@durov` with no filter. The same post arrives. The AI produces the global summary `"Ethereum is up 3% today"`. The delivered message is:

```
Ethereum is up 3% today

— Durov's Channel (@durov)
https://t.me/durov/12345
```

### Example (keyword filter, no per-user AI call)

User C is subscribed to `@durov` with a `keywords` filter of `"btc,eth"`. The same post arrives. The keyword match is local; the cycle uses the global summary. The delivered message is identical to User B's:

```
Ethereum is up 3% today

— Durov's Channel (@durov)
https://t.me/durov/12345
```

The cycle records `user_deliveries.summary = <global summary>` and `user_deliveries.filter_id = <keywords filter id>`.

## Bundled mode layout

When the user's `delivery_mode` is `bundled`, the cycle produces a single Telegram message containing all matched posts, grouped by category (when category assignments are involved) or in captured-at order. The per-message body is the same as above; the bundled layout wraps the per-message bodies with a header and a footer:

```
📰 Synapto digest for {first_name} — {window_start} → {window_end}

{per-message 1}

{per-message 2}

…

{per-message N}

{matched N} matched · {delivered N} sent · {filtered N} skipped
```

The category-grouped variant uses the v1 category headings (e.g. `# Technology`, `# Business`) as section headers, with the per-message bodies underneath. The MarkdownV2 escaping for the `# ` prefix is the same as the v1 bundled renderer.

The bundled mode has a hard character cap equal to Telegram's per-message limit (4096 chars) plus a 10% headroom. If the rendered text would exceed the cap, the renderer splits the output into N messages, each respecting the cap. The split is deterministic: a post is never broken across messages; the renderer walks the items in order and starts a new message when adding the next item would exceed the cap. The cycle records one `user_deliveries` row per matched post regardless of how many Telegram messages the bundled text was split into; the `telegram_msg_id` column stores the message id of the message in which the post was sent.

## MarkdownV2 escaping

The renderer escapes the v1 character set (`_ * [ ] ( ) ~ \` > # + - = | { } . !`) with a leading backslash when they appear in user-controlled or AI-controlled text. The `summary` field (whether global or per-user-tailored) is the highest-risk input: the AI prompt instructs the model to produce plain text, and the renderer defensively escapes the output. The v1 `escapeMarkdownV2` helper in `render.go` is reused unchanged.

## Throttling

- Per-(user, post) gap: 1.5 seconds (`perPostSendGap`).
- Per-cycle cap on deliveries: 1,000 (`PER_CYCLE_DELIVERY_CAP`).
- Per-user AI concurrency: 16 (`AI_MAX_USER_FILTER_CONCURRENCY`).
- Cycle time cap: 5 minutes.

When the per-cycle cap is hit, the deferred deliveries are recorded as `status='send_failed'` with `send_error='cycle_overrun'`. The next cycle picks them up.

## Failure handling

- Telegram returns "bot was blocked": the cycle sets `status='send_failed'` with `send_error='blocked'` and records the `telegram.send.blocked` op event. Future cycles skip this user (a per-user "stop sending" flag on the `users` row, set to true on first block). The cycle continues with other users.
- Telegram returns 429: the cycle honors the Retry-After delay (reuses `tooManyRequestsDelay` from `telegram/real.go:430`) and retries once. A second failure records `status='send_failed'`.
- Telegram returns 5xx: the cycle records `status='send_failed'` with the error string. The next cycle retries.
- AI returns a per-user error (network, timeout, ErrUnavailable): the cycle falls back to a degraded per-user delivery using the global summary and a `[best effort — AI unavailable]` prefix, records `status='sent'` with `send_error='ai_degraded'`. The next cycle retries with a real AI call. (Per FR-016: never silently drop the message.)

## Audit trail

Every per-user delivery (regardless of status) creates one `user_deliveries` row and one `op_events` row of the corresponding kind (`delivery.sent`, `delivery.filtered_out`, `delivery.no_match`, `delivery.send_failed`, `delivery.cycle_overrun`). The user's `GET /api/v2/deliveries` view reads from `user_deliveries` only; the `op_events` view is operator-only and lives behind a future admin endpoint.
