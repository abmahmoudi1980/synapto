package sqlite

import (
	"context"
	"errors"
	"time"

	"github.com/synapto/assistant/internal/store"
)

type settingsRow struct {
	ID                     string `db:"id"`
	DigestIntervalSeconds  int    `db:"digest_interval_seconds"`
	TelegramBotTokenRef    string `db:"telegram_bot_token_ref"`
	TelegramSubscriberChat int64  `db:"telegram_subscriber_chat"`
	AIProvider             string `db:"ai_provider"`
	AIModel                string `db:"ai_model"`
	AIAPIKeyRef            string `db:"ai_api_key_ref"`
	AIBaseURL              string `db:"ai_base_url"`
	UncategorizedLabel     string `db:"uncategorized_label"`
	DeliveryMode           string `db:"delivery_mode"`
	UpdatedAt              string `db:"updated_at"`
}

func (r settingsRow) toEntity() store.Settings {
	return store.Settings{
		DigestIntervalSeconds:  r.DigestIntervalSeconds,
		TelegramBotTokenRef:    r.TelegramBotTokenRef,
		TelegramSubscriberChat: r.TelegramSubscriberChat,
		AIProvider:             r.AIProvider,
		AIModel:                r.AIModel,
		AIAPIKeyRef:            r.AIAPIKeyRef,
		AIBaseURL:              r.AIBaseURL,
		UncategorizedLabel:     r.UncategorizedLabel,
		DeliveryMode:           store.DeliveryMode(r.DeliveryMode),
		UpdatedAt:              parseTimeStr(r.UpdatedAt),
	}
}

// GetSettings returns the singleton settings row.
func (s *Store) GetSettings(ctx context.Context) (store.Settings, error) {
	var r settingsRow
	if err := s.db.GetContext(ctx, &r, `SELECT * FROM settings WHERE id = 'singleton'`); err != nil {
		return store.Settings{}, err
	}
	return r.toEntity(), nil
}

// UpdateSettings applies a partial update to the settings row.
func (s *Store) UpdateSettings(ctx context.Context, u store.SettingsUpdate) (store.Settings, error) {
	cur, err := s.GetSettings(ctx)
	if err != nil {
		return store.Settings{}, err
	}
	if u.DigestIntervalSeconds != nil {
		v := *u.DigestIntervalSeconds
		if v < 60 || v > 86400 {
			return store.Settings{}, store.ErrInvalidInterval
		}
		cur.DigestIntervalSeconds = v
	}
	if u.TelegramSubscriberChat != nil {
		cur.TelegramSubscriberChat = *u.TelegramSubscriberChat
	}
	if u.UncategorizedLabel != nil {
		v := *u.UncategorizedLabel
		if v == "" {
			return store.Settings{}, errors.New("uncategorized_label must not be empty")
		}
		cur.UncategorizedLabel = v
	}
	if u.DeliveryMode != nil {
		v := *u.DeliveryMode
		if v != store.DeliveryBundled && v != store.DeliveryPerPost {
			return store.Settings{}, store.ErrInvalidDeliveryMode
		}
		cur.DeliveryMode = v
	}
	_, err = s.db.ExecContext(ctx, `UPDATE settings SET
		digest_interval_seconds = ?, telegram_subscriber_chat = ?, uncategorized_label = ?,
		delivery_mode = ?, updated_at = ?
		WHERE id = 'singleton'`,
		cur.DigestIntervalSeconds, cur.TelegramSubscriberChat, cur.UncategorizedLabel,
		string(cur.DeliveryMode), nowISO())
	if err != nil {
		return store.Settings{}, err
	}
	cur.UpdatedAt = time.Now().UTC()
	return cur, nil
}

// SyncAISettings overwrites the AI-related fields in the settings row
// with the supplied values. The bot token ref and the
// operator-tunable fields (digest interval, subscriber chat id,
// uncategorized label) are left untouched. Called from main at boot
// so the admin panel reflects the env file's ASSISTANT_AI_PROVIDER /
// AI_MODEL / AI_BASE_URL / AI_API_KEY, regardless of whatever
// hardcoded defaults the initial seed used.
func (s *Store) SyncAISettings(ctx context.Context, provider, model, baseURL, keyRef string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE settings SET
		ai_provider = ?, ai_model = ?, ai_base_url = ?, ai_api_key_ref = ?, updated_at = ?
		WHERE id = 'singleton'`,
		provider, model, baseURL, keyRef, nowISO())
	return err
}
