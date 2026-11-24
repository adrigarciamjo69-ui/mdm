// Package adminapi implementa la API de la consola (/api/v1): sesiones,
// control de roles y los recursos que gestionan los administradores.
package adminapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/files"
	"mdm/internal/server/httpx"
	"mdm/internal/server/ratelimit"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

// maxBody es el tamaño máximo del cuerpo de una petición de la consola.
const maxBody = 64 * 1024

type Deps struct {
	Store        *store.Store
	Audit        *audit.Logger
	LoginLimiter *ratelimit.Limiter
	Now          func() time.Time
	// TrustProxy indica que hay un proxy inverso delante y la IP del cliente
	// viaja en X-Forwarded-For.
	TrustProxy       bool
	CheckinIntervalS int
	// PublicURL es la dirección pública del servidor, sin barra final.
	PublicURL string
	// Secrets cifra y descifra los datos sensibles (claves de recuperación…).
	Secrets *secrets.Box
	// Releases puede ser nulo.
	Releases Releases
	// Files guarda los instaladores subidos.
	Files *files.Store
	// AndroidAPIBase es la dirección de la Android Management API (vacío = la de Google).
	AndroidAPIBase string
	// Version del servidor (para el User-Agent de los webhooks).
	Version string
	// AuthBase y GraphBase sustituyen las direcciones de Microsoft (solo pruebas).
	AuthBase  string
	GraphBase string
}

type api struct {
	d Deps
}

// New devuelve el manejador de la API de consola. Las rutas son relativas:
// quien lo monta decide el prefijo (/api/v1).
func New(d Deps) http.Handler {
	a := &api{d: d}
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Esta ruta no existe.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeInvalidRequest, "Método no permitido.")
	})

	r.Post("/auth/login", a.login)
	r.Group(func(r chi.Router) {
		r.Use(a.requirePartialSession)
		r.Post("/auth/totp/setup", a.totpSetup)
		r.Post("/auth/totp/verify", a.totpVerify)
	})
	r.Group(func(r chi.Router) {
		r.Use(a.requireSession(auth.RoleReadonly))
		r.Get("/auth/me", a.me)
		r.Post("/auth/logout", a.logout)
		r.Post("/auth/password", a.changeOwnPassword)
	})
	a.routes(r)
	return r
}

func (a *api) ip(r *http.Request) string { return httpx.ClientIP(r, a.d.TrustProxy) }

func (a *api) serverError(w http.ResponseWriter, r *http.Request, err error) {
	logError(r, err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "Ha ocurrido un error inesperado. Inténtalo de nuevo.")
}
