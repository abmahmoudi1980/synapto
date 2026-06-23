package sqlite

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/synapto/assistant/internal/store"
)

func TestStore_SyncAISettingsOverwritesDefaults(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := Open(context.Background(), dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Defaults after first Open.
	pre, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("get pre: %v", err)
	}
	if pre.AIProvider != "fake" || pre.AIModel != "gpt-4o-mini" {
		t.Fatalf("expected hardcoded defaults after first Open, got provider=%q model=%q",
			pre.AIProvider, pre.AIModel)
	}

	// Simulate a boot with a real env: sync the AI fields.
	if err := st.SyncAISettings(context.Background(),
		"anthropic", "minimax-m3",
		"https://opencode.ai/zen/go/v1", "env:AI_API_KEY"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	post, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("get post: %v", err)
	}
	if post.AIProvider != "anthropic" {
		t.Errorf("AIProvider: got %q, want anthropic", post.AIProvider)
	}
	if post.AIModel != "minimax-m3" {
		t.Errorf("AIModel: got %q, want minimax-m3", post.AIModel)
	}
	if post.AIBaseURL != "https://opencode.ai/zen/go/v1" {
		t.Errorf("AIBaseURL: got %q", post.AIBaseURL)
	}
	if post.AIAPIKeyRef != "env:AI_API_KEY" {
		t.Errorf("AIAPIKeyRef: got %q", post.AIAPIKeyRef)
	}

	// SyncAISettings must leave the operator-tunable fields alone.
	if post.DigestIntervalSeconds != pre.DigestIntervalSeconds {
		t.Errorf("SyncAISettings touched digest_interval_seconds: %d -> %d",
			pre.DigestIntervalSeconds, post.DigestIntervalSeconds)
	}
	if post.TelegramSubscriberChat != pre.TelegramSubscriberChat {
		t.Errorf("SyncAISettings touched telegram_subscriber_chat: %d -> %d",
			pre.TelegramSubscriberChat, post.TelegramSubscriberChat)
	}
	if post.UncategorizedLabel != pre.UncategorizedLabel {
		t.Errorf("SyncAISettings touched uncategorized_label: %q -> %q",
			pre.UncategorizedLabel, post.UncategorizedLabel)
	}
}

func TestStore_DeliveryModeDefaultsAndUpdates(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := Open(context.Background(), dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cur, err := st.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cur.DeliveryMode != store.DeliveryPerPost {
		t.Errorf("default delivery_mode = %q, want %q", cur.DeliveryMode, store.DeliveryPerPost)
	}

	// PATCH to bundled.
	bundled := store.DeliveryBundled
	if _, err := st.UpdateSettings(context.Background(), store.SettingsUpdate{DeliveryMode: &bundled}); err != nil {
		t.Fatalf("patch bundled: %v", err)
	}
	cur, _ = st.GetSettings(context.Background())
	if cur.DeliveryMode != store.DeliveryBundled {
		t.Errorf("after patch: delivery_mode = %q, want bundled", cur.DeliveryMode)
	}

	// PATCH to per_post.
	perPost := store.DeliveryPerPost
	if _, err := st.UpdateSettings(context.Background(), store.SettingsUpdate{DeliveryMode: &perPost}); err != nil {
		t.Fatalf("patch per_post: %v", err)
	}
	cur, _ = st.GetSettings(context.Background())
	if cur.DeliveryMode != store.DeliveryPerPost {
		t.Errorf("after patch: delivery_mode = %q, want per_post", cur.DeliveryMode)
	}

	// Invalid value is rejected.
	invalid := store.DeliveryMode("rocket")
	_, err = st.UpdateSettings(context.Background(), store.SettingsUpdate{DeliveryMode: &invalid})
	if err != store.ErrInvalidDeliveryMode {
		t.Errorf("invalid value: got err = %v, want ErrInvalidDeliveryMode", err)
	}
}
