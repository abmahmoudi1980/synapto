package ai

import (
	"context"
	"strings"
	"unicode/utf8"
)

// FakeRule pairs a match predicate with a canned Output. Rules are evaluated
// in order; the first matching rule wins.
type FakeRule struct {
	Match  func(Input) bool
	Output Output
}

// Fake is a rule-based Summarizer for tests and local development.
// It is safe for concurrent use (the rule slice is read-only after construction).
type Fake struct {
	rules            []FakeRule
	uncategorized    string
	defaultConfidence float64
}

// NewFake builds a Fake summarizer. If no rule matches, the fake returns a
// truncated copy of the input text under the uncategorized label.
func NewFake(rules []FakeRule, uncategorizedLabel string) *Fake {
	if uncategorizedLabel == "" {
		uncategorizedLabel = "Uncategorized"
	}
	return &Fake{
		rules:             rules,
		uncategorized:     uncategorizedLabel,
		defaultConfidence: 0.5,
	}
}

// Summarize implements Summarizer. It never returns an error in phase 1;
// tests that need to exercise the error paths should use a custom fake.
func (f *Fake) Summarize(_ context.Context, in Input) (Output, error) {
	for _, r := range f.rules {
		if r.Match != nil && r.Match(in) {
			return r.Output, nil
		}
	}
	// Default fallback: truncate the text and label as Uncategorized.
	summary := truncate(clean(in.Text), 280)
	if summary == "" {
		summary = "[" + string(in.MediaKind) + "]"
	}
	return Output{
		Summary:    summary,
		Category:   f.uncategorized,
		Confidence: f.defaultConfidence,
	}, nil
}

// truncate cuts s to at most n runes, appending an ellipsis when shortened.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

// clean collapses whitespace and strips control characters from a string.
func clean(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
