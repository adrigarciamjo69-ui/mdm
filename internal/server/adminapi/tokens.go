package adminapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type tokenJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	State     string     `json:"state"`
	GroupID   *string    `json:"group_id"`
	GroupName string     `json:"group_name"`
	ExpiresAt *time.Time `json:"expires_at"`
	MaxUses   *int       `json:"max_uses"`
	Uses      int        `json:"uses"`
	CreatedAt time.Time  `json:"created_at"`
}

// tokenCreatedJSON es la única respuesta que lleva el token en claro.
type tokenCreatedJSON struct {
	tokenJSON
	Token   string          `json:"token"`
	Install installCommands `json:"install"`
}

type installCommands struct {
	Linux   string `json:"linux"`
	Windows string `json:"windows"`
}

func (a *api) tokenToJSON(t store.EnrollmentToken, groupName string) tokenJSON {
	out := tokenJSON{ID: t.ID.String(), Name: t.Name, State: store.TokenState(t, a.d.Now()), GroupName: groupName,
		ExpiresAt: t.ExpiresAt, MaxUses: t.MaxUses, Uses: t.Uses, CreatedAt: t.CreatedAt}
	if t.GroupID != nil {
		s := t.GroupID.String()
		out.GroupID = &s
	}
	return out
}

// installFor compone los comandos de instalación del agente con el token.
func (a *api) installFor(token string) installCommands {
	u := a.d.PublicURL
	return installCommands{
		Linux:   "curl -fsSL " + u + "/downloads/install.sh | sudo sh -s -- --server " + u + " --token " + token,
		Windows: `powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm ` + u + `/downloads/install.ps1))) -Server ` + u + ` -Token ` + token + `"`,
	}
}

func (a *api) listTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := a.d.Store.ListTokens(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]tokenJSON, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, a.tokenToJSON(t.EnrollmentToken, t.GroupName))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string     `json:"name"`
		GroupID   *string    `json:"group_id"`
		ExpiresAt *time.Time `json:"expires_at"`
		MaxUses   *int       `json:"max_uses"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	name, ok := cleanName(in.Name, 100)
	if !ok {
		badRequest(w, "El nombre del token es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	if in.MaxUses != nil && (*in.MaxUses < 1 || *in.MaxUses > 100000) {
		badRequest(w, "El número máximo de usos debe estar entre 1 y 100.000.")
		return
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(a.d.Now()) {
		badRequest(w, "La fecha de caducidad debe ser futura.")
		return
	}
	admin := adminFrom(r.Context())
	p := store.TokenParams{Name: name, ExpiresAt: in.ExpiresAt, MaxUses: in.MaxUses, CreatedBy: admin.ID}
	groupName := ""
	if in.GroupID != nil && *in.GroupID != "" {
		gid, err := uuid.Parse(*in.GroupID)
		if err != nil {
			badRequest(w, "El grupo elegido no es válido.")
			return
		}
		g, ok := a.requireStaticGroup(w, r, gid)
		if !ok {
			return
		}
		p.GroupID, groupName = &gid, g.Name
	}
	plain, hash, err := auth.NewSecret()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.TokenHash = hash
	t, err := a.d.Store.CreateToken(r.Context(), p)
	if errors.Is(err, store.ErrBadReference) { // el grupo se borró entre la comprobación y la inserción
		badRequest(w, "El grupo elegido ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.TokenCreated, TargetType: "token",
		TargetID: t.ID.String(), Details: map[string]any{"name": t.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, tokenCreatedJSON{tokenJSON: a.tokenToJSON(t, groupName), Token: plain, Install: a.installFor(plain)})
}

func (a *api) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.d.Store.RevokeToken(r.Context(), id, a.d.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este token ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.TokenRevoked, TargetType: "token", TargetID: id.String(), IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
