// Command assistant is the single-binary entrypoint for the Synapto
// Telegram News Digest Assistant. It wires configuration, logging, the
// SQLite store, the AI summarizer (real or fake), the Telegram client
// (real or fake), the admin HTTP server, and the digest scheduler.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/synapto/assistant/internal/adminapi"
	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/config"
	"github.com/synapto/assistant/internal/digest"
	"github.com/synapto/assistant/internal/logging"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"

	openai "github.com/sashabaranov/go-openai"
)

// version is overridable at link time with -ldflags "-X main.version=...".
var version = "0.1.0-dev"

// defaultCategoryNames is the shipped category set, inserted on first boot
// (and re-asserted on every subsequent boot, no-op if already present).
var defaultCategoryNames = []string{
	"Politics",
	"Technology",
	"Business",
	"Sports",
	"World",
	"Other",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "assistant: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	log := logging.New(cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("assistant starting",
		"version", version,
		"ai_provider", cfg.AIProvider,
		"digest_interval", cfg.DigestInterval,
		"admin_addr", cfg.AdminListenAddr,
		"db_path", cfg.DBPath,
	)

	// Storage.
	st, err := sqlite.Open(ctx, cfg.DBPath, log)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close() //nolint:errcheck,govet // shutdown path

	// Ensure the default category set is present. Idempotent; a no-op on
	// every boot after the first.
	if err := (sqlite.CategoryStore{S: st}).EnsureDefaults(ctx, defaultCategoryNames); err != nil {
		return fmt.Errorf("ensure default categories: %w", err)
	}
	// Sync the AI-related settings with the live env. The settings row
	// is seeded with hardcoded defaults; the env file is the source of
	// truth for provider / model / base URL / API key ref, so refresh
	// those on every boot. Operator-tunable fields (digest interval,
	// subscriber chat, uncategorized label) are left as-is.
	if err := st.SyncAISettings(ctx, cfg.AIProvider, cfg.AIModel, cfg.AIBaseURL, "env:AI_API_KEY"); err != nil {
		return fmt.Errorf("sync ai settings: %w", err)
	}

	// AI summarizer. Used by the digest cycle. Category names are
	// pulled from the seeded defaults so the system prompt lists the
	// real set.
	categories := make([]string, 0, len(defaultCategoryNames))
	categories = append(categories, defaultCategoryNames...)
	summarizer := newSummarizer(cfg, log, categories)

	// Settings store handle. Used by the Telegram client to persist
	// auto-discovered subscriber chat ids.
	settingsStore := sqlite.SettingsStore{S: st}

	// Telegram client.
	tgClient, err := newTelegramClient(cfg, log, settingsStore)
	if err != nil {
		return fmt.Errorf("telegram client: %w", err)
	}
	defer tgClient.Close() //nolint:errcheck // shutdown path

	// Scheduler state probe (live-read by the health endpoint).
	var schedulerState atomic.Value
	schedulerState.Store("idle")
	schedulerStateFn := func() string {
		if v, ok := schedulerState.Load().(string); ok {
			return v
		}
		return "idle"
	}

	// Admin HTTP server. Use thin adapter wrappers so a single *Store can
	// satisfy several repository interfaces that share method names.
	srv := adminapi.New(adminapi.Deps{
		Log:               log,
		Version:           version,
		Channels:          sqlite.ChannelStore{S: st},
		Categories:        sqlite.CategoryStore{S: st},
		Settings:          sqlite.SettingsStore{S: st},
		Cycles:            sqlite.CycleStore{S: st},
		Digests:           sqlite.DigestStore{S: st},
		Health:            sqlite.HealthStore{S: st},
		Posts:             sqlite.PostStore{S: st},
		Telegram:          tgClient,
		SchedulerState:    schedulerStateFn,
		TelegramReachable: func() bool { return true },
		// The AI summarizer is reachable when the provider is the
		// always-on fake, or when a real provider is configured with
		// a non-empty API key. This is a static configuration check;
		// the test-ai endpoint issues a live probe on demand.
		AIReachable:       func() bool { return cfg.AIProvider == "fake" || cfg.APIKey != "" },
		StartedAt:         time.Now(),
		AdminPassword:     cfg.AdminPassword,
		Dev:               cfg.Dev,
	})

	// Digest cycle + scheduler. The cycle wires all repositories together;
	// the scheduler runs it on a fixed interval with restart safety.
	cycle := digest.NewCycle(digest.CycleDeps{
		Log:              log,
		Telegram:         tgClient,
		Summarizer:       summarizer,
		Channels:         sqlite.ChannelStore{S: st},
		Categories:       sqlite.CategoryStore{S: st},
		Settings:         sqlite.SettingsStore{S: st},
		Cycles:           sqlite.CycleStore{S: st},
		Digests:          sqlite.DigestStore{S: st},
		Health:           sqlite.HealthStore{S: st},
		Posts:            sqlite.PostStore{S: st},
		SubscriberChatID: cfg.TelegramSubscriberChat,
	})
	scheduler := digest.NewScheduler(cycle, cfg.DigestInterval, sqlite.CycleStore{S: st}, log)

	// HTTP server goroutine.
	errCh := make(chan error, 3)
	go func() {
		log.Info("admin http listening", "addr", cfg.AdminListenAddr)
		if err := srv.Serve(cfg.AdminListenAddr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("admin http: %w", err)
		}
	}()

	// Scheduler goroutine. Runs until ctx is canceled.
	go func() {
		if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("scheduler: %w", err)
		}
	}()

	// Settings watcher: polls the settings row and pushes interval changes
	// into the scheduler live. This satisfies SC-005/010 — the operator
	// can change the digest interval from the admin panel without
	// restarting the service.
	go watchSettings(ctx, settingsStore, sqlite.HealthStore{S: st}, scheduler, log, cfg.DigestInterval)

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Info("signal received, shutting down", "signal", sig.String())
	case err := <-errCh:
		log.Error("fatal error", "err", err)
		return err
	}

	// Graceful shutdown sequence (T061):
	//   1. Stop accepting new admin HTTP requests (30s budget).
	//   2. Signal scheduler + watchers to stop via context cancel.
	//   3. Wait for the in-flight cycle to finish (up to 30s).
	//   4. Close the database handle.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("admin http shutdown error", "err", err)
	}
	cancel() // stop scheduler Run loop and settings watcher

	// Wait for the currently-running cycle, with its own deadline so
	// we don't block the process past the budget.
	cycleCtx, cycleCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cycleCancel()
	if err := scheduler.WaitIdle(cycleCtx); err != nil {
		log.Warn("graceful shutdown: cycle did not finish in time", "err", err)
	} else {
		log.Info("graceful shutdown: cycle idle")
	}

	log.Info("assistant stopped")
	return nil
}

// watchSettings polls the settings row on a short interval and pushes
// any change to digest_interval_seconds into the scheduler live. The
// settings row is also where uncategorized_label and subscriber_chat_id
// live; the cycle reads those on every fire, so they pick up changes
// without explicit watching.
func watchSettings(
	ctx context.Context,
	settings store.SettingsRepo,
	health store.HealthRepo,
	scheduler *digest.Scheduler,
	log *slog.Logger,
	initialInterval time.Duration,
) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	lastInterval := initialInterval
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			cur, err := settings.Get(ctx)
			if err != nil {
				log.Warn("settings watcher: get failed", "err", err)
				continue
			}
			d := time.Duration(cur.DigestIntervalSeconds) * time.Second
			if d != lastInterval {
				log.Info("settings watcher: interval changed",
					"from", lastInterval, "to", d)
				scheduler.SetInterval(d)
				lastInterval = d
				_ = health.RecordEvent(ctx, store.OpEvent{
					OccurredAt: time.Now().UTC(),
					Level:      "info",
					Kind:       "settings.changed",
					Message:    "digest_interval_seconds: " + d.String(),
				})
			}
		}
	}
}

// newSummarizer picks the AI implementation based on config.
func newSummarizer(cfg config.Config, log *slog.Logger, categories []string) ai.Summarizer {
	switch cfg.AIProvider {
	case "openai":
		// Resolve the API key from the AI_API_KEY env var. The
		// "env:AI_API_KEY" ref pattern in the settings table is
		// preserved by the admin API; the runtime path resolves
		// it directly from process env.
		key, err := resolveSecret("env:AI_API_KEY")
		if err != nil {
			log.Warn("ai: cannot resolve api key ref, falling back to fake",
				"ref", "env:AI_API_KEY", "err", err)
			return ai.NewFake(nil, "Uncategorized")
		}
		clientCfg := openai.DefaultConfig(key)
		if cfg.AIBaseURL != "" {
			clientCfg.BaseURL = cfg.AIBaseURL
		}
		client := openai.NewClientWithConfig(clientCfg)
		uncat := "Uncategorized"
		log.Info("ai summarizer: openai", "model", cfg.AIModel, "base_url", cfg.AIBaseURL)
		return ai.NewOpenAISummarizer(
			client,
			cfg.AIModel,
			categories,
			uncat,
			cfg.AIPerCallTimeout,
			log,
		)
	case "anthropic":
		key, err := resolveSecret("env:AI_API_KEY")
		if err != nil {
			log.Warn("ai: cannot resolve api key ref, falling back to fake",
				"ref", "env:AI_API_KEY", "err", err)
			return ai.NewFake(nil, "Uncategorized")
		}
		uncat := "Uncategorized"
		log.Info("ai summarizer: anthropic", "model", cfg.AIModel, "base_url", cfg.AIBaseURL)
		return ai.NewAnthropicSummarizer(
			cfg.AIBaseURL,
			cfg.AIModel,
			key,
			categories,
			uncat,
			cfg.AIPerCallTimeout,
			log,
		)
	default:
		log.Info("ai summarizer: fake", "rules", 0)
		return ai.NewFake(nil, "Uncategorized")
	}
}

// resolveSecret parses a "ref" string like "env:NAME" and returns the
// resolved value. Returns an error if the ref is empty, has an
// unsupported prefix, or the referenced env var is unset.
func resolveSecret(ref string) (string, error) {
	if ref == "" {
		return "", errors.New("empty secret ref")
	}
	const envPrefix = "env:"
	if !strings.HasPrefix(ref, envPrefix) {
		return "", fmt.Errorf("unsupported secret ref %q (only %s* is supported in phase 1)", ref, envPrefix)
	}
	name := strings.TrimPrefix(ref, envPrefix)
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("env var %q is not set", name)
	}
	return v, nil
}

// newTelegramClient picks the Telegram implementation based on config.
// When TELEGRAM_BOT_TOKEN is set and TELEGRAM_USE_FAKE is false, it
// returns a real Bot API client that long-polls getUpdates for
// channel_post events. The onSubscriberChat callback persists the chat
// id of the first private message the bot sees, so the operator does
// not have to read it from getUpdates and patch the settings manually.
//
// TELEGRAM_SOURCE=preview swaps the read path to the public web
// preview (t.me/s/<handle>), so the bot does not need to be a member
// of the channel. SendMessage still uses the Bot API either way.
func newTelegramClient(cfg config.Config, log *slog.Logger, settings store.SettingsRepo) (telegram.Client, error) {
	if cfg.TelegramUseFake || cfg.TelegramBotToken == "" {
		log.Info("telegram client: fake",
			"seed", cfg.TelegramFakeSeed,
			"out", cfg.TelegramFakeOut,
		)
		return telegram.NewFake(cfg.TelegramFakeSeed, cfg.TelegramFakeOut)
	}
	source := strings.ToLower(strings.TrimSpace(cfg.TelegramSource))
	if source == "" {
		source = "longpoll"
	}
	if source != "longpoll" && source != "preview" {
		return nil, fmt.Errorf("config: unknown TELEGRAM_SOURCE %q (want longpoll or preview)", cfg.TelegramSource)
	}
	if source == "preview" {
		log.Info("telegram client: preview (public web)",
			"preview_base", cfg.TelegramPreviewBase,
			"token_set", true,
		)
		return telegram.NewHTTPPreviewWithBases(cfg.TelegramBotToken, cfg.TelegramPreviewBase, ""), nil
	}
	onChat := func(chatID int64) {
		cur, err := settings.Get(context.Background())
		if err != nil {
			log.Warn("telegram real: cannot load settings for chat persistence", "err", err)
			return
		}
		if cur.TelegramSubscriberChat == chatID {
			return
		}
		v := chatID
		_, err = settings.Update(context.Background(), store.SettingsUpdate{TelegramSubscriberChat: &v})
		if err != nil {
			log.Warn("telegram real: cannot persist subscriber chat", "err", err, "chat_id", chatID)
			return
		}
		log.Info("telegram real: persisted subscriber chat", "chat_id", chatID)
	}
	log.Info("telegram client: real", "token_set", true)
	return telegram.NewReal(cfg.TelegramBotToken, log, onChat)
}
