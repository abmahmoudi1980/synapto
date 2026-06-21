package adminapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/adminapi"
	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"
)

// newTestServer builds an admin API server backed by a temp SQLite store
// and a fake Telegram client, ready for httptest requests.
func newTestServer(t *testing.T) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := sqlite.Open(context.Background(), dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	seedPath := filepath.Join(dir, "seed.json")
	tg, err := telegram.NewFake(seedPath, "")
	if err != nil {
		t.Fatalf("new fake telegram: %v", err)
	}

	srv := adminapi.New(adminapi.Deps{
		Log:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Version:    "test",
		Channels:   sqlite.ChannelStore{S: st},
		Categories: sqlite.CategoryStore{S: st},
		Settings:   sqlite.SettingsStore{S: st},
		Cycles:     sqlite.CycleStore{S: st},
		Digests:    sqlite.DigestStore{S: st},
		Health:     sqlite.HealthStore{S: st},
		Telegram:   tg,
		StartedAt:  time.Now(),
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestChannels_ListEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsGET(t, ts, "/api/channels")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var body struct {
		Channels []json.RawMessage `json:"channels"`
	}
	decodeBody(t, res, &body)
	if len(body.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(body.Channels))
	}
}

func TestChannels_AddHappyPath(t *testing.T) {
	ts, _ := newTestServer(t)
	body := `{"handle":"sample_news"}`
	res := tsPOST(t, ts, "/api/channels", body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", res.StatusCode, readAll(res))
	}
	var resp struct {
		Channel struct {
			ID          string `json:"id"`
			Handle      string `json:"handle"`
			DisplayName string `json:"display_name"`
			Status      string `json:"status"`
		} `json:"channel"`
	}
	decodeBody(t, res, &resp)
	if resp.Channel.Handle != "sample_news" {
		t.Errorf("expected handle sample_news, got %s", resp.Channel.Handle)
	}
	if resp.Channel.Status != "active" {
		t.Errorf("expected status active, got %s", resp.Channel.Status)
	}
	if resp.Channel.ID == "" {
		t.Error("expected non-empty id")
	}
}

func TestChannels_AddInvalidHandle(t *testing.T) {
	ts, _ := newTestServer(t)
	cases := []string{
		`{"handle":""}`,
		`{"handle":"x"}`,
		`{"handle":"123abc"}`,
		`{"handle":"_underscore_start"}`,
	}
	for _, body := range cases {
		res := tsPOST(t, ts, "/api/channels", body)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for %s, got %d", body, res.StatusCode)
		}
		var errResp struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		decodeBody(t, res, &errResp)
		if errResp.Error.Code != "invalid_handle" {
			t.Errorf("expected invalid_handle, got %s", errResp.Error.Code)
		}
	}
}

func TestChannels_AddDuplicate(t *testing.T) {
	ts, _ := newTestServer(t)
	tsPOST(t, ts, "/api/channels", `{"handle":"sample_news"}`)
	res := tsPOST(t, ts, "/api/channels", `{"handle":"sample_news"}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d", res.StatusCode)
	}
}

func TestChannels_DeleteHappyPath(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsPOST(t, ts, "/api/channels", `{"handle":"sample_news"}`)
	var resp struct {
		Channel struct{ ID string `json:"id"` } `json:"channel"`
	}
	decodeBody(t, res, &resp)

	delRes := tsDELETE(t, ts, "/api/channels/"+resp.Channel.ID)
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", delRes.StatusCode)
	}
}

func TestChannels_DeleteNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	res := tsDELETE(t, ts, "/api/channels/nonexistent-id")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
}

// --- HTTP helpers ---

func tsGET(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func tsPOST(t *testing.T, ts *httptest.Server, path, body string) *http.Response {
	t.Helper()
	res, err := http.Post(ts.URL+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

func tsDELETE(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("DELETE", ts.URL+path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return res
}

func decodeBody(t *testing.T, res *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func readAll(res *http.Response) string {
	data := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(data)
}

// Ensure store is used (avoids unused import in some build configurations).
var _ = store.ChannelActive
