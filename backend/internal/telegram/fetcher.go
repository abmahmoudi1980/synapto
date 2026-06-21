package telegram

import (
	"context"
	"strings"
)

// Fetcher wraps a Client to provide higher-level fetch operations used by
// the admin API (validate-on-add) and the cycle (fetch new posts). It is
// a convenience layer; the cycle can also use Client directly.
type Fetcher struct {
	Client Client
}

// NewFetcher constructs a Fetcher over the given Client.
func NewFetcher(c Client) *Fetcher { return &Fetcher{Client: c} }

// ValidateHandle calls GetChat to confirm the channel exists and the bot
// can see it. Returns the display name on success, or an error from the
// telegram sentinel set (ErrChannelNotFound, ErrBotNotInChannel, ErrUnavailable).
func (f *Fetcher) ValidateHandle(ctx context.Context, handle string) (string, error) {
	h := strings.TrimPrefix(handle, "@")
	info, err := f.Client.GetChat(ctx, h)
	if err != nil {
		return "", err
	}
	return info.Title, nil
}
