# Contract: AI Summarizer Interface

**Feature**: 001-telegram-news-assistant
**Version**: 0.1.0
**Audience**: any Go package that needs to summarize + categorize a source message. The cycle uses one `ai.Summarizer` instance configured at startup; tests and local dev use a fake.

## Interface

```go
package ai

import (
    "context"
    "errors"
)

type MediaKind string

const (
    MediaText  MediaKind = "text"
    MediaImage MediaKind = "image"
    MediaVideo MediaKind = "video"
    MediaVoice MediaKind = "voice"
    MediaOther MediaKind = "other"
)

type Input struct {
    ChannelHandle string    `json:"channel_handle"`
    Text          string    `json:"text"`
    MediaKind     MediaKind `json:"media_kind"`
    Captions      []string  `json:"captions,omitempty"`
}

type Output struct {
    Summary    string  `json:"summary"`     // <= 280 chars, single line, no leading/trailing whitespace
    Category   string  `json:"category"`    // must match a name in the configured category set
    Confidence float64 `json:"confidence"`  // 0..1; 0 if unknown
}

type Summarizer interface {
    Summarize(ctx context.Context, in Input) (Output, error)
}

var (
    // ErrUnavailable signals a transient provider outage; the cycle degrades gracefully.
    ErrUnavailable = errors.New("ai: provider unavailable")
    // ErrInvalidInput signals the input was empty or malformed; the cycle drops the item.
    ErrInvalidInput = errors.New("ai: invalid input")
    // ErrCategoryUnknown signals the provider returned a category not in the configured set;
    // the cycle maps it to the configured uncategorized_label.
    ErrCategoryUnknown = errors.New("ai: category not in configured set")
)
```

## Behavioral contract

- **Latency budget**: each call must return within **8 seconds** (default; configurable via `AI_PER_CALL_TIMEOUT`). Callers (`digest.Cycle`) enforce this with `context.WithTimeout`; the implementation may also enforce it internally.
- **No retention**: the implementation must not log the input text or the summary at INFO level. DEBUG logging of truncated / hashed content is acceptable.
- **Determinism for equal inputs**: equal `Input` values (including `MediaKind` and `Captions`) must produce equal `Output` values up to the `Confidence` field, which may vary slightly between calls. The cycle does not depend on exact determinism; this rule is for test stability.
- **Output shape**:
  - `Output.Summary`: a single line (no `\n`); trimmed; ≤ 280 characters; in the same language as the input text by default; no leading or trailing whitespace.
  - `Output.Category`: exactly one of the category names from the configured `categories` table (case-sensitive, no leading/trailing whitespace). If the provider cannot decide, it must return `settings.uncategorized_label` and `ErrCategoryUnknown`.
  - `Output.Confidence`: a float in `[0, 1]`. `0` if the provider does not return a score. The cycle does not act on this field in phase 1 (it is recorded for future ranking), but a value must always be present.
- **Error semantics**:
  - `ErrUnavailable`: the cycle marks the item's digest entry with a raw-headline fallback (degraded mode flag on the cycle) and continues.
  - `ErrInvalidInput`: the cycle drops the item from the digest and records a `warn` event.
  - `ErrCategoryUnknown`: the cycle accepts the returned category if it is non-empty, otherwise substitutes `uncategorized_label`; the returned error is logged at `warn` for taxonomy tuning.
  - Any other error is treated as `ErrUnavailable` for cycle-flow purposes (defensive).
- **Concurrency**: implementations must be safe for concurrent use; the cycle may fan out up to 8 concurrent `Summarize` calls per cycle.

## Default implementation: OpenAI Chat Completions

The default implementation (`internal/ai/openai.go`) calls the **OpenAI Chat Completions API** at `<ai_base_url>/chat/completions` with a model of `<ai_model>`. The implementation uses `github.com/sashabaranov/go-openai` as the transport.

### Request shape

The implementation sends a single system + user message pair:

- **System message** (fixed):
  ```
  You are a news digest assistant. You will receive a single Telegram channel
  post. Produce a one-line summary (≤ 280 chars) in the same language as the
  post, and assign the post to exactly one of the following categories:
  <comma-joined category names from the configured set, with
   settings.uncategorized_label included as a last-resort option>.

  Respond with strict JSON: {"summary": "...", "category": "...",
  "confidence": 0.0..1.0}. No prose, no markdown fences.
  ```

- **User message** (templated):
  ```
  Channel: @<handle>
  Media kind: <MediaKind>

  Text:
  <<<text>>>

  Captions:
  <<<each caption on its own line; omitted if empty>>>
  ```

### Response handling

- The implementation reads the assistant message, strips any markdown fences, decodes the JSON, and validates the three fields against the contract.
- If the response is not valid JSON, the call returns `ErrUnavailable`.
- If the response is valid JSON but the `category` value is not in the configured set and is not `uncategorized_label`, the implementation returns `(Output{Summary, settings.uncategorized_label, Confidence}, ErrCategoryUnknown)`.
- HTTP / network errors are mapped to `ErrUnavailable`.

## Fake implementation (tests, dev)

`internal/ai/fake.go` provides `ai.NewFake(map[string]FakeRule)` where `FakeRule` is `{ Match func(Input) bool; Output Output }`. The fake iterates the rules in order and returns the first match's `Output`. If no rule matches, it returns a default `{Summary: "<truncated text>", Category: settings.uncategorized_label, Confidence: 0.5}`. The fake is wired automatically when `ASSISTANT_AI_PROVIDER=fake` or when no AI env vars are present at startup.

## Configuration

| Env var | Required | Default | Notes |
|---|---|---|---|
| `ASSISTANT_AI_PROVIDER` | no | `openai` | one of `openai`, `fake` |
| `AI_BASE_URL` | yes (when provider=openai) | `https://api.openai.com/v1` | any OpenAI-compatible endpoint |
| `AI_MODEL` | yes (when provider=openai) | `gpt-4o-mini` | any chat-completions model on the endpoint |
| `AI_API_KEY` | yes (when provider=openai) | — | the API key (loaded into `ai_api_key_ref` as `env:AI_API_KEY`) |
| `AI_PER_CALL_TIMEOUT` | no | `8s` | Go duration string |
| `AI_MAX_CONCURRENCY` | no | `8` | max in-flight `Summarize` calls per cycle |

## Versioning

- The interface signature is part of the **internal** Go API and may change between minor versions. A breaking change to the on-the-wire request/response shape is allowed only with a new contract version (this file). The OpenAI implementation is the de-facto reference; future adapters (Anthropic, local models) are expected to map onto the same `Summarizer` interface.
