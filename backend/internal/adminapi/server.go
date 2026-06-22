// Package adminapi exposes the HTTP admin panel + JSON API served by the
// assistant binary. In phase 1 there is no auth; the listener address is
// configurable and expected to be behind a reverse proxy / VPN.
package adminapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/synapto/assistant/internal/store"
	"github.com/synapto/assistant/internal/telegram"
)

// Deps bundles the repository dependencies the admin API needs.
// Individual handlers pull only the fields they care about.
type Deps struct {
	Log     *slog.Logger
	Version string

	Channels   store.ChannelRepo
	Categories store.CategoryRepo
	Settings   store.SettingsRepo
	Cycles     store.CycleRepo
	Digests    store.DigestRepo
	Health     store.HealthRepo
	Posts      store.PostRepo

	// Telegram is the client used to validate channel handles on add.
	// May be nil in tests; the handler skips Telegram validation when nil.
	Telegram telegramClient

	// SchedulerState is read live by the health endpoint.
	SchedulerState func() string

	// TelegramReachable / AIReachable are probed live by the health endpoint.
	TelegramReachable func() bool
	AIReachable       func() bool

	// StartedAt is the process start time, used for uptime.
	StartedAt time.Time

	// AdminPassword is the single-admin password for /api/*. Empty
	// disables auth (development mode). When set, the API issues
	// session cookies signed with SessionSecret.
	AdminPassword string
	Dev           bool // when true, the session cookie is non-Secure (allows http://)
}

// telegramClient is the subset of telegram.Client the admin API needs.
// The full interface is in internal/telegram; we narrow it here so tests
// can provide a minimal fake without implementing the whole Client.
type telegramClient interface {
	GetChat(ctx context.Context, handle string) (telegram.ChannelInfo, error)
}

// Server is the admin HTTP server.
type Server struct {
	deps          Deps
	r             chi.Router
	http          *http.Server
	sessionSecret []byte
}

// New constructs a Server and registers all routes.
func New(deps Deps) *Server {
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	if deps.Version == "" {
		deps.Version = "0.0.0-dev"
	}
	if deps.StartedAt.IsZero() {
		deps.StartedAt = time.Now()
	}
	s := &Server{
		deps: deps,
		// Derive a per-process session secret from the password (HMAC key).
		// A real deployment should supply its own 32-byte secret via env
		// (ADMIN_SESSION_SECRET); for v1 the password-derived key is
		// sufficient because the operator controls both.
		sessionSecret: deriveSessionSecret(deps.AdminPassword),
	}
	s.r = s.buildRouter()
	s.http = &http.Server{
		Handler:      s.r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// deriveSessionSecret returns a 32-byte HMAC key derived from the
// admin password. When password is empty, returns an empty slice and
// the auth middleware is a no-op.
func deriveSessionSecret(password string) []byte {
	if password == "" {
		return nil
	}
	sum := sha256.Sum256([]byte("synapto-session-v1:" + password))
	return sum[:]
}

// buildRouter wires the route table.
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(loggingMiddleware(s.deps.Log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(s.AuthMiddleware)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Post("/auth/login", s.handleAuthLogin)
		r.Post("/auth/logout", s.handleAuthLogout)
		r.Get("/auth/status", s.handleAuthStatus)
		s.registerChannelRoutes(r)
		s.registerCategoryRoutes(r)
		s.registerSettingsRoutes(r)
		s.registerHistoryRoutes(r)
		s.registerPostRoutes(r)
	})

	// SPA fallback (serves frontend/build/ via //go:embed).
	r.Handle("/*", spaHandler())

	return r
}

// Handler returns the http.Handler for callers that want to mount it.
func (s *Server) Handler() http.Handler { return s.r }

// Serve starts listening on addr. Blocks until the listener returns.
func (s *Server) Serve(addr string) error {
	s.http.Addr = addr
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// loggingMiddleware logs each request via slog.
func loggingMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// writeJSON sends v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends a structured error response per contracts/admin-api.md.
func writeError(w http.ResponseWriter, code int, errCode, message, field string) {
	type errBody struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Field   string `json:"field,omitempty"`
		} `json:"error"`
	}
	var b errBody
	b.Error.Code = errCode
	b.Error.Message = message
	b.Error.Field = field
	writeJSON(w, code, b)
}
