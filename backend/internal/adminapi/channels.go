package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// channelJSON is the API response shape for one channel, per contracts/admin-api.md.
type channelJSON struct {
	ID             string  `json:"id"`
	Handle         string  `json:"handle"`
	DisplayName    string  `json:"display_name"`
	Status         string  `json:"status"`
	LastObservedAt *string `json:"last_observed_at"`
	LastError      *string `json:"last_error"`
}

func channelToJSON(c store.Channel) channelJSON {
	out := channelJSON{
		ID:          c.ID,
		Handle:      c.Handle,
		DisplayName: c.DisplayName,
		Status:      string(c.Status),
	}
	if !c.LastObservedAt.IsZero() {
		t := c.LastObservedAt.UTC().Format("2006-01-02T15:04:05Z")
		out.LastObservedAt = &t
	}
	if c.LastError != "" {
		out.LastError = &c.LastError
	}
	return out
}

// handleListChannels: GET /api/channels
func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.deps.Channels.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	out := make([]channelJSON, 0, len(channels))
	for _, c := range channels {
		out = append(out, channelToJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

// handleAddChannel: POST /api/channels
func (s *Server) handleAddChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle string `json:"handle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), "handle")
		return
	}
	handle := strings.TrimSpace(req.Handle)
	if handle == "" {
		writeError(w, http.StatusBadRequest, "invalid_handle", "handle must not be empty", "handle")
		return
	}

	// Validate via Telegram GetChat (if a client is wired).
	displayName := strings.TrimPrefix(handle, "@")
	if s.deps.Telegram != nil {
		info, err := s.deps.Telegram.GetChat(r.Context(), strings.TrimPrefix(handle, "@"))
		if err != nil {
			switch {
			case errors.Is(err, telegram.ErrChannelNotFound):
				writeError(w, http.StatusBadRequest, "channel_not_found_on_telegram", "channel not found on Telegram", "handle")
			case errors.Is(err, telegram.ErrBotNotInChannel):
				writeError(w, http.StatusBadRequest, "bot_not_in_channel", "bot is not a member of the channel", "handle")
			case errors.Is(err, telegram.ErrUnavailable):
				writeError(w, http.StatusServiceUnavailable, "telegram_unavailable", "Telegram API unavailable", "")
			default:
				writeError(w, http.StatusServiceUnavailable, "telegram_unavailable", err.Error(), "")
			}
			return
		}
		if info.Title != "" {
			displayName = info.Title
		}
	}

	ch, err := s.deps.Channels.Add(r.Context(), handle, displayName)
	if err != nil {
		if strings.Contains(err.Error(), "invalid") {
			writeError(w, http.StatusBadRequest, "invalid_handle", err.Error(), "handle")
			return
		}
		if isDuplicate(err) {
			writeError(w, http.StatusConflict, "duplicate_channel", "channel already selected", "handle")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": channelToJSON(ch)})
}

// handleDeleteChannel: DELETE /api/channels/{id}
func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "channel id is required", "id")
		return
	}
	err := s.deps.Channels.Remove(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "channel_not_found", "channel not found", "id")
		case errors.Is(err, store.ErrChannelHasHistory):
			writeError(w, http.StatusConflict, "channel_has_history", "channel has digest history and cannot be removed", "id")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isDuplicate returns true if the error looks like a UNIQUE constraint violation.
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate")
}

// registerChannelRoutes wires the channel endpoints onto the /api router.
func (s *Server) registerChannelRoutes(r chi.Router) {
	r.Get("/channels", s.handleListChannels)
	r.Post("/channels", s.handleAddChannel)
	r.Delete("/channels/{id}", s.handleDeleteChannel)
}
