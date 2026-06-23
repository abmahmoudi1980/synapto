package telegram

import (
	"context"
	"strings"
	"time"
)

// Sender wraps a Client to provide higher-level send operations used by the
// digest cycle: multi-message batches with a 250ms inter-message gap and
// a one-shot plain-text fallback if MarkdownV2 is rejected by Telegram.
type Sender struct {
	Client Client
}

// NewSender constructs a Sender over the given Client.
func NewSender(c Client) *Sender { return &Sender{Client: c} }

// SendBatch delivers each message in order with a 250ms gap between sends.
// If a MarkdownV2 send fails with a parse error, the sender retries that
// message once with parseMode="" (plain text) after stripping escape
// characters. Returns the SendResult of the first message and the first
// error encountered (nil if all succeeded).
func (s *Sender) SendBatch(ctx context.Context, chatID int64, messages []string, parseMode string) (SendResult, error) {
	var firstResult SendResult
	for i, msg := range messages {
		res, err := s.Client.SendMessage(ctx, chatID, msg, parseMode)
		if err != nil {
			// Retry once with plain text if the parse mode was MarkdownV2.
			if parseMode == "MarkdownV2" {
				plain := stripMarkdownV2Escapes(msg)
				res, err = s.Client.SendMessage(ctx, chatID, plain, "")
			}
			if err != nil {
				return firstResult, err
			}
		}
		if i == 0 {
			firstResult = res
		}
		if res.Blocked {
			return firstResult, ErrBlocked
		}
		if i < len(messages)-1 {
			select {
			case <-ctx.Done():
				return firstResult, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return firstResult, nil
}

// StripMarkdownV2 removes the backslash-escape sequences produced by
// the renderer so a plain-text retry shows clean text. Exported for
// the per-post cycle path, which sends individual messages and falls
// back to plain text on parse errors.
func StripMarkdownV2(s string) string { return stripMarkdownV2Escapes(s) }

func stripMarkdownV2Escapes(s string) string {
	s = strings.ReplaceAll(s, `\_`, "_")
	s = strings.ReplaceAll(s, `\*`, "*")
	s = strings.ReplaceAll(s, `\[`, "[")
	s = strings.ReplaceAll(s, `\]`, "]")
	s = strings.ReplaceAll(s, `\(`, "(")
	s = strings.ReplaceAll(s, `\)`, ")")
	s = strings.ReplaceAll(s, `\~`, "~")
	s = strings.ReplaceAll(s, `\>`, ">")
	s = strings.ReplaceAll(s, `\#`, "#")
	s = strings.ReplaceAll(s, `\+`, "+")
	s = strings.ReplaceAll(s, `\-`, "-")
	s = strings.ReplaceAll(s, `\=`, "=")
	s = strings.ReplaceAll(s, `\|`, "|")
	s = strings.ReplaceAll(s, `\{`, "{")
	s = strings.ReplaceAll(s, `\}`, "}")
	s = strings.ReplaceAll(s, `\.`, ".")
	s = strings.ReplaceAll(s, `\!`, "!")
	// ZWSP (used to neutralize ` and * in summaries) is stripped too.
	s = strings.ReplaceAll(s, "\u200b", "")
	return s
}
