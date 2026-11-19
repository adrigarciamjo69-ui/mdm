package adminapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// adminJSON nunca incluye el hash de la contraseña ni el secreto TOTP.
type adminJSON struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Disabled    bool       `json:"disabled"`
	HasTOTP     bool       `json:"has_totp"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

func adminToJSON(a store.Admin) adminJSON {
	return adminJSON{ID: a.ID.String(), Email: a.Email, Role: a.Role, Disabled: a.Disabled,
		HasTOTP: a.TOTPSecret != nil, CreatedAt: a.CreatedAt, LastLoginAt: a.LastLoginAt}
}

func (a *api) listAdmins(w http.ResponseWriter, r *http.Request) {
	admins, err := a.d.Store.ListAdmins(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]adminJSON, 0, len(admins))
	for _, x := range admins {
		out = append(out, adminToJSON(x))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createAdmin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	email := strings.TrimSpace(in.Email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		badRequest(w, "Escribe una dirección de correo válida.")
		return
	}
	role, err := auth.ParseRole(in.Role)
	if err != nil {
		badRequest(w, "El rol debe ser admin, operator o readonly.")
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
	created, err := a.d.Store.CreateAdmin(r.Context(), email, hash, string(role))
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe un administrador con ese correo.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	actor := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &actor, Action: audit.AdminCreated, TargetType: "admin",
		TargetID: created.ID.String(), Details: map[string]any{"email": created.Email, "role": created.Role}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, adminToJSON(created))
}

func (a *api) updateAdmin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	if in.Role == nil && in.Disabled == nil {
		badRequest(w, "No se ha indicado ningún cambio.")
		return
	}
	if in.Role != nil {
		if _, err := auth.ParseRole(*in.Role); err != nil {
			badRequest(w, "El rol debe ser admin, operator o readonly.")
			return
		}
	}
	actor := adminFrom(r.Context())
	if id == actor.ID {
		// Evita que alguien se quede fuera por error; otro administrador sí puede hacerlo.
		conflict(w, "No puedes cambiar tu propio rol ni desactivar tu propia cuenta.")
		return
	}
	err := a.d.Store.UpdateAdmin(r.Context(), id, store.AdminPatch{Role: in.Role, Disabled: in.Disabled})
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este administrador ya no existe.")
		return
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Debe quedar al menos un administrador activo.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	updated, err := a.d.Store.AdminByID(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	details := map[string]any{"email": updated.Email}
	if in.Role != nil {
		details["role"] = *in.Role
	}
	if in.Disabled != nil {
		details["disabled"] = *in.Disabled
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &actor, Action: audit.AdminUpdated, TargetType: "admin",
		TargetID: id.String(), Details: details, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, adminToJSON(updated))
}
