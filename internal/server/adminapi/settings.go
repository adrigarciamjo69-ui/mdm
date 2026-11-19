package adminapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/settings"
)

func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := settings.Load(r.Context(), a.d.Store)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

// putSettings aplica una actualización parcial: solo cambian los campos presentes.
func (a *api) putSettings(w http.ResponseWriter, r *http.Request) {
	current, err := settings.Load(r.Context(), a.d.Store)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var patch map[string]json.RawMessage
	if !httpx.DecodeJSON(w, r, maxBody, &patch) {
		return
	}
	cur, _ := json.Marshal(current)
	var merged map[string]json.RawMessage
	_ = json.Unmarshal(cur, &merged)
	var changed []string
	for k, v := range patch {
		old, known := merged[k]
		if !known {
			badRequest(w, "Ajuste desconocido: «"+k+"».")
			return
		}
		if k == "min_os_version" {
			// Se fusiona por plataforma para no borrar las no indicadas.
			var o, n map[string]string
			_ = json.Unmarshal(old, &o)
			if err := json.Unmarshal(v, &n); err != nil {
				badRequest(w, "Las versiones mínimas de sistema deben ser un objeto por plataforma.")
				return
			}
			for p, ver := range n {
				o[p] = ver
			}
			v, _ = json.Marshal(o)
		}
		if !bytes.Equal(old, v) {
			changed = append(changed, k)
		}
		merged[k] = v
	}
	var next settings.Values
	b, _ := json.Marshal(merged)
	if err := json.Unmarshal(b, &next); err != nil {
		badRequest(w, "Algún valor no tiene el tipo esperado.")
		return
	}
	if err := next.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	admin := adminFrom(r.Context())
	if err := settings.Save(r.Context(), a.d.Store, next, admin.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(changed) > 0 {
		sort.Strings(changed)
		a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.SettingsUpdated, Details: map[string]any{"fields": changed}, IP: a.ip(r)})
	}
	httpx.WriteJSON(w, http.StatusOK, next)
}
