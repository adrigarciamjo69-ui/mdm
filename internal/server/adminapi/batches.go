package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"mdm/internal/server/batches"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// commandRoles es el rol mínimo para crear cada tipo de orden.
var commandRoles = map[string]auth.Role{
	protocol.CmdReboot: auth.RoleOperator, protocol.CmdShutdown: auth.RoleOperator,
	protocol.CmdRefreshInventory: auth.RoleOperator, protocol.CmdEvaluatePolicies: auth.RoleOperator,
	protocol.CmdRunScript: auth.RoleAdmin, protocol.CmdRetire: auth.RoleAdmin,
	protocol.CmdScanUpdates: auth.RoleOperator, protocol.CmdInstallUpdatesNow: auth.RoleOperator,
	protocol.CmdLock: auth.RoleOperator, protocol.CmdLostModeStart: auth.RoleOperator, protocol.CmdLostModeStop: auth.RoleOperator,
	protocol.CmdResetPassword: auth.RoleAdmin, protocol.CmdWipe: auth.RoleAdmin,
}

type commandJSON struct {
	ID         string          `json:"id"`
	DeviceID   string          `json:"device_id"`
	Hostname   string          `json:"hostname"`
	BatchID    *string         `json:"batch_id"`
	Type       string          `json:"type"`
	Params     json.RawMessage `json:"params"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	SentAt     *time.Time      `json:"sent_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
	ExitCode   *int            `json:"exit_code"`
	Error      string          `json:"error"`
	Stdout     *string         `json:"stdout,omitempty"`
	Stderr     *string         `json:"stderr,omitempty"`
}

// publicParams quita de la carga lo que no debe mostrarse en listados: el
// contenido de un script se ve en su versión, no en cada orden.
func publicParams(typ string, payload json.RawMessage) json.RawMessage {
	if typ == protocol.CmdResetPassword {
		return json.RawMessage(`{}`) // la contraseña nueva nunca se muestra
	}
	if typ != protocol.CmdRunScript {
		return payload
	}
	var p protocol.ScriptPayload
	if json.Unmarshal(payload, &p) != nil {
		return json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(map[string]any{"script_name": p.ScriptName, "version": p.Version, "timeout_s": p.TimeoutS, "env": p.Env})
	return b
}

func toCommandJSON(c store.Command, withOutput bool) commandJSON {
	out := commandJSON{ID: c.ID.String(), DeviceID: c.DeviceID.String(), Hostname: c.Hostname, Type: c.Type,
		Params: publicParams(c.Type, c.Payload), Status: c.Status, CreatedAt: c.CreatedAt, SentAt: c.SentAt, StartedAt: c.StartedAt,
		FinishedAt: c.FinishedAt, ExpiresAt: c.ExpiresAt, ExitCode: c.ExitCode, Error: c.Error}
	if c.BatchID != nil {
		s := c.BatchID.String()
		out.BatchID = &s
	}
	if withOutput {
		out.Stdout, out.Stderr = &c.Stdout, &c.Stderr
	}
	return out
}

type batchJSON struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Params            json.RawMessage `json:"params"`
	TargetDescription string          `json:"target_description"`
	DeviceCount       int             `json:"device_count"`
	CreatedByEmail    string          `json:"created_by_email"`
	CreatedAt         time.Time       `json:"created_at"`
	Counts            map[string]int  `json:"counts"`
	Commands          []commandJSON   `json:"commands,omitempty"`
}

func toBatchJSON(b store.Batch) batchJSON {
	return batchJSON{ID: b.ID.String(), Type: b.Type, Params: publicParams(b.Type, b.Params), TargetDescription: b.TargetDescription,
		DeviceCount: b.DeviceCount, CreatedByEmail: b.CreatedByEmail, CreatedAt: b.CreatedAt, Counts: b.Counts}
}

type skippedJSON struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Reason   string `json:"reason"`
}

// buildPayload valida los parámetros de la orden y devuelve la carga que
// recibirá el agente, y los equipos de destino que no la admiten.
// run_script la completa la biblioteca de scripts (scripts.go).
var buildPayload = func(a *api, r *http.Request, typ string, params json.RawMessage, targets []store.DeviceRow) (json.RawMessage, []store.DeviceRow, []skippedJSON, error) {
	payload, keep, skipped, err := batches.Build(r.Context(), a.d.Store, a.d.Secrets, typ, params, targets)
	out := make([]skippedJSON, 0, len(skipped))
	for _, s := range skipped {
		out = append(out, skippedJSON(s))
	}
	return payload, keep, out, err
}

func (a *api) createBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Type      string          `json:"type"`
		Params    json.RawMessage `json:"params"`
		DeviceIDs []string        `json:"device_ids"`
		GroupID   string          `json:"group_id"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	min, known := commandRoles[in.Type]
	if !known {
		badRequest(w, "Tipo de orden desconocido.")
		return
	}
	admin := adminFrom(r.Context())
	if !auth.Role(admin.Role).AtLeast(min) {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "No tienes permiso para esta acción.")
		return
	}

	var targets []store.DeviceRow
	var description string
	switch {
	case in.GroupID != "" && len(in.DeviceIDs) > 0:
		badRequest(w, "Indica un grupo o una lista de equipos, no ambos.")
		return
	case in.GroupID != "":
		gid, err := uuid.Parse(in.GroupID)
		if err != nil {
			badRequest(w, "El grupo indicado no es válido.")
			return
		}
		g, err := a.d.Store.GroupByID(r.Context(), gid)
		if errors.Is(err, store.ErrNotFound) {
			badRequest(w, "El grupo indicado ya no existe.")
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		rows, _, err := a.d.Store.ListDevices(r.Context(), store.DeviceFilter{GroupID: &gid, All: true, Now: a.d.Now(), CheckinIntervalS: a.d.CheckinIntervalS})
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		targets = rows
		description = fmt.Sprintf("Grupo %s (%d equipos)", g.Name, len(rows))
	case len(in.DeviceIDs) > 0:
		if len(in.DeviceIDs) > store.MaxBatchDevices {
			badRequest(w, "Un lote admite como mucho 1.000 equipos.")
			return
		}
		for _, s := range in.DeviceIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				badRequest(w, "Algún identificador de equipo no es válido.")
				return
			}
			row, err := a.d.Store.DeviceRowByID(r.Context(), id)
			if errors.Is(err, store.ErrNotFound) {
				badRequest(w, "Algún equipo indicado ya no existe.")
				return
			}
			if err != nil {
				a.serverError(w, r, err)
				return
			}
			targets = append(targets, row)
		}
		if len(targets) == 1 {
			description = targets[0].Hostname
		} else {
			description = strconv.Itoa(len(targets)) + " equipos seleccionados"
		}
	default:
		badRequest(w, "Indica los equipos o el grupo de destino.")
		return
	}
	if len(targets) > store.MaxBatchDevices {
		badRequest(w, "Un lote admite como mucho 1.000 equipos.")
		return
	}

	// Los equipos revocados no pueden recibir órdenes.
	var skipped []skippedJSON
	active := targets[:0:0]
	for _, t := range targets {
		if t.RevokedAt != nil {
			skipped = append(skipped, skippedJSON{ID: t.ID.String(), Hostname: t.Hostname, Reason: "Equipo revocado."})
			continue
		}
		active = append(active, t)
	}
	// Cada plataforma recibe solo las órdenes que puede ejecutar.
	active, wrongPlatform := batches.FilterPlatform(in.Type, active)
	for _, s := range wrongPlatform {
		skipped = append(skipped, skippedJSON(s))
	}
	payload, active, more, err := buildPayload(a, r, in.Type, in.Params, active)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	skipped = append(skipped, more...)
	if len(active) == 0 {
		badRequest(w, "Ningún equipo de destino puede recibir esta orden.")
		return
	}
	ids := make([]uuid.UUID, len(active))
	for i, t := range active {
		ids[i] = t.ID
	}
	b, err := a.d.Store.CreateBatch(r.Context(), store.NewBatch{Type: in.Type, Params: payload, CreatedBy: admin.ID,
		CreatedByEmail: admin.Email, TargetDescription: description, At: a.d.Now().UTC()}, ids)
	if errors.Is(err, store.ErrBadReference) {
		badRequest(w, "Algún equipo indicado ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.BatchCreated, TargetType: "batch", TargetID: b.ID.String(),
		Details: map[string]any{"type": in.Type, "devices": len(ids), "target": description}, IP: a.ip(r)})

	full, err := a.d.Store.GetBatch(r.Context(), b.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	bj := toBatchJSON(full)
	cmds, err := a.d.Store.BatchCommands(r.Context(), b.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, c := range cmds {
		bj.Commands = append(bj.Commands, toCommandJSON(c, false))
	}
	if skipped == nil {
		skipped = []skippedJSON{}
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"batch": bj, "skipped": skipped})
}

func (a *api) listBatches(w http.ResponseWriter, r *http.Request) {
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	bs, total, err := a.d.Store.ListBatches(r.Context(), page, perPage)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]batchJSON, 0, len(bs))
	for _, b := range bs {
		items = append(items, toBatchJSON(b))
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[batchJSON]{Items: items, Total: total, Page: page, PerPage: perPage})
}

func (a *api) getBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	b, err := a.d.Store.GetBatch(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este lote no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cmds, err := a.d.Store.BatchCommands(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	bj := toBatchJSON(b)
	bj.Commands = []commandJSON{}
	for _, c := range cmds {
		bj.Commands = append(bj.Commands, toCommandJSON(c, false))
	}
	httpx.WriteJSON(w, http.StatusOK, bj)
}

func (a *api) deviceCommands(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	cmds, err := a.d.Store.DeviceCommands(r.Context(), row.ID, 100)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]commandJSON, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, toCommandJSON(c, false))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) getCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	c, err := a.d.Store.GetCommand(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta orden no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toCommandJSON(c, true))
}

func (a *api) cancelCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.d.Store.CancelCommand(r.Context(), id, a.d.Now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Esta orden no existe.")
		return
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Solo se pueden cancelar órdenes que aún no han empezado.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.CommandCancel, TargetType: "command", TargetID: id.String(), IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
