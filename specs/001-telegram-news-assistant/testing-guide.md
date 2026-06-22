# Testing Guide: Telegram News Digest Assistant

**Feature**: 001-telegram-news-assistant
**Purpose**: a practical, step-by-step checklist to manually verify the app works end-to-end on a Windows PowerShell machine. The binary is already built at `bin\assistant.exe`.

This guide mirrors the three validation tracks from [`quickstart.md`](./quickstart.md) but uses **native PowerShell 5.1 commands** (no Git Bash / WSL required). Run them in order; stop after Track A if you only want a smoke test.

| Track | What it validates | Needs | Time |
|---|---|---|---|
| **A** | Cycle, store, renderer, admin API, SPA | Nothing external | ~5 min |
| **B** | Real Telegram send + read paths | Bot token + public channel | ~20 min |
| **C** | Full end-to-end incl. real AI + degraded mode | Bot token + OpenAI key | ~30 min |

---

## Prerequisites (one-time check)

```powershell
# 1. Confirm the binary exists
Test-Path .\bin\assistant.exe        # → True

# 2. (Optional) Confirm jq is available; if not, we'll use ConvertFrom-Json instead
Get-Command jq -ErrorAction SilentlyContinue
```

> All API calls below use PowerShell's built-in `Invoke-RestMethod`, so `jq` is **not** required.
> If you prefer `curl`, use `curl.exe` (not `curl`, which is an alias for `Invoke-WebRequest` in PS 5.1).

---

## Track A — Pure-local validation (≈ 5 min)

Uses the in-process **fake** Telegram client + **fake** AI summarizer. No network, no credentials.

### A1. Prepare the runtime directory and env file

```powershell
New-Item -ItemType Directory -Path .runtime -Force | Out-Null

@"
ASSISTANT_AI_PROVIDER=fake
DIGEST_INTERVAL=10s
ADMIN_LISTEN_ADDR=127.0.0.1:8080
DB_PATH=./.runtime/assistant.db
TELEGRAM_FAKE_OUT=./.runtime/telegram-sent.jsonl
TELEGRAM_FAKE_SEED=./.runtime/source-messages.yaml
LOG_LEVEL=info
"@ | Set-Content -Path .\.runtime\assistant.env -Encoding ASCII
```

> `DIGEST_INTERVAL=10s` makes a cycle fire every 10 seconds so you don't have to wait.
> The admin API has **no auth** because `ADMIN_PASSWORD` is unset.

### A2. Start the service

```powershell
# Load env vars into the current PowerShell session
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') {
        Set-Item -Path "env:$($Matches[1])" -Value $Matches[2]
    }
}

# Start the binary in a new window so logs are visible
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 3
Write-Host "Service PID: $($proc.Id)"
```

Keep that PowerShell window open (or the console where logs appear) so you can watch cycle events. The service is now listening on `http://127.0.0.1:8080`.

### A3. Verify the health endpoint

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/health" | ConvertTo-Json -Depth 5
```

**Expected**: `"status": "ok"`, `"db_ok": true`, `"scheduler_state": "idle"` (or `"running"` if a cycle is in flight), `"version"` present.

### A4. Open the admin panel in your browser

Navigate to <http://127.0.0.1:8080/>.

**You should see**:
- **Overview** page: `Last successful cycle: never`, `Scheduler: idle/running`, `DB: ok`, a `HealthBadge`.
- **Channels** page: empty list + an "Add channel" form.
- **Categories** page: the six defaults — Politics, Technology, Business, Sports, World, Other.
- **Settings** page: digest interval shown, Telegram/AI status indicators.
- **History** page: empty list (no cycles yet).

> SC-009 (admin panel renders in < 2s): eyeball it — pages should load near-instantly on localhost.

### A5. Add a channel via the admin API

```powershell
$body = '{"handle":"sample_news"}'
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/channels" `
    -Method Post -ContentType "application/json" -Body $body | ConvertTo-Json -Depth 5
```

**Expected**: HTTP 201 (PowerShell won't print the status, but the response body should contain `"status": "active"` and a UUID `id`). Refresh the **Channels** page in the browser — one row should appear.

> The fake Telegram client accepts any well-formed handle, so this always succeeds in Track A.
> In Track B this same call hits Telegram `getChat` and can fail with `bot_not_in_channel`.

### A6. Seed synthetic source messages

The fake client reads from `.runtime/source-messages.yaml`. Write three posts:

```powershell
@"
- channel: sample_news
  messages:
    - id: 1001
      text: "Telegram rolls out scheduled messages in channels."
      media: text
    - id: 1002
      text: "EU parliament passes the AI Liability Directive."
      media: text
    - id: 1003
      text: "A new open-source LLM beats GPT-4 on a public benchmark."
      media: text
"@ | Set-Content -Path .\.runtime\source-messages.yaml -Encoding UTF8
```

### A7. Restart the service so the fake client reloads the seed file

```powershell
Stop-Process -Id $proc.Id -Force
Start-Sleep -Seconds 1

# Re-source env (the Start-Process child doesn't inherit our session vars after restart,
# so we set them again right before launching)
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') {
        Set-Item -Path "env:$($Matches[1])" -Value $Matches[2]
    }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 3
Write-Host "Service PID: $($proc.Id)"
```

> If `Stop-Process` complains the process has already exited, just continue — the important part is that the new process starts and picks up the YAML.

### A8. Wait for one cycle and verify the digest was sent

The interval is 10s, so wait ~15s:

```powershell
Start-Sleep -Seconds 15

# 1. The fake client should have appended one line to the sent log
Test-Path .\.runtime\telegram-sent.jsonl
(Get-Content .\.runtime\telegram-sent.jsonl | Measure-Object -Line).Lines   # → >= 1
```

**Expected**: at least one line in `telegram-sent.jsonl` — that's the rendered digest the fake "sent" to your chat.

### A9. Inspect the cycle through the admin API

```powershell
$cycles = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1"
$cycles.cycles[0] | ConvertTo-Json
```

**Expected**: `"status": "succeeded"`, `"input_msg_count": 3`, `"output_items": 3`, `"degraded": false`.

Grab the cycle id and look at the full digest:

```powershell
$cid = $cycles.cycles[0].id
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$cid"
$detail.digest.rendered_text
$detail.items_by_category | ConvertTo-Json -Depth 6
```

**Expected**: rendered text matching the format in `contracts/telegram-render.md`:

```
📰 News digest — <YYYY-MM-DD HH:MM> UTC

# <Category>
• <summary>  _(sample_news)
...

— cycle <8-char id> · 3 items · ok
```

(Exact category assignment depends on the fake AI's rule matching — what matters is that items are grouped under headings, not the specific heading per item.)

### A10. View the digest in the History page

Open <http://127.0.0.1:8080/history> in your browser. You should see one cycle row. Click it — the detail view should show the same categorized text as `$detail.digest.rendered_text` above.

> This validates FR-014 (history) and the SPA rendering of past digests.

### A11. Validate the remaining Track-A success criteria

Run each block below and check the **Expected** result.

#### SC-002 — No message when no new items

Empty the seed file so the fake client has nothing new to deliver (the per-channel cursor already advanced past ids 1001–1003):

```powershell
@"
- channel: sample_news
  messages: []
"@ | Set-Content -Path .\.runtime\source-messages.yaml -Encoding UTF8

# Restart to pick up the empty seed
Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 25      # let ~2 cycles fire

$before = (Get-Content .\.runtime\telegram-sent.jsonl | Measure-Object -Line).Lines
Start-Sleep -Seconds 25
$after = (Get-Content .\.runtime\telegram-sent.jsonl | Measure-Object -Line).Lines
Write-Host "Lines before=$before after=$after (expect equal)"
$cycles = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=2"
$cycles.cycles | Select-Object status, input_msg_count | ConvertTo-Json
```

**Expected**: `before` == `after` (sent log did not grow), and at least one recent cycle has `"status": "skipped_no_items"` with `input_msg_count: 0`.

#### SC-005 — Channel change reflected in the next cycle

Re-seed messages and add a second channel:

```powershell
@"
- channel: sample_news
  messages:
    - id: 2001
      text: "Second-channel test post."
      media: text
- channel: extra_feed
  messages:
    - id: 3001
      text: "A post from the extra feed."
      media: text
"@ | Set-Content -Path .\.runtime\source-messages.yaml -Encoding UTF8

Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/channels" `
    -Method Post -ContentType "application/json" -Body '{"handle":"extra_feed"}' | ConvertTo-Json

# Restart to reload the seed
Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 15

$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.items_by_category | ForEach-Object { $_.items.channel.handle } | Sort-Object -Unique
```

**Expected**: the channel handles list contains **both** `sample_news` and `extra_feed`.

#### SC-006 — Category change reflected in the next cycle

Rename the "Politics" category to "Policy":

```powershell
$cats = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories"
$politics = $cats.categories | Where-Object { $_.name -eq 'Politics' }
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories/$($politics.id)" `
    -Method Patch -ContentType "application/json" -Body '{"name":"Policy"}' | ConvertTo-Json

Start-Sleep -Seconds 15     # wait one cycle
$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.digest.rendered_text
```

**Expected**: the rendered text uses `# Policy` as a heading (no `# Politics`).

Also try removing a **custom** (non-default) category — should succeed with 204 — and removing a **default** category — should fail:

```powershell
# Add a custom category, then remove it — should work
$new = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories" `
    -Method Post -ContentType "application/json" -Body '{"name":"AI & ML"}'
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories/$($new.category.id)" -Method Delete

# Try to remove a default — should fail with cannot_remove_default
$tech = $cats.categories | Where-Object { $_.name -eq 'Technology' }
try {
    Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories/$($tech.id)" -Method Delete
} catch {
    Write-Host "Rejected as expected: $($_.Exception.Message)"
}
```

#### SC-008 — No double-deliver on restart mid-window

```powershell
$before = (Get-Content .\.runtime\telegram-sent.jsonl | Measure-Object -Line).Lines
Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 25      # restart mid-window + a full cycle
$after = (Get-Content .\.runtime\telegram-sent.jsonl | Measure-Object -Line).Lines
Write-Host "Sent lines before=$before after=$after"
```

**Expected**: the sent-log grows by at most the number of cycles that actually had new items — never by 2× for the same window. Check `GET /api/cycles` for duplicate `window_end` values; there should be none.

#### SC-010 — Config survives restart

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/channels" | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories" | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings"  | ConvertTo-Json -Depth 5

Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 3

Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/channels"  | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/categories" | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings"   | ConvertTo-Json -Depth 5
```

**Expected**: channels, categories, and settings are **identical** before and after restart (SQLite persists everything).

#### SC-011 — Single Telegram message for ≤ 50 items

Seed 30 messages in one channel, wait one cycle, verify the digest is one row and not split:

```powershell
$msgs = 1..30 | ForEach-Object {
    "    - id: $_`n      text: `"News item number $_ for burst test.`"`n      media: text"
}
@"
- channel: sample_news
  messages:
$($msgs -join "`n")
"@ | Set-Content -Path .\.runtime\source-messages.yaml -Encoding UTF8

Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 15

$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.digest.rendered_text.Length       # should be < 4096
$detail.digest.degraded                   # should be false
```

**Expected**: `rendered_text.Length` < 4096 (one message), `degraded` is `false`.

> For the split-path (> 4096 chars), seed 60+ long messages and confirm the renderer splits into multiple messages per `contracts/telegram-render.md`. The fake client records each part as a separate line in `telegram-sent.jsonl`.

### A12. Test the operator settings page (US4)

Change the interval via the API and confirm the next cycle fires on the new cadence:

```powershell
# Set interval to 30 seconds
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings" `
    -Method Patch -ContentType "application/json" `
    -Body '{"digest_interval_seconds":30}' | ConvertTo-Json -Depth 5

Start-Sleep -Seconds 35
$cycles = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=3"
$cycles.cycles | Select-Object started_at, status | ConvertTo-Json
```

**Expected**: settings response shows `digest_interval_seconds: 30`, and the gap between consecutive `started_at` values is ~30s (not 10s).

Also try the **test-telegram** and **test-ai** endpoints (they should both succeed against the fakes):

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings/test-telegram" -Method Post | ConvertTo-Json
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings/test-ai" -Method Post | ConvertTo-Json
```

**Expected**: both return `"ok": true`.

### A13. Check the operational events log (US5)

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/events?limit=10" | ConvertTo-Json -Depth 5
```

**Expected**: a list newest-first, with `kind` values like `cycle.start`, `cycle.success`, `settings.changed`, etc.

### A14. Tear down Track A

```powershell
Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force .\.runtime -ErrorAction SilentlyContinue
Remove-Item -Force .\assistant.db -ErrorAction SilentlyContinue
```

---

## Track B — Real Telegram + fake AI (≈ 20 min)

Validates the real send/read paths against Telegram. You need:

- A bot token from `@BotFather` (`/newbot`).
- A **public** Telegram channel the bot is a member of.
- Your personal chat id (send any message to your bot, then hit `https://api.telegram.org/bot<TOKEN>/getUpdates` and read `result[-1].message.chat.id`).

### B1. Write the env file with real credentials

```powershell
# Set these in your session first:
$env:TELEGRAM_BOT_TOKEN = "123456:ABC..."
$env:TELEGRAM_SUBSCRIBER_CHAT = "111222333"

New-Item -ItemType Directory -Path .runtime -Force | Out-Null
@"
ASSISTANT_AI_PROVIDER=fake
DIGEST_INTERVAL=1m
ADMIN_LISTEN_ADDR=127.0.0.1:8080
DB_PATH=./.runtime/assistant.db
TELEGRAM_BOT_TOKEN=$env:TELEGRAM_BOT_TOKEN
TELEGRAM_SUBSCRIBER_CHAT=$env:TELEGRAM_SUBSCRIBER_CHAT
LOG_LEVEL=info
"@ | Set-Content -Path .\.runtime\assistant.env -Encoding ASCII
```

### B2. Start the service

```powershell
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 3
```

### B3. Probe the bot token from the admin panel

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/settings/test-telegram" -Method Post | ConvertTo-Json
```

**Expected**: `"ok": true` and a `bot` object with `username`. If you get `invalid_token`, the token is wrong.

### B4. Add the channel via the API

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/channels" `
    -Method Post -ContentType "application/json" -Body '{"handle":"your_channel_handle"}' | ConvertTo-Json -Depth 5
```

**Expected**: `201` with `status: active`. If you get `bot_not_in_channel`, add the bot to the channel first.

### B5. Post a message in the channel and wait

Manually post one new message in your Telegram channel, then wait up to one interval + 10s:

```powershell
Start-Sleep -Seconds 70
$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$latest | ConvertTo-Json
```

**Expected**: `status: succeeded`, `input_msg_count >= 1`, and you should have **received a real Telegram message** from the bot in your personal chat.

### B6. Validate SC-001 (delivery within interval + margin)

```powershell
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.digest.sent_at
$detail.cycle.window_end
```

**Expected**: `sent_at - window_end` is at most `interval + 60s` (here ~2 minutes max).

### B7. Validate failure surfacing (SC-015 / FR-015)

Block the bot from your personal Telegram chat (Telegram → bot → Block). Wait one cycle:

```powershell
Start-Sleep -Seconds 70
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/events?limit=5" | ConvertTo-Json -Depth 5
```

**Expected**: an event with `kind: telegram.send.blocked` (or `telegram.send.failed`). The cycle's digest row should have `send_status: blocked` / `failed`.

### B8. Tear down Track B

```powershell
Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force .\.runtime -ErrorAction SilentlyContinue
# Revoke the bot token via @BotFather /revoke
```

---

## Track C — Real Telegram + real AI (≈ 30 min)

Same as Track B, with the AI summarizer pointed at a real OpenAI-compatible endpoint. Add these to your env:

```powershell
$env:AI_BASE_URL = "https://api.openai.com/v1"   # or your compatible endpoint
$env:AI_MODEL = "gpt-4o-mini"
$env:AI_API_KEY = "sk-..."

@"
ASSISTANT_AI_PROVIDER=openai
DIGEST_INTERVAL=10m
ADMIN_LISTEN_ADDR=127.0.0.1:8080
DB_PATH=./.runtime/assistant.db
TELEGRAM_BOT_TOKEN=$env:TELEGRAM_BOT_TOKEN
TELEGRAM_SUBSCRIBER_CHAT=$env:TELEGRAM_SUBSCRIBER_CHAT
AI_BASE_URL=$env:AI_BASE_URL
AI_MODEL=$env:AI_MODEL
AI_API_KEY=$env:AI_API_KEY
LOG_LEVEL=info
"@ | Set-Content -Path .\.runtime\assistant.env -Encoding ASCII
```

Start the service the same way as B2, add the channel, post a **long** message (2 KB+), wait one interval.

### C1. Validate SC-003 (real summaries, not verbatim)

```powershell
$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.items_by_category | ForEach-Object {
    $_.items | ForEach-Object { Write-Host "summary length: $($_.summary.Length)" }
}
```

**Expected**: each summary is ≤ 280 chars even when the source was 2 KB+. The text is clearly a paraphrase, not a copy.

### C2. Validate SC-007 (degraded mode on AI outage)

Point the AI at an unreachable host and restart:

```powershell
Stop-Process -Id $proc.Id -Force; Start-Sleep -Seconds 1
$env:AI_BASE_URL = "http://127.0.0.1:9/unreachable"
Get-Content .\.runtime\assistant.env | ForEach-Object {
    if ($_ -match '^AI_BASE_URL=') { "AI_BASE_URL=$env:AI_BASE_URL" }
    elseif ($_ -match '^\s*([^=]+)=(.*)$') { Set-Item -Path "env:$($Matches[1])" -Value $Matches[2] }
}
$proc = Start-Process -FilePath ".\bin\assistant.exe" -PassThru -NoNewWindow
Start-Sleep -Seconds 620     # one 10-minute cycle

$latest = (Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles?limit=1").cycles[0]
$latest.degraded            # → true
$detail = Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/cycles/$($latest.id)"
$detail.digest.rendered_text   # should end with "· degraded (AI unavailable)"
```

**Expected**: `degraded: true`, the footer contains `degraded (AI unavailable)`, and you still received a digest in Telegram with `⚠️`-prefixed raw headlines.

### C3. Tear down Track C

```powershell
Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force .\.runtime -ErrorAction SilentlyContinue
```

---

## Automated tests & lint (optional but recommended)

If you have `make` (Git Bash) or want to run the underlying tools directly:

```powershell
# Backend tests (in-memory fakes — no network)
go -C .\backend test ./...

# Frontend component tests
Push-Location .\frontend; npm test; Pop-Location

# Lint
go -C .\backend vet ./...
Push-Location .\frontend; npm run lint; Pop-Location
```

Or, if you have GNU make installed:

```powershell
make test     # both suites
make lint     # golangci-lint (or go vet) + eslint/prettier
```

---

## Success criteria coverage matrix

| SC | Where validated | Track |
|---|---|---|
| **SC-001** (delivery within interval + margin) | B6 | B, C |
| **SC-002** (no message when no items) | A11 | A |
| **SC-003** (real summaries, not verbatim) | C1 | C |
| **SC-004** (95% cycle success over 7 days) | needs a soak run — out of scope for this guide | — |
| **SC-005** (channel change next cycle) | A11 | A |
| **SC-006** (category change next cycle) | A11 | A |
| **SC-007** (degraded mode on AI outage) | C2 | C |
| **SC-008** (no double-deliver on restart) | A11 | A |
| **SC-009** (admin panel < 2s) | A4 | A, B, C |
| **SC-010** (config survives restart) | A11 | A |
| **SC-011** (single message ≤ 50 items) | A11 | A |

## Troubleshooting

- **`Invoke-RestMethod` errors with "Unable to connect"**: the service isn't running or `ADMIN_LISTEN_ADDR` is wrong. Check `$proc.HasExited` and the service logs.
- **No line in `telegram-sent.jsonl` after a cycle**: the seed file is empty, the channel handle doesn't match the YAML, or the per-channel cursor already advanced past all seeded ids (use fresh ids).
- **`bot_not_in_channel` (Track B)**: add the bot as a member/admin of the public channel.
- **`invalid_token` (Track B/C)**: re-create the bot via `@BotFather /newbot` or `/revoke` first.
- **Cycles never fire**: check `LOG_LEVEL=debug` output for scheduler state; confirm `DIGEST_INTERVAL` was set (not empty).
- **Port 8080 in use**: change `ADMIN_LISTEN_ADDR` in the env file to a different port and restart.
