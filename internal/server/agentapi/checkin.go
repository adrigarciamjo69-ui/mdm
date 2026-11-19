package agentapi

import (
	"net/http"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

func (a *api) checkin(w http.ResponseWriter, r *http.Request) {
	var req protocol.CheckinRequest
	if !httpx.DecodeJSON(w, r, maxSmallBody, &req) {
		return
	}
	dev := deviceFrom(r.Context())
	now := a.d.Now().UTC()
	caps := cleanCapabilities(req.Capabilities)
	version := store.CleanString(req.AgentVersion)
	if dev.AgentVersion != "" && version != "" && version != dev.AgentVersion {
		a.d.Audit.Log(r.Context(), audit.Entry{Action: audit.AgentUpdated, TargetType: "device", TargetID: dev.ID.String(),
			Details: map[string]any{"hostname": dev.Hostname, "from": dev.AgentVersion, "to": version}, IP: a.ip(r)})
	}
	if err := a.d.Store.RecordCheckin(r.Context(), dev.ID, store.CheckinParams{
		AgentVersion: version, IP: a.ip(r), At: now, Capabilities: caps,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), dev.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cmds, err := a.d.Store.NextCommands(r.Context(), dev.ID, caps, now, maxCommandsPerCheckin)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	update, err := a.updateFor(r.Context(), dev, version, caps)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, protocol.CheckinResponse{
		PolicyHash:  eff.Hash,
		AgentUpdate: update,
		Commands:    cmds,
		Config: protocol.AgentConfig{
			CheckinIntervalS:   protocol.DefaultCheckinIntervalS,
			InventoryIntervalS: protocol.DefaultInventoryIntervalS,
		},
		SendInventory: needsInventory(dev, req.InventoryHash, now),
	})
}

// needsInventory decide si pedir el inventario completo: cuando el servidor no
// tiene ninguno, cuando la huella del agente difiere de la guardada, o cuando
// el guardado supera el intervalo de renovación.
func needsInventory(dev store.Device, agentHash string, now time.Time) bool {
	if dev.InventoryAt == nil || dev.InventoryHash == "" {
		return true
	}
	if agentHash != dev.InventoryHash {
		return true
	}
	return now.Sub(*dev.InventoryAt) >= protocol.DefaultInventoryIntervalS*time.Second
}

// cleanCapabilities sanea la lista de capacidades del agente: como mucho 32
// entradas cortas. Un agente de la fase 1 no envía ninguna: queda vacía.
func cleanCapabilities(caps []string) []string {
	if caps == nil {
		return []string{}
	}
	out := []string{}
	for _, c := range caps {
		c = store.CleanString(c)
		if c != "" && len(c) <= 64 && len(out) < 32 {
			out = append(out, c)
		}
	}
	return out
}
