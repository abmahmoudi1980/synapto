# Research: Telegram News Digest Assistant

**Feature**: 001-telegram-news-assistant
**Date**: 2026-06-21
**Purpose**: Resolve all `NEEDS CLARIFICATION` markers from the plan, and document the non-obvious technology decisions required to implement the spec.

## Unknowns surfaced by the Technical Context

The plan's Technical Context did not contain any explicit `NEEDS CLARIFICATION` markers, but six real decisions were identified while writing the plan. They are resolved below.

---

## R1. How does the service read channel messages — Bot API or MTProto (gotd/td)?

**Decision**: Use the **Telegram Bot API** (server-side, requires a `@BotFather` token) for both reading source channels and sending the digest. The read side is restricted to channels where the designated bot is a member (for private channels) or that the bot has explicitly joined (for public channels). The service does not backfill history — it tracks a per-channel cursor (`last_seen_message_id`) and only processes new messages from the cursor forward.

**Rationale**:
- The spec explicitly states "designated Telegram bot" and the assumption section commits to a single bot identity. Using MTProto (a user-account client like `gotd/td`) would introduce a second identity, violate the assumption, and require storing a session string — a non-trivial secret.
- The Bot API is well-documented, has a stable Go client (`go-telegram-bot-api/v5`), and the volume of messages per cycle in phase 1 (tens, not thousands) is well within Bot API limits.
- Per-channel cursors make the "no backfill, no double-deliver" requirement trivial: each cycle only ever sees messages with id > cursor.

**Alternatives considered**:
- **`gotd/td` (MTProto user client)**: more powerful, can read any public channel without joining, supports deep history. Rejected because (a) it requires a user account, contradicting the "designated bot" constraint, and (b) it stores a session string that is harder to rotate than a bot token.
- **Telegram's official `telegram-bot-api` server (C++) with `forwardMessage` from a user account**: rejected — same issue as `gotd/td` and adds a separate process to operate.

**Consequences carried into the design**:
- A channel must be added to the bot's membership list before it can be read. The admin panel must surface channels the bot is not yet a member of so the operator can invite the bot (e.g., via the channel's admin UI). The service treats "bot not in channel" as a per-channel error state, not a global failure (edge case: "Channel privacy / access").
- Reading is restricted to messages posted after the bot joined. The cycle is therefore purely a forward-looking sweep; no historical messages are ever processed.

---

## R2. Which Go Telegram library?

**Decision**: `github.com/go-telegram-bot-api/telegram-bot-api/v5` for the read and send paths. It is the de-facto Go Bot API client, has stable releases, and supports long polling and webhook modes (we will use **long polling via `getUpdates`** for the cycle, since we don't need to receive user messages in phase 1; the long-polling loop is short-lived per cycle and exits after we have all new channel posts or after a short timeout).

**Rationale**: Mature, widely deployed, small surface, and avoids the session-management complexity of `gotd/td`. Phase 1 doesn't need MTProto features (typing indicators, secret chats, full chat list).

**Alternatives considered**:
- `telegram-bot-api` (the C++ server): we use the Bot API HTTP endpoints, not this.
- `gotd/td`: rejected in R1.
- Hand-rolled HTTP client: rejected — `go-telegram-bot-api/v5` already does retries, error decoding, and rate-limit handling.

**Consequences**:
- The fetcher and sender interfaces in `internal/telegram` wrap `go-telegram-bot-api` so the cycle logic is testable with an in-memory fake.

---

## R3. AI summarizer interface and default provider

**Decision**: Define a single Go interface, `ai.Summarizer`, in `internal/ai/summarizer.go`:

```go
type Input struct {
    ChannelHandle string
    Text          string
    MediaKind     string // "text" | "image" | "video" | "voice" | "other"
    Captions      []string
}

type Output struct {
    Summary   string
    Category  string
    Confidence float64 // 0..1; 0 if unknown
}

type Summarizer interface {
    Summarize(ctx context.Context, in Input) (Output, error)
}
```

The default implementation targets the **OpenAI Chat Completions API** (works for OpenAI proper and for any compatible endpoint such as OpenRouter, Together, vLLM with an OpenAI-compatible server, or local llama.cpp). A second implementation, `ai.FakeSummarizer`, is used in tests and is wired by default when `ASSISTANT_AI_PROVIDER=fake`.

**Rationale**: A single, narrow interface keeps the cycle logic provider-agnostic (FR-018). The OpenAI-compatible standard is the widest portable target in 2026 and gives the operator freedom to swap providers without recompiling.

**Alternatives considered**:
- Anthropic Messages API as the default: rejected as the default because the OpenAI-compatible target has more compatible upstreams; the `Summarizer` interface is provider-agnostic anyway and an Anthropic adapter is a small follow-up.
- Local model only: rejected as the default because phase 1 prioritizes correctness over cost; the interface allows a local adapter later.

**Consequences**:
- The cycle has a hard per-call timeout (default 8s) and a hard per-cycle budget (default 45s total). On any summarizer error or timeout, the affected item is emitted as a "raw headline" digest entry (degraded mode).
- Categories returned by the AI must match a category in the configured set; if the AI returns an unknown category, the cycle falls back to the configured "uncategorized" label and the value is logged for later taxonomy tuning.

---

## R4. Storage: SQLite vs PostgreSQL

**Decision**: **SQLite** via `modernc.org/sqlite` (pure Go, no CGo) for phase 1. The DB is a single file owned by the Go process; WAL mode is enabled for safe concurrent reads from the admin API while the cycle writes.

**Rationale**:
- The spec's scale is small (1 subscriber, tens of channels, ~10⁴–10⁵ digests/year). SQLite is well within its comfort zone.
- A single-file DB makes "single binary, single artifact" deploys trivial — no separate DB process to operate.
- `modernc.org/sqlite` removes the CGo toolchain requirement, which is a major portability win for cross-compilation.

**Alternatives considered**:
- **PostgreSQL**: stronger under heavy concurrent writes, better for future multi-subscriber. Rejected for phase 1 because it adds a second deployable and a network round-trip for every cycle. The repository layer in `internal/store` is interface-driven so a Postgres adapter can be added later without changing cycle logic.
- **BoltDB / bbolt**: rejected because we want a relational schema (joins across `digests`, `digest_items`, `channels`, `categories`).

**Consequences**:
- The repository layer in `internal/store` exposes interfaces (`ChannelRepo`, `CategoryRepo`, `DigestRepo`, `CursorRepo`, `HealthRepo`, `SettingsRepo`) backed by a single SQLite implementation in phase 1.
- A future PostgreSQL adapter would implement the same interfaces.

---

## R5. Admin panel deployment model: embedded SPA vs separate origin

**Decision**: Build the SvelteKit admin panel with `@sveltejs/adapter-static` and embed the resulting static assets into the Go binary via `//go:embed`. The Go admin HTTP server serves the SPA at `/` and the JSON admin API at `/api/*`. A single binary, a single port, no CORS surface, no separate static host.

**Rationale**:
- Matches the spec's "single logical deployment" assumption and the operator's "one service" mental model.
- Eliminates CORS, separate auth, and cross-origin cookie configuration — the SPA and the API share an origin.
- `//go:embed` is a standard, well-supported Go feature.

**Alternatives considered**:
- **Separate frontend + backend deployments**: rejected because it doubles the deploy surface and forces the operator to manage CORS, cookies, and two service endpoints for a phase-1 single-subscriber service.
- **Server-side SvelteKit (Node)**: rejected because the spec calls for a Svelte frontend, not a SvelteKit Node server, and the embed approach is simpler and faster.

**Consequences**:
- The admin panel does not require its own server runtime (no Node process in production).
- Frontend builds must complete before the Go binary is built; the `Makefile` enforces this order.

---

## R6. Scheduler: built-in ticker vs library

**Decision**: A small in-process scheduler built on `time.Ticker` plus an explicit "fire and queue" guard (a mutex + a `state` field on the cycle) so a cycle never overlaps itself. A long cycle is allowed to slip the next fire; we never start a new cycle before the previous one finishes.

**Rationale**: The cycle is a single goroutine in a single process. A library like `robfig/cron` is overkill for a single periodic job, and the in-house version is < 60 lines and easy to reason about.

**Alternatives considered**:
- `robfig/cron/v3`: rejected for phase 1 because we have exactly one periodic job and no need for cron expressions; the operator configures the interval as a duration.
- External scheduler (systemd timer, k8s CronJob): rejected because we want the cycle to react to configuration changes (FR-013, FR-016) without redeploying, and we want restart-safe cursor handling in-process.

**Consequences**:
- The scheduler is a `digest.Scheduler` type with a single `Run(ctx)` method. It owns the mutex and the "is a cycle running" state.
- On restart, the scheduler reads the last successful cycle's window-end timestamp and the per-channel cursors, so the first post-restart cycle picks up exactly where the last one stopped — satisfying FR-016 and SC-008.

---

## R7. Pluggable read source: long-poll vs public web preview (added after v1)

**Problem**: The Bot API long-poll path (R1) requires the bot to be a member of every channel the subscriber wants to monitor. Operators running a "news aggregator" service often follow public channels they do not administer and cannot invite the bot to (R1 §Consequences). For these, the long-poll path silently produces `skipped_no_items` cycles with no diagnostic.

**Decision**: Add a second `telegram.Client` implementation, `HTTPPreview`, that reads the public web preview at `t.me/s/<handle>` and walks its paginated pages. The bot membership is **not** required for the read path. `SendMessage` still uses the Bot API (the bot is still needed to deliver the digest to the subscriber's chat). The choice is exposed as `TELEGRAM_SOURCE={longpoll,preview}` (default: `longpoll`). The cycle, the cursor, the deduper, the AI summarizer, the renderer, the admin API, and the data model are all unchanged.

**Rationale**:
- The `telegram.Client` interface (`GetChat`, `FetchNewPosts`, `SendMessage`, `Close`) was designed in R2 with pluggability in mind; adding a second implementation is a small, contained change.
- The public web preview is a public surface that any web browser can read, so the "you need to be a member" constraint of the Bot API does not apply. Cursors still work: each cycle asks for posts with `MessageID > last_seen_msg_id`; the page walker stops as soon as it hits the boundary.
- Sending the digest still goes through the Bot API, so the long-poll-vs-preview choice is purely a read-path swap.

**Alternatives considered**:
- **MTProto (`gotd/td`) user client**: rejected in R1 (ToS / session storage). Still rejected here.
- **A third-party RSS bridge service**: rejected — adds an external dependency and a ToS surface; defeats the "single binary, no sidecars" goal.
- **A scraper that renders the page in a headless browser**: overkill; the public preview is plain HTML with stable markup.

**Consequences**:
- The web preview is an **unofficial** surface. t.me may change its markup or rate-limit. The implementation rate-limits itself (1 request per handle per second, with a 20-page cap per fetch to keep cycles bounded) and parses defensively: any structural change that breaks the regex yields zero posts rather than a crash, surfacing the issue in `cycle.skipped_no_items` and `op_events`.
- The preview cannot read private channels (the public preview is not served for them). Long-poll remains the only path for private channels and is still the default.
- Private channels (and operators who want a more reliable source) should keep using `TELEGRAM_SOURCE=longpoll` and arrange for the bot to be added to the channel by an admin.

---

## Cross-cutting design decisions

- **Configuration loading**: env-driven via `caarlos0/env/v10`. A `.env` file (gitignored) is allowed for local dev. Required envs: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_SUBSCRIBER_CHAT_ID`, `AI_BASE_URL`, `AI_API_KEY`, `AI_MODEL`, `ADMIN_LISTEN_ADDR` (default `:8080`), `DIGEST_INTERVAL` (default `10m`), `DB_PATH` (default `./assistant.db`).
- **Logging**: `log/slog` with a JSON handler in production, a text handler in dev. Cycle logs include `cycle_id`, `window_start`, `window_end`, `message_count`, `summary_count`, `telegram_send_ok`, `duration_ms`.
- **Authentication for the admin API**: not in scope for phase 1 (single-tenant, single operator). The admin API binds to a configurable address and the operator is expected to put it behind a reverse proxy / VPN. This is explicitly documented in the **Assumptions** of the spec and is called out as a follow-up in the plan.
- **Testing strategy**:
  - Unit tests for the deduper, the renderer, and the categorizer fallback.
  - Golden-file tests for the digest renderer (one fixture per category-bucket layout).
  - A full end-to-end cycle test using the fake Telegram client and the fake AI summarizer, asserting: cursors advance, dedup works, a single digest is produced, no item is sent twice, a degraded cycle is still recorded.
  - Svelte component smoke tests for the admin panel (one render test per page).

---

## Open questions / follow-ups (not blocking)

- **Auth for the admin API**: explicitly deferred (single-tenant, single operator). When the service grows past one human, the API should add a session cookie + a single admin password read from env.
- **Multi-subscriber / multi-tenant**: out of scope; the SQLite schema is intentionally minimal and would need `subscriber_id` columns added throughout if revisited.
- **OCR / ASR for non-text messages**: out of scope for phase 1; non-text items are emitted with a `[Image]` / `[Video]` / `[Voice]` marker and the original caption if any (FR-017).
- **Horizontal scaling**: out of scope; the single-process design is a hard constraint and the scheduler's mutex model is the right shape for a single node.

---

## R8. Why a persistent post queue instead of in-memory dedup

**Decision**: Replace the per-cycle in-memory `Dedup()` map with a persistent `posts` table keyed on `(channel_id, source_msg_id)`. The cycle reads from this table for the summarize and send steps; the per-channel `last_seen_msg_id` cursor remains as a fetch-side optimization, but the post-row `status` field is the source of truth for the lifecycle.

**Rationale**:
- The per-cycle dedup map is lost on restart. A cycle that fetches a post, advances the cursor, and then crashes before delivering the digest will never re-fetch that post (the cursor is already past it) — the post is silently lost.
- A persistent row per post makes the lifecycle observable at the row level: an operator can answer "what's the status of post 4711?" with a SQL query.
- A failed Telegram send leaves the post in `send_failed`. The next cycle re-bundles it via `ListUnsent` and tries again. No manual retry step.
- Cross-channel content dedup (FR-009, a forwarded post into two channels) still works via the `dedup_key` column on the new `posts` table; the dedup happens at upsert time on the second channel's row.

**Alternatives considered**:
- **Keep in-memory dedup, snapshot the dedup map to disk on every cycle**: rejected because it adds a serialization step and still loses the "which post went into which cycle" linkage.
- **Tie post identity to a `digests` row**: rejected because it couples the durable post to the cycle's transient state.

**Consequences carried into the design**:
- New `posts` table in migration `0002_posts_queue.sql`, plus a backfill of one post per existing `digest_items` row.
- New `PostRepo` interface in `internal/store`, SQLite implementation in `internal/store/sqlite/posts.go`.
- Cycle pipeline rewritten: fetch → `Upsert` (received); summarize → `MarkSummarized`; bundle → `MarkIncluded`; send → `MarkSent` / `MarkSendFailed`.
- New admin endpoints: `GET /api/posts?status=` and `GET /api/posts/{id}`. The per-post view is what makes the queue observable to the operator.
- The unique constraint on `(channel_id, source_msg_id)` is the new write-side safety net; the per-channel cursor remains the read-side optimization.
