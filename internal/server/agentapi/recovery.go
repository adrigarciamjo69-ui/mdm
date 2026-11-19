package agentapi

import (
	"net/http"
	"regexp"
	"strings"

	"mdm/internal/protocol"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// Una contraseña de recuperación de BitLocker son 8 grupos de 6 cifras.
var recoveryPasswordRe = regexp.MustCompile(`^[0-9]{6}(-[0-9]{6}){7}$`)

func (a *api) postRecoveryKey(w http.ResponseWriter, r *http.Request) {
	var k protocol.RecoveryKey
	if !httpx.DecodeJSON(w, r, maxSmallBody, &k) {
		return
	}
	k.KeyID = strings.TrimSpace(store.CleanText(k.KeyID, 100))
	k.Volume = strings.TrimSpace(store.CleanText(k.Volume, 50))
	if k.KeyID == "" || k.Volume == "" || !recoveryPasswordRe.MatchString(k.RecoveryPassword) {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "La clave de recuperación no tiene el formato de BitLocker.")
		return
	}
	if a.d.Secrets == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "El servidor no tiene configurada la clave de cifrado.")
		return
	}
	sealed, err := a.d.Secrets.Seal([]byte(k.RecoveryPassword))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.SaveRecoveryKey(r.Context(), deviceFrom(r.Context()).ID, k.Volume, k.KeyID, sealed, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
