package adminapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type auditJSON struct {
	ID         int64           `json:"id"`
	At         time.Time       `json:"at"`
	ActorEmail string          `json:"actor_email"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Details    json.RawMessage `json:"details"`
	IP         string          `json:"ip"`
}

// listAudit devuelve el registro de auditoría paginado. No existe ninguna ruta
// que lo modifique.
func (a *api) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditFilter{ActorEmail: strings.TrimSpace(q.Get("actor")), Action: q.Get("action")}
	if f.Action != "" && !slices.Contains(audit.Actions, f.Action) {
		badRequest(w, "Acción desconocida.")
		return
	}
	for name, dst := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				badRequest(w, "La fecha «"+name+"» no es válida.")
				return
			}
			*dst = &t
		}
	}
	var ok bool
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	entries, total, err := a.d.Store.ListAudit(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]auditJSON, 0, len(entries))
	for _, e := range entries {
		items = append(items, auditJSON{ID: e.ID, At: e.At, ActorEmail: e.ActorEmail, Action: e.Action,
			TargetType: e.TargetType, TargetID: e.TargetID, Details: e.Details, IP: e.IP})
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[auditJSON]{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage})
}
