package agentapi

import (
	"net/http"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/compliance"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	maxComplianceBody = 256 * 1024
	maxResultMessage  = 1024
)

func (a *api) getPolicy(w http.ResponseWriter, r *http.Request) {
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), deviceFrom(r.Context()).ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, eff)
}

var validStatus = map[string]bool{
	protocol.StatusCompliant: true, protocol.StatusNoncompliant: true, protocol.StatusError: true, protocol.StatusNotApplicable: true,
}

func (a *api) postCompliance(w http.ResponseWriter, r *http.Request) {
	var rep protocol.ComplianceReport
	if !httpx.DecodeJSON(w, r, maxComplianceBody, &rep) {
		return
	}
	for i := range rep.Results {
		res := &rep.Results[i]
		if !validStatus[res.Status] {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Estado de ajuste desconocido: "+res.Status)
			return
		}
		if _, err := uuid.Parse(res.PolicyID); err != nil || res.Key == "" {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "Cada resultado necesita la política y la clave del ajuste.")
			return
		}
		res.Key = store.CleanString(res.Key)
		res.Message = store.CleanText(res.Message, maxResultMessage)
	}
	rep.PolicyHash = store.CleanString(rep.PolicyHash)
	dev := deviceFrom(r.Context())
	now := a.d.Now().UTC()
	if err := a.d.Store.SaveComplianceReport(r.Context(), dev.ID, rep, now); err != nil {
		a.serverError(w, r, err)
		return
	}
	changed, err := compliance.Recompute(r.Context(), a.d.Store, dev.ID, now)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if changed {
		// Los grupos dinámicos pueden depender del cumplimiento.
		_ = store.EnqueueRecomputeDevice(r.Context(), a.d.Store.Pool(), dev.ID, now)
	}
	w.WriteHeader(http.StatusNoContent)
}
