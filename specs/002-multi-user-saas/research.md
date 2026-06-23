# Research: Multi-User Telegram News Aggregator

**Feature**: 002-multi-user-saas
**Date**: 2026-06-23
**Purpose**: Resolve all `NEEDS CLARIFICATION` markers from the plan, and document the non-obvious technology decisions required to implement the spec.

## Unknowns surfaced by the Technical Context

The plan's Technical Context did not contain any explicit `NEEDS CLARIFICATION` markers, because all five user-locked architectural decisions (storage engine, bot topology, auth surface, AI filter shape, v1 coexistence) were resolved during the prior planning conversation. Ten real design questions were identified while writing the plan. They are resolved below.

---

## R1. How is the Telegram Login Widget payload verified?

**Decision**: Verify with **HMAC-SHA-256** using a secret derived from the bot token. Two distinct secret derivations, one per Telegram surface, both implemented in `internal/auth/telegram.go`:

- **Login Widget** (the user's chosen primary surface): `secret = SHA-256(bot_token)`. The widget POSTs `id, first_name, last_name?, username?, photo_url?, auth_date, hash` to our callback; `hash = HMAC-SHA-256(secret, data_check_string)` where `data_check_string` is every field except `hash` joined with `\n` in sorted order.
- **Mini App initData** (supported for parity, not the primary path): `secret = HMAC-SHA-256("WebAppData", SHA-256(bot_token))`. The shape is the same; the only difference is the secret derivation.

Both paths share a single `verifyTelegramPayload(botToken, rawQuery, secretPrefix)` function. The same HMAC machinery; only the secret derivation differs.

**Rationale**:
- This is exactly what Telegram's docs prescribe; deviating breaks the wire format.
- HMAC is symmetric, fast, and uses the bot token the operator already configures — no new secret material to rotate.
- The 5-minute `auth_date` freshness check (rejected if older) is built into both paths; replay is bounded by a separate `auth_nonces` table (see R2).
- The same secret prefix doesn't apply to both surfaces; trying to "unify" the two would be wrong.

**Alternatives considered**:
- **Asymmetric (RSA/Ed25519) signature**: not what Telegram uses. Rejected.
- **Hand-rolled HTTP verification without a library**: ~80 lines of code; nothing to gain. The only library we'd reach for is `crypto/hmac` and `crypto/sha256`, which are stdlib.

**Consequences carried into the design**:
- A new `auth.Verifier` interface is introduced so the cycle and handlers can take either Telegram path interchangeably. The cycle never calls the verifier; only the auth handlers do.
- The bot token stays a single operator secret in env vars. We never read it from the DB.

---

## R2. How are sessions represented — JWT, opaque session cookies, or both?

**Decision**: **HS256 JWT** with a `jti` (session id) claim that is looked up in the `user_sessions` table on every request. A row in `user_sessions` is the source of truth for revocation. A revoked `jti` is rejected on the next request.

JWT structure: `{sub: user.id, tid: user.telegram_id, jti: user_sessions.id, iat: now, exp: now + sessionTTL (12h)}`. Signed with the operator-supplied `JWT_SECRET` env var. The secret is required at boot — a missing secret is a hard startup error.

**Rationale**:
- JWTs are stateless on the client side and trivially verifiable in middleware. The signed payload is small (~250 bytes) and self-contained.
- The `jti` lookup is the one piece of state: a single indexed SELECT on the `user_sessions` table, with `revoked_at IS NULL` and `expires_at > now`. This is fast enough to do on every request (O(1) PK lookup) and gives the operator the ability to log out a compromised user in real time.
- Using the bot token (per R1) for session signing would couple auth to bot rotation. A separate `JWT_SECRET` is the right boundary.
- Opaque session cookies (the v1 approach) are simpler but harder to scale across multiple replicas and harder to inspect when debugging. The v1 model also leaked the `AdminPassword` into the session-secret derivation, which we explicitly do not want in v2.

**Alternatives considered**:
- **Opaque session tokens (random IDs) in a server-side table**: simpler in some ways, but every API call is a DB hit and the data model grows with active sessions. The JWT approach makes the token itself a small, self-describing envelope.
- **Long-lived JWTs without a `jti` revocation list**: simplest, but a compromised token is valid for its full TTL. With a `user_sessions` lookup, the operator can kill a token in seconds.
- **Refresh tokens + short-lived access tokens**: overkill for a single-binary service with 12-hour TTLs.

**Consequences carried into the design**:
- `JWT_SECRET` is a new required env var; missing at boot is fatal.
- `internal/auth/jwt.go` owns the `Issue`, `Parse`, and `Revoke` operations. The middleware does the parse + revocation check on every `/api/v2/*` request and sets `X-Synapto-User` + `X-Synapto-Session` for downstream handlers.
- The `user_sessions` table needs a `revoked_at` index; the `expires_at` is a soft TTL — expired rows are cleaned up by a small background goroutine that runs hourly (no criticality if it falls behind).
- Logout sets `revoked_at` and clears the cookie in the same handler. The next request with the same JWT fails the revocation check and gets 401.

---

## R3. How does the cycle keep per-user fan-out bounded?

**Decision**: **Two caps** working together, both configured by the operator (sensible defaults):

1. **Per-cycle cap on deliveries**: a single cycle will attempt at most `PER_CYCLE_DELIVERY_CAP` (default 1,000) per-user sends. The rest are deferred to the next cycle. The cap is per cycle, not per user — a single cycle is allowed to spend all of it on one user's backlog.
2. **Per-user AI concurrency**: the per-user `ApplyUserFilter` calls run with at most `AI_MAX_USER_FILTER_CONCURRENCY` (default 16) in flight at once. A buffered channel semaphore governs this. The same cap applies to the global `Summarize` call (already 8 in v1; stays 8).

A cycle is also bounded by time: `maxCycleDuration = 5 minutes` (hard cap). If the fan-out is still running at the cap, the cycle marks the unfinished deliveries as `send_failed` with reason `"cycle_overrun"` and the scheduler fires the next cycle normally.

**Rationale**:
- The headline concern with single-bot fan-out is O(posts × subscribers) AI calls. With 50 posts and 1,000 users, the unconstrained cost is 50,000 AI calls. At an average 200ms each (OpenAI gpt-4o-mini, sequential), that's ~2.8 hours — well over the 10-minute cycle interval. With concurrency 16 and a 5-minute hard cap, the worst-case cycle stays under the interval.
- Keyword filters are O(text) and add no AI cost; they don't need a cap.
- The per-cycle delivery cap is a backpressure release valve: if the AI is slow, the cycle still delivers some users and queues the rest, rather than missing the cycle entirely.
- The time cap is a hard "no cycle runs forever" guarantee; it matches the v1 spirit (`maxSendAttempts` on per-post retries).

**Alternatives considered**:
- **Per-user background workers** (each user has a goroutine): fans out asynchronously, but complicates restart safety — a crashed worker has to recover its own backlog. Too much state for a 1,000-user service.
- **Per-user rate limiting only**: lets the cycle run forever, which can starve subsequent cycles. Rejected because the v1 scheduler's restart-safety guarantee is "no window skipped" — a long-running cycle would skip a window.
- **Drop the per-user tail (no retry)**: simplest, but loses data. Rejected per FR-014.

**Consequences carried into the design**:
- The cycle now has a single new method on `PostRepo`: `ListReceivedForFanout(ctx, limit int) ([]Post, error)` returning only the `received` posts still needing the global summarize step.
- `user_deliveries` rows whose status is `send_failed` with reason `cycle_overrun` are picked up on the next cycle via the existing `ListUnsentByUser` per-user query (added in the new SQLite files).
- The settings row gains `PER_CYCLE_DELIVERY_CAP` and `AI_MAX_USER_FILTER_CONCURRENCY` (operator-only, not per-user).

---

## R4. How does the hard cut from v1 work without losing data?

**Decision**: The migration `0004_multi_user.sql` runs in a single transaction and:

1. **Creates the new tables** (`users`, `user_sessions`, `user_settings`, `user_channels`, `user_filters`, `user_deliveries`, `auth_nonces`).
2. **Migrates the v1 data forward**:
   - The v1 `channels` table becomes the global catalog. The v1 columns stay; a new `telegram_id` column is added.
   - The v1 `settings.telegram_subscriber_chat` is read once; if non-zero, a synthetic "v1 admin" user is created with `telegram_id = telegram_subscriber_chat` and given ownership of the existing `channels` (one `user_channels` row per channel) and the existing `posts` delivery history (one `user_deliveries` row per `digest_items` row).
   - Existing `categories` are global; the v1 rows carry over unchanged.
   - Existing `cycles`, `digests`, `digest_items`, `posts`, `op_events` rows are kept as global infrastructure. Per-user delivery history is reconstructed from `digest_items` rows, with `user_id` set to the synthetic v1 admin user.
3. **Drops the v1 admin-only fields**: `settings.telegram_subscriber_chat` is removed from the `settings` table; `channels.telegram_id` is set to a placeholder (Telegram channel ids were never used in v1).
4. **Leaves a one-time operator warning** in the migration log if the v1 admin user was auto-created — the operator should rename it to a real name via the new `PATCH /api/v2/users/<id>` operator-only endpoint (out of scope for v2; the synthetic row is just `first_name = "Imported (v1)"` so it's visually distinct).

**Rationale**:
- The hard-cut user decision was explicit; there is no v1 coexistence path. But the operator who is mid-migration should not lose their existing channel list or their historical digests.
- The synthetic v1 user is the simplest "port over the v1 admin" approach. The operator can later delete the synthetic user (which deletes their `user_channels` rows, but the catalog `channels` rows stay, so the data is preserved for new users).
- The transaction wraps the whole thing, so a partial migration is impossible.

**Alternatives considered**:
- **Leave v1's `settings` and `telegram_subscriber_chat` in place** as a fallback: keeps v1 working for the operator, but doubles the auth surface. The user explicitly rejected this.
- **Delete v1 data on upgrade**: simplest, but loses the operator's existing channel list. Unacceptable.

**Consequences carried into the design**:
- The migration is destructive at the schema level (drops columns, drops tables) but conservative at the data level (everything readable in v1 is still readable in v2 via the synthetic user).
- The `Makefile` gains a `make backup-before-migration` target that copies the SQLite file before applying `0004` (so an operator can roll back if they realize they want to revert to v1).
- A `BACKUP_BEFORE_MIGRATIONS=1` env var gates the auto-backup; default off in dev, on in production.

---

## R5. How is a per-user filter resolved for a (user, channel) pair?

**Decision**: A user's effective filter for a channel is the **first non-null** of these three, in this order:

1. `user_channels.custom_filter_id` — the user explicitly pinned a filter to that specific channel. Wins always.
2. `user_filters WHERE user_id = ? AND channel_id = ? AND is_active = 1` — a per-channel filter the user created.
3. `user_filters WHERE user_id = ? AND channel_id IS NULL AND is_active = 1` — the user's "default" filter, applied to any channel without a per-channel filter.

If none of the three exists, the user receives every post (no filter).

**Rationale**:
- The precedence rule is documented and deterministic. The user can reason about which filter applies to which channel from the dashboard.
- The "default" filter model matches the spec's FR-011.
- The "no filter = receive everything" fallback is the simplest and least surprising default.

**Alternatives considered**:
- **Stacking (intersect multiple filters)**: more expressive, but the spec scopes filter types to keywords / ai_prompt / category and does not specify composition. v1 of the SaaS does not need stacking.
- **Templated filters (e.g. "if crypto category AND keyword in list")**: same as above — out of scope for v1.

**Consequences carried into the design**:
- `UserFilterRepo.ResolveFor(ctx, userID, channelID)` returns a single `*UserFilter` or nil. It's called once per (post, subscriber) in the cycle.
- The "default" filter is stored as a row with `channel_id = NULL`; the `UNIQUE (user_id, channel_id, filter_type)` constraint is preserved by treating NULL as a distinct value (SQLite allows multiple NULLs in a UNIQUE column).
- The dashboard shows the resolved filter for a (user, channel) inline in the channels page.

---

## R6. How do keyword filters handle media-only posts?

**Decision**: Keyword filters are evaluated against the **captured text + media caption(s)** — exactly the same content the AI summarizer sees. For a text post, the filter is checked against the text. For an image/video/voice post with a caption, the filter is checked against the caption (because the text field already holds the caption, copied from `m.Caption` in `internal/telegram/real.go:238`). For a media-only post with no caption, the filter is checked against an empty string and never matches.

**Rationale**:
- This is consistent with what the user typed in the dashboard. If a user adds the keyword "ethereum" to a channel, they expect posts that mention ethereum in the caption to match, and pure image posts to not match (no text to search).
- The summarizer already concatenates `RawText` and `Captions` for AI input. We reuse the same logic for keyword matching.

**Alternatives considered**:
- **OCR/ASR for media**: explicitly out of scope (already called out in the v1 spec's "Non-text messages" edge case).
- **Match against the rendered summary**: cheaper than the post text, but loses fidelity ("eth" in the source might be cleaned to "ethereum" in the summary). Rejected.

**Consequences carried into the design**:
- A new `internal/digest/textmatch.go` helper `MatchKeywords(text, captions []string, keywords []string) bool` does the case-insensitive substring check.
- The same helper is reused by both the per-user fan-out and the existing v1 cross-channel dedup helper (which already does hash-based dedup at the post level).

---

## R7. What happens to the v1 `posts` queue, `cycles` log, and `digest_items`?

**Decision**: **All three stay as global infrastructure.** Per-user delivery is layered on top:

- `posts` remains one row per unique `(channel_id, source_msg_id)`. The `summarize` step is global (one call per post), and the result is shared across all subscribers.
- `cycles` remains one row per cycle. The cycle's `input_msg_count` and `output_items` fields keep their meaning (number of source messages, number of user-level deliveries).
- `digest_items` is **removed** in `0004`. It is replaced by `user_deliveries` rows, which is the per-(user, post, cycle) record. The v1 cycle wrote a single bundled `digest_items` row per post; the v2 cycle writes N `user_deliveries` rows (one per subscriber) when matching, or 0 if no subscriber matched.

**Rationale**:
- The post-level deduplication (cross-channel dedup, persistent post-queue) is a property of the source data, not the delivery. Keeping `posts` global avoids re-fetching the same post N times and lets the AI be called once.
- `user_deliveries` is the natural per-user history the user sees in the dashboard.
- Removing `digest_items` simplifies the data model; the migration drops the table after copying the rows to `user_deliveries` (best-effort — the rendered text is reconstructed from the new per-user summary column).

**Alternatives considered**:
- **Keep `digest_items` and add `user_deliveries` as a parallel table**: doubles the storage, doubles the write path. Rejected.
- **Make `posts` per-user**: would require re-fetching the same post N times and re-summarizing it. Rejected per FR-014 (dedup at the post level).

**Consequences carried into the design**:
- The migration 0004 migrates v1 `digest_items` rows to `user_deliveries` rows (one per `digest_items`, attached to the synthetic v1 admin user).
- The v1 `digests` table is kept; the cycle still records one `digests` row per cycle for the bundled render text (used by the admin history view and the "what was the system-level cycle output" audit).
- The cycle's `output_items` column now means "per-user deliveries sent" (was "posts bundled"), so the meaning shifts but the column name stays.

---

## R8. How is the per-user send rate limited?

**Decision**: Reuse the v1 per-post gap (`perPostSendGap = 1.5s`) but apply it **per (user, post)** instead of per post. The cap is per cycle, not per user; the existing per-cycle cap from R3 (1,000 deliveries) is the backstop.

**Rationale**:
- Telegram's per-chat limit is ≈1 msg/sec; the global limit is ≈30/sec. 1.5s/post is well within both, with headroom for the 1,000-user × 50-post case (50,000 deliveries / 1.5s ≈ 21 hours of pure send time at full load). The cycle's 5-minute cap and the per-cycle delivery cap from R3 are the real bound; the per-send gap is just to avoid hammering the API.
- No change to the v1 `Telegram.SendMessage` path; the gap is implemented in the cycle's send loop, not in the client.

**Alternatives considered**:
- **A token-bucket rate limiter in the `telegram.Client`**: more correct long-term, but the cycle is the only caller and the per-post gap is the right granularity for "wait between messages to the same chat." Defer.
- **Adaptive gap based on Telegram's 429 response**: already implemented in v1 (`tooManyRequestsDelay` in `real.go:430`); reuse.

**Consequences carried into the design**:
- `perPostSendGap` becomes a function of the (user, post) pair, not the post alone. The constant moves from `per_post_cycle.go:19` to `cycle.go` (in the new per-user send section).
- The existing `isTooManyRequestsErr` and `tooManyRequestsDelay` paths in `telegram/real.go` are reused unchanged.

---

## R9. How does the system handle a user with no subscriptions?

**Decision**: An empty watch-list is a normal state. The dashboard shows "Add a channel to start." No digest cycles run for that user (the cycle's fan-out iterates `users` × `user_channels`; if `user_channels` is empty for a user, no AI calls and no sends happen for that user). No "no items" message is sent.

**Rationale**:
- Matches the v1 behavior (no cycle work for an empty channel set, no "no items" message).
- The cycle's per-user cycle opt-out is a future feature (e.g. a user can pause their subscriptions); not in v1.

**Alternatives considered**:
- **A per-user "active" toggle on `users`**: clean future API, but the v1 spec does not require it. Defer.

**Consequences carried into the design**:
- The cycle's per-user fan-out iterates only `user_channels` rows where the underlying channel is active. An empty result is the no-op.
- The dashboard's empty state is a static component, not a special API call.

---

## R10. How does an operator-set system default filter work?

**Decision**: A new column on the v2 `settings` table, `system_default_filter TEXT`, holds a single string in the same format as `user_filters.filter_value` (comma-separated keywords or a natural-language prompt). At user creation (first login), if the field is non-null, the system creates one `user_filters` row with `channel_id = NULL` (the user's "default" filter) and `filter_value = settings.system_default_filter`, `is_active = 1`, `filter_type = 'keywords'` if the value contains commas, otherwise `'ai_prompt'` (heuristic).

**Rationale**:
- A simple operator policy ("block ads everywhere") is a one-line config, not a per-user onboarding flow.
- The user can override their inherited default at any time (FR-024). The override is a normal `user_filters` update.
- The heuristic (commas → keywords, else → ai_prompt) is a reasonable default for an operator who wants to ship a small set of "boring news" exclusions as keywords; if the operator wants an AI prompt, they can write it without commas.

**Alternatives considered**:
- **Per-user onboarding flow with operator-set categories**: more flexible, but the spec scopes filters to keywords / ai_prompt / category, not to a guided "set your first filter" UX. Defer.
- **No system default filter**: simplest, but operators hosting the service in a regulated environment need a baseline. Rejected.

**Consequences carried into the design**:
- A new `user_filters` row is created as part of the `UserRepo.UpsertFromLogin` path (called on every successful login if the user is new). Idempotent: existing users don't get the default retroactively.
- The operator can update the field at runtime via `PATCH /api/v2/admin/settings` (operator-only, in a future iteration; for v1 the field is set via env at deploy time or via direct DB write).
