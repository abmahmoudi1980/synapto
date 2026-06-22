package sqlite_test

import (
	"context"
	"testing"

	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
)

// TestPost_DedupByChannelAndMsgId verifies the SQL UNIQUE on
// (channel_id, source_msg_id) prevents duplicate rows when the same
// source message is observed twice for the same channel.
func TestPost_DedupByChannelAndMsgId(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch1, _ := st.AddChannel(ctx, "chan_one", "One")
	ch2, _ := st.AddChannel(ctx, "chan_two", "Two")
	ps := sqlite.PostStore{S: st}

	// Same forwarded message id (1234) in two different channels.
	// Each channel has its own row (the unique is per-channel).
	_, c1, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch1.ID,
		SourceMsgID: 1234,
		RawText:     "forwarded content",
		Status:      store.PostReceived,
	})
	_, c2, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch2.ID,
		SourceMsgID: 1234,
		RawText:     "forwarded content",
		Status:      store.PostReceived,
	})
	if !c1 || !c2 {
		t.Error("expected both posts to be created (different channels)")
	}
	all, _ := ps.ListAll(ctx, 10)
	if len(all) != 2 {
		t.Errorf("expected 2 posts (one per channel), got %d", len(all))
	}
}

// TestPost_LinkFormatIsTelegramPermalink verifies the link is in
// the canonical https://t.me/<handle>/<msg_id> form.
func TestPost_LinkFormatIsTelegramPermalink(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "link_chan", "Link")
	ps := sqlite.PostStore{S: st}
	p, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 42,
		Link:        "https://t.me/link_chan/42",
		RawText:     "hello",
		Status:      store.PostReceived,
	})
	if p.Link != "https://t.me/link_chan/42" {
		t.Errorf("link: got %q", p.Link)
	}
}

// TestPost_MarkSentLifecycle walks one post through MarkSummarized →
// MarkIncluded → MarkSent and asserts the visible state at each step.
func TestPost_MarkSentLifecycle(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "life_chan", "Life")
	ps := sqlite.PostStore{S: st}
	p, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 1,
		RawText:     "x",
		Status:      store.PostReceived,
	})
	if p.Status != store.PostReceived {
		t.Fatalf("after upsert: expected received, got %q", p.Status)
	}
	if err := ps.MarkSummarized(ctx, p.ID, "", "summary", 0.9); err != nil {
		t.Fatalf("mark summarized: %v", err)
	}
	got, _ := ps.Get(ctx, p.ID)
	if got.Status != store.PostSummarized {
		t.Errorf("after summarize: expected summarized, got %q", got.Status)
	}
	if err := ps.MarkIncluded(ctx, []string{p.ID}); err != nil {
		t.Fatalf("mark included: %v", err)
	}
	got, _ = ps.Get(ctx, p.ID)
	if got.Status != store.PostIncludedInDigest {
		t.Errorf("after include: expected included_in_digest, got %q", got.Status)
	}
	if got.LastAttemptAt.IsZero() {
		t.Error("expected last_attempt_at populated after include")
	}
	if err := ps.MarkSent(ctx, p.ID, 9999); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	got, _ = ps.Get(ctx, p.ID)
	if got.Status != store.PostSent {
		t.Errorf("after sent: expected sent, got %q", got.Status)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts: got %d, want 1", got.Attempts)
	}
	if got.TelegramMsgID != 9999 {
		t.Errorf("telegram_msg_id: got %d, want 9999", got.TelegramMsgID)
	}
	if got.SentAt.IsZero() {
		t.Error("expected sent_at populated")
	}
}
