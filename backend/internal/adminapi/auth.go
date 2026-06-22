// Package adminapi — auth middleware + login/logout handlers (T065).
//
// Single-admin-password model: when ADMIN_PASSWORD is set, every
// /api/* request (except /api/auth/*) must carry a valid session
// cookie. When ADMIN_PASSWORD is empty, the middleware is a no-op so
// the v1 dev workflow keeps working without auth.
package adminapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName = "synapto_session"
	sessionTTL        = 12 * time.Hour
	// authBypassHeader is the internal request header set by the
	// middleware after a successful auth check. Downstream handlers
	// may use it to identify the requester (single-user in v1).
	authBypassHeader = "X-Synapto-Auth"
)

// sessionToken is the signed cookie value. Format:
//
//	<session_id>.<exp_unix>.<hmac_hex>
//
// where hmac = HMAC-SHA256(secret, session_id + "." + exp_unix).
type sessionToken struct {
	SessionID string `json:"session_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// sign returns a signed session token or "" if secret is empty.
func sign(secret []byte, sessionID string, expiresAt int64) string {
	if len(secret) == 0 {
		return ""
	}
	// Encode payload as base64(json) for forward compatibility.
	payload, _ := json.Marshal(sessionToken{SessionID: sessionID, ExpiresAt: expiresAt})
	pl := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(pl))
	sig := mac.Sum(nil)
	return pl + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// verify returns the session id from a signed token, or an error if
// the signature is invalid or the token is expired.
func verify(secret []byte, token string) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("auth not configured")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("malformed session token")
	}
	pl, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(pl))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return "", errors.New("malformed signature")
	}
	if !hmac.Equal(want, got) {
		return "", errors.New("signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(pl)
	if err != nil {
		return "", errors.New("malformed payload")
	}
	var st sessionToken
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", errors.New("malformed payload json")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return "", errors.New("session expired")
	}
	return st.SessionID, nil
}

// newSessionID returns a 128-bit random ID, hex-encoded.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// AuthMiddleware gates /api/* behind a session cookie when
// ADMIN_PASSWORD is configured. If password is empty, the middleware
// is a no-op (development mode).
//
// The middleware is registered at /api/* and explicitly skipped for
// /api/auth/login, /api/auth/logout, /api/auth/status, and the static
// SPA.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth disabled in dev.
		if s.deps.AdminPassword == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Skip auth for the auth endpoints, the health probe, and the SPA.
		path := r.URL.Path
		if path == "/api/auth/login" ||
			path == "/api/auth/logout" ||
			path == "/api/auth/status" ||
			path == "/api/health" ||
			!strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required", "")
			return
		}
		sessionID, err := verify(s.sessionSecret, c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "session invalid", "")
			return
		}
		r.Header.Set(authBypassHeader, sessionID)
		next.ServeHTTP(w, r)
	})
}

// requestIsSecure reports whether the current request reached us over
// HTTPS — either directly (r.TLS != nil when Go's http.Server is
// configured with TLS) or via a reverse proxy that sets the
// X-Forwarded-Proto header. Used to decide the session cookie's
// Secure flag so the cookie round-trips over plain HTTP when the
// deployment is HTTP, and is locked to HTTPS when behind TLS.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

// handleAuthLogin: POST /api/auth/login { password: "..." }
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.deps.AdminPassword == "" {
		// Auth disabled; succeed so the SPA can show "no auth required".
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "auth_required": false})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error(), "")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "missing_password", "password is required", "password")
		return
	}
	// Constant-time compare to avoid timing attacks.
	if !hmac.Equal([]byte(req.Password), []byte(s.deps.AdminPassword)) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid password", "")
		return
	}
	sessionID, err := newSessionID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error(), "")
		return
	}
	expiresAt := time.Now().Add(sessionTTL).Unix()
	tok := sign(s.sessionSecret, sessionID, expiresAt)
	if tok == "" {
		writeError(w, http.StatusInternalServerError, "internal", "auth not configured", "")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(expiresAt, 0),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"auth_required": true,
		"expires_at":    time.Unix(expiresAt, 0).UTC().Format(time.RFC3339),
	})
}

// handleAuthLogout: POST /api/auth/logout
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

// handleAuthStatus: GET /api/auth/status
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	authRequired := s.deps.AdminPassword != ""
	authenticated := false
	if authRequired {
		if c, err := r.Cookie(sessionCookieName); err == nil {
			if _, err := verify(s.sessionSecret, c.Value); err == nil {
				authenticated = true
			}
		}
	} else {
		authenticated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": authenticated,
		"auth_required": authRequired,
	})
}
