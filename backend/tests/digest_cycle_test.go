package digest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/digest"
	"github.com/synapto/assistant/internal/logging"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"
)

// newTestStore opens a SQLite store in a temp directory for testing.
func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := sqlite.Open(context.Background(), dbPath, logging.New("debug"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newTestTelegram creates a fake Telegram client with a seed file and a
// JSONL output path in the test's temp directory.
func newTestTelegram(t *testing.T, seedJSON string) (*telegram.Fake, string) {
	t.Helper()
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "seed.json")
	if seedJSON != "" {
		if err := os.WriteFile(seedPath, []byte(seedJSON), 0o644); err != nil {
			t.Fatalf("write seed: %v", err)
		}
	}
	outPath := filepath.Join(dir, "sent.jsonl")
	tg, err := telegram.NewFake(seedPath, outPath)
	if err != nil {
		t.Fatalf("new fake telegram: %v", err)
	}
	return tg, outPath
}

// readSentFile reads the JSONL output and returns the number of lines.
func readSentFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read sent file: %v", err)
	}
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		count++
	}
	return count
}

func TestCycle_EndToEnd_OneChannelThreeMessages(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Seed: one channel with three messages in JSON format.
	seed := `[{"channel":"sample_news","messages":[
		{"id":1001,"text":"Telegram rolls out scheduled messages in channels","media":"text"},
		{"id":1002,"text":"EU parliament passes the AI Liability Directive","media":"text"},
		{"id":1003,"text":"A new open-source LLM beats GPT-4 on a public benchmark","media":"text"}
	]}]`
	tg, sentPath := newTestTelegram(t, seed)

	// Add the channel to the store so the cycle can find it.
	ch, err := st.AddChannel(ctx, "sample_news", "Sample News")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}
	_ = ch

	// Fake summarizer: simple rule-based.
	fakeAI := ai.NewFake(nil, "Uncategorized")

	// Settings: get the default settings (interval = 600s, subscriber chat = 0).
	// We set a non-zero subscriber chat so the send path is exercised.
	_, err = st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(123456789),
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       fakeAI,
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 123456789,
	})

	windowStart := time.Now().UTC().Add(-10 * time.Minute)
	windowEnd := time.Now().UTC()

	cycleID, err := cycle.Run(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}
	if cycleID == "" {
		t.Fatal("cycleID should not be empty")
	}

	// Verify: exactly one cycle row.
	cycles, err := st.ListCycles(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list cycles: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle row, got %d", len(cycles))
	}
	// The cycle should be succeeded or degraded (not failed, not skipped).
	if cycles[0].Status != store.CycleSucceeded && cycles[0].Status != store.CycleDegraded {
		t.Errorf("expected succeeded or degraded, got %s", cycles[0].Status)
	}
	if cycles[0].InputMsgCount != 3 {
		t.Errorf("expected input_msg_count=3, got %d", cycles[0].InputMsgCount)
	}

	// Verify: exactly one digest row.
	digestRow, err := st.GetDigestByCycle(ctx, cycleID)
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if digestRow.RenderedText == "" {
		t.Error("digest rendered text should not be empty")
	}
	if digestRow.SendStatus != store.SendOK {
		t.Errorf("expected send_status=ok, got %s", digestRow.SendStatus)
	}

	// Verify: exactly 3 digest items.
	items, err := st.ListDigestItemsByCycle(ctx, cycleID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 digest items, got %d", len(items))
	}

	// Verify: one message was sent to Telegram.
	sentCount := readSentFile(t, sentPath)
	if sentCount != 1 {
		t.Errorf("expected 1 sent message, got %d", sentCount)
	}

	// Verify: cursor advanced on the channel.
	updated, err := st.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}
	if updated.LastSeenMsgID != 1003 {
		t.Errorf("expected cursor advanced to 1003, got %d", updated.LastSeenMsgID)
	}
}

func TestCycle_NoNewItems_SkipsAndDoesNotSend(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Seed: one channel with NO messages.
	seed := `[{"channel":"empty_chan","messages":[]}]`
	tg, sentPath := newTestTelegram(t, seed)

	_, err := st.AddChannel(ctx, "empty_chan", "Empty")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}

	_, err = st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(123456789),
	})
	if err != nil {
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
		SubscriberChatID: 123456789,
	})

	cycleID, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}

	// Verify: cycle status is skipped_no_items.
	c, err := st.GetCycle(ctx, cycleID)
	if err != nil {
		t.Fatalf("get cycle: %v", err)
	}
	if c.Status != store.CycleSkippedNoItems {
		t.Errorf("expected skipped_no_items, got %s", c.Status)
	}

	// Verify: no digest row exists.
	_, err = st.GetDigestByCycle(ctx, cycleID)
	if err == nil {
		t.Error("expected no digest row for skipped cycle")
	}

	// Verify: nothing was sent.
	if n := readSentFile(t, sentPath); n != 0 {
		t.Errorf("expected 0 sent messages, got %d", n)
	}
}

// TestCycle_NoSubscriberChat_MarksSendFailed covers the case where the
// cycle has items to send but no recipient is configured (DB settings
// subscriber chat is 0 and the env fallback is 0). The send loop is
// skipped; the digest must NOT be marked as send_status=ok — that
// would silently hide the misconfiguration. It must be marked failed
// so the operator notices in the history.
func TestCycle_NoSubscriberChat_MarksSendFailed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	seed := `[{"channel":"no_recipient_chan","messages":[
		{"id":1,"text":"hello world","media":"text"}
	]}]`
	tg, sentPath := newTestTelegram(t, seed)

	if _, err := st.AddChannel(ctx, "no_recipient_chan", "NoRecipient"); err != nil {
		t.Fatalf("add channel: %v", err)
	}

	// Deliberately do NOT set telegram_subscriber_chat in settings.
	// SubscriberChatID env fallback is 0.

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
		SubscriberChatID: 0, // no recipient
	})

	cycleID, err := cycle.Run(ctx, time.Now().Add(-10*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("cycle.Run: %v", err)
	}

	// Verify: cycle status is failed (not succeeded, not degraded).
	c, err := st.GetCycle(ctx, cycleID)
	if err != nil {
		t.Fatalf("get cycle: %v", err)
	}
	if c.Status != store.CycleFailed {
		t.Errorf("expected failed (no recipient), got %s", c.Status)
	}

	// Verify: digest exists and is marked failed, not ok.
	d, err := st.GetDigestByCycle(ctx, cycleID)
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if d.SendStatus != store.SendFailed {
		t.Errorf("expected digest send_status=failed, got %s", d.SendStatus)
	}
	if d.TelegramMsgID != 0 {
		t.Errorf("expected telegram_msg_id=0 (no send), got %d", d.TelegramMsgID)
	}

	// Verify: nothing was actually sent on the wire.
	if n := readSentFile(t, sentPath); n != 0 {
		t.Errorf("expected 0 sent messages, got %d", n)
	}

	// Verify: the operator-visible event records the no-recipient reason.
	events, err := st.RecentEvents(ctx, 50)
	if err != nil {
		t.Fatalf("recent events: %v", err)
	}
	foundNoRecipient := false
	for _, e := range events {
		if e.Kind == "telegram.send.no_recipient" {
			foundNoRecipient = true
			break
		}
	}
	if !foundNoRecipient {
		t.Errorf("expected an op_event with kind=telegram.send.no_recipient, got %d events", len(events))
	}
}

func TestCycle_RestartSafety_NoDoubleDelivery(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	seed := `[{"channel":"restart_chan","messages":[
		{"id":2001,"text":"First message after restart","media":"text"}
	]}]`
	tg, sentPath := newTestTelegram(t, seed)

	ch, err := st.AddChannel(ctx, "restart_chan", "Restart")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}
	_ = ch

	_, err = st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(123456789),
	})
	if err != nil {
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
		SubscriberChatID: 123456789,
	})

	// Run the cycle once.
	windowStart := time.Now().UTC().Add(-10 * time.Minute)
	windowEnd := time.Now().UTC()
	_, err = cycle.Run(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("first cycle.Run: %v", err)
	}

	// Run the SAME window again (simulating a restart mid-window).
	_, err = cycle.Run(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("second cycle.Run: %v", err)
	}

	// Verify: exactly 1 sent message (no double-deliver), because the second
	// cycle finds no new items (cursor already advanced past 2001).
	sentCount := readSentFile(t, sentPath)
	if sentCount != 1 {
		t.Errorf("expected 1 sent message (no double-deliver), got %d", sentCount)
	}

	// Verify: 2 cycle rows (both ran), but the second should be skipped.
	cycles, err := st.ListCycles(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list cycles: %v", err)
	}
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycle rows, got %d", len(cycles))
	}
}

// int64Ptr is a helper for settings updates.
func int64Ptr(v int64) *int64 { return &v }

// intPtr is a helper for settings updates.
func intPtr(v int) *int { return &v }

// TestCycle_RenamedCategoryAppearsInDigest covers SC-006: after renaming a
// default category, the next cycle groups items under the new heading.
// We seed the fake AI to return "Politics" for the test message, then
// rename "Politics" to "Policy" and re-run; the rendered text must show
// the new heading (or — if the rename isn't honored — fall back to
// uncategorized_label; we assert on the policy path).
func TestCycle_RenamedCategoryAppearsInDigest(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	seed := `[{"channel":"policy_news","messages":[
		{"id":3001,"text":"Government announces new regulation","media":"text"}
	]}]`
	tg, sentPath := newTestTelegram(t, seed)

	if _, err := st.AddChannel(ctx, "policy_news", "Policy News"); err != nil {
		t.Fatalf("add channel: %v", err)
	}
	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		TelegramSubscriberChat: int64Ptr(123456789),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// Fake AI: route the test message to "Politics" deterministically.
	fakeAIPolitics := ai.NewFake([]ai.FakeRule{
		{
			Match: func(in ai.Input) bool {
				return in.ChannelHandle == "policy_news"
			},
			Output: ai.Output{
				Summary:    "Government announces new regulation",
				Category:   "Politics",
				Confidence: 0.9,
			},
		},
	}, "Uncategorized")

	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg,
		Summarizer:       fakeAIPolitics,
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 123456789,
	})

	// Step 1: run a cycle, confirm "Politics" heading.
	ws1 := time.Now().UTC().Add(-20 * time.Minute)
	we1 := time.Now().UTC().Add(-10 * time.Minute)
	cycleID1, err := cycle.Run(ctx, ws1, we1)
	if err != nil {
		t.Fatalf("first cycle.Run: %v", err)
	}
	d1, err := st.GetDigestByCycle(ctx, cycleID1)
	if err != nil {
		t.Fatalf("get digest 1: %v", err)
	}
	if !strings.Contains(d1.RenderedText, "# Politics") {
		t.Errorf("expected '# Politics' in first digest, got:\n%s", d1.RenderedText)
	}

	// Step 2: rename "Politics" to "Policy" via the CategoryRepo.
	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	var politicsID string
	for _, c := range cats {
		if c.Name == "Politics" {
			politicsID = c.ID
		}
	}
	if politicsID == "" {
		t.Fatal("Politics default category not found")
	}
	if _, err := st.RenameCategory(ctx, politicsID, "Policy"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Step 3: re-seed a fresh message and run a second cycle. The fake AI
	// for the second cycle returns "Policy" (the renamed category).
	seed2 := `[{"channel":"policy_news","messages":[
		{"id":3002,"text":"Cabinet reshuffle announced","media":"text"}
	]}]`
	tg2, sentPath2 := newTestTelegram(t, seed2)
	fakeAIPolicy := ai.NewFake([]ai.FakeRule{
		{
			Match: func(in ai.Input) bool {
				return in.ChannelHandle == "policy_news"
			},
			Output: ai.Output{
				Summary:    "Cabinet reshuffle announced",
				Category:   "Policy",
				Confidence: 0.9,
			},
		},
	}, "Uncategorized")
	cycle2 := digest.NewCycle(digest.CycleDeps{
		Log:              logging.New("debug"),
		Telegram:         tg2,
		Summarizer:       fakeAIPolicy,
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: 123456789,
	})

	// Reset the cursor so the new message is treated as new.
	if _, err := st.UpdateSettings(ctx, store.SettingsUpdate{
		DigestIntervalSeconds: intPtr(600),
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	// The new message is id=3002; the previous cycle advanced the cursor
	// to 3001. To force the second cycle to pick up 3002, we need a fresh
	// telegram client; the seed only has one channel and one message. The
	// fake fetcher will read whatever is in the new seed, returning the
	// new message with id=3002 — and the channel's last_seen_msg_id is
	// 3001 from the first cycle, so 3002 > 3001, the cycle picks it up.
	_ = sentPath
	ws2 := time.Now().UTC().Add(-5 * time.Minute)
	we2 := time.Now().UTC()
	cycleID2, err := cycle2.Run(ctx, ws2, we2)
	if err != nil {
		t.Fatalf("second cycle.Run: %v", err)
	}
	d2, err := st.GetDigestByCycle(ctx, cycleID2)
	if err != nil {
		t.Fatalf("get digest 2: %v", err)
	}
	if !strings.Contains(d2.RenderedText, "# Policy") {
		t.Errorf("expected '# Policy' in second digest after rename, got:\n%s", d2.RenderedText)
	}
	if strings.Contains(d2.RenderedText, "# Politics") {
		t.Errorf("did not expect '# Politics' in second digest after rename, got:\n%s", d2.RenderedText)
	}
	if readSentFile(t, sentPath2) != 1 {
		t.Errorf("expected 1 sent message in second cycle")
	}
}
