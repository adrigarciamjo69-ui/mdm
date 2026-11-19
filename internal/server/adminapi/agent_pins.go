package adminapi

import (
	"errors"
	"net/http"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

// Releases describe los binarios del agente que distribuye el servidor.
type Releases interface {
	Version() string
	Binaries() []string
}

type agentPinJSON struct {
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
	Version   string `json:"version"`
}

// agentReleases devuelve la versión distribuida y los grupos congelados.
func (a *api) agentReleases(w http.ResponseWriter, r *http.Request) {
	pins, err := a.d.Store.ListAgentPins(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := struct {
		Version  string         `json:"version"`
		Binaries []string       `json:"binaries"`
		Pins     []agentPinJSON `json:"pins"`
	}{Binaries: []string{}, Pins: []agentPinJSON{}}
	if a.d.Releases != nil {
		out.Version, out.Binaries = a.d.Releases.Version(), a.d.Releases.Binaries()
	}
	for _, p := range pins {
		out.Pins = append(out.Pins, agentPinJSON{GroupID: p.GroupID.String(), GroupName: p.GroupName, Version: p.Version})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// putAgentPin congela la versión del agente en un grupo estático.
func (a *api) putAgentPin(w http.ResponseWriter, r *http.Request) {
	gid, ok := pathID(w, r, "group_id")
	if !ok {
		return
	}
	var in struct {
		Version string `json:"version"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	if in.Version == "" || !settings.ValidVersion(in.Version) {
		badRequest(w, "Indica una versión válida, por ejemplo 1.2.0.")
		return
	}
	g, ok := a.requireStaticGroup(w, r, gid)
	if !ok {
		return
	}
	if err := a.d.Store.SetAgentPin(r.Context(), gid, in.Version); err != nil {
		if errors.Is(err, store.ErrBadReference) {
			badRequest(w, "El grupo indicado ya no existe.")
			return
		}
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AgentPinned, TargetType: "group", TargetID: gid.String(),
		Details: map[string]any{"group": g.Name, "version": in.Version}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, agentPinJSON{GroupID: gid.String(), GroupName: g.Name, Version: in.Version})
}

func (a *api) deleteAgentPin(w http.ResponseWriter, r *http.Request) {
	gid, ok := pathID(w, r, "group_id")
	if !ok {
		return
	}
	if err := a.d.Store.DeleteAgentPin(r.Context(), gid); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AgentPinned, TargetType: "group", TargetID: gid.String(),
		Details: map[string]any{"version": nil}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
