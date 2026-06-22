# Implementation Plan: Telegram News Digest Assistant

**Branch**: `001-telegram-news-assistant` | **Date**: 2026-06-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-telegram-news-assistant/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/plan-template.md` for the execution workflow.

## Summary

Phase 1 of the user assistant service is a Telegram news digest assistant. The backend (Go) runs a scheduled loop on a configurable cadence (default 10 minutes) that fetches new messages from a list of public Telegram channels the subscriber has selected, sends each message through an AI summarizer + categorizer (provider abstracted behind a pluggable interface), groups the resulting summaries by category, and delivers a single Telegram message from a designated bot to the subscriber. An admin panel (Svelte SPA served by a small admin API on the same Go process) lets the subscriber curate channels and categories, lets the operator configure the bot token, AI credentials, and digest interval, and lets either role browse digest history. Persistence is provided by a single embedded database; configuration, source-message cursors, and digest records are all stored there so the service restarts cleanly and never re-summarizes or double-delivers.

## Technical Context

**Language/Version**: Go 1.22+ (uses `slices`, `cmp`, `log/slog`); Svelte 4 with SvelteKit (TypeScript) for the admin panel.

**Primary Dependencies**:
- Backend: `github.com/go-telegram-bot-api/telegram-bot-api/v5` for the Telegram Bot API, `net/http` + `github.com/go-chi/chi/v5` for the admin HTTP API, `modernc.org/sqlite` (pure-Go, no CGo) for storage, `github.com/jmoiron/sqlx` for typed DB access, `github.com/google/uuid` for cycle/record IDs, `github.com/sashabaranov/go-openai` (or equivalent) behind an `ai.Summarizer` interface for summarization/categorization, `github.com/caarlos0/env/v10` for config, `log/slog` for structured logging.
- Frontend: SvelteKit 2, Vite 5, TypeScript, `@sveltejs/adapter-static` (admin panel can be statically built and embedded in the Go binary via `//go:embed`), open-props or hand-rolled CSS for styling (no Tailwind unless added later).

**Storage**: SQLite (single file, embedded, durable, zero-ops) for v1. Schema is portable to PostgreSQL later if multi-subscriber / higher concurrency ever becomes a requirement. A single Go process owns the DB; no external DB service.

**Testing**: Go `testing` + `github.com/stretchr/testify`; golden-file tests for the digest renderer; an in-memory fake Telegram client and an interface-driven fake AI summarizer for cycle tests; Svelte component tests with `@testing-library/svelte` + Vitest; an end-to-end smoke scenario exercised by `quickstart.md`.

**Target Platform**: Linux server (amd64 + arm64) running as a single container or systemd service. Optional Windows support is not in scope.

**Project Type**: Web application — backend (Go) + admin frontend (Svelte/SvelteKit) embedded into the same binary (Option 2 in the template, consolidated into a single deployable).

**Performance Goals**: A digest cycle with up to ~50 source messages across ~10 channels must finish (fetch + summarize + render + send) in under 60 seconds on a 2-core VM. Admin panel pages must return in under 500 ms p95. The scheduler must be able to fire a cycle every 10 minutes even if the previous cycle took close to the full window.

**Constraints**:
- Single subscriber, single designated bot (per spec assumption).
- The Telegram Bot API limits reading channel history to messages posted after the bot joined; the service therefore tracks a per-channel cursor (`last_seen_message_id`) and never tries to backfill older history via the Bot API. A second read source (the public web preview at `t.me/s/<handle>`, selected via `TELEGRAM_SOURCE=preview`) relaxes this for public channels — see `research.md` R7.
- Telegram rate limits (≈30 messages/sec global, lower per-chat) must be respected by the send side; a single-message digest is the expected output (FR-007, FR-010, SC-011).
- AI provider latency: each call to the summarizer is treated as a hard timeout; on failure the cycle degrades to a raw-headline digest (FR-005, FR-007, edge case "AI provider outage").
- No external secret store; credentials are read from env vars / a local secrets file and never written to the SQLite DB in plaintext (only a non-secret reference and a fingerprint).

**Scale/Scope**: Phase 1 single-tenant, 1 subscriber, typical 5–50 selected channels, up to ~1000 new messages per 10-minute window in the common case, ~10⁴–10⁵ delivered digests per year. Multi-tenant, multi-subscriber, multi-bot, and horizontal scale are explicitly out of scope for this spec.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

The project's `constitution.md` is still on its unratified template (no principles filled in), so the constitution-derived gate list is currently empty. The plan therefore relies on the **default Spec Kit quality bar** plus the spec's own success criteria, and applies the following gates by convention:

- **G1 – No implementation details in the spec**: PASS (validated in `checklists/requirements.md`).
- **G2 – All FRs are testable**: PASS (every FR-001…FR-018 has at least one acceptance scenario).
- **G3 – All SCs are measurable and tech-agnostic**: PASS (SC-001…SC-011 include numbers, percentages, or count-based outcomes).
- **G4 – Architecture fits the spec scope (single binary, single subscriber)**: PASS — Option 2 of the template is collapsed into a single Go binary with the Svelte admin panel embedded, which keeps deploy + ops simple and matches the "single subscriber / single bot" scope.
- **G5 – No premature multi-tenancy / abstraction**: PASS — AI provider is abstracted behind one interface (FR-018), and storage is abstracted behind one repository interface, but no other speculative abstractions (queues, message buses, separate microservices) are introduced.
- **G6 – Edge cases are covered by FRs or explicit handling**: PASS — bursty channels, dedup, deleted/edited messages, non-text content, blocked bot, rate limits, AI outage, restart, channel privacy, empty categories, and mid-cycle config changes are all called out in the Edge Cases section and reflected in the FR set.

No violations to justify; the **Complexity Tracking** table is therefore not needed.

### Post-Phase 1 re-evaluation

After the design artifacts (`research.md`, `data-model.md`, `contracts/*`, `quickstart.md`) were produced, the same gates were re-checked:

- **G1**: PASS — no tech-stack terms leaked into `spec.md`.
- **G2**: PASS — every FR is now traceable to a contract field, a data-model field, or a quickstart step.
- **G3**: PASS — every SC is now traceable to a quickstart validation step (see the "Mapping back to the spec" table in `quickstart.md`).
- **G4**: PASS — the single-binary + embedded SPA structure in `Project Structure` matches the user-stated Go + Svelte stack and the spec's "single logical deployment" assumption.
- **G5**: PASS — only the two intentional abstractions (`ai.Summarizer`, `internal/store` interfaces) were introduced. The Telegram send path, the renderer, the scheduler, and the deduper are kept concrete and testable.
- **G6**: PASS — each edge case is now anchored to a specific code-level mechanism: dedup → `digest_items.dedup_key` + `internal/digest/dedup.go`; degraded mode → `digests.degraded` + `internal/digest/render.go`; restart safety → `cycles.LastSuccessfulWindowEnd` + `internal/digest/scheduler.go`; channel privacy → `channels.status` + `internal/telegram/fetcher.go`; rate limits → split-and-gap rules in `contracts/telegram-render.md` + `internal/telegram/sender.go`.

All gates continue to pass. No new violations.

## Project Structure

### Documentation (this feature)

```text
specs/001-telegram-news-assistant/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── admin-api.md
│   ├── telegram-render.md
│   └── ai-summarizer.md
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
backend/
├── cmd/
│   └── assistant/
│       └── main.go              # entrypoint: load config, open DB, start scheduler + admin HTTP
├── internal/
│   ├── config/                  # env-based config (chi/cobra-free)
│   ├── store/                   # sqlite repository: channels, categories, digests, cursors, ops
│   │   ├── store.go
│   │   ├── channels.go
│   │   ├── categories.go
│   │   ├── digests.go
│   │   └── health.go
│   ├── telegram/                # bot API client + cycle fetcher + sender (interfaces + fakes)
│   │   ├── client.go
│   │   ├── fetcher.go
│   │   └── sender.go
│   ├── ai/                      # Summarizer interface + default OpenAI-compatible impl + fake
│   │   ├── summarizer.go
│   │   └── openai.go
│   ├── digest/                  # the cycle: fetch → dedup → summarize → categorize → render → send
│   │   ├── cycle.go
│   │   ├── dedup.go
│   │   ├── render.go
│   │   └── scheduler.go
│   ├── adminapi/                # chi handlers for the admin panel + JSON
│   │   ├── server.go
│   │   ├── channels.go
│   │   ├── categories.go
│   │   ├── settings.go
│   │   ├── history.go
│   │   └── health.go
│   └── logging/                 # slog setup
├── migrations/                  # embedded SQL migrations (//go:embed)
│   └── 0001_init.sql
├── go.mod
├── go.sum
└── tests/
    ├── digest_cycle_test.go     # end-to-end cycle with fakes
    ├── render_test.go           # golden-file digest rendering
    ├── dedup_test.go
    └── store_test.go

frontend/
├── src/
│   ├── routes/
│   │   ├── +layout.svelte
│   │   ├── +page.svelte                        # overview / health
│   │   ├── channels/+page.svelte
│   │   ├── categories/+page.svelte
│   │   ├── history/+page.svelte
│   │   ├── history/[id]/+page.svelte
│   │   └── settings/+page.svelte
│   ├── lib/
│   │   ├── api.ts                              # typed client for the admin API
│   │   └── components/
│   │       ├── ChannelList.svelte
│   │       ├── CategoryList.svelte
│   │       ├── DigestViewer.svelte
│   │       └── HealthBadge.svelte
│   └── app.html
├── static/
├── svelte.config.js
├── vite.config.ts
├── package.json
└── tsconfig.json

# build glue
Makefile                                       # `make build` runs `npm run build` then `go build` and embeds the SPA via //go:embed
```

**Structure Decision**: Option 2 (web application) from the template, with the frontend's static build embedded into the Go binary so the service ships as a single artifact. This matches the spec's "single logical deployment" assumption and the operator's "no separate admin app" need. A `Makefile` is the only build glue; no monorepo tooling, no extra workspaces.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none) | — | — |

