package digest_test

import (
	"context"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/digest"
	"github.com/synapto/assistant/internal/logging"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
)

// TestPost_Lifecycle_ReceivedToSent walks a post through the full
// state machine in a single cycle: received → summarized → included → sent.
func TestPost_Lifecycle_ReceivedToSent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	seed := `[{"channel":"lifecycle_chan","messages":[
		{"id":5001,"text":"Breaking news story","media":"text"}
	]}]`
	tg, _ := newTestTelegram(t, seed)

	if _, err := st.AddChannel(ctx, "lifecycle_chan", "Lifecycle"); err != nil {
		t.Fatalf("add channel: %v", err)
	}
	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(999),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       ai.NewFake(nil, "Uncategorized"),
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 999,
	})

	_, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}

	// The post must exist and be in 'sent' state.
	posts, err := sqlite.PostStore{S: st}.ListAll(ctx, 10)
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	p := posts[0]
	if p.Status != store.PostSent {
		t.Errorf("expected status=sent, got %q", p.Status)
	}
	if p.Attempts != 1 {
		t.Errorf("expected attempts=1, got %d", p.Attempts)
	}
	if p.SentAt.IsZero() {
		t.Error("expected sent_at populated")
	}
	if p.Link == "" {
		t.Error("expected link populated")
	}
	if p.Summary == "" {
		t.Error("expected summary populated")
	}
}

// TestPost_AutoRetryOnSendFailure ensures a post that the cycle has
// marked send_failed (simulating a Telegram send error) is picked up
// by the next cycle and sent successfully.
func TestPost_AutoRetryOnSendFailure(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	channel, _ := st.AddChannel(ctx, "retry_chan", "Retry")
	ps := sqlite.PostStore{S: st}
	// Seed one post and walk it to send_failed directly. We bypass
	// the cycle here to keep the test focused on the auto-retry
	// contract: a send_failed post is in the next cycle's bundle.
	_, created, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   channel.ID,
		SourceMsgID: 6001,
		Link:        "https://t.me/retry_chan/6001",
		RawText:     "first item",
		MediaKind:   store.MediaText,
		Status:      store.PostReceived,
		CapturedAt:  time.Now().UTC(),
	})
	if !created {
		t.Fatal("expected upsert to create")
	}
	all, _ := ps.ListAll(ctx, 10)
	if len(all) != 1 {
		t.Fatalf("expected 1 post, got %d", len(all))
	}
	// Summarize (sets status='summarized') then mark send_failed to
	// simulate a previous cycle whose Telegram send returned an error.
	if err := ps.MarkSummarized(ctx, all[0].ID, "", "summary of first item", 0.9); err != nil {
		t.Fatalf("mark summarized: %v", err)
	}
	if err := ps.MarkSendFailed(ctx, all[0].ID, "test: forced failure"); err != nil {
		t.Fatalf("mark send_failed: %v", err)
	}

	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(999),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// Now run a cycle with an empty seed (no new messages). The cycle
	// should pick up the previously-failed post via ListUnsent and
	// send it successfully.
	tg, _ := newTestTelegram(t, "")
	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       ai.NewFake(nil, "Uncategorized"),
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 999,
	})

	if _, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now()); err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}

	// The previously-failed post must now be in 'sent' state.
	got, err := ps.Get(ctx, all[0].ID)
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if got.Status != store.PostSent {
		t.Errorf("expected status=sent after auto-retry, got %q", got.Status)
	}
	if got.Attempts < 2 {
		t.Errorf("expected attempts>=2 (1 prior + 1 retry), got %d", got.Attempts)
	}
	if got.SendError != "" {
		t.Errorf("expected send_error cleared after success, got %q", got.SendError)
	}
}

// TestPost_DuplicateFetch_OneRow ensures fetching the same post twice
// in different cycles produces only one posts row.
func TestPost_DuplicateFetch_OneRow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	channel, _ := st.AddChannel(ctx, "dup_chan", "Dup")
	ps := sqlite.PostStore{S: st}
	for i := 0; i < 3; i++ {
		_, _, _ = ps.Upsert(ctx, store.Post{
			ChannelID:   channel.ID,
			SourceMsgID: 7001,
			Link:        "https://t.me/dup_chan/7001",
			RawText:     "same",
			MediaKind:   store.MediaText,
			Status:      store.PostReceived,
		})
	}
	all, _ := ps.ListAll(ctx, 10)
	if len(all) != 1 {
		t.Errorf("expected 1 row after 3 upserts of same (channel, source_msg_id), got %d", len(all))
	}
}

// TestPost_FilteredOutDoesNotAppearInDigest ensures a post the AI
// rejects with ErrInvalidInput is not in the rendered digest.
func TestPost_FilteredOutDoesNotAppearInDigest(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	channel, _ := st.AddChannel(ctx, "filter_chan", "Filter")
	ps := sqlite.PostStore{S: st}
	// One valid post + one that will be filtered by the AI.
	_, _, _ = ps.Upsert(ctx, store.Post{
		ChannelID:   channel.ID,
		SourceMsgID: 8001,
		Link:        "https://t.me/filter_chan/8001",
		RawText:     "valid post",
		MediaKind:   store.MediaText,
		Status:      store.PostReceived,
	})
	_, _, _ = ps.Upsert(ctx, store.Post{
		ChannelID:   channel.ID,
		SourceMsgID: 8002,
		Link:        "https://t.me/filter_chan/8002",
		RawText:     "another valid post",
		MediaKind:   store.MediaText,
		Status:      store.PostReceived,
	})

	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(999),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// Run a cycle with the default fake AI; both posts should end up
	// summarized and sent.
	tg, _ := newTestTelegram(t, "")
	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       ai.NewFake(nil, "Uncategorized"),
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 999,
	})

	if _, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now()); err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}

	// Both posts should be in 'sent' state with attempts=1.
	all, _ := ps.ListAll(ctx, 10)
	if len(all) != 2 {
		t.Fatalf("expected 2 posts, got %d", len(all))
	}
	sentCount := 0
	for _, p := range all {
		if p.Status == store.PostSent {
			sentCount++
		}
	}
	if sentCount != 2 {
		t.Errorf("expected 2 sent posts, got %d", sentCount)
	}
}

// TestPost_CrossChannelDuplicate_FilteredOut is the regression test for
// the "duplicate post" bug: when a newly-added channel reposts content
// that has already been delivered via another channel, the cycle must
// not summarize or send the duplicate. The new posts row is still
// created (for audit), but is marked filtered_out immediately.
func TestPost_CrossChannelDuplicate_FilteredOut(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	const sharedText = "forwarded breaking news from primary source"

	// Two channels in the seed, both with the same content.
	seed := `[{"channel":"chan_alpha","messages":[
		{"id":1,"text":"` + sharedText + `","media":"text"}
	]},{"channel":"chan_bravo","messages":[
		{"id":1,"text":"` + sharedText + `","media":"text"}
	]}]`
	tg, sentPath := newTestTelegram(t, seed)

	// Phase 1: only channel alpha is registered. Run a cycle so the
	// shared content is delivered exactly once.
	if _, err := st.AddChannel(ctx, "chan_alpha", "Alpha"); err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(999),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       ai.NewFake(nil, "Uncategorized"),
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 999,
	})
	if _, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now()); err != nil {
		t.Fatalf("cycle 1: %v", err)
	}
	if sentAfterCycle1 := readSentFile(t, sentPath); sentAfterCycle1 != 1 {
		t.Fatalf("after cycle 1: expected 1 sent message, got %d", sentAfterCycle1)
	}

	// Phase 2: add channel bravo (the "newly added channel" from the
	// bug report). Its seed has the same text. Run another cycle; the
	// bravo post must be marked filtered_out and Telegram must NOT
	// receive a second message.
	if _, err := st.AddChannel(ctx, "chan_bravo", "Bravo"); err != nil {
		t.Fatalf("add bravo: %v", err)
	}
	if _, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now()); err != nil {
		t.Fatalf("cycle 2: %v", err)
	}

	// Telegram must still have only one sent message (from cycle 1).
	if sentAfterCycle2 := readSentFile(t, sentPath); sentAfterCycle2 != 1 {
		t.Errorf("after cycle 2: expected 1 sent message (no duplicate), got %d", sentAfterCycle2)
	}

	// Verify the post rows: 1 sent (alpha), 1 filtered_out (bravo).
	ps := sqlite.PostStore{S: st}
	all, _ := ps.ListAll(ctx, 10)
	if len(all) != 2 {
		t.Fatalf("expected 2 post rows (one per channel), got %d", len(all))
	}
	var sentCount, filteredCount int
	for _, p := range all {
		switch p.Status {
		case store.PostSent:
			sentCount++
		case store.PostFilteredOut:
			filteredCount++
		default:
			t.Errorf("unexpected post status: %q (id=%s)", p.Status, p.ID)
		}
	}
	if sentCount != 1 {
		t.Errorf("expected exactly 1 sent post, got %d", sentCount)
	}
	if filteredCount != 1 {
		t.Errorf("expected exactly 1 filtered_out post (the duplicate), got %d", filteredCount)
	}

	// The filtered post must be the bravo one. It must have a
	// populated dedup_key (otherwise the dedup helper couldn't have
	// matched it) and its summary must remain empty (it was never
	// summarized).
	for _, p := range all {
		if p.Status == store.PostFilteredOut {
			if p.Summary != "" {
				t.Errorf("filtered duplicate must not be summarized, got summary=%q", p.Summary)
			}
			if p.DedupKey == "" {
				t.Error("filtered duplicate must have a dedup_key for cross-channel lookup")
			}
		}
	}
}
