package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIClient is the small surface of the OpenAI library we need. It
// exists so the summarizer can be unit-tested with a stub.
type OpenAIClient interface {
	CreateChatCompletion(
		ctx context.Context,
		req openai.ChatCompletionRequest,
	) (openai.ChatCompletionResponse, error)
}

// OpenAISummarizer is the production AI implementation: it calls an
// OpenAI-compatible chat completions endpoint and parses a strict JSON
// response per contracts/ai-summarizer.md.
type OpenAISummarizer struct {
	client   OpenAIClient
	model    string
	category []string
	uncat    string
	timeout  time.Duration
	log      *slog.Logger
}

// NewOpenAISummarizer constructs a summarizer that calls the given
// base URL with the supplied model and key. If uncategorizedLabel is
// empty, "Uncategorized" is used.
func NewOpenAISummarizer(
	client OpenAIClient,
	model string,
	categoryNames []string,
	uncategorizedLabel string,
	perCallTimeout time.Duration,
	log *slog.Logger,
) *OpenAISummarizer {
	if log == nil {
		log = slog.Default()
	}
	if uncategorizedLabel == "" {
		uncategorizedLabel = "Uncategorized"
	}
	if perCallTimeout <= 0 {
		perCallTimeout = 8 * time.Second
	}
	return &OpenAISummarizer{
		client:   client,
		model:    model,
		category: categoryNames,
		uncat:    uncategorizedLabel,
		timeout:  perCallTimeout,
		log:      log,
	}
}

// Summarize implements Summarizer. The full text of the contract lives
// in contracts/ai-summarizer.md; this implementation honors:
//   - per-call timeout (context.WithTimeout)
//   - strict JSON response parsing
//   - ErrUnavailable on transport / decode failures
//   - ErrCategoryUnknown when the returned category is not in the
//     configured set and is not the uncategorized_label
func (s *OpenAISummarizer) Summarize(ctx context.Context, in Input) (Output, error) {
	if strings.TrimSpace(in.Text) == "" && len(in.Captions) == 0 {
		return Output{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req := openai.ChatCompletionRequest{
		Model:       s.model,
		Temperature: 0.2,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: s.systemPrompt(),
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: s.userPrompt(in),
			},
		},
	}

	resp, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		// Any transport / decoding failure is a transient provider
		// outage per the contract.
		s.log.Warn("openai: call failed", "err", err)
		return Output{}, ErrUnavailable
	}
	if len(resp.Choices) == 0 {
		return Output{}, ErrUnavailable
	}
	raw := resp.Choices[0].Message.Content
	return s.parseResponse(raw)
}

// systemPrompt builds the fixed system message per contracts/ai-summarizer.md.
func (s *OpenAISummarizer) systemPrompt() string {
	// Always include the uncategorized label as a last-resort option so
	// the model can fall back when none of the configured categories fit.
	all := append([]string{}, s.category...)
	if !contains(all, s.uncat) {
		all = append(all, s.uncat)
	}
	return fmt.Sprintf(
		`You are a news digest assistant. You will receive a single Telegram channel post. `+
			`Produce a one-line summary (≤ 280 chars) in the same language as the post, and assign the post `+
			`to exactly one of the following categories: %s. `+
			`Respond with strict JSON: {"summary": "...", "category": "...", "confidence": 0.0..1.0}. `+
			`No prose, no markdown fences.`,
		strings.Join(all, ", "),
	)
}

// userPrompt builds the per-message user prompt.
func (s *OpenAISummarizer) userPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Channel: @%s\n", in.ChannelHandle)
	fmt.Fprintf(&b, "Media kind: %s\n\n", string(in.MediaKind))
	if in.Text != "" {
		fmt.Fprintf(&b, "Text:\n<<<\n%s\n>>>\n", in.Text)
	}
	if len(in.Captions) > 0 {
		b.WriteString("\nCaptions:\n")
		for _, c := range in.Captions {
			fmt.Fprintf(&b, "<<<\n%s\n>>>\n", c)
		}
	}
	return b.String()
}

// rawResponse is the strict JSON shape we expect back from the model.
type rawResponse struct {
	Summary    string  `json:"summary"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// parseResponse decodes the model's reply and validates the fields
// against the contract. It returns ErrUnavailable for malformed JSON,
// and ErrCategoryUnknown when the returned category is not in the
// configured set and is not the uncategorized_label.
func (s *OpenAISummarizer) parseResponse(raw string) (Output, error) {
	cleaned := stripCodeFences(raw)
	var r rawResponse
	if err := json.Unmarshal([]byte(cleaned), &r); err != nil {
		return Output{}, ErrUnavailable
	}
	summary := strings.TrimSpace(r.Summary)
	if summary == "" {
		return Output{}, ErrUnavailable
	}
	category := strings.TrimSpace(r.Category)
	if category == "" {
		// No category returned; coerce to uncategorized but surface the
		// error so the cycle logs a warn event.
		return Output{Summary: summary, Category: s.uncat, Confidence: clamp01(r.Confidence)}, ErrCategoryUnknown
	}
	// Build the set of known categories plus the uncategorized label.
	known := make(map[string]bool, len(s.category)+1)
	for _, c := range s.category {
		known[c] = true
	}
	known[s.uncat] = true
	if !known[category] {
		// Substituting uncategorized_label per contracts/ai-summarizer.md.
		return Output{
			Summary:    summary,
			Category:   s.uncat,
			Confidence: clamp01(r.Confidence),
		}, ErrCategoryUnknown
	}
	return Output{
		Summary:    summary,
		Category:   category,
		Confidence: clamp01(r.Confidence),
	}, nil
}

// stripCodeFences removes ``` fences some models add around JSON.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			if i := strings.LastIndex(s, "```"); i >= 0 {
				s = s[:i]
			}
			break
		}
	}
	return strings.TrimSpace(s)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Sentinel re-export for callers that import this package only for
// the OpenAI implementation. Keeps the api surface small.
var (
	_ = errors.New
)
