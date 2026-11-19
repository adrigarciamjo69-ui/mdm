package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/automations"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/schedule"
	"mdm/internal/server/store"
)

type automationJSON struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Enabled     bool             `json:"enabled"`
	Trigger     json.RawMessage  `json:"trigger"`
	Actions     json.RawMessage  `json:"actions"`
	MaxDevices  int              `json:"max_devices"`
	Assignments []assignmentJSON `json:"assignments"`
	NeedsAdmin  bool             `json:"needs_admin"`
	LastRunAt   *time.Time       `json:"last_run_at"`
	NextRunAt   *time.Time       `json:"next_run_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func (a *api) toAutomationJSON(r *http.Request, x store.Automation) automationJSON {
	t, acts := automations.Parse(x)
	out := automationJSON{ID: x.ID.String(), Name: x.Name, Enabled: x.Enabled, Trigger: x.Trigger, Actions: x.Actions, MaxDevices: x.MaxDevices,
		Assignments: []assignmentJSON{}, NeedsAdmin: automations.NeedsAdmin(acts), LastRunAt: x.LastRunAt, UpdatedAt: x.UpdatedAt}
	for _, as := range x.Assignments {
		aj := assignmentJSON{GroupName: as.GroupName, Exclude: as.Exclude}
		if as.GroupID != nil {
			s := as.GroupID.String()
			aj.GroupID = &s
		} else {
			aj.GroupName = "Todos los equipos"
		}
		out.Assignments = append(out.Assignments, aj)
	}
	if x.Enabled && t.Type == "schedule" {
		from := a.d.Now()
		if x.LastRunAt != nil {
			from = *x.LastRunAt
		}
		next := schedule.Next(schedule.Spec{Frequency: t.Frequency, Time: t.Time, Day: t.Day}, from, a.scheduleLocation(r))
		out.NextRunAt = &next
	}
	return out
}

func (a *api) readAutomation(w http.ResponseWriter, r *http.Request) (store.Automation, []policy.Assignment, bool) {
	var in struct {
		Name        string          `json:"name"`
		Enabled     bool            `json:"enabled"`
		Trigger     json.RawMessage `json:"trigger"`
		Actions     json.RawMessage `json:"actions"`
		MaxDevices  int             `json:"max_devices"`
		Assignments []struct {
			GroupID *string `json:"group_id"`
			Exclude bool    `json:"exclude"`
		} `json:"assignments"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.Automation{}, nil, false
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		badRequest(w, "El nombre es obligatorio (como mucho 100 caracteres).")
		return store.Automation{}, nil, false
	}
	if in.MaxDevices == 0 {
		in.MaxDevices = 500
	}
	if in.MaxDevices < 1 || in.MaxDevices > 1000 {
		badRequest(w, "El máximo de equipos debe estar entre 1 y 1.000.")
		return store.Automation{}, nil, false
	}
	trig, acts, err := automations.Validate(in.Trigger, in.Actions)
	if err != nil {
		badRequest(w, err.Error())
		return store.Automation{}, nil, false
	}
	as := []policy.Assignment{}
	for _, x := range in.Assignments {
		var gid *uuid.UUID
		if x.GroupID != nil {
			id, err := uuid.Parse(*x.GroupID)
			if err != nil {
				badRequest(w, "Algún grupo indicado no es válido.")
				return store.Automation{}, nil, false
			}
			gid = &id
		}
		as = append(as, policy.Assignment{GroupID: gid, Exclude: x.Exclude})
	}
	return store.Automation{Name: name, Enabled: in.Enabled, Trigger: trig, Actions: acts, MaxDevices: in.MaxDevices}, as, true
}

// requireAdminFor exige administrador si las acciones lo piden.
func requireAdminFor(w http.ResponseWriter, r *http.Request, acts json.RawMessage) bool {
	var list []automations.Action
	_ = json.Unmarshal(acts, &list)
	if automations.NeedsAdmin(list) && adminFrom(r.Context()).Role != string(auth.RoleAdmin) {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "Una automatización que ejecuta scripts, reinicia o cambia de grupo solo la puede crear o editar un administrador.")
		return false
	}
	return true
}

func (a *api) listAutomations(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Store.ListAutomations(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]automationJSON, 0, len(list))
	for _, x := range list {
		out = append(out, a.toAutomationJSON(r, x))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createAutomation(w http.ResponseWriter, r *http.Request) {
	x, as, ok := a.readAutomation(w, r)
	if !ok || !requireAdminFor(w, r, x.Actions) {
		return
	}
	admin := adminFrom(r.Context())
	x.CreatedBy = admin.ID
	created, err := a.d.Store.CreateAutomation(r.Context(), x, as, a.d.Now().UTC())
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Ya hay una automatización con ese nombre.")
		return
	case errors.Is(err, store.ErrBadReference):
		badRequest(w, "Algún grupo indicado ya no existe.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AutomationCreated, TargetType: "automation", TargetID: created.ID.String(),
		Details: map[string]any{"name": created.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, a.toAutomationJSON(r, created))
}

func (a *api) loadAutomation(w http.ResponseWriter, r *http.Request) (store.Automation, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.Automation{}, false
	}
	x, err := a.d.Store.GetAutomation(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta automatización no existe.")
		return x, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return x, false
	}
	return x, true
}

func (a *api) updateAutomation(w http.ResponseWriter, r *http.Request) {
	cur, ok := a.loadAutomation(w, r)
	if !ok || !requireAdminFor(w, r, cur.Actions) {
		return
	}
	x, as, ok := a.readAutomation(w, r)
	if !ok || !requireAdminFor(w, r, x.Actions) {
		return
	}
	x.ID = cur.ID
	if err := a.d.Store.UpdateAutomation(r.Context(), x, as, a.d.Now().UTC()); errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya hay una automatización con ese nombre.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AutomationUpdated, TargetType: "automation", TargetID: x.ID.String(),
		Details: map[string]any{"name": x.Name, "enabled": x.Enabled}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetAutomation(r.Context(), x.ID)
	httpx.WriteJSON(w, http.StatusOK, a.toAutomationJSON(r, updated))
}

func (a *api) deleteAutomation(w http.ResponseWriter, r *http.Request) {
	cur, ok := a.loadAutomation(w, r)
	if !ok || !requireAdminFor(w, r, cur.Actions) {
		return
	}
	if err := a.d.Store.DeleteAutomation(r.Context(), cur.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AutomationDeleted, TargetType: "automation", TargetID: cur.ID.String(),
		Details: map[string]any{"name": cur.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) automationEngine() *automations.Engine {
	return automations.New(automations.Deps{Store: a.d.Store, Secrets: a.d.Secrets, Now: a.d.Now})
}

func (a *api) simulateAutomation(w http.ResponseWriter, r *http.Request) {
	x, ok := a.loadAutomation(w, r)
	if !ok {
		return
	}
	sim, err := a.automationEngine().Simulate(r.Context(), x)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sim)
}

func (a *api) automationRuns(w http.ResponseWriter, r *http.Request) {
	x, ok := a.loadAutomation(w, r)
	if !ok {
		return
	}
	runs, err := a.d.Store.AutomationRuns(r.Context(), x.ID, 100)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type runJSON struct {
		ID          int64           `json:"id"`
		TriggerInfo json.RawMessage `json:"trigger_info"`
		StartedAt   time.Time       `json:"started_at"`
		FinishedAt  *time.Time      `json:"finished_at"`
		Status      string          `json:"status"`
		DeviceCount int             `json:"device_count"`
		BatchIDs    []string        `json:"batch_ids"`
		Error       string          `json:"error"`
	}
	out := []runJSON{}
	for _, x := range runs {
		ids := []string{}
		for _, b := range x.BatchIDs {
			ids = append(ids, b.String())
		}
		out = append(out, runJSON{ID: x.ID, TriggerInfo: x.TriggerInfo, StartedAt: x.StartedAt, FinishedAt: x.FinishedAt, Status: x.Status,
			DeviceCount: x.DeviceCount, BatchIDs: ids, Error: x.Error})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) getAutomation(w http.ResponseWriter, r *http.Request) {
	if x, ok := a.loadAutomation(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, a.toAutomationJSON(r, x))
	}
}
