package sqlite

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
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
