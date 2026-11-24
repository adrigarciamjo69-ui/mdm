package adminapi

import (
	"errors"
	"net/http"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// changeOwnPassword cambia la contraseña de quien tiene la sesión. Pide la
// actual para que una sesión olvidada abierta no baste para quedarse la
// cuenta, y cierra las demás sesiones de esa cuenta.
func (a *api) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	admin, sess := adminFrom(r.Context()), sessionFrom(r.Context())
	key := "password|" + admin.ID.String()
	if a.rateLimited(w, key) {
		return
	}
	if !auth.VerifyPassword(admin.PasswordHash, in.Current) {
		a.d.LoginLimiter.Record(key)
		badRequest(w, "La contraseña actual no es correcta.")
		return
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.New == in.Current {
		badRequest(w, "La contraseña nueva tiene que ser distinta de la actual.")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.SetAdminPassword(r.Context(), admin.ID, hash, sess.TokenHash); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PasswordChanged, TargetType: "admin",
		TargetID: admin.ID.String(), Details: map[string]any{"email": admin.Email}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

// resetAdminPassword fija una contraseña nueva a otra cuenta (solo
// administradores) y cierra todas sus sesiones. Para la propia se usa
// changeOwnPassword, que pide la contraseña actual.
func (a *api) resetAdminPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	actor := adminFrom(r.Context())
	if id == actor.ID {
		conflict(w, "Para cambiar tu propia contraseña usa «Cambiar mi contraseña».")
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		badRequest(w, err.Error())
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	err = a.d.Store.SetAdminPassword(r.Context(), id, hash, "")
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este administrador ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	target, err := a.d.Store.AdminByID(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &actor, Action: audit.PasswordReset, TargetType: "admin",
		TargetID: id.String(), Details: map[string]any{"email": target.Email}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
