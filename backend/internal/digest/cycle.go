package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// CycleDeps bundles the dependencies a Cycle needs.
type CycleDeps struct {
	Log        *slog.Logger
	Telegram   telegram.Client
	Summarizer ai.Summarizer
	Channels   store.ChannelRepo
	Categories store.CategoryRepo
	Settings   store.SettingsRepo
	Cycles     store.CycleRepo
	Digests    store.DigestRepo
	Health     store.HealthRepo
	Posts      store.PostRepo
	// SubscriberChatID is the initial chat id used as a fallback when
	// the settings row has none. The cycle reads the live chat id from
	// the settings row at the start of every run, so changes (e.g. an
	// auto-discovered chat id from a /start message) take effect from
	// the next cycle without a restart.
	SubscriberChatID int64
}

// Cycle runs one digest cycle: fetch → dedup → summarize → categorize →
// render → send → record. It is safe to call concurrently with itself only
// if the Scheduler guards it (which it does).
type Cycle struct {
	deps CycleDeps
}

// NewCycle constructs a Cycle.
func NewCycle(deps CycleDeps) *Cycle {
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	return &Cycle{deps: deps}
}

// Run executes one complete digest cycle. The windowStart/windowEnd pair
// defines the time range this cycle covers; the scheduler picks them.
// Returns the cycle ID and an error only if the cycle could not be
// recorded.
//
// The cycle is now post-driven: it
//   1. fetches new posts from each channel and persists them as
//      posts rows (status='received'),
//   2. summarizes posts with status='received' (concurrent AI calls),
//   3. bundles all unsent posts (status IN
//      ('summarized','send_failed','included_in_digest')) into a single
//      digest,
//   4. sends the digest to Telegram, marking each post as sent or
//      send_failed.
// Posts that failed to send are picked up by the next cycle
// automatically (auto-retry; no manual intervention).
func (c *Cycle) Run(ctx context.Context, windowStart, windowEnd time.Time) (string, error) {
	log := c.deps.Log.With("window_start", windowStart.Format(time.RFC3339), "window_end", windowEnd.Format(time.RFC3339))
	cycleID := uuid.NewString()

	// Record the cycle as pending.
	if err := c.deps.Cycles.Create(ctx, store.Cycle{
		ID:          cycleID,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Status:      store.CyclePending,
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		return cycleID, err
	}
	log = log.With("cycle_id", cycleID)
	log.Info("cycle.start")

	// 1. Fetch: list active channels and pull new posts from each.
	// Each unique (channel_id, source_msg_id) becomes one persistent
	// posts row with status='received'. The UNIQUE constraint on
	// (channel_id, source_msg_id) prevents duplicates on refetch.
	channels, err := c.deps.Channels.List(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, 0, 0, "list channels: "+err.Error())
		return cycleID, err
	}
	fetched := 0
	for _, ch := range channels {
		if ch.Status != store.ChannelActive {
			continue
		}
		posts, err := c.deps.Telegram.FetchNewPosts(ctx, ch.Handle, ch.LastSeenMsgID)
		if err != nil {
			log.Warn("fetch channel failed", "channel", ch.Handle, "err", err)
			_ = c.deps.Channels.UpdateStatus(ctx, ch.ID, store.ChannelInaccessible, err.Error())
			_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
				OccurredAt: time.Now().UTC(), Level: "warn", Kind: "channel.fetch.failed",
				CycleID: cycleID, Message: "fetch failed for " + ch.Handle + ": " + err.Error(),
			})
			c.RecordChannelEvent(ctx, "channel.inaccessible", ch.Handle, err.Error())
			continue
		}
		if len(posts) > 0 {
			_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
				OccurredAt: time.Now().UTC(), Level: "info", Kind: "channel.fetch.ok",
				CycleID: cycleID, Message: ch.Handle,
			})
		}
		for _, p := range posts {
			dKey := computeDedupKey(p.Text, string(p.MediaKind), p.Captions)
			link := "https://t.me/" + ch.Handle + "/" + strconv.FormatInt(p.MessageID, 10)
			newPost, created, upErr := c.deps.Posts.Upsert(ctx, store.Post{
				ChannelID:   ch.ID,
				SourceMsgID: p.MessageID,
				DedupKey:    dKey,
				Link:        link,
				RawText:     p.Text,
				MediaKind:   store.MediaKind(p.MediaKind),
				CapturedAt:  time.Now().UTC(),
				Status:      store.PostReceived,
			})
			if upErr != nil {
				log.Warn("post upsert failed", "channel", ch.Handle, "msg_id", p.MessageID, "err", upErr)
				continue
			}
			if !created {
				// Re-observation of an already-known (channel, msg);
				// the UNIQUE(channel_id, source_msg_id) constraint
				// already collapsed it. Nothing to do.
				continue
			}
			// Cross-channel content dedup. If another channel's post
			// with the same dedup_key was already delivered (status
			// 'sent') or intentionally dropped (status
			// 'filtered_out'), mark this new post as filtered_out so
			// it is not summarized and sent again. The new row stays
			// in the posts table for audit (so the operator can see
			// the second channel did forward the same content).
			if existing, err := c.deps.Posts.GetFirstTerminalByDedupKey(ctx, dKey); err == nil && existing.ID != newPost.ID {
				if mfErr := c.deps.Posts.MarkFiltered(ctx, newPost.ID); mfErr != nil {
					log.Warn("mark duplicate filtered failed", "post_id", newPost.ID, "err", mfErr)
				} else {
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "info", Kind: "post.duplicate",
						CycleID: cycleID,
						Message: ch.Handle + "/" + strconv.FormatInt(p.MessageID, 10) +
							" duplicate of " + existing.ID,
					})
				}
				continue
			}
			fetched++
			_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
				OccurredAt: time.Now().UTC(), Level: "info", Kind: "post.received",
				CycleID: cycleID, Message: ch.Handle + "/" + strconv.FormatInt(p.MessageID, 10),
			})
		}
		// Advance cursor to the latest post.
		if len(posts) > 0 {
			last := posts[len(posts)-1].MessageID
			_ = c.deps.Channels.AdvanceCursor(ctx, ch.ID, last, time.Now().UTC())
		}
	}
	log.Info("cycle.fetched", "received", fetched)

	// 2. Summarize: list posts with status='received' and call AI.
	settings, err := c.deps.Settings.Get(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, 0, "get settings: "+err.Error())
		return cycleID, err
	}
	categories, err := c.deps.Categories.List(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, 0, "list categories: "+err.Error())
		return cycleID, err
	}
	categorySet := make(map[string]bool, len(categories))
	for _, cat := range categories {
		categorySet[cat.Name] = true
	}

	received, err := c.deps.Posts.ListReceived(ctx, 200)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, 0, "list received posts: "+err.Error())
		return cycleID, err
	}
	degraded := false
	if len(received) > 0 {
		degraded = c.summarizeBatch(ctx, received, categories, settings.UncategorizedLabel)
	}
	log.Info("cycle.summarized", "items", len(received), "degraded", degraded)

	// 2b. Branch on delivery mode. The fetch + summarize steps above
	// are shared; the send step is the only thing that changes.
	// Per-post mode runs the per-post send loop in per_post_cycle.go
	// and finishes the cycle there.
	if settings.DeliveryMode == store.DeliveryPerPost {
		_, _, perr := c.runPerPost(ctx, cycleID, fetched, channels, settings, categories)
		if perr != nil && perr != context.Canceled {
			return cycleID, perr
		}
		return cycleID, nil
	}

	// 3. Bundle: list all unsent posts (summarized + previously
	// send_failed). The cutoff is "now" so any post whose last
	// attempt was before the current call to ListUnsent is eligible
	// for re-bundling. A post that the current cycle just marked
	// included_in_digest has last_attempt_at = now, so a follow-up
	// ListUnsent within the same Run() would not re-bundle it.
	unsent, err := c.deps.Posts.ListUnsent(ctx, time.Now().UTC(), 200)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, fetched, 0, "list unsent posts: "+err.Error())
		return cycleID, err
	}
	if len(unsent) == 0 {
		_ = c.finishCycle(ctx, cycleID, store.CycleSkippedNoItems, fetched, 0, "")
		log.Info("cycle.skipped_no_items")
		return cycleID, nil
	}

	// 4. Render. Filter out posts whose summary is empty (filtered_out
	// or never summarized).
	type renderPost struct {
		post store.Post
		cat  string
		ord  int
	}
	renderPosts := make([]renderPost, 0, len(unsent))
	channelHandles := make(map[string]string, len(channels))
	for _, ch := range channels {
		channelHandles[ch.ID] = ch.Handle
	}
	for _, p := range unsent {
		if p.Summary == "" {
			continue
		}
		catName := ""
		for _, cat := range categories {
			if cat.ID == p.CategoryID {
				catName = cat.Name
				break
			}
		}
		if catName == "" || !categorySet[catName] {
			catName = settings.UncategorizedLabel
		}
		var ord int
		for _, cat := range categories {
			if cat.Name == catName {
				ord = cat.Ordering
				break
			}
		}
		renderPosts = append(renderPosts, renderPost{post: p, cat: catName, ord: ord})
	}

	renderItems := make([]RenderItem, 0, len(renderPosts))
	for _, rp := range renderPosts {
		handle := channelHandles[rp.post.ChannelID]
		if handle == "" {
			handle = "unknown"
		}
		renderItems = append(renderItems, RenderItem{
			Summary:       rp.post.Summary,
			CategoryName:  rp.cat,
			CategoryOrder: rp.ord,
			ChannelHandle: handle,
			MediaKind:     ai.MediaKind(rp.post.MediaKind),
		})
	}
	messages := Render(RenderInput{
		WindowEnd:     windowEnd,
		CycleID:       cycleID,
		Items:         renderItems,
		Degraded:      degraded,
		Uncategorized: settings.UncategorizedLabel,
	})

	// 5. Mark all bundled posts as 'included_in_digest' before we
	// attempt the send. If we crash between here and the actual send,
	// the next cycle will pick them up via ListUnsent (because
	// status='included_in_digest' and last_attempt_at is set to now,
	// which is < next cycle's window_start).
	postIDs := make([]string, 0, len(renderPosts))
	for _, rp := range renderPosts {
		postIDs = append(postIDs, rp.post.ID)
	}
	if err := c.deps.Posts.MarkIncluded(ctx, postIDs); err != nil {
		log.Warn("mark posts included failed", "err", err)
	}

	// 6. Send to Telegram.
	renderedText := ""
	if len(messages) > 0 {
		renderedText = messages[0]
	}
	sendStatus := store.SendOK
	var telegramMsgID int64
	chatID := settings.TelegramSubscriberChat
	if chatID == 0 {
		chatID = c.deps.SubscriberChatID
	}
	if chatID != 0 && len(messages) > 0 {
		for i, msg := range messages {
			res, err := c.deps.Telegram.SendMessage(ctx, chatID, msg, "MarkdownV2")
			if err != nil {
				log.Warn("send failed", "part", i, "err", err)
				sendStatus = store.SendFailed
				c.RecordTelegramEvent(ctx, "telegram.send.failed",
					"part "+strconv.Itoa(i)+" to chat "+strconv.FormatInt(chatID, 10)+": "+err.Error())
				break
			}
			if res.Blocked {
				sendStatus = store.SendBlocked
				c.RecordTelegramEvent(ctx, "telegram.send.blocked",
					"subscriber "+strconv.FormatInt(chatID, 10)+" blocked the bot")
				break
			}
			if i == 0 {
				telegramMsgID = res.MessageID
			}
			if i < len(messages)-1 {
				select {
				case <-ctx.Done():
					break
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	} else if len(messages) > 0 {
		sendStatus = store.SendFailed
		log.Warn("send skipped: no subscriber chat id",
			"hint", "set TELEGRAM_SUBSCRIBER_CHAT in the env, or use TELEGRAM_SOURCE=longpoll to auto-discover from /start")
		c.RecordTelegramEvent(ctx, "telegram.send.no_recipient",
			"no subscriber chat id configured; send skipped (set TELEGRAM_SUBSCRIBER_CHAT or use longpoll)")
	}

	// 7. Per-post send outcome. Sent → 'sent'; failed/blocked/no-recipient →
	// 'send_failed'. The next cycle's ListUnsent will pick up the failed
	// posts again (auto-retry).
	if len(messages) > 0 {
		if sendStatus == store.SendOK {
			for _, id := range postIDs {
				if err := c.deps.Posts.MarkSent(ctx, id, telegramMsgID); err != nil {
					log.Warn("mark post sent failed", "post_id", id, "err", err)
				} else {
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "info", Kind: "post.sent",
						CycleID: cycleID, Message: id,
					})
				}
			}
		} else {
			errMsg := "send_status=" + string(sendStatus)
			for _, id := range postIDs {
				if err := c.deps.Posts.MarkSendFailed(ctx, id, errMsg); err != nil {
					log.Warn("mark post send_failed failed", "post_id", id, "err", err)
				} else {
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "warn", Kind: "post.send_failed",
						CycleID: cycleID, Message: id + ": " + errMsg,
					})
				}
			}
		}
	}

	// 8. Record digest row.
	digestID := uuid.NewString()
	if err := c.deps.Digests.Create(ctx, store.Digest{
		ID:           digestID,
		CycleID:      cycleID,
		RenderedText: renderedText,
		Degraded:     degraded,
		SentAt:       time.Now().UTC(),
		SendStatus:   sendStatus,
	}); err != nil {
		log.Error("record digest failed", "err", err)
	}

	// 9. Record digest_items (one row per post). UNIQUE(post_id) ensures
	// a post is in at most one digest_items row.
	for order, rp := range renderPosts {
		var catID string
		for _, cat := range categories {
			if cat.Name == rp.cat {
				catID = cat.ID
				break
			}
		}
		_ = c.deps.Digests.AddItem(ctx, store.DigestItem{
			CycleID:     cycleID,
			ChannelID:   rp.post.ChannelID,
			CategoryID:  catID,
			SourceMsgID: rp.post.SourceMsgID,
			PostID:      rp.post.ID,
			DedupKey:    rp.post.DedupKey,
			RawText:     rp.post.RawText,
			MediaKind:   rp.post.MediaKind,
			Summary:     rp.post.Summary,
			Confidence:  rp.post.Confidence,
			Ordering:    order,
		})
	}

	// Update send result.
	_ = c.deps.Digests.UpdateSendResult(ctx, digestID, telegramMsgID, sendStatus)

	// 10. Finish cycle.
	status := store.CycleSucceeded
	if degraded {
		status = store.CycleDegraded
	}
	if sendStatus != store.SendOK {
		status = store.CycleFailed
	}
	_ = c.finishCycle(ctx, cycleID, status, fetched, len(renderPosts), "")
	log.Info("cycle.done", "status", status, "items", len(renderPosts))

	return cycleID, nil
}

// computeDedupKey produces the dedup signature used by both the
// in-memory dedup helper and the post-queue. Format: "text:<sha256>"
// for text items, "media:<kind>:<captions-sha>" for media-only items.
func computeDedupKey(text, kind string, captions []string) string {
	t := strings.TrimSpace(text)
	if t != "" {
		return "text:" + hashText(t)
	}
	caps := make([]string, 0, len(captions))
	for _, c := range captions {
		c = strings.TrimSpace(c)
		if c != "" {
			caps = append(caps, c)
		}
	}
	return "media:" + kind + ":" + hashText(strings.Join(caps, "|"))
}

func hashText(s string) string {
	h := sha256.Sum256([]byte(strings.ToLower(s)))
	return hex.EncodeToString(h[:])
}

// summarizeBatch runs the summarizer over the supplied posts with a
// concurrency limit. The persistent post rows are updated in place:
//   - successful call → MarkSummarized with the category id resolved
//     from the configured category set,
//   - ErrCategoryUnknown → MarkSummarized with the uncategorized label
//     and a warn event for taxonomy tuning,
//   - ErrInvalidInput → MarkFiltered (item is dropped),
//   - any other error (including ErrUnavailable) → MarkSummarized with
//     the raw headline as the summary.
//
// Returns true if any item fell back to the raw-headline path
// (degraded mode).
func (c *Cycle) summarizeBatch(ctx context.Context, posts []store.Post, categories []store.Category, uncategorized string) bool {
	categorySet := make(map[string]bool, len(categories))
	for _, cat := range categories {
		categorySet[cat.Name] = true
	}
	var mu sync.Mutex
	degraded := false

	sem := make(chan struct{}, 8) // concurrency limit
	var wg sync.WaitGroup

	for _, p := range posts {
		wg.Add(1)
		go func(post store.Post) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			handle := ""
			ch, _ := c.deps.Channels.Get(ctx, post.ChannelID)
			if ch.Handle != "" {
				handle = ch.Handle
			}
			o, err := c.deps.Summarizer.Summarize(ctx, ai.Input{
				ChannelHandle: handle,
				Text:          post.RawText,
				MediaKind:     ai.MediaKind(post.MediaKind),
				Captions:      nil,
			})
			if err != nil {
				switch {
				case errors.Is(err, ai.ErrInvalidInput):
					_ = c.deps.Posts.MarkFiltered(ctx, post.ID)
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "warn", Kind: "ai.invalid_input",
						Message: "dropped post " + post.ID + ": " + err.Error(),
					})
					return
				case errors.Is(err, ai.ErrCategoryUnknown):
					cat := o.Category
					if cat == "" || !categorySet[cat] {
						cat = uncategorized
					}
					_ = c.deps.Posts.MarkSummarized(ctx, post.ID, "", o.Summary, o.Confidence)
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "warn", Kind: "ai.category_unknown",
						Message: "category not in configured set: " + o.Category,
					})
					return
				default:
					mu.Lock()
					degraded = true
					mu.Unlock()
					raw := post.RawText
					if len(raw) > 280 {
						raw = raw[:277] + "…"
					}
					_ = c.deps.Posts.MarkSummarized(ctx, post.ID, "", raw, 0)
					_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
						OccurredAt: time.Now().UTC(), Level: "warn", Kind: "ai.unavailable",
						Message: "summarizer unavailable: " + err.Error(),
					})
					return
				}
			}
			if o.Category != "" && !categorySet[o.Category] {
				_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
					OccurredAt: time.Now().UTC(), Level: "warn", Kind: "ai.category_unknown",
					Message: "category not in configured set: " + o.Category,
				})
				o.Category = uncategorized
			}
			catID := ""
			for _, cat := range categories {
				if cat.Name == o.Category {
					catID = cat.ID
					break
				}
			}
			_ = c.deps.Posts.MarkSummarized(ctx, post.ID, catID, o.Summary, o.Confidence)
		}(p)
	}
	wg.Wait()
	return degraded
}

// finishCycle is a thin wrapper around CycleRepo.Finish that also
// records a terminal op_event (cycle.success, cycle.degraded, or
// cycle.failed) per contracts/admin-api.md.
func (c *Cycle) finishCycle(ctx context.Context, id string, status store.CycleStatus, inputCount, outputItems int, errMsg string) error {
	if err := c.deps.Cycles.Finish(ctx, id, status, inputCount, outputItems, errMsg); err != nil {
		return err
	}
	kind := "cycle.success"
	level := "info"
	msg := "cycle " + id + " succeeded with " + strconv.Itoa(outputItems) + " items"
	switch status {
	case store.CycleDegraded:
		kind = "cycle.degraded"
		msg = "cycle " + id + " delivered degraded with " + strconv.Itoa(outputItems) + " items"
	case store.CycleFailed:
		kind = "cycle.failed"
		level = "error"
		msg = "cycle " + id + " failed: " + errMsg
	case store.CycleSkippedNoItems:
		kind = "cycle.skipped_no_items"
		msg = "cycle " + id + " skipped: no new items"
	}
	_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
		OccurredAt: time.Now().UTC(),
		Level:      level,
		Kind:       kind,
		CycleID:    id,
		Message:    msg,
	})
	return nil
}

// RecordTelegramEvent emits a telegram.* op_event for the audit log.
// Called from the cycle's send path; exported so future tests / hooks
// can fire the same kind.
func (c *Cycle) RecordTelegramEvent(ctx context.Context, kind, message string) {
	level := "warn"
	if kind == "telegram.send.blocked" {
		level = "error"
	}
	_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
		OccurredAt: time.Now().UTC(),
		Level:      level,
		Kind:       kind,
		Message:    message,
	})
}

// RecordChannelEvent emits a channel.* op_event for the audit log.
func (c *Cycle) RecordChannelEvent(ctx context.Context, kind, handle, message string) {
	_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
		OccurredAt: time.Now().UTC(),
		Level:      "warn",
		Kind:       kind,
		Message:    handle + ": " + message,
	})
}

// itoa is a small allocation-free integer formatter used in event
// messages. We use strconv for simplicity now that it's already
// imported by sibling files.

// itoa64 is the int64 counterpart, kept for API symmetry.
var _ = strconv.FormatInt
