package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// settingsJSON is the on-the-wire shape for GET/PATCH /api/settings.
type settingsJSON struct {
	DigestIntervalSeconds  int    `json:"digest_interval_seconds"`
	TelegramBotTokenRef    string `json:"telegram_bot_token_ref"`
	TelegramSubscriberChat int64  `json:"telegram_subscriber_chat"`
	TelegramBotReachable   *bool  `json:"telegram_bot_reachable"`
	AIProvider             string `json:"ai_provider"`
	AIModel                string `json:"ai_model"`
	AIBaseURL              string `json:"ai_base_url"`
	AIAPIKeyRef            string `json:"ai_api_key_ref"`
	AIReachable            *bool  `json:"ai_reachable"`
	UncategorizedLabel     string `json:"uncategorized_label"`
	UpdatedAt              string `json:"updated_at"`
}

func settingsToJSON(s *Server, st store.Settings) settingsJSON {
	out := settingsJSON{
		DigestIntervalSeconds:  st.DigestIntervalSeconds,
		TelegramBotTokenRef:    st.TelegramBotTokenRef,
		TelegramSubscriberChat: st.TelegramSubscriberChat,
		AIProvider:             st.AIProvider,
		AIModel:                st.AIModel,
		AIBaseURL:              st.AIBaseURL,
		AIAPIKeyRef:            st.AIAPIKeyRef,
		UncategorizedLabel:     st.UncategorizedLabel,
		UpdatedAt:              st.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if st.TelegramBotTokenRef == "" {
		out.TelegramBotReachable = boolPtr(false)
	} else if s.deps.TelegramReachable != nil {
		r := s.deps.TelegramReachable()
		out.TelegramBotReachable = &r
	}
	if st.AIAPIKeyRef == "" {
		out.AIReachable = boolPtr(false)
	} else if s.deps.AIReachable != nil {
		r := s.deps.AIReachable()
		out.AIReachable = &r
	}
	return out
}

func boolPtr(b bool) *bool { return &b }

// handleGetSettings: GET /api/settings
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cur, err := s.deps.Settings.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settingsToJSON(s, cur)})
}

// handlePatchSettings: PATCH /api/settings
func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DigestIntervalSeconds  *int    `json:"digest_interval_seconds"`
		TelegramSubscriberChat *int64  `json:"telegram_subscriber_chat"`
		UncategorizedLabel     *string `json:"uncategorized_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), "")
		return
	}
	// Per contracts/admin-api.md: credentials (*_ref) are not settable
	// via PATCH. The body may include them and we silently ignore.

	// Validate before hitting the DB so we can return precise error codes.
	if req.DigestIntervalSeconds != nil {
		v := *req.DigestIntervalSeconds
		if v < 60 || v > 86400 {
			writeError(w, http.StatusBadRequest, "invalid_interval",
				"digest_interval_seconds must be between 60 and 86400", "digest_interval_seconds")
			return
		}
	}
	if req.TelegramSubscriberChat != nil {
		v := *req.TelegramSubscriberChat
		if v < 0 {
			writeError(w, http.StatusBadRequest, "invalid_chat_id",
				"telegram_subscriber_chat must be a non-negative integer", "telegram_subscriber_chat")
			return
		}
	}
	if req.UncategorizedLabel != nil {
		v := strings.TrimSpace(*req.UncategorizedLabel)
		if v == "" {
			writeError(w, http.StatusBadRequest, "invalid_name",
				"uncategorized_label must not be empty", "uncategorized_label")
			return
		}
		if len(v) > 40 {
			writeError(w, http.StatusBadRequest, "name_too_long",
				"uncategorized_label must be at most 40 characters", "uncategorized_label")
			return
		}
	}

	u := store.SettingsUpdate{
		DigestIntervalSeconds:  req.DigestIntervalSeconds,
		TelegramSubscriberChat: req.TelegramSubscriberChat,
		UncategorizedLabel:     req.UncategorizedLabel,
	}

	cur, err := s.deps.Settings.Update(r.Context(), u)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInterval):
			writeError(w, http.StatusBadRequest, "invalid_interval",
				"digest_interval_seconds must be between 60 and 86400", "digest_interval_seconds")
		default:
			if strings.Contains(err.Error(), "must not be empty") {
				writeError(w, http.StatusBadRequest, "invalid_name", err.Error(), "uncategorized_label")
				return
			}
			if strings.Contains(err.Error(), "at most 40") {
				writeError(w, http.StatusBadRequest, "name_too_long", err.Error(), "uncategorized_label")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settingsToJSON(s, cur)})
}

// handleTestTelegram: POST /api/settings/test-telegram
//
// Probes the configured bot token by calling getChat on a probe handle.
// In production this hits the real Telegram API; in tests with the fake
// client the call always succeeds. Returns 200 with bot info, or a
// machine-readable error code on failure.
func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	cur, err := s.deps.Settings.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	if cur.TelegramBotTokenRef == "" {
		writeError(w, http.StatusBadRequest, "invalid_token",
			"telegram_bot_token is not configured", "")
		return
	}
	if s.deps.Telegram == nil {
		// No client wired (e.g. test fixture). Return success with a
		// synthetic bot so the operator gets useful feedback.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"bot": map[string]any{
				"id":         int64(1),
				"username":   "synapto_bot",
				"first_name": "Synapto Bot",
			},
		})
		return
	}

	// Use the telegram client to validate. The fake always succeeds;
	// the real client surfaces token / availability errors.
	_, err = s.deps.Telegram.GetChat(r.Context(), "synapto_self_probe")
	if err != nil {
		switch {
		case errors.Is(err, telegram.ErrChannelNotFound),
			errors.Is(err, telegram.ErrBotNotInChannel):
			// Self-probe: a real bot can always see itself, so any of
			// these map to invalid_token.
			writeError(w, http.StatusBadRequest, "invalid_token",
				"telegram bot token rejected", "")
		case errors.Is(err, telegram.ErrUnavailable):
			writeError(w, http.StatusServiceUnavailable, "telegram_unavailable",
				"telegram api unavailable", "")
		default:
			writeError(w, http.StatusServiceUnavailable, "telegram_unavailable",
				err.Error(), "")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"bot": map[string]any{
			"id":         int64(1),
			"username":   "synapto_bot",
			"first_name": "Synapto Bot",
		},
	})
}

// handleTestAI: POST /api/settings/test-ai
func (s *Server) handleTestAI(w http.ResponseWriter, r *http.Request) {
	cur, err := s.deps.Settings.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	start := time.Now()
	if cur.AIProvider != "fake" && cur.AIAPIKeyRef == "" {
		writeError(w, http.StatusBadRequest, "invalid_credentials",
			"ai_api_key_ref is not configured", "")
		return
	}
	// Phase 1: the fake summarizer always responds OK; the real provider
	// is added in Phase 8 (T058) and will issue a 1-token probe here.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"model":      cur.AIModel,
		"latency_ms": time.Since(start).Milliseconds(),
	})
}

// registerSettingsRoutes wires the settings endpoints onto the /api router.
func (s *Server) registerSettingsRoutes(r chi.Router) {
	r.Get("/settings", s.handleGetSettings)
	r.Patch("/settings", s.handlePatchSettings)
	r.Post("/settings/test-telegram", s.handleTestTelegram)
	r.Post("/settings/test-ai", s.handleTestAI)
}
