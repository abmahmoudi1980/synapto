---
description: "Task list for the Telegram News Digest Assistant feature"
---

# Tasks: Telegram News Digest Assistant

**Input**: Design documents from `/specs/001-telegram-news-assistant/`
- `plan.md` (required): tech stack, libraries, project structure
- `spec.md` (required): user stories US1–US5
- `research.md`: R1–R6 decisions (Bot API, AI interface, SQLite, embedded SPA, in-process scheduler)
- `data-model.md`: SQL schema and repository interfaces
- `contracts/admin-api.md`, `contracts/telegram-render.md`, `contracts/ai-summarizer.md`
- `quickstart.md`: Tracks A (local), B (real Telegram), C (real AI)

**Organization**: Tasks are grouped by user story so each story can be implemented, tested, and delivered independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (`US1`…`US5`)
- **[US#] label is required** on tasks in Phases 3–7 (user story phases) and is **omitted** on Setup, Foundational, and Polish phases.
- Include exact file paths in descriptions.

## Path Conventions

- **Backend (Go)**: `backend/cmd/...`, `backend/internal/...`, `backend/migrations/...`, `backend/tests/...`
- **Frontend (Svelte)**: `frontend/src/...`, `frontend/static/...`
- **Build glue**: `Makefile` at repository root
- **Spec artifacts**: `specs/001-telegram-news-assistant/...`

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure for both the Go backend and the Svelte frontend.

- [X] T001 Create repository layout per plan.md: `backend/`, `frontend/`, `Makefile`, and `backend/migrations/` directories
- [X] T002 Initialize backend Go module in `backend/go.mod` with dependencies: `github.com/go-telegram-bot-api/telegram-bot-api/v5`, `github.com/go-chi/chi/v5`, `github.com/jmoiron/sqlx`, `modernc.org/sqlite`, `github.com/google/uuid`, `github.com/sashabaranov/go-openai`, `github.com/caarlos0/env/v10`, `github.com/stretchr/testify`
- [X] T003 [P] Initialize frontend SvelteKit project in `frontend/` with Svelte 4, SvelteKit 2, Vite 5, TypeScript, and `@sveltejs/adapter-static`
- [X] T004 [P] Configure Go linting in `backend/.golangci.yml` (gofmt, govet, errcheck, staticcheck) and add a `make lint-go` target to `Makefile`
- [X] T005 [P] Configure frontend linting/formatting in `frontend/.eslintrc.cjs`, `frontend/.prettierrc`, and add `make lint-fe` target to `Makefile`
- [X] T006 Add `Makefile` targets: `deps`, `build`, `run`, `test`, `lint`, `clean` (build target runs `npm run build` then `go build -o bin/assistant ./backend/cmd/assistant`)
- [X] T007 [P] Add empty initial migration file `backend/migrations/0001_init.sql` with a header comment block and a single `SELECT 1;` placeholder (real schema added in Phase 2)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented. After this phase, the binary boots, the store works, the fakes are wired, and the cycle can be tested in isolation.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T008 Implement migration runner with `//go:embed` in `backend/internal/store/migrate.go` (lexical-order apply, `schema_migrations` table, error-on-failed-migration aborts startup)
- [X] T009 [P] Implement env-driven config loader in `backend/internal/config/config.go` with fields: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_SUBSCRIBER_CHAT`, `AI_PROVIDER`, `AI_BASE_URL`, `AI_MODEL`, `AI_API_KEY`, `AI_PER_CALL_TIMEOUT`, `AI_MAX_CONCURRENCY`, `DIGEST_INTERVAL`, `ADMIN_LISTEN_ADDR`, `DB_PATH` (defaults: `AI_PROVIDER=fake`, `DIGEST_INTERVAL=10m`, `ADMIN_LISTEN_ADDR=127.0.0.1:8080`, `DB_PATH=./assistant.db`)
- [X] T010 [P] Implement slog setup in `backend/internal/logging/logging.go` (JSON handler for prod, text handler for dev, log level from `LOG_LEVEL` env)
- [X] T011 Implement repository interfaces in `backend/internal/store/store.go`: `ChannelRepo`, `CategoryRepo`, `SettingsRepo`, `CycleRepo`, `DigestRepo`, `CursorRepo`, `HealthRepo` (signatures from `data-model.md`)
- [X] T012 Implement concrete SQLite repositories in `backend/internal/store/sqlite/` (one file per repo: `channels.go`, `categories.go`, `settings.go`, `cycles.go`, `digests.go`, `health.go`) using `modernc.org/sqlite` + `sqlx` and the schema from `data-model.md` `0001_init.sql`
- [X] T013 [P] Implement `ai.Summarizer` interface and error sentinels (`ErrUnavailable`, `ErrInvalidInput`, `ErrCategoryUnknown`) in `backend/internal/ai/summarizer.go`
- [X] T014 [P] Implement `ai.Fake` summarizer in `backend/internal/ai/fake.go` with rule-based matching and a default fallback (truncate text → `Uncategorized`, confidence 0.5)
- [X] T015 [P] Implement `telegram.Client` interface in `backend/internal/telegram/client.go` with `GetChat`, `GetUpdates`, `SendMessage` methods (uses `go-telegram-bot-api/v5` under the hood)
- [X] T016 [P] Implement `telegram.Fake` client in `backend/internal/telegram/fake.go` that reads seed messages from `.runtime/source-messages.yaml` and records sent messages to `.runtime/telegram-sent.jsonl`
- [X] T017 Implement chi HTTP server skeleton with JSON + slog middleware in `backend/internal/adminapi/server.go` (registers `/api/*` and `/` routes; serves SPA at `/`)
- [X] T018 Implement SPA embedding via `//go:embed` in `backend/internal/adminapi/static.go` (embeds `frontend/build/` output; falls back to a "UI not built" page when the embed dir is empty in dev)
- [X] T019 Implement health endpoint `GET /api/health` in `backend/internal/adminapi/health.go` returning `{status, version, uptime_seconds, last_successful_cycle_at, last_failure_at, last_failure_reason, scheduler_state, db_ok}` per `contracts/admin-api.md`
- [X] T020 Implement main.go entrypoint in `backend/cmd/assistant/main.go` that wires config → logging → store (with migration) → ai (provider chosen by config) → telegram (real or fake) → adminapi → scheduler (stub that ticks every `DIGEST_INTERVAL` and exits cleanly) → graceful shutdown on SIGINT/SIGTERM

**Checkpoint**: Foundation ready — `make build && ./bin/assistant` boots, `curl /api/health` returns `ok` with `db_ok: true`, and the scheduler tick is observable in logs.

---

## Phase 3: User Story 1 - Receive Periodic News Digest (Priority: P1) 🎯 MVP

**Goal**: Every `DIGEST_INTERVAL`, the service fetches new messages from selected channels, runs them through the AI summarizer + categorizer, groups the results by category, and sends a single Telegram message to the subscriber. Cycles with no new items are recorded but produce no message.

**Independent Test**: With one or more channels seeded via the fake Telegram client, the next cycle fetches their messages, summarizes and categorizes them via the fake AI, renders one MarkdownV2 message, and writes a row to `digests` and an entry to `.runtime/telegram-sent.jsonl`. A cycle with no new items records a `skipped_no_items` row in `cycles` and writes nothing to the sent log (FR-007, FR-008, SC-001, SC-002, SC-011). Restarting the service mid-window must not double-deliver (SC-008).

### Tests for User Story 1

> **NOTE**: Write these tests FIRST, ensure they FAIL before implementation.

- [X] T021 [P] [US1] Golden-file test for `digest.Render` (MarkdownV2 escaping, header, category grouping, footer, single-message and split-message cases) in `backend/tests/render_test.go`
- [X] T022 [P] [US1] Table-driven test for `digest.Dedup` (per-cycle dedup, cross-channel dedup, media-only signature, ordering) in `backend/tests/dedup_test.go`
- [X] T023 [P] [US1] End-to-end cycle test with fake Telegram + fake AI in `backend/tests/digest_cycle_test.go` (one channel, three messages → exactly one digest row, three items, one send, cursor advanced)

### Implementation for User Story 1

- [X] T024 [US1] Implement `digest.Dedup` (per-cycle dedup keyed on `sha256(normalize(text))` and on a media signature for media-only items) in `backend/internal/digest/dedup.go` (depends on T021, T022, T023)
- [X] T025 [US1] Implement `digest.Render` (MarkdownV2 escaping, header, category grouping, footer, splitting with 250ms gap, degraded-mode markers) in `backend/internal/digest/render.go` per `contracts/telegram-render.md`
- [X] T026 [US1] Implement `digest.Cycle` (fetch → dedup → summarize-with-budget → categorize → render → send → record `cycles` and `digests` rows) in `backend/internal/digest/cycle.go`
- [X] T027 [US1] Implement `digest.Scheduler` (mutex-guarded ticker, reads `LastSuccessfulWindowEnd` on startup for restart safety, never overlaps a running cycle) in `backend/internal/digest/scheduler.go`
- [X] T028 [US1] Wire scheduler into `main.go` in `backend/cmd/assistant/main.go` (pass `Cycle` + `SettingsRepo`; replace the Phase-2 stub ticker with a real `Scheduler`)

**Checkpoint**: User Story 1 is fully functional — Track A of `quickstart.md` (steps A1–A8) passes end-to-end, and the four acceptance scenarios in spec.md US1 hold.

---

## Phase 4: User Story 2 - Select and Manage Source Channels (Priority: P1)

**Goal**: The subscriber can add, view, and remove Telegram channels through the admin panel. A channel added via the panel appears in the very next digest cycle (SC-005). Removing a channel excludes it from the next cycle and the change persists across restarts (SC-010).

**Independent Test**: `POST /api/channels` with `{"handle":"sample_news"}` returns 201 and the row appears in the next `GET /api/cycles`; `DELETE /api/channels/{id}` removes the row and the next cycle no longer fetches from it; both survive a service restart. Invalid handles are rejected with the `invalid_handle` or `bot_not_in_channel` error codes from `contracts/admin-api.md`.

### Tests for User Story 2

- [ ] T029 [P] [US2] Admin API contract test for channels (200 list, 201 add happy path, 400 invalid handle, 409 duplicate, 404 not found) in `backend/internal/adminapi/channels_test.go`

### Implementation for User Story 2

- [ ] T030 [P] [US2] Implement `telegram.Fetcher` in `backend/internal/telegram/fetcher.go` (uses `Client.GetChat` to validate on add, advances `channels.last_seen_msg_id` after each successful cycle, marks channels `inaccessible` on repeated failures)
- [ ] T031 [P] [US2] Implement `telegram.Sender` (MarkdownV2 send with one retry on plain-text fallback, 250ms inter-message gap) in `backend/internal/telegram/sender.go`
- [ ] T032 [P] [US2] Admin API handlers in `backend/internal/adminapi/channels.go` for `GET /api/channels`, `POST /api/channels`, `DELETE /api/channels/{id}` (depends on T029, T030)
- [ ] T033 [P] [US2] Frontend typed API client in `frontend/src/lib/api.ts` with `listChannels`, `addChannel`, `removeChannel` methods
- [ ] T034 [P] [US2] Frontend layout in `frontend/src/routes/+layout.svelte` (top nav: Overview, Channels, Categories, History, Settings) and Channels page in `frontend/src/routes/channels/+page.svelte`
- [ ] T035 [P] [US2] Frontend `ChannelList` component in `frontend/src/lib/components/ChannelList.svelte` (table with handle, display name, last observed, status badge, remove button)
- [ ] T036 [US2] Wire channel handlers in `main.go` and confirm Track A step A3 of `quickstart.md` passes (depends on T032, T034)

**Checkpoint**: US1 + US2 are both fully functional and testable independently — Track A steps A3 and A7 of `quickstart.md` pass.

---

## Phase 5: User Story 3 - View and Configure Categories (Priority: P2)

**Goal**: The subscriber can add, rename, and remove categories. Removing a default category is refused with `cannot_remove_default`. The next delivered digest reflects the change in its category headings (SC-006).

**Independent Test**: `POST /api/categories {"name":"AI & ML"}` returns 201 and the next digest groups matching items under the new heading. `DELETE /api/categories/{id_of_Politics}` returns `cannot_remove_default`. `PATCH /api/categories/{id}` with a new name changes the heading in the next digest.

### Tests for User Story 3

- [ ] T037 [P] [US3] Admin API contract test for categories (list, add, rename, delete custom, refuse delete default) in `backend/internal/adminapi/categories_test.go`

### Implementation for User Story 3

- [ ] T038 [P] [US3] Admin API handlers in `backend/internal/adminapi/categories.go` for `GET /api/categories`, `POST /api/categories`, `PATCH /api/categories/{id}`, `DELETE /api/categories/{id}`
- [ ] T039 [P] [US3] Implement `CategoryRepo.EnsureDefaults` call on startup in `backend/cmd/assistant/main.go` (idempotent insert of Politics, Technology, Business, Sports, World, Other)
- [ ] T040 [P] [US3] Update `digest.Cycle` in `backend/internal/digest/cycle.go` to map `ai.ErrCategoryUnknown` to `settings.uncategorized_label` and log a `warn` event (FR-006 acceptance scenario 3)
- [ ] T041 [P] [US3] Frontend `CategoryList` component in `frontend/src/lib/components/CategoryList.svelte` (add, inline rename, remove with confirmation)
- [ ] T042 [P] [US3] Frontend Categories page in `frontend/src/routes/categories/+page.svelte` (depends on T041)
- [ ] T043 [US3] Wire category handlers in `main.go` and confirm SC-006 in `quickstart.md` step A7 (depends on T038, T042)

**Checkpoint**: US1, US2, US3 are all functional — categories can be customized and the next digest reflects the change.

---

## Phase 6: User Story 4 - Operate the Service via Admin Panel (Priority: P2)

**Goal**: The operator can view and update the digest interval, see the bot-token and AI-provider reachability, and see a clear health/error indicator. Interval changes take effect from the next cycle (SC-005/010-style persistence).

**Independent Test**: `PATCH /api/settings {"digest_interval_seconds":300}` returns 200, `Scheduler.SetInterval(5*time.Minute)` is invoked, the next cycle fires 5 minutes later, and the change survives restart. `POST /api/settings/test-telegram` with a valid token returns 200 and bot info; with an invalid token returns `invalid_token`. The Overview page renders the health badge in < 2 s (SC-009).

### Tests for User Story 4

- [ ] T044 [P] [US4] Admin API contract test for settings (GET, PATCH interval, PATCH chat id, PATCH uncategorized_label, reject out-of-range interval) in `backend/internal/adminapi/settings_test.go`
- [ ] T045 [P] [US4] Scheduler live-reload test (`SetInterval` while a cycle is running does not interrupt it; the new interval applies from the next fire) in `backend/internal/digest/scheduler_test.go`

### Implementation for User Story 4

- [ ] T046 [P] [US4] Admin API handlers in `backend/internal/adminapi/settings.go` for `GET /api/settings`, `PATCH /api/settings`, `POST /api/settings/test-telegram`, `POST /api/settings/test-ai`
- [ ] T047 [P] [US4] Implement `digest.Scheduler.SetInterval` and a `Settings.Watch` goroutine in `backend/cmd/assistant/main.go` that observes `SettingsRepo` and calls `SetInterval` on change (depends on T045)
- [ ] T048 [P] [US4] Frontend `HealthBadge` component in `frontend/src/lib/components/HealthBadge.svelte` (renders status, last success, last failure)
- [ ] T049 [P] [US4] Frontend Overview page in `frontend/src/routes/+page.svelte` (depends on T048)
- [ ] T050 [P] [US4] Frontend Settings page in `frontend/src/routes/settings/+page.svelte` (interval input, chat id input, uncategorized label input, test-telegram/test-ai buttons)
- [ ] T051 [US4] Wire settings handlers in `main.go` and confirm SC-009 + SC-010 in `quickstart.md` Track B step B6 (depends on T046, T050)

**Checkpoint**: All operator-facing controls work; the live-reload of the interval is observable without a restart.

---

## Phase 7: User Story 5 - Observe Digest History and Audit Trail (Priority: P3)

**Goal**: The subscriber/operator can browse past cycles and open a past digest to see the full categorized summary, source channels, and timestamps. Recent operational events are listed in reverse chronological order.

**Independent Test**: After running 3 cycles, `GET /api/cycles?limit=20` returns 3 rows in reverse chronological order; `GET /api/cycles/{id}` returns the cycle, its digest, and the items grouped by category; `GET /api/events?limit=10` returns the most recent op events. The history list and detail pages render the same content as the Telegram message (per `contracts/telegram-render.md`).

### Tests for User Story 5

- [ ] T052 [P] [US5] Admin API contract test for cycles and events (list, get with items grouped by category, events newest-first) in `backend/internal/adminapi/history_test.go`

### Implementation for User Story 5

- [ ] T053 [P] [US5] Admin API handlers in `backend/internal/adminapi/history.go` for `GET /api/cycles?limit=&offset=`, `GET /api/cycles/{id}`, `GET /api/events?limit=`
- [ ] T054 [P] [US5] Frontend `DigestViewer` component in `frontend/src/lib/components/DigestViewer.svelte` (renders the same MarkdownV2-aware text as Telegram, per category)
- [ ] T055 [P] [US5] Frontend History list page in `frontend/src/routes/history/+page.svelte` (depends on T054)
- [ ] T056 [P] [US5] Frontend History detail page in `frontend/src/routes/history/[id]/+page.svelte` (depends on T054)
- [ ] T057 [US5] Wire history handlers in `main.go` and confirm Track A step A6 of `quickstart.md` (depends on T053, T056)

**Checkpoint**: All five user stories are now functional; the history view completes the audit trail.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories and bring the service to production quality.

- [ ] T058 [P] Implement real OpenAI-compatible summarizer in `backend/internal/ai/openai.go` (system + user prompt from `contracts/ai-summarizer.md`, strict JSON response parsing, per-call timeout, `ErrUnavailable` mapping)
- [ ] T059 [P] Add `make run-track-a`, `make run-track-b`, `make run-track-c` targets to `Makefile` that source the appropriate env file and start the binary
- [ ] T060 [P] Add `op_events` instrumentation throughout the cycle in `backend/internal/digest/cycle.go` and the admin handlers in `backend/internal/adminapi/*.go` (cycle.start, cycle.success, cycle.degraded, cycle.failed, telegram.send.failed, telegram.send.blocked, channel.inaccessible, channel.banned, settings.changed)
- [ ] T061 [P] Implement graceful shutdown in `backend/cmd/assistant/main.go` (cancel scheduler context, wait for current cycle to finish or hit a 30s deadline, then close DB)
- [ ] T062 [P] Add a top-level `README.md` with build, run, configure, and deploy instructions, plus a pointer to `specs/001-telegram-news-assistant/quickstart.md`
- [ ] T063 Run `quickstart.md` Track A end-to-end and capture baseline timings (cycle duration, send latency, admin API p95) into `specs/001-telegram-news-assistant/quickstart-baseline.md`
- [ ] T064 [P] Run final lint + test sweep: `make lint` (gofmt + golangci-lint + eslint + prettier --check) and `make test` (Go `go test ./...` and frontend `npm test`); capture results in `docs/qa-report.md` and fix any findings before sign-off
- [ ] T065 [P] Add admin API authentication (single admin password via `ADMIN_PASSWORD` env, session cookie, login page) — explicitly noted as a follow-up in `research.md`; included here so it ships with v1

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately.
- **Foundational (Phase 2)**: Depends on Setup completion. **BLOCKS all user stories.**
- **User Stories (Phases 3–7)**: All depend on Foundational phase completion.
  - User stories can proceed in parallel (if staffed).
  - Or sequentially in priority order: US1 → US2 → US3 → US4 → US5.
- **Polish (Phase 8)**: Depends on all desired user stories being complete.

### User Story Dependencies

- **User Story 1 (P1, MVP)**: Can start after Foundational. **No dependencies on other stories.** Can be tested with the fake Telegram + fake AI path.
- **User Story 2 (P1)**: Can start after Foundational. Integrates with US1 (the cycle reads the channels US2 manages) but is **independently testable** by inspecting the admin API and the channels table.
- **User Story 3 (P2)**: Can start after Foundational. Integrates with US1 (the cycle categorizes into the categories US3 manages) but is **independently testable** by inspecting the admin API and the categories table.
- **User Story 4 (P2)**: Can start after Foundational. Depends on US1's `Scheduler` exposing `SetInterval`. Independently testable.
- **User Story 5 (P3)**: Can start after Foundational. Reads from `cycles`, `digests`, `digest_items`, `op_events` (all populated by US1). Independently testable.

### Within Each User Story

- Tests (when included) MUST be written and FAIL before implementation.
- Repos / interfaces before handlers.
- Handlers before main.go wiring.
- Frontend API client before pages.
- Story complete before moving to the next priority.

### Parallel Opportunities

- All Setup tasks marked [P] (T003, T004, T005, T007) can run in parallel.
- All Foundational tasks marked [P] (T009, T010, T013, T014, T015, T016) can run in parallel.
- Once Foundational is complete, the five user stories can run in parallel (US1, US2, US3, US4, US5).
- Within a story, tests (T021, T022, T023 for US1; T029 for US2; etc.) can run in parallel with each other, and the models/repos/handlers/components marked [P] can run in parallel.
- Polish tasks (T058, T059, T060, T061, T062, T064, T065) can run in parallel.

---

## Parallel Examples

### Setup phase (after T001, T002 are done)

```bash
# All in parallel
Task: "T003 [P] Initialize frontend SvelteKit project in frontend/"
Task: "T004 [P] Configure Go linting in backend/.golangci.yml and Makefile"
Task: "T005 [P] Configure frontend linting/formatting in frontend/.eslintrc.cjs + .prettierrc"
Task: "T007 [P] Add empty initial migration in backend/migrations/0001_init.sql"
```

### Foundational phase (after T008 is done)

```bash
# All in parallel
Task: "T009 [P] Config loader in backend/internal/config/config.go"
Task: "T010 [P] slog setup in backend/internal/logging/logging.go"
Task: "T013 [P] ai.Summarizer interface in backend/internal/ai/summarizer.go"
Task: "T014 [P] ai.Fake summarizer in backend/internal/ai/fake.go"
Task: "T015 [P] telegram.Client interface in backend/internal/telegram/client.go"
Task: "T016 [P] telegram.Fake client in backend/internal/telegram/fake.go"
```

### User Story 1 tests (after Phase 2 is complete)

```bash
# All in parallel
Task: "T021 [P] [US1] Golden-file render test in backend/tests/render_test.go"
Task: "T022 [P] [US1] Dedup table-driven test in backend/tests/dedup_test.go"
Task: "T023 [P] [US1] End-to-end cycle test in backend/tests/digest_cycle_test.go"
```

### User Story 2 frontend (after T033 API client is done)

```bash
# All in parallel
Task: "T034 [P] [US2] Layout + Channels page in frontend/src/routes/+layout.svelte + frontend/src/routes/channels/+page.svelte"
Task: "T035 [P] [US2] ChannelList component in frontend/src/lib/components/ChannelList.svelte"
```

### Polish phase (last)

```bash
# All in parallel
Task: "T058 [P] OpenAI summarizer in backend/internal/ai/openai.go"
Task: "T059 [P] Track-A/B/C Makefile targets"
Task: "T060 [P] op_events instrumentation"
Task: "T061 [P] Graceful shutdown in main.go"
Task: "T062 [P] README.md"
Task: "T064 [P] Final lint + test sweep"
Task: "T065 [P] Admin auth (password + cookie)"
```

---

## Implementation Strategy

### MVP First (User Story 1 + the slices of US2/US3/US4 that US1 needs)

The simplest viable vertical slice is **Phase 1 + Phase 2 + Phase 3 (US1)**. The cycle runs with the fake Telegram + fake AI; the admin panel is not yet present, but the cycle is real and `digests` are written. This corresponds to **Track A of `quickstart.md`** with one channel hard-seeded in the fake Telegram client YAML.

1. Complete Phase 1: Setup.
2. Complete Phase 2: Foundational.
3. Complete Phase 3: User Story 1.
4. **STOP and VALIDATE**: Run Track A of `quickstart.md` end-to-end; verify SC-001, SC-002, SC-008, SC-010, SC-011 against the recorded cycles.
5. Deploy/demo if ready.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. US1 (P1) → cycle works end-to-end. **MVP.**
3. US2 (P1) → admin panel can manage channels.
4. US3 (P2) → admin panel can manage categories.
5. US4 (P2) → admin panel can manage settings and observe health.
6. US5 (P3) → admin panel can browse history.
7. Polish (Phase 8) → real OpenAI, graceful shutdown, README, baseline metrics, admin auth.

Each story adds value without breaking previous stories.

### Parallel Team Strategy

With multiple developers, after Foundational is complete:

- **Developer A**: User Story 1 (cycle, renderer, scheduler) — the deepest backend work.
- **Developer B**: User Story 2 (channel admin API + frontend).
- **Developer C**: User Story 3 (category admin API + frontend).
- **Developer D**: User Story 4 + 5 (settings + history admin API + frontend).

Stories complete and integrate independently because the admin API is the only seam between them and the cycle, and the cycle uses repository interfaces that don't change.

---

## Notes

- [P] tasks = different files, no dependencies.
- [Story] label maps task to specific user story for traceability.
- Each user story should be independently completable and testable.
- Verify tests fail before implementing (when tests are included).
- Commit after each task or logical group; the build glue (`make build`) must remain green at every commit.
- Stop at any checkpoint to validate the story independently.
- Avoid: vague tasks, same-file conflicts, cross-story dependencies that break independence.
- The data-model schema in `backend/migrations/0001_init.sql` is the only file touched in both Phase 2 and the user story phases. Coordinate: any schema change goes through a new migration file in `backend/migrations/NNNN_*.sql`, never by editing `0001_init.sql` after the first commit.
