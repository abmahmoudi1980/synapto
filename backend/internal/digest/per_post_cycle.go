package digest

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// perPostSendGap is the soft throttle between consecutive Telegram sends
// in per-post mode. Telegram's per-chat limit is ≈1 message/second for
// bots; the global limit is ≈30/s. 1.5s/post gives us ≤40 msg/min, well
// under either cap and leaves headroom for the AI summarization work
// that runs in the same cycle.
const perPostSendGap = 1500 * time.Millisecond

// perPostLimit caps the number of posts the per-post path will attempt
// to send in a single cycle. This is a backstop against the per-cycle
// time budget being blown by a sudden surge of incoming posts; the
// remaining posts stay in 'summarized' / 'send_failed' and are picked
// up by the next cycle.
const perPostLimit = 200

// MaxSendAttempts is the number of consecutive send failures after
// which a post is auto-marked 'dead' in per-post mode. The bundled
// mode does not mark posts dead (a single bundled send failure
// means the whole batch failed; the per-post failure attribution
// is unreliable). In per-post mode, the per-post send isolates
// the cause, so a high attempt count is a strong signal of a
// permanently broken post (e.g. text that Telegram will never
// accept, or a stale handle).
//
// 20 attempts at the default 10-minute interval is ~3.3 hours of
// repeated failure before a post is given up on. Tunable through
// this constant; not surfaced as a setting because the production
// system has a single subscriber and a single bot, so a global
// threshold is appropriate.
const MaxSendAttempts = 20

// runPerPost is the per-post delivery path. It runs after the fetch +
// summarize steps (which are shared with the bundled path). It:
//
//   1. Lists all unsent posts (status IN 'summarized', 'send_failed',
//      'included_in_digest') up to perPostLimit.
//   2. For each post, in captured_at order:
//        a. MarkIncluded (status → 'included_in_digest', last_attempt_at = now).
//        b. Throttle perPostSendGap (no delay before the first send).
//        c. Build a single-message text via RenderPerPost.
//        d. SendMessage via the Telegram client. If the MarkdownV2 send
//           errors, retry once with plain text (Telegram's parse errors
//           are common with non-Latin scripts).
//        e. MarkSent on success, MarkSendFailed on failure. The
//           per-post send_error field now records the actual
//           Telegram API error string (not a status label), so the
//           admin panel can show why a post failed.
//   3. Returns the count of sent and failed posts.
//
// No rows are written to digests or digest_items in per-post mode —
// the per-post messages have no rendered-text equivalent, and the
// per-post delivery record lives on the posts table itself
// (sent_at, telegram_msg_id, attempts).
func (c *Cycle) runPerPost(
	ctx context.Context,
	cycleID string,
	fetched int,
	channels []store.Channel,
	settings store.Settings,
	categories []store.Category,
) (sent, failed int, err error) {
	log := c.deps.Log.With("cycle_id", cycleID, "mode", "per_post")

	unsent, err := c.deps.Posts.ListUnsent(ctx, time.Now().UTC(), perPostLimit)
	if err != nil {
		return 0, 0, err
	}
	if len(unsent) == 0 {
		log.Info("cycle.per_post.skipped_no_items", "fetched", fetched)
		_ = c.finishCycle(ctx, cycleID, store.CycleSkippedNoItems, fetched, 0, "")
		return 0, 0, nil
	}
	log.Info("cycle.per_post.start", "fetched", fetched, "unsent", len(unsent))

	chatID := settings.TelegramSubscriberChat
	if chatID == 0 {
		chatID = c.deps.SubscriberChatID
	}
	if chatID == 0 {
		// No recipient: short-circuit the whole batch and record a
		// no_recipient op event. Every post is moved to
		// send_failed with a descriptive error so the admin can
		// see what would have been sent.
		c.RecordTelegramEvent(ctx, "telegram.send.no_recipient",
			"no subscriber chat id configured; per-post send skipped (set TELEGRAM_SUBSCRIBER_CHAT or use longpoll)")
		for _, p := range unsent {
			_ = c.deps.Posts.MarkSendFailed(ctx, p.ID, "no_recipient: TELEGRAM_SUBSCRIBER_CHAT not configured")
			_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
				OccurredAt: time.Now().UTC(), Level: "warn", Kind: "post.send_failed",
				Context: "{\"reason\":\"no_recipient\"}",
				Message: p.ID,
			})
		}
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, 0, "no subscriber chat id")
		log.Warn("cycle.per_post.no_recipient", "items", len(unsent))
		return 0, len(unsent), nil
	}

	handleMap := make(map[string]string, len(channels))
	for _, ch := range channels {
		handleMap[ch.ID] = ch.Handle
	}
	catNameMap := make(map[string]string, len(categories))
	for _, cat := range categories {
		catNameMap[cat.ID] = cat.Name
	}

	for i, p := range unsent {
		if i > 0 {
			select {
			case <-ctx.Done():
				log.Warn("cycle.per_post.aborted", "after", i, "of", len(unsent))
				_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, sent, "context canceled")
				return sent, failed, ctx.Err()
			case <-time.After(perPostSendGap):
			}
		}

		if err := c.deps.Posts.MarkIncluded(ctx, []string{p.ID}); err != nil {
			log.Warn("mark included failed", "post_id", p.ID, "err", err)
		}

		handle := handleMap[p.ChannelID]
		catName := catNameMap[p.CategoryID]
		if catName == "" {
			catName = settings.UncategorizedLabel
		}
		text := RenderPerPost(PerPostInput{
			Summary:       p.Summary,
			CategoryName:  catName,
			ChannelHandle: handle,
			Link:          p.Link,
			MediaKind:     ai.MediaKind(p.MediaKind),
		})

		res, sendErr := c.deps.Telegram.SendMessage(ctx, chatID, text, "MarkdownV2")
		if sendErr != nil {
			// Retry once with plain text — Telegram's MarkdownV2
			// parser occasionally rejects non-Latin content; the
			// plain-text retry is the same path the bundled
			// Sender.SendBatch uses.
			plain := telegram.StripMarkdownV2(text)
			res, sendErr = c.deps.Telegram.SendMessage(ctx, chatID, plain, "")
		}
		if sendErr != nil {
			c.markFailedAndMaybeDead(ctx, log, p, sendErr.Error(), false)
			failed++
			continue
		}
		if res.Blocked {
			errMsg := "blocked: subscriber " + strconv.FormatInt(chatID, 10) + " has blocked the bot"
			c.markFailedAndMaybeDead(ctx, log, p, errMsg, true)
			c.RecordTelegramEvent(ctx, "telegram.send.blocked",
				"subscriber "+strconv.FormatInt(chatID, 10)+" blocked the bot; remaining posts will continue in the next cycle")
			failed++
			// Do not abort: the next post may be for a chat that
			// is not blocked (different subscriber). In single-
			// subscriber mode this is a no-op distinction; in
			// future multi-subscriber mode it's the right call.
			continue
		}

		if err := c.deps.Posts.MarkSent(ctx, p.ID, res.MessageID); err != nil {
			log.Warn("mark sent failed", "post_id", p.ID, "err", err)
		}
		_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
			OccurredAt: time.Now().UTC(), Level: "info", Kind: "post.sent",
			Message: p.ID,
		})
		sent++
	}

	status := store.CycleSucceeded
	if sent == 0 && failed > 0 {
		status = store.CycleFailed
	} else if failed > 0 {
		status = store.CycleDegraded
	}
	_ = c.finishCycle(ctx, cycleID, status, fetched, sent, "")
	log.Info("cycle.per_post.done", "status", status, "sent", sent, "failed", failed, "total", len(unsent))
	return sent, failed, nil
}

// markFailedAndMaybeDead records a per-post send failure and, if the
// post has reached maxSendAttempts, transitions it to 'dead' so the
// cycle stops retrying it. Called from the per-post send loop on
// every send-side error (including blocked-subscriber). The
// 'blocked' flag is purely informational — the dead-check is the
// same in both cases.
func (c *Cycle) markFailedAndMaybeDead(ctx context.Context, log *slog.Logger, p store.Post, errMsg string, blocked bool) {
	if rmErr := c.deps.Posts.MarkSendFailed(ctx, p.ID, errMsg); rmErr != nil {
		log.Warn("mark send_failed failed", "post_id", p.ID, "err", rmErr)
	}
	_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
		OccurredAt: time.Now().UTC(), Level: "warn", Kind: "post.send_failed",
		Message: p.ID + ": " + errMsg,
	})

	// MarkSendFailed increments attempts by 1 in the DB. The
	// in-memory p.Attempts is the pre-increment value, so the
	// post-increment value is p.Attempts + 1.
	newAttempts := p.Attempts + 1
	if newAttempts < MaxSendAttempts {
		return
	}
	if mdErr := c.deps.Posts.MarkDead(ctx, p.ID); mdErr != nil {
		log.Warn("mark dead failed", "post_id", p.ID, "err", mdErr)
		return
	}
	reason := "send_failed"
	if blocked {
		reason = "blocked"
	}
	_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
		OccurredAt: time.Now().UTC(), Level: "info", Kind: "post.dead",
		Message: p.ID + ": marked dead after " + strconv.Itoa(newAttempts) +
			" attempts (" + reason + ")",
	})
	log.Warn("post marked dead",
		"post_id", p.ID, "attempts", newAttempts, "reason", reason, "err", errMsg)
}
