package agentapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/compliance"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

const (
	maxComplianceBody = 256 * 1024
	maxResultMessage  = 1024
)

func (a *api) getPolicy(w http.ResponseWriter, r *http.Request) {
	dev := deviceFrom(r.Context())
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), dev.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.openSecrets(dev.Platform, &eff); err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, eff)
}

// openSecrets descifra los parámetros secretos (la contraseña para unir al
// dominio, por ejemplo): el agente los necesita en claro y solo los recibe el
// propio equipo, por TLS. La huella no cambia: se calculó con los cifrados.
func (a *api) openSecrets(platform string, eff *protocol.EffectivePolicy) error {
	for i, s := range eff.Settings {
		d, ok := policy.Find(platform, s.Key)
		if !ok {
			continue
		}
		var params map[string]any
		changed := false
		for _, f := range d.Fields {
			if f.Type != policy.TypeSecret {
				continue
			}
			if params == nil {
				if err := json.Unmarshal(s.Params, &params); err != nil {
					return err
				}
			}
			v, _ := params[f.Name].(string)
			if !strings.HasPrefix(v, "v1:") {
				continue
			}
			if a.d.Secrets == nil {
				return errors.New("el servidor no tiene configurada la clave de cifrado")
			}
			plain, err := a.d.Secrets.Open(v)
			if err != nil {
				return fmt.Errorf("descifrar %s.%s: %w", s.Key, f.Name, err)
			}
			params[f.Name] = string(plain)
			changed = true
		}
		if changed {
			b, _ := json.Marshal(params)
			eff.Settings[i].Params = b
		}
	}
	return nil
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
