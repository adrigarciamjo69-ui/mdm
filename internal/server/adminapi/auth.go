package adminapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const sessionTTL = 12 * time.Hour

// dummyHash permite gastar el mismo tiempo de Argon2 cuando el correo no
// existe, de modo que el tiempo de respuesta no revele qué correos son válidos.
var (
	dummyOnce sync.Once
	dummyHash string
)

func burnPasswordCheck(pw string) {
	dummyOnce.Do(func() { dummyHash, _ = auth.HashPassword("valor-que-nunca-coincide") })
	auth.VerifyPassword(dummyHash, pw)
}

func limiterKey(ip, email string) string { return ip + "|" + strings.ToLower(email) }

func (a *api) rateLimited(w http.ResponseWriter, key string) bool {
	blocked, wait := a.d.LoginLimiter.Blocked(key)
	if !blocked {
		return false
	}
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.WriteError(w, http.StatusTooManyRequests, httpx.CodeRateLimited,
		"Demasiados intentos fallidos. Vuelve a intentarlo dentro de "+humanWait(wait)+".")
	return true
}

func humanWait(d time.Duration) string {
	m := int(d.Minutes()) + 1
	if m <= 1 {
		return "1 minuto"
	}
	return strconv.Itoa(m) + " minutos"
}

func (a *api) loginFailed(w http.ResponseWriter, r *http.Request, key, email string, admin *store.Admin, reason, message string) {
	a.d.LoginLimiter.Record(key)
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: admin, ActorEmail: email, Action: audit.LoginFailed,
		Details: map[string]any{"reason": reason}, IP: a.ip(r)})
	_ = a.d.Store.PublishEvent(r.Context(), store.Event{Type: store.EventLoginFailed, At: a.d.Now().UTC(),
		Data: map[string]any{"email": email, "ip": a.ip(r), "reason": reason}})
	httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, message)
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &req) {
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Escribe el correo y la contraseña.")
		return
	}
	key := limiterKey(a.ip(r), req.Email)
	if a.rateLimited(w, key) {
		return
	}

	const badCredentials = "Correo o contraseña incorrectos."
	admin, err := a.d.Store.AdminByEmail(r.Context(), req.Email)
	if errors.Is(err, store.ErrNotFound) {
		burnPasswordCheck(req.Password)
		a.loginFailed(w, r, key, req.Email, nil, "unknown_email", badCredentials)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !auth.VerifyPassword(admin.PasswordHash, req.Password) {
		a.loginFailed(w, r, key, req.Email, &admin, "bad_password", badCredentials)
		return
	}
	if admin.Disabled {
		a.loginFailed(w, r, key, req.Email, &admin, "disabled", badCredentials)
		return
	}

	token, tokenHash, err := auth.NewSecret()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	csrf, _, err := auth.NewSecret()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	now := a.d.Now().UTC()
	if err := a.d.Store.CreateSession(r.Context(), store.Session{
		TokenHash: tokenHash, AdminID: admin.ID, CSRFToken: csrf,
		CreatedAt: now, ExpiresAt: now.Add(sessionTTL), IP: a.ip(r),
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	next := "totp"
	if admin.TOTPSecret == nil {
		next = "totp_setup"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"next": next})
}

// totpSetup propone un secreto TOTP a un administrador que aún no tiene. El
// secreto queda en la sesión y solo pasa a la cuenta cuando se confirma con un
// código, para que abandonar el alta a medias no deje la cuenta bloqueada.
func (a *api) totpSetup(w http.ResponseWriter, r *http.Request) {
	admin, sess := adminFrom(r.Context()), sessionFrom(r.Context())
	if admin.TOTPSecret != nil {
		httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, "Esta cuenta ya tiene configurado el segundo factor.")
		return
	}
	secret, uri, err := auth.NewTOTP(admin.Email)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.SetSessionPendingTOTP(r.Context(), sess.TokenHash, secret); err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": uri})
}

func (a *api) totpVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &req) {
		return
	}
	admin, sess := adminFrom(r.Context()), sessionFrom(r.Context())
	key := limiterKey(a.ip(r), admin.Email)
	if a.rateLimited(w, key) {
		return
	}

	enrolling := admin.TOTPSecret == nil
	secret := ""
	switch {
	case !enrolling:
		secret = *admin.TOTPSecret
	case sess.PendingTOTP != nil:
		secret = *sess.PendingTOTP
	}
	code := strings.ReplaceAll(strings.TrimSpace(req.Code), " ", "")
	if secret == "" || !auth.VerifyTOTP(secret, code) {
		a.loginFailed(w, r, key, admin.Email, &admin, "bad_totp", "El código no es válido. Comprueba la hora de tu teléfono e inténtalo de nuevo.")
		return
	}

	if enrolling {
		if err := a.d.Store.SetAdminTOTP(r.Context(), admin.ID, &secret); err != nil {
			a.serverError(w, r, err)
			return
		}
		a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.TOTPEnrolled, TargetType: "admin", TargetID: admin.ID.String(), IP: a.ip(r)})
	}
	if err := a.d.Store.VerifySession(r.Context(), sess.TokenHash); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.TouchAdminLogin(r.Context(), admin.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.LoginLimiter.Reset(key)
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.LoginOK, IP: a.ip(r)})
	writeMe(w, admin, sess)
}

func writeMe(w http.ResponseWriter, admin store.Admin, sess store.Session) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"id": admin.ID.String(), "email": admin.Email, "role": admin.Role, "csrf_token": sess.CSRFToken,
	})
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	writeMe(w, adminFrom(r.Context()), sessionFrom(r.Context()))
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	admin, sess := adminFrom(r.Context()), sessionFrom(r.Context())
	if err := a.d.Store.DeleteSession(r.Context(), sess.TokenHash); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.Logout, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
