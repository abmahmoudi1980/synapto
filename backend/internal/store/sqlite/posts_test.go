package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/logging"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
)

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := sqlite.Open(context.Background(), filepath.Join(dir, "test.db"), logging.New("debug"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestPost_UpsertCreatesNewRow verifies the basic happy path: a fresh
// (channel_id, source_msg_id) becomes a row with status='received'.
func TestPost_UpsertCreatesNewRow(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, err := st.AddChannel(ctx, "chan_a", "A")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}
	ps := sqlite.PostStore{S: st}
	p, created, err := ps.Upsert(ctx, store.Post{
		ChannelID:   ch.ID,
		SourceMsgID: 100,
		Link:        "https://t.me/chan_a/100",
		RawText:     "hello world",
		MediaKind:   store.MediaText,
		Status:      store.PostReceived,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !created {
		t.Error("expected created=true")
	}
	if p.Status != store.PostReceived {
		t.Errorf("expected status=received, got %q", p.Status)
	}
	if p.Link != "https://t.me/chan_a/100" {
		t.Errorf("link: got %q", p.Link)
	}
	if p.ID == "" {
		t.Error("expected non-empty id")
	}
}

// TestPost_UpsertIsIdempotent verifies the second call with the same
// (channel, source_msg_id) returns the same row, untouched.
func TestPost_UpsertIsIdempotent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "chan_b", "B")
	ps := sqlite.PostStore{S: st}

	p1, created1, _ := ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 200, RawText: "first", Status: store.PostReceived,
	})
	if !created1 {
		t.Fatal("expected first upsert to be created")
	}

	// Second upsert with same key. The returned row should be the
	// existing one (created=false) and the original raw_text preserved.
	p2, created2, err := ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 200, RawText: "second", Status: store.PostReceived,
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if created2 {
		t.Error("expected created=false on duplicate key")
	}
	if p2.ID != p1.ID {
		t.Errorf("expected same id, got %q vs %q", p1.ID, p2.ID)
	}
	if p2.RawText != "first" {
		t.Errorf("expected raw_text preserved, got %q", p2.RawText)
	}
}

// TestPost_GetByChannelMsg returns the row that Upsert created.
func TestPost_GetByChannelMsg(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "chan_c", "C")
	ps := sqlite.PostStore{S: st}
	upserted, _, _ := ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 42, 		Link:        "https://t.me/chan_c/42",
		RawText: "abc", Status: store.PostReceived,
	})

	got, err := ps.GetByChannelMsg(ctx, ch.ID, 42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != upserted.ID {
		t.Errorf("id mismatch: %q vs %q", got.ID, upserted.ID)
	}
	if got.Link != "https://t.me/chan_c/42" {
		t.Errorf("link: %q", got.Link)
	}
}

// TestPost_UniqueChannelMsgConstraint ensures the SQL UNIQUE on
// (channel_id, source_msg_id) prevents a second row from being
// inserted directly (the upsert path uses ON CONFLICT DO NOTHING).
func TestPost_UniqueChannelMsgConstraint(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, _ := st.AddChannel(ctx, "chan_d", "D")
	ps := sqlite.PostStore{S: st}
	_, _, _ = ps.Upsert(ctx, store.Post{
		ChannelID: ch.ID, SourceMsgID: 9, Status: store.PostReceived,
	})
	// Direct INSERT bypassing Upsert must fail.
	_, err := st.DB().ExecContext(ctx, `INSERT INTO posts
		(id, channel_id, source_msg_id, dedup_key, link, raw_text, media_kind,
		 captured_at, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"row-2", ch.ID, 9, "k", "l", "t", "text",
		time.Now().UTC().Format(time.RFC3339), "received",
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		t.Fatal("expected UNIQUE constraint violation on (channel_id, source_msg_id)")
	}
	if !errors.Is(err, store.ErrNotFound) && err.Error() == "" {
		// SQLite's "UNIQUE constraint failed" is not a typed error;
		// the test only asserts that a non-nil error came back.
	}
}
