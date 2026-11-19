package adminapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/mobile/android"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

// androidClient construye el cliente de Google con la clave guardada.
func (a *api) androidClient(r *http.Request) (*android.Client, store.AndroidConfig, error) {
	cfg, err := a.d.Store.AndroidConfig(r.Context())
	if err != nil {
		return nil, cfg, err
	}
	if a.d.Secrets == nil {
		return nil, cfg, errors.New("el servidor no tiene configurada MDM_SECRET_KEY")
	}
	raw, err := a.d.Secrets.Open(cfg.SealedKey)
	if err != nil {
		return nil, cfg, err
	}
	sa, err := android.ParseServiceAccount(raw)
	if err != nil {
		return nil, cfg, err
	}
	return android.NewClient(sa, a.d.AndroidAPIBase, nil), cfg, nil
}

func (a *api) androidStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.d.Store.AndroidConfig(r.Context())
	out := map[string]any{"configured": false, "linked": false}
	if err == nil {
		out = map[string]any{"configured": true, "project_id": cfg.ProjectID, "client_email": cfg.ClientEmail,
			"linked": cfg.EnterpriseName != "", "enterprise_name": cfg.EnterpriseName, "enterprise_display_name": cfg.EnterpriseDisplayName,
			"linked_at": cfg.LinkedAt, "last_sync_at": cfg.LastSyncAt, "last_sync_error": cfg.LastSyncError}
	} else if !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// putAndroidKey valida la clave de la cuenta de servicio (incluido pedir un
// token a Google) y la guarda cifrada.
func (a *api) putAndroidKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KeyJSON string `json:"key_json"`
	}
	if !httpx.DecodeJSON(w, r, 64*1024, &in) {
		return
	}
	sa, err := android.ParseServiceAccount([]byte(in.KeyJSON))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := android.NewClient(sa, a.d.AndroidAPIBase, nil).CheckToken(r.Context()); err != nil {
		badRequest(w, "Google no acepta esta clave: "+err.Error()+".")
		return
	}
	if a.d.Secrets == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servidor no tiene configurada la clave de cifrado.")
		return
	}
	sealed, err := a.d.Secrets.Seal([]byte(in.KeyJSON))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.SaveAndroidKey(r.Context(), sealed, sa.ProjectID, sa.ClientEmail, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AndroidConfigured, TargetType: "integration", TargetID: "android",
		Details: map[string]any{"project_id": sa.ProjectID, "client_email": sa.ClientEmail}, IP: a.ip(r)})
	a.androidStatus(w, r)
}

func randomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// androidSignup inicia el registro de la empresa y devuelve la dirección de Google.
func (a *api) androidSignup(w http.ResponseWriter, r *http.Request) {
	c, _, err := a.androidClient(r)
	if errors.Is(err, store.ErrNotFound) {
		conflict(w, "Primero sube la clave de la cuenta de servicio de Google.")
		return
	}
	if errors.Is(err, secrets.ErrUndecryptable) {
		conflict(w, "La clave de Google guardada no se puede descifrar con la clave actual del servidor: vuelve a subirla.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	state := randomState()
	callback := a.d.PublicURL + "/api/v1/integrations/android/callback?state=" + state
	su, err := c.CreateSignupURL(r.Context(), callback)
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "Google no ha aceptado la petición: "+err.Error())
		return
	}
	admin := adminFrom(r.Context())
	if err := a.d.Store.SetAndroidSignup(r.Context(), su.Name, state, admin.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"url": su.URL})
}

// androidCallback es la vuelta del navegador desde Google. No lleva sesión
// (la cookie SameSite=Strict no viaja en esa redirección): la protege el
// state de un solo uso generado en androidSignup.
func (a *api) androidCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cfg, err := a.d.Store.AndroidConfig(r.Context())
	if err != nil || cfg.SignupState == "" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(cfg.SignupState)) != 1 || q.Get("enterpriseToken") == "" {
		badRequest(w, "Este enlace de vuelta no es válido o ya se usó. Inicia la vinculación otra vez desde la consola.")
		return
	}
	c, _, err := a.androidClient(r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	e, err := c.CreateEnterprise(r.Context(), cfg.SignupURLName, q.Get("enterpriseToken"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "Google no ha completado la vinculación: "+err.Error())
		return
	}
	if err := a.d.Store.LinkAndroidEnterprise(r.Context(), e.Name, e.DisplayName, cfg.SignupBy, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	entry := audit.Entry{Action: audit.AndroidLinked, TargetType: "integration", TargetID: "android",
		Details: map[string]any{"enterprise": e.Name, "display_name": e.DisplayName}, IP: a.ip(r)}
	if cfg.SignupBy != nil {
		if admin, err := a.d.Store.AdminByID(r.Context(), *cfg.SignupBy); err == nil {
			entry.Actor = &admin
		}
	}
	a.d.Audit.Log(r.Context(), entry)
	http.Redirect(w, r, "/integraciones/android?vinculada=1", http.StatusFound)
}

func (a *api) androidUnlink(w http.ResponseWriter, r *http.Request) {
	c, cfg, err := a.androidClient(r)
	if errors.Is(err, store.ErrNotFound) || (err == nil && cfg.EnterpriseName == "") {
		conflict(w, "No hay ninguna empresa vinculada.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := c.DeleteEnterprise(r.Context(), cfg.EnterpriseName); err != nil {
		var apiErr *android.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			httpx.WriteError(w, http.StatusBadGateway, httpx.CodeUnavailable, "Google no ha borrado la empresa: "+err.Error())
			return
		}
	}
	if err := a.d.Store.UnlinkAndroidEnterprise(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	// Google restablece los móviles: dejan de estar gestionados.
	if _, err := a.d.Store.RevokeAndroidMissing(r.Context(), []string{}, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AndroidUnlinked, TargetType: "integration", TargetID: "android",
		Details: map[string]any{"enterprise": cfg.EnterpriseName}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
