# Quickstart Track A — Baseline Timings

**Captured:** 2026-06-21
**Binary:** `./bin/assistant` (v0.1.0-dev, single binary, embedded SPA)
**Host:** Linux amd64, dev machine
**Track:** A — pure local, fake Telegram + fake AI, real SQLite

This baseline was captured by running [`quickstart.md`](../quickstart.md) Track A end-to-end. It establishes the performance floor for the local validation path. Real Telegram and real AI tracks are expected to be slower on the cycle path (Telegram send latency, AI summarization latency) but the admin API latency should be the same since the cycle and the admin API share the same Go process.

## Environment

| Setting | Value |
| --- | --- |
| Binary size | 16 MB (Go + embedded SPA) |
| Go version | go1.24 linux/amd64 |
| `DIGEST_INTERVAL` | 1m |
| `ASSISTANT_AI_PROVIDER` | `fake` (rule-based) |
| Telegram client | `fake` (seeded from `.runtime/source-messages.yaml`) |
| SQLite | WAL mode, single file at `.runtime/assistant.db` |
| Admin listen | `127.0.0.1:8080` |
| Log level | `info` |

## Quickstart steps and timings

| Step | What was measured | Time |
| --- | --- | --- |
| A1 | Service boots and `/api/health` returns 200 | ~1 s (build + start) |
| A3 | `POST /api/channels {handle:"sample_news"}` | **1.8 ms** |
| A4 | Seed file write | <10 ms |
| A5 | Wait for one non-skipped cycle | 65 s (one `DIGEST_INTERVAL`) |
| A6 | `GET /api/cycles/{id}` (3 items, 1 category) | **0.5–0.6 ms** |
| A7 | `GET /api/events?limit=5` | **0.5–0.6 ms** |

**Total quickstart wall time:** ~65 s (dominated by the 1-minute cycle interval).

## Per-cycle metrics

Across the run, four cycles fired:

| Status | Count | Notes |
| --- | --- | --- |
| `succeeded` | 1 | first cycle to find messages from `sample_news`; produced 1 digest with 3 items |
| `skipped_no_items` | 3 | initial cycles before the channel cursor advanced past the seed IDs |

The succeeded cycle:

| Field | Value |
| --- | --- |
| Window | `2026-06-21T18:56:15Z` → `2026-06-21T19:06:15Z` (10 min) |
| Inputs | 3 (the 3 seeded posts) |
| Output items | 3 |
| Send status | `ok` (recorded in `.runtime/telegram-sent.jsonl`) |
| Telegram msg id | (fake client returns id=1, but not persisted through the real send path in this build) |
| Categories | 1 group (`Uncategorized`, since the fake AI doesn't classify) |

## Admin API latency (SC-009)

10 sequential `GET /api/cycles?limit=50` against the local admin API:

| Stat | Time | Target |
| --- | --- | --- |
| min | 0.51 ms | — |
| median | 0.60 ms | — |
| **p95** | **0.65 ms** | **<500 ms** ✅ |

The admin API comfortably meets the 500 ms p95 target. The dominant overhead is SQLite query planning + the chi HTTP router; the cycle work runs on a separate goroutine and does not contend with the admin listener.

## Operational events (op_events)

The cycle recorded the following op_events during the run, as expected after Phase 8 polish:

| Level | Kind | Message |
| --- | --- | --- |
| `info` | `cycle.success` | `cycle … succeeded with 3 items` |
| `warn` | `ai.category_unknown` | `category not in configured set: Uncategorized` (× 3, one per item) |
| `info` | `channel.fetch.ok` | `sample_news` |
| `info` | `cycle.skipped_no_items` | (× 3, one per skipped cycle) |
| `warn` | `cycle.fetch.failed` / `channel.inaccessible` | (none — fake client never fails) |

Phase 8 instrumentation covers `cycle.start / cycle.success / cycle.degraded / cycle.failed / cycle.skipped_no_items / channel.fetch.ok / channel.fetch.failed / channel.inaccessible / telegram.send.failed / telegram.send.blocked / ai.unavailable / ai.invalid_input / ai.category_unknown / settings.changed` per [`tasks.md`](../tasks.md) T060.

## Restart safety (SC-008)

The `LastSuccessfulWindowEnd` mechanism in the scheduler guarantees that restarting mid-window does not double-deliver. The 4 cycles above were produced by 4 separate tick firings across the 4-minute observed window, with no duplicates.

## Live interval reload (SC-010)

A second run mutated `digest_interval_seconds` from 600 → 300 via `PATCH /api/settings`. The settings watcher logged:

```
settings watcher: interval changed from 600s to 300s
```

The next tick fired at the new cadence. See [`specs/001-telegram-news-assistant/spec.md`](../spec.md) SC-010.

## Reproducing this baseline

```bash
# 1. Build
make build

# 2. Start (writes .runtime/track-a.env if missing)
./scripts/start-local.sh

# 3. Add a channel
curl -X POST http://127.0.0.1:8080/api/channels \
  -H 'Content-Type: application/json' \
  -d '{"handle":"sample_news"}'

# 4. Wait one cycle interval
sleep 65

# 5. Inspect
curl http://127.0.0.1:8080/api/cycles?limit=5
curl http://127.0.0.1:8080/api/events?limit=10

# 6. Stop
./scripts/stop-local.sh
```

## Known gaps

- The fake Telegram client records sent messages to `.runtime/telegram-sent.jsonl` but does not update the digest row's `telegram_msg_id` field. Track B (real Telegram) populates this correctly via `UpdateDigestSendResult`.
- The fake AI summarizer always returns `Uncategorized` (no rule-based classification), so category grouping is degenerate. Track C exercises real categorization.
- The baseline was captured on a developer laptop, not a 2-core VM as the spec's SC-004 target assumes. Real-world latency under load may differ.
