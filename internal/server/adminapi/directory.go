package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/directory"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type dirUserJSON struct {
	ID          string     `json:"id"`
	EntraID     string     `json:"entra_id"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	UPN         string     `json:"upn"`
	Department  string     `json:"department"`
	JobTitle    string     `json:"job_title"`
	EmployeeID  string     `json:"employee_id"`
	Phone       string     `json:"phone"`
	Office      string     `json:"office"`
	Enabled     bool       `json:"enabled"`
	DeletedAt   *time.Time `json:"deleted_at"`
	SyncedAt    time.Time  `json:"synced_at"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"`
	AssetCount  int        `json:"asset_count"`
}

func dirUserToJSON(u store.DirectoryUser, assets int) dirUserJSON {
	st := "active"
	switch {
	case u.DeletedAt != nil:
		st = "deleted"
	case !u.Enabled:
		st = "disabled"
	}
	return dirUserJSON{ID: u.ID.String(), EntraID: u.EntraID, DisplayName: u.DisplayName, Email: u.Contact(), UPN: u.UPN,
		Department: u.Department, JobTitle: u.JobTitle, EmployeeID: u.EmployeeID, Phone: u.Phone, Office: u.Office,
		Enabled: u.Enabled, DeletedAt: u.DeletedAt, SyncedAt: u.SyncedAt, Notes: u.Notes, Status: st, AssetCount: assets}
}

func (a *api) syncer() *directory.Syncer {
	return directory.NewSyncer(directory.Deps{Store: a.d.Store, Secrets: a.d.Secrets, AuthBase: a.d.AuthBase,
		GraphBase: a.d.GraphBase, Now: a.d.Now})
}

func (a *api) listDirectoryUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.DirectoryFilter{Q: strings.TrimSpace(q.Get("q")), Department: q.Get("department"), Status: q.Get("status")}
	switch f.Status {
	case "", "active", "disabled", "deleted", "leavers":
	default:
		badRequest(w, "Estado desconocido.")
		return
	}
	var ok bool
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	rows, total, err := a.d.Store.ListDirectoryUsers(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]dirUserJSON, 0, len(rows))
	for _, row := range rows {
		items = append(items, dirUserToJSON(row.DirectoryUser, row.AssetCount))
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[dirUserJSON]{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage})
}

func (a *api) directoryDepartments(w http.ResponseWriter, r *http.Request) {
	deps, err := a.d.Store.DirectoryDepartments(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, deps)
}

func (a *api) loadDirectoryUser(w http.ResponseWriter, r *http.Request) (store.DirectoryUser, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.DirectoryUser{}, false
	}
	u, err := a.d.Store.DirectoryUserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta persona no existe.")
		return store.DirectoryUser{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.DirectoryUser{}, false
	}
	return u, true
}

// directoryUserExtras añade a la ficha de una persona lo que tiene asignado.
// Las tareas de licencias y actas lo amplían.
var directoryUserExtras []func(a *api, r *http.Request, u store.DirectoryUser, out map[string]any) error

func (a *api) getDirectoryUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadDirectoryUser(w, r)
	if !ok {
		return
	}
	rows, _, err := a.d.Store.ListAssets(r.Context(), store.AssetFilter{OwnerID: &u.ID, All: true, Sort: "name"})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	assets := make([]assetJSON, 0, len(rows))
	for _, row := range rows {
		assets = append(assets, a.assetToJSON(row))
	}
	base := dirUserToJSON(u, len(assets))
	out := map[string]any{}
	if err := remarshal(base, &out); err != nil {
		a.serverError(w, r, err)
		return
	}
	out["assets"] = assets
	for _, extra := range directoryUserExtras {
		if err := extra(a, r, u, out); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) patchDirectoryUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadDirectoryUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Notes string `json:"notes"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	notes := strings.TrimSpace(body.Notes)
	if len([]rune(notes)) > maxAssetNotes {
		badRequest(w, "Las notas no pueden tener más de 4000 caracteres.")
		return
	}
	if err := a.d.Store.SetDirectoryUserNotes(r.Context(), u.ID, notes); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DirectoryUserUpdated, TargetType: "directory_user",
		TargetID: u.ID.String(), Details: map[string]any{"name": u.DisplayName}, IP: a.ip(r)})
	u.Notes = notes
	httpx.WriteJSON(w, http.StatusOK, dirUserToJSON(u, 0))
}

type directoryStatusJSON struct {
	Configured    bool       `json:"configured"`
	LastSyncAt    *time.Time `json:"last_sync_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error"`
	LastCount     int        `json:"last_count"`
	SecretExpires string     `json:"secret_expires"`
}

func statusOf(c store.EntraConfig) directoryStatusJSON {
	return directoryStatusJSON{Configured: c.TenantID != "" && c.ClientID != "" && c.SealedSecret != "",
		LastSyncAt: c.LastSyncAt, LastSuccessAt: c.LastSuccessAt, LastError: c.LastError, LastCount: c.LastCount,
		SecretExpires: dateStr(c.SecretExpires)}
}

func (a *api) directoryStatus(w http.ResponseWriter, r *http.Request) {
	c, err := a.d.Store.EntraConfig(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, statusOf(c))
}

func (a *api) syncDirectory(w http.ResponseWriter, r *http.Request) {
	sy := a.syncer()
	if _, err := sy.Credentials(r.Context()); errors.Is(err, directory.ErrNotConfigured) {
		conflict(w, "Entra ID no está configurado: un administrador debe hacerlo en Ajustes.")
		return
	}
	if err := sy.Run(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	c, err := a.d.Store.EntraConfig(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DirectorySynced,
		Details: map[string]any{"users": c.LastCount, "error": c.LastError}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, statusOf(c))
}

type entraConfigJSON struct {
	TenantID      string `json:"tenant_id"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	SecretExpires string `json:"secret_expires"`
}

func (a *api) getEntraConfig(w http.ResponseWriter, r *http.Request) {
	c, err := a.d.Store.EntraConfig(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	secret := ""
	if c.SealedSecret != "" {
		secret = secretMask
	}
	st := statusOf(c)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tenant_id": c.TenantID, "client_id": c.ClientID, "client_secret": secret,
		"secret_expires": st.SecretExpires, "configured": st.Configured, "last_sync_at": st.LastSyncAt,
		"last_success_at": st.LastSuccessAt, "last_error": st.LastError, "last_count": st.LastCount})
}

// entraInput valida el cuerpo y resuelve la máscara del secreto.
func (a *api) entraInput(w http.ResponseWriter, r *http.Request) (store.EntraConfig, directory.Credentials, bool) {
	var in entraConfigJSON
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.EntraConfig{}, directory.Credentials{}, false
	}
	in.TenantID, in.ClientID = strings.TrimSpace(in.TenantID), strings.TrimSpace(in.ClientID)
	if in.TenantID == "" || in.ClientID == "" || strings.ContainsAny(in.TenantID+in.ClientID, " /?#") {
		badRequest(w, "Indica el identificador del inquilino y el de la aplicación tal como aparecen en Entra.")
		return store.EntraConfig{}, directory.Credentials{}, false
	}
	exp, ok := parseOptDate(in.SecretExpires)
	if !ok {
		badRequest(w, "La caducidad del secreto debe tener el formato AAAA-MM-DD.")
		return store.EntraConfig{}, directory.Credentials{}, false
	}
	if a.d.Secrets == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "Falta MDM_SECRET_KEY en el servidor: no se puede guardar el secreto.")
		return store.EntraConfig{}, directory.Credentials{}, false
	}
	cfg := store.EntraConfig{TenantID: in.TenantID, ClientID: in.ClientID, SecretExpires: exp}
	secret := in.ClientSecret
	if secret == secretMask {
		prev, err := a.d.Store.EntraConfig(r.Context())
		if err != nil || prev.SealedSecret == "" {
			badRequest(w, "Escribe el secreto de cliente.")
			return store.EntraConfig{}, directory.Credentials{}, false
		}
		plain, err := a.d.Secrets.Open(prev.SealedSecret)
		if err != nil {
			badRequest(w, "No se puede leer el secreto guardado: escríbelo de nuevo.")
			return store.EntraConfig{}, directory.Credentials{}, false
		}
		cfg.SealedSecret, secret = prev.SealedSecret, string(plain)
	} else {
		if strings.TrimSpace(secret) == "" {
			badRequest(w, "Escribe el secreto de cliente.")
			return store.EntraConfig{}, directory.Credentials{}, false
		}
		sealed, err := a.d.Secrets.Seal([]byte(secret))
		if err != nil {
			a.serverError(w, r, err)
			return store.EntraConfig{}, directory.Credentials{}, false
		}
		cfg.SealedSecret = sealed
	}
	return cfg, directory.Credentials{TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: secret}, true
}

func (a *api) putEntraConfig(w http.ResponseWriter, r *http.Request) {
	cfg, _, ok := a.entraInput(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.SaveEntraConfig(r.Context(), cfg); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DirectoryConfigured,
		Details: map[string]any{"tenant_id": cfg.TenantID, "client_id": cfg.ClientID}, IP: a.ip(r)})
	a.getEntraConfig(w, r)
}

func (a *api) testEntraConfig(w http.ResponseWriter, r *http.Request) {
	_, cr, ok := a.entraInput(w, r)
	if !ok {
		return
	}
	n, err := a.syncer().Test(r.Context(), cr)
	if err != nil {
		badRequest(w, "La conexión con Entra ID ha fallado: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": n})
}

// remarshal convierte v (una estructura con etiquetas JSON) en un mapa.
func remarshal(v any, out *map[string]any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
