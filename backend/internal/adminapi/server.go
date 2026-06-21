// Package adminapi exposes the HTTP admin panel + JSON API served by the
// assistant binary. In phase 1 there is no auth; the listener address is
// configurable and expected to be behind a reverse proxy / VPN.
package adminapi

import (
	"context"
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
}

// telegramClient is the subset of telegram.Client the admin API needs.
// The full interface is in internal/telegram; we narrow it here so tests
// can provide a minimal fake without implementing the whole Client.
type telegramClient interface {
	GetChat(ctx context.Context, handle string) (telegram.ChannelInfo, error)
}

// Server is the admin HTTP server.
type Server struct {
	deps Deps
	r    chi.Router
	http *http.Server
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
	s := &Server{deps: deps}
	s.r = s.buildRouter()
	s.http = &http.Server{
		Handler:      s.r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// buildRouter wires the route table.
func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(loggingMiddleware(s.deps.Log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		s.registerChannelRoutes(r)
		s.registerCategoryRoutes(r)
		s.registerSettingsRoutes(r)
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
