package adminapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/synapto/assistant/internal/store"
)

// cycleListEntryJSON is the API shape for one row in the cycles list.
type cycleListEntryJSON struct {
	ID            string `json:"id"`
	WindowStart   string `json:"window_start"`
	WindowEnd     string `json:"window_end"`
	Status        string `json:"status"`
	InputMsgCount int    `json:"input_msg_count"`
	OutputItems   int    `json:"output_items"`
	Degraded      bool   `json:"degraded"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at"`
}

func cycleListEntryToJSON(e store.DigestListEntry) cycleListEntryJSON {
	out := cycleListEntryJSON{
		ID:            e.Cycle.ID,
		WindowStart:   e.Cycle.WindowStart.UTC().Format(time.RFC3339),
		WindowEnd:     e.Cycle.WindowEnd.UTC().Format(time.RFC3339),
		Status:        string(e.Cycle.Status),
		InputMsgCount: e.Cycle.InputMsgCount,
		OutputItems:   e.Cycle.OutputItems,
		Degraded:      e.Degraded,
		StartedAt:     e.Cycle.StartedAt.UTC().Format(time.RFC3339),
	}
	if !e.Cycle.FinishedAt.IsZero() {
		out.FinishedAt = e.Cycle.FinishedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func cycleToJSON(c store.Cycle) cycleListEntryJSON {
	return cycleListEntryJSON{
		ID:            c.ID,
		WindowStart:   c.WindowStart.UTC().Format(time.RFC3339),
		WindowEnd:     c.WindowEnd.UTC().Format(time.RFC3339),
		Status:        string(c.Status),
		InputMsgCount: c.InputMsgCount,
		OutputItems:   c.OutputItems,
		StartedAt:     c.StartedAt.UTC().Format(time.RFC3339),
	}
}

// digestJSON is the API shape for a digest in the cycle-detail response.
type digestJSON struct {
	ID            string `json:"id"`
	RenderedText  string `json:"rendered_text"`
	Degraded      bool   `json:"degraded"`
	TelegramMsgID *int64 `json:"telegram_msg_id"`
	SentAt        string `json:"sent_at"`
	SendStatus    string `json:"send_status"`
}

func digestToJSON(d store.Digest) digestJSON {
	out := digestJSON{
		ID:           d.ID,
		RenderedText: d.RenderedText,
		Degraded:     d.Degraded,
		SentAt:       d.SentAt.UTC().Format(time.RFC3339),
		SendStatus:   string(d.SendStatus),
	}
	if d.TelegramMsgID > 0 {
		id := d.TelegramMsgID
		out.TelegramMsgID = &id
	}
	return out
}

// channelSummaryJSON is the slim channel shape used in items_by_category.
type channelSummaryJSON struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

// categorySummaryJSON is the slim category shape used in items_by_category.
type categorySummaryJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Ordering  int    `json:"ordering"`
	IsDefault bool   `json:"is_default"`
}

type digestItemJSON struct {
	ID          string             `json:"id"`
	Channel     channelSummaryJSON `json:"channel"`
	SourceMsgID int64              `json:"source_msg_id"`
	MediaKind   string             `json:"media_kind"`
	Summary     string             `json:"summary"`
	Confidence  *float64           `json:"confidence"`
}

type itemsByCategoryJSON struct {
	Category categorySummaryJSON `json:"category"`
	Items    []digestItemJSON    `json:"items"`
}

// handleListCycles: GET /api/cycles?limit=&offset=
func (s *Server) handleListCycles(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r, 20, 200)
	ctx := r.Context()
	entries, err := s.deps.Cycles.ListWithDegraded(ctx, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	total, err := s.deps.Cycles.Count(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	out := make([]cycleListEntryJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, cycleListEntryToJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cycles": out,
		"total":  total,
	})
}

// handleGetCycle: GET /api/cycles/{id}
func (s *Server) handleGetCycle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "cycle id is required", "id")
		return
	}
	ctx := r.Context()
	cycle, err := s.deps.Cycles.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "cycle_not_found", "cycle not found", "id")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}

	resp := map[string]any{"cycle": cycleToJSON(cycle)}

	// A cycle that produced no items has no digest. Surface that with
	// 410 Gone + digest_not_available so clients can render a clear
	// "skipped / failed" view.
	if cycle.Status == store.CycleSkippedNoItems {
		writeError(w, http.StatusGone, "digest_not_available",
			"this cycle produced no items", "id")
		return
	}

	digestRow, err := s.deps.Digests.GetByCycle(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusGone, "digest_not_available",
				"no digest was produced for this cycle", "id")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	resp["digest"] = digestToJSON(digestRow)

	itemsByCat, err := s.buildItemsByCategory(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	resp["items_by_category"] = itemsByCat

	writeJSON(w, http.StatusOK, resp)
}

// buildItemsByCategory groups digest items by their assigned category.
// Categories are returned in the same order used elsewhere (ordering ASC,
// name ASC); the items within a group are sorted by their per-cycle
// ordering.
func (s *Server) buildItemsByCategory(ctx context.Context, cycleID string) ([]itemsByCategoryJSON, error) {
	items, err := s.deps.Digests.ListItemsByCycle(ctx, cycleID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []itemsByCategoryJSON{}, nil
	}
	categories, err := s.deps.Categories.List(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := s.deps.Channels.List(ctx)
	if err != nil {
		return nil, err
	}
	catByID := make(map[string]store.Category, len(categories))
	for _, c := range categories {
		catByID[c.ID] = c
	}
	chByID := make(map[string]store.Channel, len(channels))
	for _, c := range channels {
		chByID[c.ID] = c
	}

	// Group items by category. Items without a category are placed
	// under the "Uncategorized" pseudo-group rendered last.
	type bucket struct {
		cat   store.Category
		items []store.DigestItem
	}
	byCat := make(map[string]*bucket, len(categories))
	var order []string
	for _, it := range items {
		var key string
		if it.CategoryID != "" {
			if _, exists := byCat[it.CategoryID]; !exists {
				byCat[it.CategoryID] = &bucket{cat: catByID[it.CategoryID]}
				order = append(order, it.CategoryID)
			}
			key = it.CategoryID
		} else {
			if _, exists := byCat[""]; !exists {
				byCat[""] = &bucket{cat: store.Category{Name: "Uncategorized", IsDefault: false, Ordering: 9999}}
				order = append(order, "")
			}
			key = ""
		}
		byCat[key].items = append(byCat[key].items, it)
	}

	out := make([]itemsByCategoryJSON, 0, len(order))
	for _, key := range order {
		b := byCat[key]
		catJSON := categorySummaryJSON{
			ID:        b.cat.ID,
			Name:      b.cat.Name,
			Ordering:  b.cat.Ordering,
			IsDefault: b.cat.IsDefault,
		}
		itemJSONs := make([]digestItemJSON, 0, len(b.items))
		for _, it := range b.items {
			ch := chByID[it.ChannelID]
			dij := digestItemJSON{
				ID:          it.ID,
				Channel:     channelSummaryJSON{ID: ch.ID, Handle: ch.Handle, DisplayName: ch.DisplayName},
				SourceMsgID: it.SourceMsgID,
				MediaKind:   string(it.MediaKind),
				Summary:     it.Summary,
			}
			if it.Confidence > 0 {
				cf := it.Confidence
				dij.Confidence = &cf
			}
			itemJSONs = append(itemJSONs, dij)
		}
		out = append(out, itemsByCategoryJSON{Category: catJSON, Items: itemJSONs})
	}
	return out, nil
}

// opEventJSON is the API shape for op_events entries.
type opEventJSON struct {
	ID         int64  `json:"id"`
	OccurredAt string `json:"occurred_at"`
	Level      string `json:"level"`
	Kind       string `json:"kind"`
	CycleID    string `json:"cycle_id,omitempty"`
	Message    string `json:"message"`
	Context    string `json:"context,omitempty"`
}

func opEventToJSON(e store.OpEvent) opEventJSON {
	out := opEventJSON{
		ID:         e.ID,
		OccurredAt: e.OccurredAt.UTC().Format(time.RFC3339),
		Level:      e.Level,
		Kind:       e.Kind,
		CycleID:    e.CycleID,
		Message:    e.Message,
		Context:    e.Context,
	}
	return out
}

// handleListEvents: GET /api/events?limit=50
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := parseLimitOffset(r, 50, 200)
	events, err := s.deps.Health.RecentEvents(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error(), "")
		return
	}
	out := make([]opEventJSON, 0, len(events))
	for _, e := range events {
		out = append(out, opEventToJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

// parseLimitOffset extracts limit/offset from the query string, with
// defaults and bounds.
func parseLimitOffset(r *http.Request, def, max int) (limit, offset int) {
	limit = def
	offset = 0
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > max {
		limit = max
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// registerHistoryRoutes wires the history endpoints onto the /api router.
func (s *Server) registerHistoryRoutes(r chi.Router) {
	r.Get("/cycles", s.handleListCycles)
	r.Get("/cycles/{id}", s.handleGetCycle)
	r.Get("/events", s.handleListEvents)
}
