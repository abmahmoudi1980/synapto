// Package ai provides the summarizer abstraction used by the digest cycle.
package ai

import (
	"context"
	"errors"
)

// MediaKind describes the primary content type of a source message.
type MediaKind string

const (
	MediaText  MediaKind = "text"
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
	MediaVoice MediaKind = "voice"
	MediaOther MediaKind = "other"
)

// Input is the data handed to a Summarizer for one source message.
type Input struct {
	ChannelHandle string    `json:"channel_handle"`
	Text          string    `json:"text"`
	MediaKind     MediaKind `json:"media_kind"`
	Captions      []string  `json:"captions,omitempty"`
}

// Output is the summarizer's response: a one-line summary and a category
// name that must match a name in the configured category set.
type Output struct {
	Summary    string  `json:"summary"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// Summarizer turns a source message into a summary + category.
// Implementations must be safe for concurrent use.
type Summarizer interface {
	Summarize(ctx context.Context, in Input) (Output, error)
}

// Sentinel errors returned by Summarizer implementations.
var (
	// ErrUnavailable signals a transient provider outage; the cycle degrades.
	ErrUnavailable = errors.New("ai: provider unavailable")
	// ErrInvalidInput signals the input was empty or malformed; the item is dropped.
	ErrInvalidInput = errors.New("ai: invalid input")
	// ErrCategoryUnknown signals the returned category is not in the configured set.
	ErrCategoryUnknown = errors.New("ai: category not in configured set")
)
