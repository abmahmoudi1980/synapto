package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// MaxBufferedPostsPerChannel caps the per-channel post buffer to keep
// memory bounded if the cycle is stalled. The cycle advances the cursor
// to the latest drained MessageID, so a dropped oldest post is acceptable
// (it is already older than any cursor the cycle will ever set).
const MaxBufferedPostsPerChannel = 5000

// Real implements Client against the live Telegram Bot API. It uses
// long-polling getUpdates to receive channel_post events for channels
// the bot has been added to (typically as administrator).
//
// The Bot API does not expose channel history to bots: there is no
// "list channel messages" endpoint. The only way a bot can observe
// new posts in a channel is by receiving them via getUpdates. The
// Real client therefore buffers posts as they arrive and exposes them
// through FetchNewPosts in ascending MessageID order.
//
// For a public channel to deliver channel_post updates to the bot,
// the bot must be a member of the channel (admin recommended). The
// service does not add the bot automatically; the operator must do
// that once via the Telegram client.
//
// The underlying tgbotapi.BotAPI is constructed lazily on the first
// long-poll tick. This means a transient Telegram outage at boot
// (or a sandbox without internet) does not prevent the service from
// starting: the long-poll goroutine keeps retrying with backoff
// until the API is reachable, then it just works.
type Real struct {
	token string
	bot   *tgbotapi.BotAPI // lazily constructed
	log   *slog.Logger

	// mu protects channels, postsByHandle, offset, subscriberChatID, and bot.
	mu                sync.Mutex
	channels          map[string]ChannelInfo // by handle, lowercase
	postsByHandle     map[string][]Post
	offset            int
	subscriberChatID  int64
	subscriberChatSet bool

	// ctx is canceled by Close to stop the long-poll goroutine.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// onSubscriberChat is called (in a background goroutine) when the
	// bot observes a private message from the subscriber. The wiring
	// layer can use it to persist the chat id back to the settings row
	// before the next cycle reads it.
	onSubscriberChat func(chatID int64)
}

// NewReal constructs a Real client for the given bot token. The
// long-poll goroutine is started immediately and lazily constructs
// the underlying tgbotapi.BotAPI on the first tick. Network or token
// errors are NOT fatal at construction time — the long-poll goroutine
// retries with backoff, and per-operation calls surface their own
// errors. This keeps the service bootable when the Bot API is briefly
// unreachable (e.g. a sandbox without internet, or a transient DNS
// hiccup). The token is validated the first time a getUpdates call
// succeeds.
func NewReal(token string, log *slog.Logger, onSubscriberChat func(chatID int64)) (*Real, error) {
	if token == "" {
		return nil, errors.New("telegram real: empty bot token")
	}
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Real{
		token:            token,
		log:              log,
		channels:         make(map[string]ChannelInfo),
		postsByHandle:    make(map[string][]Post),
		ctx:              ctx,
		cancel:           cancel,
		done:             make(chan struct{}),
		onSubscriberChat: onSubscriberChat,
	}
	go r.longPoll()
	return r, nil
}

// ensureBot returns a live *tgbotapi.BotAPI, constructing it on the
// first call. Returns an error if the Bot API is unreachable or the
// token is rejected; callers should back off and retry.
func (r *Real) ensureBot() (*tgbotapi.BotAPI, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bot != nil {
		return r.bot, nil
	}
	bot, err := tgbotapi.NewBotAPI(r.token)
	if err != nil {
		return nil, err
	}
	r.bot = bot
	return bot, nil
}

// longPoll runs the getUpdates loop until ctx is canceled. It updates
// the post buffer and the channel map as updates arrive. The
// underlying *tgbotapi.BotAPI is constructed lazily so a transient
// Telegram outage at boot does not prevent the service from starting.
func (r *Real) longPoll() {
	defer close(r.done)
	backoff := 2 * time.Second
	maxBackoff := 30 * time.Second
	for {
		if r.ctx.Err() != nil {
			return
		}
		bot, err := r.ensureBot()
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			r.log.Warn("telegram real: cannot reach bot api, retrying",
				"err", err, "backoff", backoff)
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
		r.mu.Lock()
		offset := r.offset
		r.mu.Unlock()
		cfg := tgbotapi.NewUpdate(offset)
		cfg.Timeout = 25 // seconds, long-poll window
		cfg.AllowedUpdates = []string{"channel_post", "edited_channel_post", "message"}
		updates, err := bot.GetUpdates(cfg)
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			r.log.Warn("telegram real: getUpdates failed", "err", err)
			select {
			case <-r.ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		for _, u := range updates {
			r.handleUpdate(u)
		}
	}
}

// handleUpdate processes one Update: buffers channel posts, captures
// subscriber chat id from private messages, and advances the offset.
func (r *Real) handleUpdate(u tgbotapi.Update) {
	r.mu.Lock()
	if u.UpdateID >= r.offset {
		r.offset = u.UpdateID + 1
	}
	r.mu.Unlock()

	switch {
	case u.ChannelPost != nil:
		r.bufferPost(u.ChannelPost)
	case u.EditedChannelPost != nil:
		// Treat edits as fresh posts with the same MessageID so the
		// digest uses the latest content (spec edge case: deleted/edited
		// messages — we keep the version captured at fetch time).
		r.bufferPost(u.EditedChannelPost)
	case u.Message != nil:
		// Private message from a user (subscriber). Capture chat id
		// so the service can deliver digests without a manual setup.
		if u.Message.Chat.ID != 0 {
			r.recordSubscriberChat(u.Message.Chat.ID)
		}
	}
}

// recordSubscriberChat remembers the chat id of the first private
// message the bot sees, and fires the optional callback exactly once
// (so we don't write to the settings row on every /start).
func (r *Real) recordSubscriberChat(chatID int64) {
	r.mu.Lock()
	alreadySet := r.subscriberChatSet
	r.mu.Unlock()
	if alreadySet {
		return
	}
	r.mu.Lock()
	if r.subscriberChatSet {
		r.mu.Unlock()
		return
	}
	r.subscriberChatSet = true
	r.subscriberChatID = chatID
	cb := r.onSubscriberChat
	r.mu.Unlock()
	r.log.Info("telegram real: subscriber chat auto-discovered", "chat_id", chatID)
	if cb != nil {
		// Run the callback asynchronously to avoid blocking the
		// long-poll goroutine on a slow DB write.
		go cb(chatID)
	}
}

// bufferPost appends a channel post to the per-handle buffer, registering
// the channel info if this is the first time we've seen it.
func (r *Real) bufferPost(m *tgbotapi.Message) {
	if m == nil || m.Chat.ID == 0 {
		return
	}
	handle := normalizeHandle(m.Chat.UserName)
	post := Post{
		ChannelID: m.Chat.ID,
		MessageID: int64(m.MessageID),
		Text:      m.Text,
		MediaKind: mediaKindFromMessage(m),
		Captions:  captionsFromMessage(m),
		SentAt:    time.Unix(int64(m.Date), 0).UTC(),
	}
	if m.Caption != "" && post.Text == "" {
		// Telegram puts the caption on Photo/Video/Voice/Document
		// messages; surface it as Text for the summarizer to see.
		post.Text = m.Caption
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.channels[handle]; !ok {
		r.channels[handle] = ChannelInfo{
			ID:          m.Chat.ID,
			Username:    handle,
			Title:       m.Chat.Title,
			Description: m.Chat.Description,
		}
	} else {
		// Keep display info fresh in case the channel renamed.
		ci := r.channels[handle]
		if m.Chat.Title != "" {
			ci.Title = m.Chat.Title
		}
		ci.Description = m.Chat.Description
		r.channels[handle] = ci
	}
	buf := r.postsByHandle[handle]
	buf = append(buf, post)
	if len(buf) > MaxBufferedPostsPerChannel {
		// Drop the oldest. The cycle will have already advanced the
		// cursor past anything older than sinceMsgID.
		buf = buf[len(buf)-MaxBufferedPostsPerChannel:]
	}
	r.postsByHandle[handle] = buf
}

// GetChat resolves a public channel by its handle. The real client
// first checks the local channel map (populated as channel_post updates
// arrive); if the handle is unknown it falls back to a Bot API getChat
// call by SuperGroupUsername.
func (r *Real) GetChat(ctx context.Context, handle string) (ChannelInfo, error) {
	h := normalizeHandle(handle)
	r.mu.Lock()
	if ci, ok := r.channels[h]; ok && ci.Title != "" {
		r.mu.Unlock()
		return ci, nil
	}
	r.mu.Unlock()
	bot, err := r.ensureBot()
	if err != nil {
		return ChannelInfo{}, fmt.Errorf("telegram real: getChat: %w", err)
	}
	chat, err := bot.GetChat(tgbotapi.ChatInfoConfig{
		ChatConfig: tgbotapi.ChatConfig{SuperGroupUsername: "@" + h},
	})
	if err != nil {
		return ChannelInfo{}, classifyChatError(err)
	}
	info := ChannelInfo{
		ID:          chat.ID,
		Username:    normalizeHandle(chat.UserName),
		Title:       chat.Title,
		Description: chat.Description,
	}
	if info.Username == "" {
		info.Username = h
	}
	r.mu.Lock()
	r.channels[h] = info
	r.mu.Unlock()
	return info, nil
}

// FetchNewPosts returns buffered posts for the channel with
// MessageID > sinceMsgID, in ascending order. Posts are removed from
// the buffer once returned. The caller is expected to advance its
// cursor to the largest MessageID it observed (the cycle does this).
func (r *Real) FetchNewPosts(ctx context.Context, channelHandle string, sinceMsgID int64) ([]Post, error) {
	h := normalizeHandle(channelHandle)
	r.mu.Lock()
	defer r.mu.Unlock()
	buf := r.postsByHandle[h]
	kept := buf[:0]
	var out []Post
	for _, p := range buf {
		if p.MessageID > sinceMsgID {
			out = append(out, p)
		} else {
			kept = append(kept, p)
		}
	}
	r.postsByHandle[h] = kept
	// out is already in insertion order, which equals ascending
	// MessageID (Telegram delivers updates in order).
	return out, nil
}

// SendMessage delivers one text message to the given chat. It honors
// the parseMode ("" or "MarkdownV2" or "HTML"). On a 403 it returns
// ErrBlocked; on 429 it honors RetryAfter and retries once; on other
// 4xx/5xx it returns an error.
func (r *Real) SendMessage(ctx context.Context, chatID int64, text, parseMode string) (SendResult, error) {
	if chatID == 0 {
		return SendResult{}, errors.New("telegram real: chat id is 0")
	}
	bot, err := r.ensureBot()
	if err != nil {
		return SendResult{}, fmt.Errorf("telegram real: send: %w", err)
	}
	msg := tgbotapi.MessageConfig{
		BaseChat: tgbotapi.BaseChat{ChatID: chatID},
		Text:     text,
	}
	switch parseMode {
	case "MarkdownV2":
		msg.ParseMode = tgbotapi.ModeMarkdownV2
	case "HTML":
		msg.ParseMode = tgbotapi.ModeHTML
	}
	res, err := bot.Send(msg)
	if err != nil {
		// Detect 403 / "bot was blocked" / "forbidden" / "user is
		// deactivated" responses and surface as ErrBlocked.
		if isBlockedErr(err) {
			return SendResult{Blocked: true}, ErrBlocked
		}
		// 429: respect RetryAfter. The library returns
		// *tgbotapi.Error with RetryAfter when the server asks to
		// back off.
		if isTooManyRequestsErr(err) {
			d := tooManyRequestsDelay(err)
			r.log.Warn("telegram real: 429, backing off", "retry_after", d)
			select {
			case <-ctx.Done():
				return SendResult{}, ctx.Err()
			case <-time.After(d):
			}
			res, err = bot.Send(msg)
			if err == nil {
				return SendResult{MessageID: int64(res.MessageID), OK: true}, nil
			}
		}
		return SendResult{}, fmt.Errorf("telegram real: send: %w", err)
	}
	return SendResult{MessageID: int64(res.MessageID), OK: true}, nil
}

// Close stops the long-poll goroutine and releases its resources.
func (r *Real) Close() error {
	r.cancel()
	<-r.done
	return nil
}

// classifyChatError maps a Bot API error from getChat to a sentinel.
func classifyChatError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "chat not found"):
		return ErrChannelNotFound
	case strings.Contains(msg, "bot is not a member"), strings.Contains(msg, "bot was kicked"),
		strings.Contains(msg, "bot can't initiate conversation"):
		return ErrBotNotInChannel
	}
	return fmt.Errorf("telegram real: getChat: %w", err)
}

// isBlockedErr returns true when the error from Send indicates the
// subscriber blocked the bot or the chat is otherwise unreachable for
// private-message delivery.
func isBlockedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bot was blocked") ||
		strings.Contains(msg, "forbidden: bot was blocked") ||
		strings.Contains(msg, "user is deactivated") ||
		strings.Contains(msg, "chat not found") ||
		strings.Contains(msg, "have no rights to send")
}

// isTooManyRequestsErr returns true when the error is a 429.
func isTooManyRequestsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many requests") || strings.Contains(msg, "retry after")
}

// tooManyRequestsDelay extracts the RetryAfter duration from a 429
// error. Falls back to 1 second.
func tooManyRequestsDelay(err error) time.Duration {
	var apiErr *tgbotapi.Error
	if errors.As(err, &apiErr) && apiErr.RetryAfter != 0 {
		return time.Duration(apiErr.RetryAfter) * time.Second
	}
	return 1 * time.Second
}

// mediaKindFromMessage infers a MediaKind label from a Message.
// The values returned here match the ai.MediaKind constants so the
// downstream cycle can treat the buffer uniformly.
func mediaKindFromMessage(m *tgbotapi.Message) string {
	switch {
	case len(m.Photo) > 0:
		return "image"
	case m.Video != nil:
		return "video"
	case m.Voice != nil:
		return "voice"
	case m.Audio != nil, m.Document != nil, m.Animation != nil, m.Sticker != nil:
		return "other"
	}
	return "text"
}

// captionsFromMessage returns any non-text caption text for the
// summarizer to consume alongside the primary text.
func captionsFromMessage(m *tgbotapi.Message) []string {
	if m.Caption == "" {
		return nil
	}
	return []string{m.Caption}
}

// normalizeHandle lowercases and strips a leading @.
func normalizeHandle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "@")
	return strings.ToLower(s)
}
