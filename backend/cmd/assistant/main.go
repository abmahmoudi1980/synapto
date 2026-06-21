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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/synapto/assistant/internal/adminapi"
	"github.com/synapto/assistant/internal/ai"
	"github.com/synapto/assistant/internal/config"
	"github.com/synapto/assistant/internal/digest"
	"github.com/synapto/assistant/internal/logging"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"
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

	// AI summarizer. Used by the digest cycle.
	summarizer := newSummarizer(cfg, log)

	// Telegram client.
	tgClient, err := newTelegramClient(cfg, log)
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
		Telegram:          tgClient,
		SchedulerState:    schedulerStateFn,
		TelegramReachable: func() bool { return true },
		AIReachable:       func() bool { return cfg.AIProvider == "fake" },
		StartedAt:         time.Now(),
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
		SubscriberChatID: cfg.TelegramSubscriberChat,
	})
	scheduler := digest.NewScheduler(cycle, cfg.DigestInterval, sqlite.CycleStore{S: st}, log)

	// HTTP server goroutine.
	errCh := make(chan error, 2)
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("admin http shutdown error", "err", err)
	}
	cancel()
	log.Info("assistant stopped")
	return nil
}

// newSummarizer picks the AI implementation based on config.
func newSummarizer(cfg config.Config, log *slog.Logger) ai.Summarizer {
	switch cfg.AIProvider {
	case "openai":
		// The real OpenAI implementation is added in Phase 8 (T058).
		// For now, fall through to fake so the binary always boots.
		log.Warn("ai provider openai not yet implemented; using fake", "ai_provider", cfg.AIProvider)
		fallthrough
	default:
		log.Info("ai summarizer: fake", "rules", 0)
		return ai.NewFake(nil, "Uncategorized")
	}
}

// newTelegramClient picks the Telegram implementation based on config.
func newTelegramClient(cfg config.Config, log *slog.Logger) (telegram.Client, error) {
	if cfg.TelegramUseFake || cfg.TelegramBotToken == "" {
		log.Info("telegram client: fake",
			"seed", cfg.TelegramFakeSeed,
			"out", cfg.TelegramFakeOut,
		)
		return telegram.NewFake(cfg.TelegramFakeSeed, cfg.TelegramFakeOut)
	}
	log.Info("telegram client: real (not yet implemented; using fake)", "token_set", cfg.TelegramBotToken != "")
	// The real Bot API client is added in Phase 4 (T030). For now, use fake.
	return telegram.NewFake(cfg.TelegramFakeSeed, cfg.TelegramFakeOut)
}
