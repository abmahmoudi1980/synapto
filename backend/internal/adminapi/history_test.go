package adminapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/store"
)

func TestHistory_ListCyclesEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/cycles")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Cycles []json.RawMessage `json:"cycles"`
		Total  int               `json:"total"`
	}
	decodeBody(t, res, &body)
	if len(body.Cycles) != 0 {
		t.Errorf("expected 0 cycles, got %d", len(body.Cycles))
	}
	if body.Total != 0 {
		t.Errorf("expected total=0, got %d", body.Total)
	}
}

func TestHistory_CycleNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/cycles/nonexistent-id")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "cycle_not_found" {
		t.Errorf("expected cycle_not_found, got %q", er.Error.Code)
	}
}

func TestHistory_GetCycleSkippedHasNoDigest(t *testing.T) {
	ts, st := newTestServer(t)
	// Seed a skipped cycle directly via the store.
	cycleID := "skipped-test-1"
	now := time.Now().UTC()
	if err := st.CreateCycle(context.Background(), store.Cycle{
		ID:          cycleID,
		WindowStart: now.Add(-10 * time.Minute),
		WindowEnd:   now,
		Status:      store.CycleSkippedNoItems,
		StartedAt:   now,
	}); err != nil {
		t.Fatalf("create cycle: %v", err)
	}

	res := tsGET(t, ts, "/api/cycles/"+cycleID)
	// Skipped cycle → no digest → 410 Gone with digest_not_available.
	if res.StatusCode != http.StatusGone {
		t.Fatalf("expected 410, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "digest_not_available" {
		t.Errorf("expected digest_not_available, got %q", er.Error.Code)
	}
}

func TestHistory_GetCycleSucceededReturnsDigestAndItems(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()

	// Build a succeeded cycle: 1 channel, 2 default categories, 4 items.
	ch, err := st.AddChannel(ctx, "hist_chan", "History Chan")
	if err != nil {
		t.Fatalf("add channel: %v", err)
	}
	allCats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	var cat1, cat2 store.Category
	for _, c := range allCats {
		if c.Name == "Politics" {
			cat1 = c
		}
		if c.Name == "Technology" {
			cat2 = c
		}
	}
	if cat1.ID == "" || cat2.ID == "" {
		t.Fatalf("expected default Politics + Technology categories to be seeded")
	}

	cycleID := "hist-cycle-1"
	now := time.Now().UTC()
	if err := st.CreateCycle(ctx, store.Cycle{
		ID:            cycleID,
		WindowStart:   now.Add(-10 * time.Minute),
		WindowEnd:     now,
		Status:        store.CycleSucceeded,
		InputMsgCount: 4,
		OutputItems:   4,
		StartedAt:     now.Add(-30 * time.Second),
		FinishedAt:    now,
	}); err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	digestID := "hist-digest-1"
	if err := st.CreateDigest(ctx, store.Digest{
		ID:            digestID,
		CycleID:       cycleID,
		RenderedText:  "📰 News digest\n\n# Politics\n• x  _(hist_chan)_\n\n# Technology\n• y  _(hist_chan)_",
		Degraded:      false,
		SentAt:        now,
		SendStatus:    store.SendOK,
	}); err != nil {
		t.Fatalf("create digest: %v", err)
	}
	_ = st.UpdateDigestSendResult(ctx, digestID, 4711, store.SendOK)

	for i, catID := range []string{cat1.ID, cat1.ID, cat2.ID, cat2.ID} {
		_ = st.AddDigestItem(ctx, store.DigestItem{
			CycleID:     cycleID,
			ChannelID:   ch.ID,
			CategoryID:  catID,
			SourceMsgID: int64(1000 + i),
			DedupKey:    "k-" + cycleID + "-" + string(rune('a'+i)),
			RawText:     "raw",
			MediaKind:   store.MediaText,
			Summary:     "summary " + string(rune('a'+i)),
			Confidence:  0.9,
			Ordering:    i,
		})
	}

	res := tsGET(t, ts, "/api/cycles/"+cycleID)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Cycle struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"cycle"`
		Digest struct {
			ID           string `json:"id"`
			RenderedText string `json:"rendered_text"`
			TelegramMsgID int64  `json:"telegram_msg_id"`
			SendStatus    string `json:"send_status"`
		} `json:"digest"`
		ItemsByCategory []struct {
			Category struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				IsDefault bool   `json:"is_default"`
			} `json:"category"`
			Items []struct {
				Summary    string  `json:"summary"`
				SourceMsgID int64  `json:"source_msg_id"`
				Channel    struct {
					Handle string `json:"handle"`
				} `json:"channel"`
			} `json:"items"`
		} `json:"items_by_category"`
	}
	decodeBody(t, res, &body)
	if body.Cycle.ID != cycleID {
		t.Errorf("expected cycle id %q, got %q", cycleID, body.Cycle.ID)
	}
	if body.Cycle.Status != "succeeded" {
		t.Errorf("expected status succeeded, got %q", body.Cycle.Status)
	}
	if body.Digest.TelegramMsgID != 4711 {
		t.Errorf("expected telegram_msg_id 4711, got %d", body.Digest.TelegramMsgID)
	}
	if body.Digest.SendStatus != "ok" {
		t.Errorf("expected send_status ok, got %q", body.Digest.SendStatus)
	}
	if len(body.ItemsByCategory) != 2 {
		t.Fatalf("expected 2 category groups, got %d", len(body.ItemsByCategory))
	}
	// Items per category should be 2 each.
	for _, group := range body.ItemsByCategory {
		if len(group.Items) != 2 {
			t.Errorf("category %q: expected 2 items, got %d", group.Category.Name, len(group.Items))
		}
	}
}

func TestHistory_EventsNewestFirst(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, kind := range []string{"first", "second", "third"} {
		_ = st.RecordEvent(ctx, store.OpEvent{
			OccurredAt: now.Add(time.Duration(i) * time.Second),
			Level:      "info",
			Kind:       kind,
			Message:    kind,
		})
	}

	res := tsGET(t, ts, "/api/events?limit=10")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Events []struct {
			ID         int64  `json:"id"`
			Kind       string `json:"kind"`
			OccurredAt string `json:"occurred_at"`
		} `json:"events"`
	}
	decodeBody(t, res, &body)
	if len(body.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(body.Events))
	}
	if body.Events[0].Kind != "third" {
		t.Errorf("expected first event to be 'third', got %q", body.Events[0].Kind)
	}
	if body.Events[2].Kind != "first" {
		t.Errorf("expected last event to be 'first', got %q", body.Events[2].Kind)
	}
}

func TestHistory_EventsLimitRespected(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = st.RecordEvent(ctx, store.OpEvent{Level: "info", Kind: "k", Message: "m"})
	}
	res := tsGET(t, ts, "/api/events?limit=1")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Events []json.RawMessage `json:"events"`
	}
	decodeBody(t, res, &body)
	if len(body.Events) != 1 {
		t.Errorf("expected 1 event with limit=1, got %d", len(body.Events))
	}
}

func TestHistory_CyclesListWithTotal(t *testing.T) {
	ts, st := newTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	// Insert 3 skipped cycles (no digest, but they still count).
	for i := 0; i < 3; i++ {
		if err := st.CreateCycle(ctx, store.Cycle{
			ID:          "list-cycle-" + string(rune('a'+i)),
			WindowStart: now.Add(time.Duration(-i) * time.Hour),
			WindowEnd:   now.Add(time.Duration(-i)*time.Hour + 10*time.Minute),
			Status:      store.CycleSkippedNoItems,
			StartedAt:   now,
		}); err != nil {
			t.Fatalf("create cycle %d: %v", i, err)
		}
	}
	res := tsGET(t, ts, "/api/cycles?limit=10")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Cycles []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"cycles"`
		Total int `json:"total"`
	}
	decodeBody(t, res, &body)
	if len(body.Cycles) != 3 {
		t.Errorf("expected 3 cycles, got %d", len(body.Cycles))
	}
	if body.Total != 3 {
		t.Errorf("expected total=3, got %d", body.Total)
	}
}
