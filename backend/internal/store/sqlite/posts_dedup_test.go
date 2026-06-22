package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

// TestPost_GetFirstTerminalByDedupKey_SentReturnsHit verifies that
// when a post with a given dedup_key has been sent, the helper
// returns it (so the cycle can mark duplicates as filtered_out).
func TestPost_GetFirstTerminalByDedupKey_SentReturnsHit(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "term_chan_a", "A")
	ps := sqlite.PostStore{S: st}
	p, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 10,
		RawText:     "shared content",
		Status:      store.PostReceived,
	})
	// Walk to 'sent' to simulate a delivered post.
	_ = ps.MarkSummarized(ctx, p.ID, "", "summary", 0.9)
	_ = ps.MarkIncluded(ctx, []string{p.ID})
	_ = ps.MarkSent(ctx, p.ID, 123)

	got, err := ps.GetFirstTerminalByDedupKey(ctx, p.DedupKey)
	if err != nil {
		t.Fatalf("GetFirstTerminalByDedupKey: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("expected id=%q, got %q", p.ID, got.ID)
	}
	if got.Status != store.PostSent {
		t.Errorf("expected status=sent, got %q", got.Status)
	}
}

// TestPost_GetFirstTerminalByDedupKey_FilteredOutReturnsHit verifies
// that posts the AI rejected (status='filtered_out') also block new
// posts with the same content, since re-running them would just hit
// the same rejection.
func TestPost_GetFirstTerminalByDedupKey_FilteredOutReturnsHit(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "term_chan_b", "B")
	ps := sqlite.PostStore{S: st}
	p, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 11,
		RawText:     "rejected content",
		Status:      store.PostReceived,
	})
	_ = ps.MarkFiltered(ctx, p.ID)

	got, err := ps.GetFirstTerminalByDedupKey(ctx, p.DedupKey)
	if err != nil {
		t.Fatalf("GetFirstTerminalByDedupKey: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("expected id=%q, got %q", p.ID, got.ID)
	}
	if got.Status != store.PostFilteredOut {
		t.Errorf("expected status=filtered_out, got %q", got.Status)
	}
}

// TestPost_GetFirstTerminalByDedupKey_NonTerminalReturnsNotFound
// verifies that a post still in 'received' or 'summarized' is NOT
// returned. We only want to skip a new post when the existing one
// has reached a terminal state; an in-flight one might still fail
// and need a re-run.
func TestPost_GetFirstTerminalByDedupKey_NonTerminalReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "term_chan_c", "C")
	ps := sqlite.PostStore{S: st}
	p, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 12,
		RawText:     "in-flight content",
		Status:      store.PostReceived,
	})

	// Status is 'received' — no terminal hit.
	_, err := ps.GetFirstTerminalByDedupKey(ctx, p.DedupKey)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for non-terminal status, got %v", err)
	}

	// Move to 'summarized' — still not terminal.
	_ = ps.MarkSummarized(ctx, p.ID, "", "summary", 0.9)
	_, err = ps.GetFirstTerminalByDedupKey(ctx, p.DedupKey)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for summarized status, got %v", err)
	}

	// 'send_failed' is also non-terminal (auto-retried); must not block.
	_ = ps.MarkSendFailed(ctx, p.ID, "fake")
	_, err = ps.GetFirstTerminalByDedupKey(ctx, p.DedupKey)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected ErrNotFound for send_failed status, got %v", err)
	}
}

// TestPost_GetFirstTerminalByDedupKey_PicksEarliest verifies that
// when several posts share a dedup_key and are all in terminal
// states, the helper returns the one with the smallest captured_at.
func TestPost_GetFirstTerminalByDedupKey_PicksEarliest(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch1, _ := st.AddChannel(ctx, "term_chan_d1", "D1")
	ch2, _ := st.AddChannel(ctx, "term_chan_d2", "D2")
	ps := sqlite.PostStore{S: st}

	earlier, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch1.ID,
		SourceMsgID: 20,
		RawText:     "duplicate content",
		Status:      store.PostReceived,
		CapturedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	_ = ps.MarkSummarized(ctx, earlier.ID, "", "summary", 0.9)
	_ = ps.MarkIncluded(ctx, []string{earlier.ID})
	_ = ps.MarkSent(ctx, earlier.ID, 1)

	// Same dedup_key, later captured_at, also 'sent'.
	dedup := earlier.DedupKey
	later, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID:   ch2.ID,
		SourceMsgID: 21,
		RawText:     "duplicate content",
		DedupKey:    dedup,
		Status:      store.PostReceived,
		CapturedAt:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	})
	_ = ps.MarkSummarized(ctx, later.ID, "", "summary", 0.9)
	_ = ps.MarkIncluded(ctx, []string{later.ID})
	_ = ps.MarkSent(ctx, later.ID, 2)

	got, err := ps.GetFirstTerminalByDedupKey(ctx, dedup)
	if err != nil {
		t.Fatalf("GetFirstTerminalByDedupKey: %v", err)
	}
	if got.ID != earlier.ID {
		t.Errorf("expected earliest=%q, got %q", earlier.ID, got.ID)
	}
}
