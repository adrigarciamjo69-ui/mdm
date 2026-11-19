package adminapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/ratelimit"
	"mdm/internal/server/store"
	"time"
)

const audit_loginFailed = audit.LoginFailed

func storeAdminPatchDisabled(b *bool) store.AdminPatch { return store.AdminPatch{Disabled: b} }

// handlerWithProbe monta una ruta de prueba protegida con el rol mínimo dado.
func (a *testAPI) handlerWithProbe(min auth.Role) http.Handler {
	api := &api{d: Deps{Store: a.store, Audit: audit.New(a.store), LoginLimiter: ratelimit.New(5, time.Minute, a.clock.Now), Now: a.clock.Now}}
	r := chi.NewRouter()
	r.With(api.requireSession(min)).Get("/probe", func(w http.ResponseWriter, r *http.Request) {
		_ = adminFrom(r.Context())
		w.WriteHeader(200)
	})
	return r
}
