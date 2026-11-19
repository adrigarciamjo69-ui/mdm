package adminapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

type recoveryKeyJSON struct {
	KeyID      string    `json:"key_id"`
	Volume     string    `json:"volume"`
	ReportedAt time.Time `json:"reported_at"`
}

func (a *api) listRecoveryKeys(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	keys, err := a.d.Store.ListRecoveryKeys(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]recoveryKeyJSON, 0, len(keys))
	for _, k := range keys {
		out = append(out, recoveryKeyJSON{KeyID: k.KeyID, Volume: k.Volume, ReportedAt: k.ReportedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// revealRecoveryKey descifra y devuelve una clave de recuperación. Cada
// consulta queda en la auditoría.
func (a *api) revealRecoveryKey(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	meta, sealed, err := a.d.Store.RecoveryKeySecret(r.Context(), row.ID, chi.URLParam(r, "key_id"))
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este equipo no tiene custodiada esa clave.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if a.d.Secrets == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servidor no tiene configurada la clave de cifrado.")
		return
	}
	plain, err := a.d.Secrets.Open(sealed)
	if errors.Is(err, secrets.ErrUndecryptable) {
		conflict(w, "La clave está custodiada, pero no se puede descifrar con la clave actual del servidor (MDM_SECRET_KEY ha cambiado).")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.RecoveryKeyViewed, TargetType: "device", TargetID: row.ID.String(),
		Details: map[string]any{"hostname": row.Hostname, "key_id": meta.KeyID, "volume": meta.Volume}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"key_id": meta.KeyID, "volume": meta.Volume, "recovery_password": string(plain)})
}
