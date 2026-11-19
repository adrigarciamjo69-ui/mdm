// Package agentapi implementa la API que usan los agentes (/agent/v1):
// inscripción, check-in e inventario.
package agentapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"mdm/internal/server/audit"
	"mdm/internal/server/files"
	"mdm/internal/server/httpx"
	"mdm/internal/server/ratelimit"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

// Límites de tamaño de las peticiones de los agentes.
const (
	maxSmallBody             = 16 * 1024
	maxInventoryCompressed   = 1 << 20
	maxInventoryDecompressed = 8 << 20
)

type Deps struct {
	Store *store.Store
	Audit *audit.Logger
	// EnrollLimiter cuenta los intentos de inscripción fallidos por IP.
	EnrollLimiter *ratelimit.Limiter
	Now           func() time.Time
	TrustProxy    bool
	Secrets       *secrets.Box
	// Releases puede ser nulo: entonces no se ofrecen actualizaciones.
	Releases Releases
	Files    *files.Store
}

type api struct {
	d Deps
}

// New devuelve el manejador de la API de agentes, con rutas relativas.
func New(d Deps) http.Handler {
	a := &api{d: d}
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Esta ruta no existe.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeInvalidRequest, "Método no permitido.")
	})
	r.Post("/enroll", a.enroll)
	a.routes(r)
	return r
}

func (a *api) ip(r *http.Request) string { return httpx.ClientIP(r, a.d.TrustProxy) }

func (a *api) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("error interno", "method", r.Method, "path", r.URL.Path, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "Error interno del servidor.")
}

func tooManyRequests(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	httpx.WriteError(w, http.StatusTooManyRequests, httpx.CodeRateLimited, "Demasiados intentos fallidos desde esta dirección. Espera antes de reintentar.")
}
