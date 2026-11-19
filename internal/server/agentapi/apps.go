package agentapi

import (
	"net/http"

	"mdm/internal/protocol"
	"mdm/internal/server/httpx"
)

var appStates = map[string]bool{
	protocol.AppPending: true, protocol.AppWaitingWindow: true, protocol.AppDownloading: true, protocol.AppInstalling: true,
	protocol.AppInstalled: true, protocol.AppAbsent: true, protocol.AppFailed: true, protocol.AppNotApplicable: true,
}

// postAppStatus guarda el estado de los despliegues del equipo. Los que no le
// aplican se ignoran.
func (a *api) postAppStatus(w http.ResponseWriter, r *http.Request) {
	var rep protocol.AppStatusReport
	if !httpx.DecodeJSON(w, r, 256*1024, &rep) {
		return
	}
	for _, it := range rep.Items {
		if !appStates[it.State] {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Estado de despliegue desconocido: "+it.State)
			return
		}
	}
	dev := deviceFrom(r.Context())
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), dev.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	mine := map[string]bool{}
	for _, app := range eff.Apps {
		mine[app.DeploymentID] = true
	}
	keep := rep.Items[:0]
	now := a.d.Now().UTC()
	for _, it := range rep.Items {
		if mine[it.DeploymentID] {
			it.UpdatedAt = now
			keep = append(keep, it)
		}
	}
	if err := a.d.Store.SaveAppStatus(r.Context(), dev.ID, keep); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
