package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	sessionCookie = "mdm_session"
	csrfHeader    = "X-CSRF-Token"
)

type ctxKey int

const (
	keyAdmin ctxKey = iota
	keySession
)

// adminFrom devuelve el administrador autenticado de la petición.
func adminFrom(ctx context.Context) store.Admin {
	a, _ := ctx.Value(keyAdmin).(store.Admin)
	return a
}

func sessionFrom(ctx context.Context) store.Session {
	s, _ := ctx.Value(keySession).(store.Session)
	return s
}

func unauthorized(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "Inicia sesión para continuar.")
}

// loadSession lee la cookie y devuelve la sesión vigente. Responde y devuelve
// ok=false si no hay sesión válida.
func (a *api) loadSession(w http.ResponseWriter, r *http.Request) (store.Session, store.Admin, bool) {
	ck, err := r.Cookie(sessionCookie)
	if err != nil || ck.Value == "" {
		unauthorized(w)
		return store.Session{}, store.Admin{}, false
	}
	sess, admin, err := a.d.Store.SessionByTokenHash(r.Context(), auth.HashSecret(ck.Value), a.d.Now())
	if errors.Is(err, store.ErrNotFound) {
		unauthorized(w)
		return store.Session{}, store.Admin{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.Session{}, store.Admin{}, false
	}
	return sess, admin, true
}

func withSession(r *http.Request, sess store.Session, admin store.Admin) *http.Request {
	ctx := context.WithValue(r.Context(), keyAdmin, admin)
	ctx = context.WithValue(ctx, keySession, sess)
	return r.WithContext(ctx)
}

// requireSession exige una sesión completa (con el segundo factor superado),
// el rol mínimo indicado y, en las peticiones que modifican datos, la cabecera
// X-CSRF-Token con el valor de la sesión.
func (a *api) requireSession(min auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, admin, ok := a.loadSession(w, r)
			if !ok {
				return
			}
			if !sess.Verified {
				unauthorized(w)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if !auth.ConstantTimeEqual(r.Header.Get(csrfHeader), sess.CSRFToken) {
					httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "La petición no es válida. Recarga la página e inténtalo de nuevo.")
					return
				}
			}
			if !auth.Role(admin.Role).AtLeast(min) {
				httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "No tienes permiso para hacer esto.")
				return
			}
			next.ServeHTTP(w, withSession(r, sess, admin))
		})
	}
}

// requirePartialSession admite una sesión que aún no ha superado el segundo
// factor. Solo protege las rutas de TOTP. No exige la cabecera CSRF porque el
// cliente todavía no conoce el token; la cookie SameSite=Strict y el propio
// código TOTP protegen estas rutas.
func (a *api) requirePartialSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, admin, ok := a.loadSession(w, r)
		if !ok {
			return
		}
		next.ServeHTTP(w, withSession(r, sess, admin))
	})
}

func logError(r *http.Request, err error) {
	slog.Error("error interno", "method", r.Method, "path", r.URL.Path, "err", err)
}
