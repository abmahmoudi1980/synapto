// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v10"
)

// Config holds all runtime configuration for the assistant service.
type Config struct {
	// Telegram
	TelegramBotToken       string `env:"TELEGRAM_BOT_TOKEN" envDefault:""`
	TelegramSubscriberChat int64  `env:"TELEGRAM_SUBSCRIBER_CHAT" envDefault:"0"`
	TelegramFakeOut        string `env:"TELEGRAM_FAKE_OUT" envDefault:"./.runtime/telegram-sent.jsonl"`
	TelegramFakeSeed       string `env:"TELEGRAM_FAKE_SEED" envDefault:"./.runtime/source-messages.yaml"`
	TelegramUseFake        bool   `env:"TELEGRAM_USE_FAKE" envDefault:"false"`

	// AI
	AIProvider       string        `env:"ASSISTANT_AI_PROVIDER" envDefault:"fake"`
	AIBaseURL        string        `env:"AI_BASE_URL" envDefault:"https://api.openai.com/v1"`
	AIModel          string        `env:"AI_MODEL" envDefault:"gpt-4o-mini"`
	APIKey           string        `env:"AI_API_KEY" envDefault:""`
	AIPerCallTimeout time.Duration `env:"AI_PER_CALL_TIMEOUT" envDefault:"8s"`
	AIMaxConcurrency int           `env:"AI_MAX_CONCURRENCY" envDefault:"8"`

	// Scheduler / digest
	DigestInterval time.Duration `env:"DIGEST_INTERVAL" envDefault:"10m"`

	// Admin HTTP
	AdminListenAddr string `env:"ADMIN_LISTEN_ADDR" envDefault:"127.0.0.1:8080"`
	AdminPassword   string `env:"ADMIN_PASSWORD" envDefault:""` // empty = no auth (v1 default; required for v2)

	// Storage
	DBPath string `env:"DB_PATH" envDefault:"./assistant.db"`

	// Logging
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	// Process
	Dev bool `env:"ASSISTANT_DEV" envDefault:"false"`
}

// Load reads configuration from environment variables. Returns an error if
// a required variable is missing or a value cannot be parsed.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

// Validate returns an error if the configuration is internally inconsistent
// or missing required values for the selected provider. It does NOT require
// credentials when the provider is "fake" so that a fresh checkout boots.
func (c Config) Validate() error {
	if c.AIProvider != "fake" && c.AIProvider != "openai" {
		return fmt.Errorf("config: unknown AI_PROVIDER %q (want fake or openai)", c.AIProvider)
	}
	if c.AIProvider == "openai" {
		if c.APIKey == "" {
			return fmt.Errorf("config: AI_API_KEY is required when AI_PROVIDER=openai")
		}
		if c.AIBaseURL == "" {
			return fmt.Errorf("config: AI_BASE_URL is required when AI_PROVIDER=openai")
		}
		if c.AIModel == "" {
			return fmt.Errorf("config: AI_MODEL is required when AI_PROVIDER=openai")
		}
	}
	if c.DigestInterval < time.Minute {
		return fmt.Errorf("config: DIGEST_INTERVAL must be at least 1m, got %s", c.DigestInterval)
	}
	if c.DigestInterval > 24*time.Hour {
		return fmt.Errorf("config: DIGEST_INTERVAL must be at most 24h, got %s", c.DigestInterval)
	}
	if c.AdminListenAddr == "" {
		return fmt.Errorf("config: ADMIN_LISTEN_ADDR must not be empty")
	}
	if c.DBPath == "" {
		return fmt.Errorf("config: DB_PATH must not be empty")
	}
	return nil
}
