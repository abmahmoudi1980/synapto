package digest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// CycleDeps bundles the dependencies a Cycle needs.
type CycleDeps struct {
	Log         *slog.Logger
	Telegram    telegram.Client
	Summarizer  ai.Summarizer
	Channels    store.ChannelRepo
	Categories  store.CategoryRepo
	Settings    store.SettingsRepo
	Cycles      store.CycleRepo
	Digests     store.DigestRepo
	Health      store.HealthRepo
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
// Returns the cycle ID and an error only if the cycle could not be recorded.
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
	channels, err := c.deps.Channels.List(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, 0, 0, "list channels: "+err.Error())
		return cycleID, err
	}
	var allItems []Item
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
			continue
		}
		if len(posts) > 0 {
			_ = c.deps.Health.RecordEvent(ctx, store.OpEvent{
				OccurredAt: time.Now().UTC(), Level: "info", Kind: "channel.fetch.ok",
				CycleID: cycleID, Message: ch.Handle, Context: "",
			})
		}
		for _, p := range posts {
			allItems = append(allItems, Item{
				ChannelID:     ch.ID,
				ChannelHandle: ch.Handle,
				SourceMsgID:   p.MessageID,
				Text:          p.Text,
				MediaKind:     ai.MediaKind(p.MediaKind),
				Captions:      p.Captions,
			})
		}
		// Advance cursor to the latest post.
		if len(posts) > 0 {
			last := posts[len(posts)-1].MessageID
			_ = c.deps.Channels.AdvanceCursor(ctx, ch.ID, last, time.Now().UTC())
		}
	}

	// 2. Dedup.
	items := Dedup(allItems)
	log.Info("cycle.fetched", "raw", len(allItems), "deduped", len(items))

	// 3. No items → skip.
	if len(items) == 0 {
		_ = c.finishCycle(ctx, cycleID, store.CycleSkippedNoItems, 0, 0, "")
		log.Info("cycle.skipped_no_items")
		return cycleID, nil
	}

	// 4. Summarize + categorize (with concurrency limit).
	settings, err := c.deps.Settings.Get(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, len(items), 0, "get settings: "+err.Error())
		return cycleID, err
	}
	categories, err := c.deps.Categories.List(ctx)
	if err != nil {
		_ = c.finishCycle(ctx, cycleID, store.CycleFailed, len(items), 0, "list categories: "+err.Error())
		return cycleID, err
	}
	categorySet := make(map[string]bool, len(categories))
	for _, cat := range categories {
		categorySet[cat.Name] = true
	}

	summaries, degraded := c.summarizeBatch(ctx, items, categorySet, settings.UncategorizedLabel)
	log.Info("cycle.summarized", "items", len(summaries), "degraded", degraded)

	// 5. Render.
	renderItems := make([]RenderItem, 0, len(summaries))
	for i, s := range summaries {
		catName := s.Category
		catOrder := 0
		if !categorySet[catName] {
			catName = settings.UncategorizedLabel
		}
		for _, cat := range categories {
			if cat.Name == catName {
				catOrder = cat.Ordering
				break
			}
		}
		renderItems = append(renderItems, RenderItem{
			Summary:       s.Summary,
			CategoryName:  catName,
			CategoryOrder: catOrder,
			ChannelHandle: items[i].ChannelHandle,
			MediaKind:     items[i].MediaKind,
		})
	}
	messages := Render(RenderInput{
		WindowEnd:     windowEnd,
		CycleID:       cycleID,
		Items:         renderItems,
		Degraded:      degraded,
		Uncategorized: settings.UncategorizedLabel,
	})

	// 6. Send to Telegram.
	renderedText := ""
	if len(messages) > 0 {
		renderedText = messages[0]
		if len(messages) > 1 {
			// For phase 1, join with a separator; the sender handles the actual
			// multi-message send. For recording, we store the first message.
			renderedText = messages[0]
		}
	}
	sendStatus := store.SendOK
	var telegramMsgID int64
	if c.deps.SubscriberChatID != 0 && len(messages) > 0 {
		for i, msg := range messages {
			res, err := c.deps.Telegram.SendMessage(ctx, c.deps.SubscriberChatID, msg, "MarkdownV2")
			if err != nil {
				log.Warn("send failed", "part", i, "err", err)
				sendStatus = store.SendFailed
				break
			}
			if res.Blocked {
				sendStatus = store.SendBlocked
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
	}

	// 7. Record digest.
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

	// Record digest items.
	for i, s := range summaries {
		catName := s.Category
		if !categorySet[catName] {
			catName = settings.UncategorizedLabel
		}
		var catID string
		for _, cat := range categories {
			if cat.Name == catName {
				catID = cat.ID
				break
			}
		}
		_ = c.deps.Digests.AddItem(ctx, store.DigestItem{
			CycleID:     cycleID,
			ChannelID:   items[i].ChannelID,
			CategoryID:  catID,
			SourceMsgID: items[i].SourceMsgID,
			DedupKey:    string(items[i].Key()),
			RawText:     items[i].Text,
			MediaKind:   store.MediaKind(items[i].MediaKind),
			Summary:     s.Summary,
			Confidence:  s.Confidence,
			Ordering:    i,
		})
	}

	// Update send result.
	_ = c.deps.Digests.UpdateSendResult(ctx, digestID, telegramMsgID, sendStatus)

	// 8. Finish cycle.
	status := store.CycleSucceeded
	if degraded {
		status = store.CycleDegraded
	}
	if sendStatus != store.SendOK {
		status = store.CycleFailed
	}
	_ = c.finishCycle(ctx, cycleID, status, len(items), len(summaries), "")
	log.Info("cycle.done", "status", status, "items", len(summaries))

	return cycleID, nil
}

// summarizeBatch runs the summarizer over all items with a concurrency
// limit. If any call returns ErrUnavailable, the cycle is marked degraded
// and the remaining items use raw text as the summary.
func (c *Cycle) summarizeBatch(ctx context.Context, items []Item, categories map[string]bool, uncategorized string) ([]ai.Output, bool) {
	out := make([]ai.Output, len(items))
	degraded := false

	var mu sync.Mutex
	sem := make(chan struct{}, 8) // concurrency limit
	var wg sync.WaitGroup

	for i, it := range items {
		wg.Add(1)
		go func(idx int, item Item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			o, err := c.deps.Summarizer.Summarize(ctx, ai.Input{
				ChannelHandle: item.ChannelHandle,
				Text:          item.Text,
				MediaKind:     item.MediaKind,
				Captions:      item.Captions,
			})
			if err != nil {
				mu.Lock()
				if err == ai.ErrUnavailable || err != ai.ErrInvalidInput {
					degraded = true
					// Fall back to raw text (truncated).
					raw := item.Text
					if len(raw) > 280 {
						raw = raw[:277] + "…"
					}
					out[idx] = ai.Output{Summary: raw, Category: uncategorized, Confidence: 0}
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			out[idx] = o
			mu.Unlock()
		}(i, it)
	}
	wg.Wait()
	return out, degraded
}

// finishCycle is a thin wrapper around CycleRepo.Finish.
func (c *Cycle) finishCycle(ctx context.Context, id string, status store.CycleStatus, inputCount, outputItems int, errMsg string) error {
	return c.deps.Cycles.Finish(ctx, id, status, inputCount, outputItems, errMsg)
}
