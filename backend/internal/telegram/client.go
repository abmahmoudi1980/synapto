// Package telegram wraps the Telegram Bot API for the assistant's two
// responsibilities: reading channel posts and sending digest messages.
package telegram

import (
	"context"
	"errors"
	"time"
)

// ChannelInfo describes a Telegram channel returned by GetChat.
type ChannelInfo struct {
	ID          int64
	Username    string // handle without leading @
	Title       string
	Description string
}

// Post is a single message observed in a channel.
type Post struct {
	ChannelID int64
	MessageID int64
	Text      string
	MediaKind string // "text" | "image" | "video" | "voice" | "other"
	Captions  []string
	SentAt    time.Time
}

// SendResult is the outcome of a SendMessage call.
type SendResult struct {
	MessageID int64
	OK        bool
	Blocked   bool // true when Telegram reports the bot was blocked by the user
}

// Client is the surface the digest cycle and admin API depend on. The
// concrete implementation wraps go-telegram-bot-api/v5; tests use Fake.
type Client interface {
	// GetChat resolves a public channel by its handle (without leading @).
	GetChat(ctx context.Context, handle string) (ChannelInfo, error)

	// FetchNewPosts returns posts in the channel with id > sinceMsgID, in
	// ascending order of MessageID. The caller advances its cursor with the
	// largest MessageID observed. The channel is identified by its handle
	// (without leading @) so the fetcher can resolve it via GetChat or
	// look it up in its internal map.
	FetchNewPosts(ctx context.Context, channelHandle string, sinceMsgID int64) ([]Post, error)

	// SendMessage delivers a single text message to the subscriber chat.
	SendMessage(ctx context.Context, chatID int64, text string, parseMode string) (SendResult, error)

	// Close releases any underlying resources (long-poll goroutines, etc).
	Close() error
}

// Sentinel errors for the telegram package.
var (
	// ErrChannelNotFound means Telegram replied with a 404 for the handle.
	ErrChannelNotFound = errors.New("telegram: channel not found")
	// ErrBotNotInChannel means the bot is not a member of the channel.
	ErrBotNotInChannel = errors.New("telegram: bot is not a member of the channel")
	// ErrUnavailable means the Telegram API is unreachable or returned 5xx.
	ErrUnavailable = errors.New("telegram: api unavailable")
	// ErrBlocked means the subscriber has blocked the bot.
	ErrBlocked = errors.New("telegram: subscriber blocked the bot")
)
