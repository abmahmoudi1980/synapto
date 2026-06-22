# Contract: Telegram Digest Render Format

**Feature**: 001-telegram-news-assistant
**Version**: 0.1.0
**Audience**: the `digest.Render` function in `internal/digest/render.go` and any client (the Telegram bot, the admin "history" view) that needs to display the same content.

## Goals

- The rendered text fits in **one Telegram message** for the common case (≤ 50 items, ≤ 4 KB of UTF-8 text). When it would exceed Telegram's hard limit (4096 characters per message), the renderer splits deterministically (see "Splitting" below).
- The format is human-readable on a phone screen: clear category headings, short bullet lines, a header line with the digest window, and a footer with the cycle id and a degraded-mode indicator when applicable.
- The format is also greppable / parseable enough for the admin history view to show the same content it would in Telegram (no separate "rendered" form per surface).

## Top-level shape

```
📰 News digest — <window_end in local-ish time>

# <Category 1>
• <summary 1>  _(channel_handle)_
• <summary 2>  _(channel_handle)_

# <Category 2>
• <summary 3>  _(channel_handle)_

(footer line, always present)
— cycle <short_id> · <item_count> items · <degraded|ok>
```

### Field-by-field rules

- **Header**: `📰 News digest — <window_end in YYYY-MM-DD HH:MM UTC>`. The leading `📰` is a fixed emoji; the timestamp uses UTC for determinism (no per-user timezone in phase 1).
- **Category heading**: a single line, `\# <name>` (the leading `#` is backslash-escaped so Telegram's MarkdownV2 parser accepts it as a literal `#`, not as a header marker). Names are taken verbatim from the `categories` table; if a category has been removed, items in it are placed under the `uncategorized_label` from settings. The escaped heading renders as plain `# Technology` text — not a bold heading — which is the trade-off for parseability.
- **Item line**: a single bullet, `• <summary>  _(<channel_handle>)_`. The trailing `_(...)_` is the channel handle, lowercased, no leading `@`. For non-text items, the summary is prefixed with `[<MediaKind>] ` (e.g. `[Image]`, `[Video]`, `[Voice]`); the bracketed prefix is rendered as part of the summary text.
- **Footer**: a single line, `— cycle <short_id> · <N> items · <status>`. `<short_id>` is the first 8 chars of the cycle UUID. `<status>` is `ok` for a clean cycle, `degraded` for a cycle that fell back to raw headlines (FR-007 edge case).
- **No trailing newlines** other than the one terminating the footer line.

### Summary text rules

- Summaries are produced by the AI summarizer. If the AI returns a summary longer than 280 characters, the renderer truncates at 277 characters and appends `…`.
- Summaries are written on a single line; embedded newlines in the AI output are replaced with a single space.
- The renderer is fully MarkdownV2-aware. Every reserved character in the rendered text is escaped so Telegram accepts the message. The set, in priority order:
  - `` ` `` → `` ` `` + ZWSP (`\u200b`) before it (so it stays visible but doesn't open a code span)
  - `*` → `*` + ZWSP before it (so the AI's mid-word asterisks don't open bold/italic)
  - `_` → `\_` (Telegram MarkdownV2 escape; appears as a literal `_`)
  - `[` → `\[`, `]` → `\]`, `(` → `\(`, `)` → `\)` (literal brackets and parens)
  - `~` → `\~`, `` ` `` → `` ` ``+ZWSP, `>` → `\>`, `#` → `\#`, `+` → `\+`, `-` → `\-` (date hyphens, headings, etc.)
  - `=` → `\=`, `|` → `\|`, `{` → `\{`, `}` → `\}`, `.` → `\.`, `!` → `\!`
- The renderer uses **Telegram MarkdownV2** (`parse_mode = MarkdownV2`) for the send call. This is documented in `internal/telegram/sender.go`; if MarkdownV2 is rejected by Telegram for any reason, the sender retries once with `parse_mode = ""` (plain text) using the same content with all escape characters stripped.

### Non-text items

- `media_kind = image` → `[Image] <caption or media_kind if no caption>`
- `media_kind = video` → `[Video] <caption or media_kind>`
- `media_kind = voice` → `[Voice] <caption or media_kind>`
- `media_kind = other` → `[Media] <caption or media_kind>`

The bracketed prefix is part of the summary text, not a separate field, so the admin history view shows the same thing Telegram shows.

## Empty / suppressed cycles

When the cycle finds no new items in any selected channel, the cycle is recorded as `skipped_no_items` and **no message is sent to Telegram** (FR-008, SC-002). The renderer does not produce a "no items" message; the cycle row carries the state.

When the cycle finds new items but the send side fails, the digest row's `send_status` is set to `failed` (or `blocked` if Telegram returned a "bot was blocked by the user" error). The rendered text is still recorded in `digests.rendered_text` so the admin history view can show what would have been sent.

## Splitting (when a single message would exceed 4096 characters)

Telegram's per-message cap is 4096 characters. The renderer is responsible for keeping each part under that cap. The split rules, in order of precedence:

1. If a single item's summary (with its channel suffix) is longer than 3500 characters, the renderer truncates that item to 3497 characters and appends `…`; that item alone occupies its own message.
2. Otherwise, items are packed into messages in their existing order (categories in `categories.ordering ASC`, items in `digest_items.ordering ASC`). The renderer greedily fills each message until the next item would push the total over 3900 characters (leaving headroom for the header/footer of the final message and the Telegram protocol overhead). When the cap would be exceeded, the current message is closed, a new message is started, and the next item is placed in the new one.
3. The header line is included in the **first** message only. Every subsequent message in the split starts with `📰 News digest (continued) — <window_end>`.
4. The footer line is included in the **last** message only.
5. The Telegram `sender.SendBatch` call sends each part sequentially with a 250 ms gap to stay well under Telegram's per-second send rate.

## Degraded mode indicator

When a cycle's AI summarizer failed and the renderer fell back to raw headlines:

- Each item line begins with `⚠️ ` instead of `• `.
- The footer ends with ` · degraded (AI unavailable)`.
- The `digests.degraded` column is set to `1`.

## Worked example (single message, two categories, three items)

The example below shows the **raw string sent over the wire**, including all MarkdownV2 escapes. The human sees the same text without the backslashes (Telegram renders the escaped form).

```
📰 News digest — 2026\-06\-21 07:20 UTC

\# Technology
• Telegram rolls out scheduled messages in channels  \_(telegram\)
• A new open\-source LLM beats GPT\-4 on a public benchmark  \_(ml\_news\)

\# Politics
• EU parliament passes the AI Liability Directive  \_(eu\_updates\)

— cycle 8a3f1c20 · 3 items · ok
```

## Worked example (split across two messages, same cycle)

Message 1:
```
📰 News digest — 2026\-06\-21 07:20 UTC

\# Technology
• … (items 1..N)

\# Politics
• … (items N+1..M)
```

Message 2:
```
📰 News digest (continued) — 2026\-06\-21 07:20 UTC

\# Sports
• … (items M+1..K)

— cycle 8a3f1c20 · K items · ok
```

## Worked example (degraded mode, AI unavailable)

```
📰 News digest — 2026\-06\-21 07:20 UTC

\# Technology
⚠️ <verbatim message text, ≤ 280 chars>  \_(telegram\)

— cycle 8a3f1c20 · 1 items · degraded (AI unavailable)
```

## Why this format

- One emoji + plain text headings + bullet items reads cleanly in the Telegram mobile app.
- The `_(channel_handle)_` suffix gives the subscriber a quick way to know where a story came from without leaving the message.
- The footer gives the cycle id for cross-reference with the admin history view, and the degraded-mode indicator is visible inline (no separate system message).
- MarkdownV2 gives a clean look while the explicit escaping rules keep summaries from accidentally breaking the format.
