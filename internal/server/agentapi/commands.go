package agentapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	maxCommandsPerCheckin = 10
	maxResultBody         = 300 * 1024
)

func commandNotFound(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Esta orden no existe para este equipo.")
}

func (a *api) commandID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		commandNotFound(w)
		return uuid.Nil, false
	}
	return id, true
}

func (a *api) startCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := a.commandID(w, r)
	if !ok {
		return
	}
	err := a.d.Store.StartCommand(r.Context(), deviceFrom(r.Context()).ID, id, a.d.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		commandNotFound(w)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) finishCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := a.commandID(w, r)
	if !ok {
		return
	}
	var res protocol.CommandResult
	if !httpx.DecodeJSON(w, r, maxResultBody, &res) {
		return
	}
	if res.Status != protocol.CommandSucceeded && res.Status != protocol.CommandFailed {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "El resultado debe ser succeeded o failed.")
		return
	}
	dev := deviceFrom(r.Context())
	c, err := a.d.Store.FinishCommand(r.Context(), dev.ID, id, res, a.d.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		commandNotFound(w)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if c.Changed && c.Type == protocol.CmdRetire && c.Status == protocol.CommandSucceeded {
		a.d.Audit.Log(r.Context(), audit.Entry{Action: audit.DeviceRetired, TargetType: "device", TargetID: dev.ID.String(),
			Details: map[string]any{"hostname": dev.Hostname, "command_id": id.String()}, IP: a.ip(r)})
	}
	w.WriteHeader(http.StatusNoContent)
}
