package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// AnthropicSummarizer calls an Anthropic Messages API compatible
// endpoint (https://docs.anthropic.com/en/api/messages). The MiniMax
// models exposed via opencode.ai/zen/go/v1/messages speak this protocol.
//
// The endpoint is identified by AI_BASE_URL (e.g.
// "https://opencode.ai/zen/go/v1"); the library POSTs to
// <baseURL>/messages with x-api-key + anthropic-version headers.
type AnthropicSummarizer struct {
	baseURL  string
	model    string
	apiKey   string
	category []string
	uncat    string
	timeout  time.Duration
	client   *http.Client
	log      *slog.Logger
}

// NewAnthropicSummarizer constructs a summarizer that targets the
// Anthropic Messages API. baseURL must NOT include the trailing
// /messages path; it is appended at call time.
func NewAnthropicSummarizer(
	baseURL string,
	model string,
	apiKey string,
	categoryNames []string,
	uncategorizedLabel string,
	perCallTimeout time.Duration,
	log *slog.Logger,
) *AnthropicSummarizer {
	if log == nil {
		log = slog.Default()
	}
	if uncategorizedLabel == "" {
		uncategorizedLabel = "Uncategorized"
	}
	if perCallTimeout <= 0 {
		perCallTimeout = 8 * time.Second
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &AnthropicSummarizer{
		baseURL:  baseURL,
		model:    model,
		apiKey:   apiKey,
		category: categoryNames,
		uncat:    uncategorizedLabel,
		timeout:  perCallTimeout,
		client:   &http.Client{Timeout: perCallTimeout + 2*time.Second},
		log:      log,
	}
}

// messagesRequest is the JSON body POSTed to /v1/messages.
type messagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []messagesReqEntry `json:"messages"`
}

// messagesReqEntry is one message in the request.
type messagesReqEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesResponse is the JSON body returned by /v1/messages on success.
type messagesResponse struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Role      string             `json:"role"`
	Model     string             `json:"model"`
	Content   []messagesRespPart `json:"content"`
	StopReason string            `json:"stop_reason"`
}

// messagesRespPart is one block of the assistant content. We only
// consume the "text" parts; any "tool_use" or other type is ignored.
type messagesRespPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messagesError is the JSON body returned by /v1/messages on failure.
type messagesError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Summarize implements Summarizer. The full contract lives in
// contracts/ai-summarizer.md; this implementation honors:
//   - per-call timeout (context.WithTimeout + http.Client.Timeout)
//   - strict JSON response parsing (after stripping any code fences)
//   - ErrUnavailable on transport / decode / API-error failures
//   - ErrCategoryUnknown when the returned category is not in the
//     configured set and is not the uncategorized_label
func (s *AnthropicSummarizer) Summarize(ctx context.Context, in Input) (Output, error) {
	if strings.TrimSpace(in.Text) == "" && len(in.Captions) == 0 {
		return Output{}, ErrInvalidInput
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req := messagesRequest{
		Model:     s.model,
		MaxTokens: 512,
		System:    s.systemPrompt(),
		Messages: []messagesReqEntry{
			{Role: "user", Content: s.userPrompt(in)},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Output{}, ErrUnavailable
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return Output{}, ErrUnavailable
	}
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.log.Warn("anthropic: call failed", "err", err)
		return Output{}, ErrUnavailable
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Output{}, ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr messagesError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			s.log.Warn("anthropic: api error",
				"status", resp.StatusCode,
				"type", apiErr.Error.Type,
				"message", apiErr.Error.Message)
		} else {
			s.log.Warn("anthropic: api error",
				"status", resp.StatusCode,
				"body", truncateBody(string(respBody), 200))
		}
		return Output{}, ErrUnavailable
	}

	var ok messagesResponse
	if err := json.Unmarshal(respBody, &ok); err != nil {
		s.log.Warn("anthropic: decode failed", "err", err)
		return Output{}, ErrUnavailable
	}
	// Concatenate any text parts. Most responses are a single text
	// part, but be defensive in case the model returns multiple.
	var b strings.Builder
	for _, p := range ok.Content {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return s.parseResponse(b.String())
}

// systemPrompt builds the fixed system message per contracts/ai-summarizer.md.
// The Anthropic API exposes a top-level system field, so we put the
// instructions there and keep the user prompt as a single user turn.
func (s *AnthropicSummarizer) systemPrompt() string {
	all := append([]string{}, s.category...)
	if !contains(all, s.uncat) {
		all = append(all, s.uncat)
	}
	return fmt.Sprintf(
		`You are a news digest assistant. You will receive a single Telegram channel post. `+
			`Produce a one-line summary (≤ 280 chars) in the same language as the post, and assign the post `+
			`to exactly one of the following categories: %s. `+
			`Respond with strict JSON: {"summary": "...", "category": "...", "confidence": 0.0..1.0}. `+
			`No prose, no markdown fences, no leading or trailing characters outside the JSON object.`,
		strings.Join(all, ", "),
	)
}

// userPrompt builds the per-message user prompt. The format mirrors
// the OpenAI summarizer so the same fixtures can be reused in tests.
func (s *AnthropicSummarizer) userPrompt(in Input) string {
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

// parseResponse decodes the model's reply and validates the fields
// against the contract. Returns ErrUnavailable for malformed JSON,
// and ErrCategoryUnknown when the returned category is not in the
// configured set and is not the uncategorized_label.
func (s *AnthropicSummarizer) parseResponse(raw string) (Output, error) {
	cleaned := stripCodeFences(raw)
	cleaned = strings.TrimSpace(cleaned)
	// Be forgiving: models occasionally wrap the JSON in stray
	// prose like "Here is the JSON: {…}". Find the first '{' and
	// the matching last '}' and parse the slice in between.
	if i := strings.Index(cleaned, "{"); i >= 0 {
		if j := strings.LastIndex(cleaned, "}"); j > i {
			cleaned = cleaned[i : j+1]
		}
	}
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
		return Output{Summary: summary, Category: s.uncat, Confidence: clamp01(r.Confidence)}, ErrCategoryUnknown
	}
	known := make(map[string]bool, len(s.category)+1)
	for _, c := range s.category {
		known[c] = true
	}
	known[s.uncat] = true
	if !known[category] {
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

func truncateBody(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
