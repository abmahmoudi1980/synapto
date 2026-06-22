package adminapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/synapto/assistant/internal/store"
)

// postJSON is the on-the-wire shape for one post. See
// contracts/admin-api.md.
type postJSON struct {
	ID            string  `json:"id"`
	ChannelID     string  `json:"channel_id"`
	ChannelHandle *string `json:"channel_handle,omitempty"`
	SourceMsgID   int64   `json:"source_msg_id"`
	Link          string  `json:"link"`
	RawText       string  `json:"raw_text"`
	MediaKind     string  `json:"media_kind"`
	CapturedAt    string  `json:"captured_at"`
	Status        string  `json:"status"`
	CategoryID    *string `json:"category_id,omitempty"`
	CategoryName  *string `json:"category_name,omitempty"`
	Summary       string  `json:"summary"`
	Confidence    *float64 `json:"confidence,omitempty"`
	Attempts      int     `json:"attempts"`
	LastAttemptAt *string `json:"last_attempt_at,omitempty"`
	SentAt        *string `json:"sent_at,omitempty"`
	TelegramMsgID *int64  `json:"telegram_msg_id,omitempty"`
	SendError     *string `json:"send_error,omitempty"`
}

func postToJSON(p store.Post, channels map[string]store.Channel, categories map[string]store.Category) postJSON {
	out := postJSON{
		ID:          p.ID,
		ChannelID:   p.ChannelID,
		SourceMsgID: p.SourceMsgID,
		Link:        p.Link,
		RawText:     p.RawText,
		MediaKind:   string(p.MediaKind),
		CapturedAt:  p.CapturedAt.UTC().Format(time.RFC3339),
		Status:      string(p.Status),
		Summary:     p.Summary,
		Attempts:    p.Attempts,
	}
	if ch, ok := channels[p.ChannelID]; ok {
		h := ch.Handle
		out.ChannelHandle = &h
	}
	if p.CategoryID != "" {
		cid := p.CategoryID
		out.CategoryID = &cid
		if c, ok := categories[p.CategoryID]; ok {
			n := c.Name
			out.CategoryName = &n
		}
	}
	if p.Confidence > 0 {
		cf := p.Confidence
		out.Confidence = &cf
	}
	if !p.LastAttemptAt.IsZero() {
		t := p.LastAttemptAt.UTC().Format(time.RFC3339)
		out.LastAttemptAt = &t
	}
	if !p.SentAt.IsZero() {
		t := p.SentAt.UTC().Format(time.RFC3339)
		out.SentAt = &t
	}
	if p.TelegramMsgID > 0 {
		id := p.TelegramMsgID
		out.TelegramMsgID = &id
	}
	if p.SendError != "" {
		e := p.SendError
		out.SendError = &e
	}
	return out
}

// loadPostLookups fetches the channel and category maps used to
// enrich post JSON responses.
func (s *Server) loadPostLookups(ctx contextLike) (map[string]store.Channel, map[string]store.Category, error) {
	channels, err := s.deps.Channels.List(asCtx(ctx))
	if err != nil {
		return nil, nil, err
	}
	categories, err := s.deps.Categories.List(asCtx(ctx))
	if err != nil {
		return nil, nil, err
	}
	chMap := make(map[string]store.Channel, len(channels))
	for _, c := range channels {
		chMap[c.ID] = c
	}
	catMap := make(map[string]store.Category, len(categories))
	for _, c := range categories {
		catMap[c.ID] = c
	}
	return chMap, catMap, nil
}

// handleListPosts: GET /api/posts?status=&limit=
func (s *Server) handleListPosts(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r, 100, 500)
	q := r.URL.Query()
	statusFilter := q.Get("status")
	var posts []store.Post
	var err error
	if statusFilter == "" {
		posts, err = s.deps.Posts.ListAll(r.Context(), limit)
	} else {
		posts, err = s.deps.Posts.ListByStatus(r.Context(), store.PostStatus(statusFilter), limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	channels, categories, lerr := s.loadPostLookups(r.Context())
	if lerr != nil {
		writeError(w, http.StatusInternalServerError, "db_error", lerr.Error(), "")
		return
	}
	out := make([]postJSON, 0, len(posts))
	for _, p := range posts {
		out = append(out, postToJSON(p, channels, categories))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"posts": out,
		"count": len(out),
	})
}

// handleGetPost: GET /api/posts/{id}
func (s *Server) handleGetPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "post id is required", "id")
		return
	}
	p, err := s.deps.Posts.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "post_not_found", "post not found", "id")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	channels, categories, lerr := s.loadPostLookups(r.Context())
	if lerr != nil {
		writeError(w, http.StatusInternalServerError, "db_error", lerr.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"post": postToJSON(p, channels, categories)})
}

// registerPostRoutes wires the post endpoints onto the /api router.
func (s *Server) registerPostRoutes(r chi.Router) {
	r.Get("/posts", s.handleListPosts)
	r.Get("/posts/{id}", s.handleGetPost)
}

// parseStatusFilter extracts a status= query param. Used by
// /api/posts?status=send_failed. Returns the empty string when unset.
func parseStatusFilter(r *http.Request) string {
	return r.URL.Query().Get("status")
}

// contextLike is the small subset of context.Context that the post
// helpers use. Allows the helpers to be called with any
// context.Context-shaped value (e.g. *http.Request's r.Context()).
type contextLike interface {
	Deadline() (time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}

// asCtx narrows a contextLike to the value the repository interfaces
// expect (which is the standard context.Context shape).
func asCtx(c contextLike) contextLike { return c }
