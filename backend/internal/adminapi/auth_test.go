package adminapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/synapto/assistant/internal/adminapi"
	"github.com/synapto/assistant/internal/store/sqlite"
	"github.com/synapto/assistant/internal/telegram"
)

// newAuthServer builds a server with a specific admin password wired
// into Deps. Empty password = auth disabled (v1 default).
func newAuthServer(t *testing.T, password string) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := sqlite.Open(context.Background(), dbPath, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	tg, err := telegram.NewFake(filepath.Join(dir, "seed.json"), "")
	if err != nil {
		t.Fatalf("new fake telegram: %v", err)
	}

	srv := adminapi.New(adminapi.Deps{
		Log:           slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Version:       "test",
		Channels:      sqlite.ChannelStore{S: st},
		Categories:    sqlite.CategoryStore{S: st},
		Settings:      sqlite.SettingsStore{S: st},
		Cycles:        sqlite.CycleStore{S: st},
		Digests:       sqlite.DigestStore{S: st},
		Health:        sqlite.HealthStore{S: st},
		Telegram:      tg,
		StartedAt:     time.Now(),
		AdminPassword: password,
		Dev:           true,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestAuth_LoginRejectsWrongPassword(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	res := tsPOST(t, ts, "/api/auth/login", `{"password":"wrong"}`)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "invalid_credentials" {
		t.Errorf("expected invalid_credentials, got %q", er.Error.Code)
	}
}

func TestAuth_LoginRejectsMissingPassword(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	res := tsPOST(t, ts, "/api/auth/login", `{"password":""}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestAuth_LoginSucceedsAndSetsCookie(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	res := tsPOST(t, ts, "/api/auth/login", `{"password":"supersecret"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, readAll(res))
	}
	var body struct {
		Authenticated bool   `json:"authenticated"`
		AuthRequired  bool   `json:"auth_required"`
		ExpiresAt     string `json:"expires_at"`
	}
	decodeBody(t, res, &body)
	if !body.Authenticated {
		t.Error("expected authenticated=true")
	}
	if !body.AuthRequired {
		t.Error("expected auth_required=true")
	}
	// Cookie should be set.
	var found bool
	for _, c := range res.Cookies() {
		if c.Name == "synapto_session" {
			found = true
			if c.Value == "" {
				t.Error("session cookie value is empty")
			}
			if !c.HttpOnly {
				t.Error("session cookie should be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("session cookie not set")
	}
}

func TestAuth_AuthenticatedRequestUsesCookie(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	// Login.
	loginRes := tsPOST(t, ts, "/api/auth/login", `{"password":"supersecret"}`)
	if loginRes.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d", loginRes.StatusCode)
	}
	// Pull the session cookie.
	var cookie *http.Cookie
	for _, c := range loginRes.Cookies() {
		if c.Name == "synapto_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie set")
	}
	// Make a request to /api/channels with the cookie.
	req, _ := http.NewRequest("GET", ts.URL+"/api/channels", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
}

func TestAuth_UnauthenticatedRequestReturns401(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	res := tsGET(t, ts, "/api/channels")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
	var er struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, res, &er)
	if er.Error.Code != "unauthenticated" {
		t.Errorf("expected unauthenticated, got %q", er.Error.Code)
	}
}

func TestAuth_InvalidCookieReturns401(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	req, _ := http.NewRequest("GET", ts.URL+"/api/channels", nil)
	req.AddCookie(&http.Cookie{Name: "synapto_session", Value: "garbage.signature"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.StatusCode)
	}
}

func TestAuth_LogoutClearsCookie(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	// Login.
	loginRes := tsPOST(t, ts, "/api/auth/login", `{"password":"supersecret"}`)
	var cookie *http.Cookie
	for _, c := range loginRes.Cookies() {
		if c.Name == "synapto_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	// Logout with the cookie.
	req, _ := http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", res.StatusCode)
	}
	// The response should clear the cookie.
	cleared := false
	for _, c := range res.Cookies() {
		if c.Name == "synapto_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout did not clear the session cookie")
	}
}

func TestAuth_StatusAfterLogin(t *testing.T) {
	ts, _ := newAuthServer(t, "supersecret")
	loginRes := tsPOST(t, ts, "/api/auth/login", `{"password":"supersecret"}`)
	var cookie *http.Cookie
	for _, c := range loginRes.Cookies() {
		if c.Name == "synapto_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/status", nil)
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer res.Body.Close()
	var body struct {
		Authenticated bool `json:"authenticated"`
		AuthRequired  bool `json:"auth_required"`
	}
	decodeBody(t, res, &body)
	if !body.Authenticated {
		t.Error("expected authenticated=true after login")
	}
	if !body.AuthRequired {
		t.Error("expected auth_required=true")
	}
}

func TestAuth_HealthEndpointIsUnauthenticated(t *testing.T) {
	// /api/health is allowed even with auth enabled, so an external
	// liveness probe doesn't need a session.
	ts, _ := newAuthServer(t, "supersecret")
	res := tsGET(t, ts, "/api/health")
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (health is unauthenticated), got %d", res.StatusCode)
	}
}
