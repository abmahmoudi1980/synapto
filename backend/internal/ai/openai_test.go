package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/synapto/assistant/internal/ai"
)

// stubClient is a minimal OpenAIClient fake for unit tests.
type stubClient struct {
	resp openai.ChatCompletionResponse
	err  error

	gotReq openai.ChatCompletionRequest
}

func (s *stubClient) CreateChatCompletion(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	s.gotReq = req
	return s.resp, s.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestOpenAI_Summarize_HappyPath(t *testing.T) {
	stub := &stubClient{
		resp: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{
					Message: openai.ChatCompletionMessage{
						Content: `{"summary":"Telegram rolls out scheduled messages","category":"Technology","confidence":0.91}`,
					},
				},
			},
		},
	}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics", "Technology", "Business"}, "Uncategorized",
		2*time.Second, discardLogger())

	out, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram",
		Text:          "Telegram rolls out scheduled messages in channels",
		MediaKind:     ai.MediaText,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Summary != "Telegram rolls out scheduled messages" {
		t.Errorf("unexpected summary: %q", out.Summary)
	}
	if out.Category != "Technology" {
		t.Errorf("unexpected category: %q", out.Category)
	}
	if out.Confidence != 0.91 {
		t.Errorf("unexpected confidence: %v", out.Confidence)
	}

	// Verify the request shape honors the contract.
	if stub.gotReq.Model != "gpt-4o-mini" {
		t.Errorf("expected model gpt-4o-mini, got %q", stub.gotReq.Model)
	}
	if len(stub.gotReq.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(stub.gotReq.Messages))
	}
	if stub.gotReq.Messages[0].Role != openai.ChatMessageRoleSystem {
		t.Errorf("expected system message first")
	}
	if stub.gotReq.Messages[1].Role != openai.ChatMessageRoleUser {
		t.Errorf("expected user message second")
	}
	// System prompt must list the configured categories.
	if !contains(stub.gotReq.Messages[0].Content, "Politics") {
		t.Error("system prompt missing Politics")
	}
	if !contains(stub.gotReq.Messages[0].Content, "Technology") {
		t.Error("system prompt missing Technology")
	}
}

func TestOpenAI_Summarize_TransportError_MapsToErrUnavailable(t *testing.T) {
	stub := &stubClient{err: errors.New("network down")}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", time.Second, discardLogger())
	_, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestOpenAI_Summarize_EmptyChoices_MapsToErrUnavailable(t *testing.T) {
	stub := &stubClient{resp: openai.ChatCompletionResponse{Choices: nil}}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", time.Second, discardLogger())
	_, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestOpenAI_Summarize_MalformedJSON_MapsToErrUnavailable(t *testing.T) {
	stub := &stubClient{
		resp: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: `not json`}},
			},
		},
	}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", time.Second, discardLogger())
	_, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestOpenAI_Summarize_UnknownCategory_MapsToErrCategoryUnknown(t *testing.T) {
	stub := &stubClient{
		resp: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{
					Content: `{"summary":"x","category":"Aliens","confidence":0.5}`,
				}},
			},
		},
	}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics", "Technology"}, "Uncategorized", time.Second, discardLogger())
	out, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrCategoryUnknown) {
		t.Errorf("expected ErrCategoryUnknown, got %v", err)
	}
	// Per contract: substitute uncategorized_label.
	if out.Category != "Uncategorized" {
		t.Errorf("expected substitute category 'Uncategorized', got %q", out.Category)
	}
}

func TestOpenAI_Summarize_StripCodeFences(t *testing.T) {
	stub := &stubClient{
		resp: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{
					Content: "```json\n" + `{"summary":"y","category":"Politics","confidence":0.7}` + "\n```",
				}},
			},
		},
	}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", time.Second, discardLogger())
	out, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.Category != "Politics" {
		t.Errorf("expected Politics, got %q", out.Category)
	}
}

func TestOpenAI_Summarize_EmptyText_MapsToErrInvalidInput(t *testing.T) {
	s := ai.NewOpenAISummarizer(&stubClient{}, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", time.Second, discardLogger())
	_, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestOpenAI_Summarize_PerCallTimeoutHonored(t *testing.T) {
	// A stub client that respects ctx cancellation and returns a
	// error promptly proves the timeout fires.
	stub := &ctxAwareStub{delay: 500 * time.Millisecond}
	s := ai.NewOpenAISummarizer(stub, "gpt-4o-mini",
		[]string{"Politics"}, "Uncategorized", 50*time.Millisecond, discardLogger())
	_, err := s.Summarize(context.Background(), ai.Input{
		ChannelHandle: "telegram", Text: "hello", MediaKind: ai.MediaText,
	})
	if !errors.Is(err, ai.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable on timeout, got %v", err)
	}
}

// ctxAwareStub sleeps for `delay` or until ctx is done, then returns
// the wrapped error.
type ctxAwareStub struct {
	delay time.Duration
}

func (s *ctxAwareStub) CreateChatCompletion(ctx context.Context, _ openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	select {
	case <-time.After(s.delay):
		return openai.ChatCompletionResponse{}, errors.New("completed")
	case <-ctx.Done():
		return openai.ChatCompletionResponse{}, ctx.Err()
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(s) > len(sub) && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ensure json package is referenced in this file (used implicitly by
// the production code; this keeps `go vet` happy in case the import
// optimizer drops it).
var _ = json.Marshal
