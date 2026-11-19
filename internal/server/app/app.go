package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mdm/internal/protocol"
	"mdm/internal/server/adminapi"
	"mdm/internal/server/agentapi"
	"mdm/internal/server/audit"
	"mdm/internal/server/files"
	"mdm/internal/server/httpx"
	"mdm/internal/server/ratelimit"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

const (
	loginAttempts  = 5
	loginWindow    = 15 * time.Minute
	enrollAttempts = 10
	enrollWindow   = time.Minute

	consoleCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'"
)

// NewHandler monta todas las rutas del servidor.
func NewHandler(cfg Config, s *store.Store, static fs.FS, now func() time.Time) http.Handler {
	auditLog := audit.New(s).WithClock(now)
	releases, err := LoadReleases(cfg.DownloadsDir, cfg.Version)
	if err != nil {
		slog.Error("no se pudieron leer las versiones del agente", "err", err)
		releases = nil
	}
	fileStore := files.New(cfg.FilesDir, 1<<30)
	box, err := secrets.New(cfg.SecretKey)
	if err != nil {
		slog.Error("MDM_SECRET_KEY no válida: las funciones con datos cifrados no estarán disponibles", "err", err)
		box = nil
	}
	health := &dbHealth{store: s, ttl: 2 * time.Second}

	admin := adminapi.New(adminapi.Deps{
		Store: s, Audit: auditLog, Now: now, TrustProxy: cfg.TrustProxy,
		LoginLimiter:     ratelimit.New(loginAttempts, loginWindow, now),
		CheckinIntervalS: protocol.DefaultCheckinIntervalS,
		PublicURL:        cfg.PublicURL,
		Secrets:          box,
		Releases:         releases,
		Files:            fileStore,
		AndroidAPIBase:   cfg.AndroidAPIBase,
		Version:          cfg.Version,
	})
	agent := agentapi.New(agentapi.Deps{
		Store: s, Audit: auditLog, Now: now, TrustProxy: cfg.TrustProxy,
		EnrollLimiter: ratelimit.New(enrollAttempts, enrollWindow, now),
		Secrets:       box,
		Releases:      releases,
		Files:         fileStore,
	})

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if !health.ok(r.Context()) {
			unavailable(w)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.With(health.guard).Mount("/api/v1", admin)
	r.With(health.guard).Mount("/agent/v1", agent)
	// Cualquier otra ruta bajo /api o /agent es un error de la API, no la consola.
	apiNotFound := func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Esta ruta no existe.")
	}
	r.HandleFunc("/api/*", apiNotFound)
	r.HandleFunc("/agent/*", apiNotFound)

	r.Get("/downloads", http.NotFound)
	r.Get("/downloads/*", downloads(cfg.DownloadsDir))
	r.NotFound(console(static))
	return r
}

func unavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "30")
	httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servicio no está disponible en este momento. Inténtalo de nuevo en unos minutos.")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/agent/") {
			h.Set("Content-Security-Policy", consoleCSP)
		}
		next.ServeHTTP(w, r)
	})
}

// dbHealth recuerda durante unos segundos si la base de datos responde, para
// contestar 503 de inmediato cuando está caída sin consultarla en cada petición.
type dbHealth struct {
	store *store.Store
	ttl   time.Duration

	mu      sync.Mutex
	checked time.Time
	healthy bool
}

func (h *dbHealth) ok(ctx context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if time.Since(h.checked) < h.ttl {
		return h.healthy
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	h.healthy = h.store.Ping(pingCtx) == nil
	h.checked = time.Now()
	return h.healthy
}

func (h *dbHealth) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.ok(r.Context()) {
			unavailable(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
