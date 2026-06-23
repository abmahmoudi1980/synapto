package digest_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/digest"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"
)

// recordingTelegram captures every SendMessage call so the test can
// assert order, content, and timing.
type recordingTelegram struct {
	messages []sentMessage
	chatID   int64
	failNext bool
	failAll  bool
	failErr  error
}

type sentMessage struct {
	chatID    int64
	text      string
	parseMode string
	at        time.Time
}

func (r *recordingTelegram) GetChat(ctx context.Context, handle string) (telegram.ChannelInfo, error) {
	return telegram.ChannelInfo{Username: handle, Title: handle}, nil
}

func (r *recordingTelegram) FetchNewPosts(ctx context.Context, handle string, sinceMsgID int64) ([]telegram.Post, error) {
	// No-op: the cycle's fetch step is exercised through a different test.
	return nil, nil
}

func (r *recordingTelegram) SendMessage(ctx context.Context, chatID int64, text string, parseMode string) (telegram.SendResult, error) {
	r.messages = append(r.messages, sentMessage{chatID: chatID, text: text, parseMode: parseMode, at: time.Now()})
	if r.failAll {
		return telegram.SendResult{}, r.failErr
	}
	if r.failNext {
		r.failNext = false
		return telegram.SendResult{}, r.failErr
	}
	return telegram.SendResult{MessageID: int64(len(r.messages)), OK: true}, nil
}

func (r *recordingTelegram) Close() error { return nil }

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(context.Background(), filepath.Join(dir, "test.db"), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func newCycleDeps(t *testing.T) (digest.CycleDeps, *recordingTelegram, *sqlite.Store) {
	t.Helper()
	st := openTestStore(t)
	cs := sqlite.ChannelStore{S: st}
	cat := sqlite.CategoryStore{S: st}
	set := sqlite.SettingsStore{S: st}
	cyc := sqlite.CycleStore{S: st}
	dig := sqlite.DigestStore{S: st}
	hl := sqlite.HealthStore{S: st}
	ps := sqlite.PostStore{S: st}
	if err := cat.EnsureDefaults(context.Background(), []string{"Politics", "Technology", "Business", "Sports", "World", "Other"}); err != nil {
		t.Fatalf("ensure defaults: %v", err)
	}
	// Force the delivery mode to per_post so the branch under test is
	// taken. SyncAISettings is not relevant here; we update directly.
	if _, err := set.Update(context.Background(), store.SettingsUpdate{
		DeliveryMode: func() *store.DeliveryMode { m := store.DeliveryPerPost; return &m }(),
	}); err != nil {
		t.Fatalf("set per_post: %v", err)
	}
	tg := &recordingTelegram{chatID: 999}
	deps := digest.CycleDeps{
		Log:              slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Telegram:         tg,
		Summarizer:       ai.NewFake(nil, "Uncategorized"),
		Channels:         cs,
		Categories:       cat,
		Settings:         set,
		Cycles:           cyc,
		Digests:          dig,
		Health:           hl,
		Posts:            ps,
		SubscriberChatID: 999,
	}
	return deps, tg, st
}

// seedChannelAndPosts adds a channel and three posts so the cycle has
// something to send.
func seedChannelAndPosts(t *testing.T, st *sqlite.Store) (string, []string) {
	t.Helper()
	cs := sqlite.ChannelStore{S: st}
	ps := sqlite.PostStore{S: st}

	ch, err := cs.Add(context.Background(), "test_chan", "Test Channel")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}
	// Seed three posts directly (bypassing fetch) with status=summarized
	// so the per-post send step picks them up.
	postIDs := []string{}
	for i, msgID := range []int64{1001, 1002, 1003} {
		p, _, err := ps.Upsert(context.Background(), store.Post{
			ChannelID:   ch.ID,
			SourceMsgID: msgID,
			DedupKey:    "text:" + string(rune('a'+i)),
			Link:        "https://t.me/test_chan/" + itoa64(msgID),
			RawText:     "raw text " + itoa64(msgID),
			MediaKind:   store.MediaText,
			Status:      store.PostSummarized,
			Summary:     "summary " + itoa64(msgID),
			CategoryID:  "", // uncategorized
		})
		if err != nil {
			t.Fatalf("upsert post: %v", err)
		}
		postIDs = append(postIDs, p.ID)
	}
	return ch.ID, postIDs
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestRunPerPost_SendsEachPostIndividually(t *testing.T) {
	deps, tg, st := newCycleDeps(t)
	cycle := digest.NewCycle(deps)

	_, _ = seedChannelAndPosts(t, st)

	now := time.Now().UTC()
	cycleID, err := cycle.Run(context.Background(), now.Add(-1*time.Minute), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cycleID == "" {
		t.Fatal("expected non-empty cycle id")
	}

	if len(tg.messages) != 3 {
		t.Fatalf("expected 3 telegram sends, got %d", len(tg.messages))
	}
	for i, m := range tg.messages {
		if m.chatID != 999 {
			t.Errorf("message %d: chatID = %d, want 999", i, m.chatID)
		}
		if m.parseMode != "MarkdownV2" {
			t.Errorf("message %d: parseMode = %q, want MarkdownV2", i, m.parseMode)
		}
	}

	// All three posts should be marked 'sent'.
	ps := sqlite.PostStore{S: st}
	for _, id := range []string{} {
		_ = id
	}
	posts, err := ps.ListByStatus(context.Background(), store.PostSent, 100)
	if err != nil {
		t.Fatalf("ListByStatus sent: %v", err)
	}
	if len(posts) != 3 {
		t.Errorf("expected 3 sent posts, got %d", len(posts))
	}

	// No digests row should be created in per-post mode.
	var digestCount int
	if err := st.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM digests`).Scan(&digestCount); err != nil {
		t.Fatalf("count digests: %v", err)
	}
	if digestCount != 0 {
		t.Errorf("expected 0 digest rows in per-post mode, got %d", digestCount)
	}
}

func TestRunPerPost_ThrottleBetweenSends(t *testing.T) {
	deps, tg, st := newCycleDeps(t)
	cycle := digest.NewCycle(deps)

	_, _ = seedChannelAndPosts(t, st)

	now := time.Now().UTC()
	_, err := cycle.Run(context.Background(), now.Add(-1*time.Minute), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(tg.messages) < 2 {
		t.Fatalf("expected ≥ 2 sends, got %d", len(tg.messages))
	}
	gap := tg.messages[1].at.Sub(tg.messages[0].at)
	// Allow 100ms slack below the 1500ms target (the runtime uses
	// time.After which is a soft timer).
	if gap < 1400*time.Millisecond {
		t.Errorf("gap between sends = %v, want ≥ 1.4s", gap)
	}
}

func TestRunPerPost_NoRecipientMarksAllFailed(t *testing.T) {
	deps, tg, st := newCycleDeps(t)
	// Override the chat id to 0 (no recipient).
	set := sqlite.SettingsStore{S: st}
	if _, err := set.Update(context.Background(), store.SettingsUpdate{
		TelegramSubscriberChat: func() *int64 { z := int64(0); return &z }(),
	}); err != nil {
		t.Fatalf("set chat 0: %v", err)
	}
	deps.SubscriberChatID = 0
	cycle := digest.NewCycle(deps)

	_, _ = seedChannelAndPosts(t, st)

	now := time.Now().UTC()
	_, err := cycle.Run(context.Background(), now.Add(-1*time.Minute), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(tg.messages) != 0 {
		t.Errorf("expected 0 sends with no recipient, got %d", len(tg.messages))
	}
	ps := sqlite.PostStore{S: st}
	failed, err := ps.ListByStatus(context.Background(), store.PostSendFailed, 100)
	if err != nil {
		t.Fatalf("ListByStatus send_failed: %v", err)
	}
	if len(failed) != 3 {
		t.Errorf("expected 3 send_failed posts, got %d", len(failed))
	}
	for _, p := range failed {
		if p.SendError == "" {
			t.Errorf("post %s: expected non-empty send_error, got empty", p.ID)
		}
		if p.SendError != "no_recipient: TELEGRAM_SUBSCRIBER_CHAT not configured" {
			t.Errorf("post %s: unexpected send_error %q", p.ID, p.SendError)
		}
	}
}

func TestRunPerPost_SendFailureRecordsActualError(t *testing.T) {
	deps, tg, st := newCycleDeps(t)
	// Make every send fail (and the plain-text retry also fail) with a
	// recognizable non-status error string. The retry happens within
	// the per-post send loop, so failAll must be true to bypass the
	// MarkdownV2 -> plain-text retry.
	tg.failAll = true
	tg.failErr = telegram.ErrUnavailable
	cycle := digest.NewCycle(deps)

	_, _ = seedChannelAndPosts(t, st)

	now := time.Now().UTC()
	_, err := cycle.Run(context.Background(), now.Add(-1*time.Minute), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	ps := sqlite.PostStore{S: st}
	failed, err := ps.ListByStatus(context.Background(), store.PostSendFailed, 100)
	if err != nil {
		t.Fatalf("ListByStatus send_failed: %v", err)
	}
	if len(failed) != 3 {
		t.Fatalf("expected 3 send_failed posts, got %d", len(failed))
	}
	// The actual error should be the telegram client error, NOT a
	// status label like "send_status=failed".
	for _, p := range failed {
		if p.SendError == "" {
			t.Errorf("post %s: send_error is empty; expected the actual telegram error", p.ID)
		}
		if p.SendError == "send_status=failed" {
			t.Errorf("post %s: send_error is a status label (%q), not the actual error; the bundled-mode bug has leaked into per-post", p.ID, p.SendError)
		}
		if p.SendError != telegram.ErrUnavailable.Error() {
			t.Errorf("post %s: send_error = %q, want %q", p.ID, p.SendError, telegram.ErrUnavailable.Error())
		}
	}
}

// TestRunPerPost_MarksDeadAfterMaxAttempts verifies that a post
// which has failed maxSendAttempts - 1 times is moved to 'dead' on
// the next failure (per-post mode only; the bundled mode never
// marks posts dead because the per-post failure attribution is
// unreliable there). The test pre-sets attempts=19 via direct SQL
// so we don't have to run 20 cycles.
func TestRunPerPost_MarksDeadAfterMaxAttempts(t *testing.T) {
	deps, tg, st := newCycleDeps(t)
	tg.failAll = true
	tg.failErr = telegram.ErrUnavailable
	cycle := digest.NewCycle(deps)

	chID, postIDs := seedChannelAndPosts(t, st)
	ps := sqlite.PostStore{S: st}

	// Pre-set attempts to maxSendAttempts - 1 for the first post
	// only, so we can prove the threshold is per-post and the
	// other two stay in send_failed.
	if _, err := st.DB().ExecContext(context.Background(),
		`UPDATE posts SET attempts = ? WHERE id = ?`,
		digest.MaxSendAttempts-1, postIDs[0]); err != nil {
		t.Fatalf("set attempts: %v", err)
	}
	_ = chID

	now := time.Now().UTC()
	_, err := cycle.Run(context.Background(), now.Add(-1*time.Minute), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// First post: should be 'dead'.
	dead, err := ps.ListByStatus(context.Background(), store.PostDead, 100)
	if err != nil {
		t.Fatalf("ListByStatus dead: %v", err)
	}
	if len(dead) != 1 {
		t.Fatalf("expected 1 dead post, got %d", len(dead))
	}
	if dead[0].ID != postIDs[0] {
		t.Errorf("dead post id = %s, want %s", dead[0].ID, postIDs[0])
	}
	if dead[0].Attempts != digest.MaxSendAttempts {
		t.Errorf("dead post attempts = %d, want %d", dead[0].Attempts, digest.MaxSendAttempts)
	}

	// Other two: still send_failed (attempts=1).
	failed, err := ps.ListByStatus(context.Background(), store.PostSendFailed, 100)
	if err != nil {
		t.Fatalf("ListByStatus send_failed: %v", err)
	}
	if len(failed) != 2 {
		t.Errorf("expected 2 send_failed posts, got %d", len(failed))
	}
	for _, p := range failed {
		if p.Attempts != 1 {
			t.Errorf("post %s attempts = %d, want 1", p.ID, p.Attempts)
		}
	}

	// 'dead' must not be re-tried on the next cycle. Run again
	// (with a non-failing client this time so we can count
	// successful sends cleanly) and confirm the dead post is
	// absent: we should see sends for the 2 still-send_failed
	// posts, not the 1 dead one.
	tg.failAll = false
	tg.failNext = false
	tg.messages = nil
	now2 := time.Now().UTC().Add(2 * time.Minute)
	_, err = cycle.Run(context.Background(), now2.Add(-1*time.Minute), now2)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	// With the fake non-failing client, each post generates one
	// SendMessage call (the MarkdownV2 attempt). 2 live posts
	// → 2 messages.
	if len(tg.messages) != 2 {
		t.Errorf("expected 2 sends on second run, got %d", len(tg.messages))
	}
	// The dead post's handle (test_chan, source_msg_id 1001)
	// must NOT appear in any of the rendered texts.
	deadLink := "https://t.me/test_chan/1001"
	for _, m := range tg.messages {
		if m.text == "" {
			continue
		}
		if len(deadLink) > 0 && contains(m.text, deadLink) {
			t.Errorf("second run re-tried the dead post: %q", m.text)
		}
	}
}

// contains is a tiny stdlib-free substring helper used in the
// per-post test above; avoids pulling strings into the test file's
// import set for one call site.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
