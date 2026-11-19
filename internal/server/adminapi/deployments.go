package adminapi

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

var deploymentStates = []string{"pending", "waiting_window", "downloading", "installing", "installed", "absent", "failed", "not_applicable"}

type deploymentJSON struct {
	ID            string           `json:"id"`
	AppID         string           `json:"app_id"`
	AppName       string           `json:"app_name"`
	AppSource     string           `json:"app_source"`
	Platform      string           `json:"platform"`
	Intent        string           `json:"intent"`
	KeepUpdated   bool             `json:"keep_updated"`
	StartAt       *time.Time       `json:"start_at"`
	RespectWindow bool             `json:"respect_window"`
	Reboot        string           `json:"reboot"`
	Assignments   []assignmentJSON `json:"assignments"`
	Counts        map[string]int   `json:"counts"`
	CreatedAt     time.Time        `json:"created_at"`
}

func toDeploymentJSON(d store.Deployment, counts map[string]int) deploymentJSON {
	out := deploymentJSON{ID: d.ID.String(), AppID: d.AppID.String(), AppName: d.AppName, AppSource: d.AppSource, Platform: d.AppPlatform,
		Intent: d.Intent, KeepUpdated: d.KeepUpdated, StartAt: d.StartAt, RespectWindow: d.RespectWindow, Reboot: d.Reboot,
		Assignments: []assignmentJSON{}, Counts: map[string]int{}, CreatedAt: d.CreatedAt}
	for _, st := range deploymentStates {
		out.Counts[st] = counts[st]
	}
	for _, a := range d.Assignments {
		aj := assignmentJSON{GroupName: a.GroupName, Exclude: a.Exclude}
		if a.GroupID != nil {
			s := a.GroupID.String()
			aj.GroupID = &s
		} else {
			aj.GroupName = "Todos los equipos"
		}
		out.Assignments = append(out.Assignments, aj)
	}
	return out
}

type deploymentInput struct {
	AppID         string     `json:"app_id"`
	Intent        string     `json:"intent"`
	KeepUpdated   bool       `json:"keep_updated"`
	StartAt       *time.Time `json:"start_at"`
	RespectWindow bool       `json:"respect_window"`
	Reboot        string     `json:"reboot"`
}

func validateDeployment(in deploymentInput, app store.App) error {
	if app.ArchivedAt != nil {
		return errors.New("La aplicación está archivada.")
	}
	if in.Intent == "available" && app.Source != "google_play" {
		return errors.New("«Disponible» solo existe en Android: la aplicación aparece en la Play Store gestionada para que el usuario la instale.")
	}
	if in.Intent != "required" && in.Intent != "uninstall" && in.Intent != "available" {
		return errors.New("La intención debe ser «required» (instalar), «uninstall» (desinstalar) o «available» (solo Android).")
	}
	if in.KeepUpdated && app.Source == "upload" {
		return errors.New("«Mantener actualizada» solo existe para winget, apt y dnf: con un instalador propio, sube la versión nueva.")
	}
	if in.Reboot != "never" && in.Reboot != "notify" && in.Reboot != "in_window" {
		return errors.New("El reinicio debe ser «never», «notify» o «in_window».")
	}
	return nil
}

func (a *api) listDeployments(w http.ResponseWriter, r *http.Request) {
	ds, err := a.d.Store.ListDeployments(r.Context(), "")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	counts, err := a.deploymentCounts(r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]deploymentJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, toDeploymentJSON(d, counts[d.ID]))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// deploymentCounts cuenta, por despliegue, los equipos a los que aplica en
// cada estado (los que aún no han informado cuentan como pendientes).
func (a *api) deploymentCounts(r *http.Request) (map[uuid.UUID]map[string]int, error) {
	out := map[uuid.UUID]map[string]int{}
	ds, err := a.d.Store.ListDeployments(r.Context(), "")
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		rows, err := a.deploymentRows(r, d)
		if err != nil {
			return nil, err
		}
		c := map[string]int{}
		for _, row := range rows {
			c[row.State]++
		}
		out[d.ID] = c
	}
	return out, nil
}

// deploymentRows devuelve una fila por equipo al que aplica el despliegue.
func (a *api) deploymentRows(r *http.Request, d store.Deployment) ([]store.DeploymentStatusRow, error) {
	pol := store.Policy{ID: d.ID, Platform: d.AppPlatform, Assignments: d.Assignments}
	devs, err := a.d.Store.DevicesForAssignments(r.Context(), pol)
	if err != nil {
		return nil, err
	}
	saved, err := a.d.Store.DeploymentStatusByID(r.Context(), d.ID)
	if err != nil {
		return nil, err
	}
	byDev := map[uuid.UUID]store.DeploymentStatusRow{}
	for _, s := range saved {
		byDev[s.DeviceID] = s
	}
	rows := make([]store.DeploymentStatusRow, 0, len(devs))
	for _, dv := range devs {
		row, ok := byDev[dv.ID]
		if !ok {
			row = store.DeploymentStatusRow{DeviceID: dv.ID, Hostname: dv.Hostname, DeploymentID: d.ID, AppName: d.AppName, Intent: d.Intent, State: "pending"}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Hostname < rows[j].Hostname })
	return rows, nil
}

func (a *api) loadDeployment(w http.ResponseWriter, r *http.Request) (store.Deployment, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.Deployment{}, false
	}
	d, err := a.d.Store.GetDeployment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este despliegue no existe.")
		return d, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return d, false
	}
	return d, true
}

func (a *api) getDeployment(w http.ResponseWriter, r *http.Request) {
	if d, ok := a.loadDeployment(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toDeploymentJSON(d, nil))
	}
}

func (a *api) createDeployment(w http.ResponseWriter, r *http.Request) {
	var in deploymentInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	appID, err := uuid.Parse(in.AppID)
	if err != nil {
		badRequest(w, "Indica la aplicación.")
		return
	}
	app, err := a.d.Store.GetApp(r.Context(), appID)
	if errors.Is(err, store.ErrNotFound) {
		badRequest(w, "La aplicación indicada no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := validateDeployment(in, app); err != nil {
		badRequest(w, err.Error())
		return
	}
	admin := adminFrom(r.Context())
	d, err := a.d.Store.CreateDeployment(r.Context(), store.Deployment{AppID: app.ID, Intent: in.Intent, KeepUpdated: in.KeepUpdated,
		StartAt: in.StartAt, RespectWindow: in.RespectWindow, Reboot: in.Reboot}, admin.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeploymentCreated, TargetType: "deployment", TargetID: d.ID.String(),
		Details: map[string]any{"app": app.Name, "intent": d.Intent}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toDeploymentJSON(d, nil))
}

func (a *api) updateDeployment(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDeployment(w, r)
	if !ok {
		return
	}
	var in deploymentInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	if in.AppID != "" && in.AppID != d.AppID.String() {
		badRequest(w, "No se puede cambiar la aplicación de un despliegue: crea otro.")
		return
	}
	app, err := a.d.Store.GetApp(r.Context(), d.AppID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := validateDeployment(in, app); err != nil {
		badRequest(w, err.Error())
		return
	}
	d.Intent, d.KeepUpdated, d.StartAt, d.RespectWindow, d.Reboot = in.Intent, in.KeepUpdated, in.StartAt, in.RespectWindow, in.Reboot
	if err := a.d.Store.UpdateDeployment(r.Context(), d, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeploymentUpdated, TargetType: "deployment", TargetID: d.ID.String(),
		Details: map[string]any{"app": d.AppName, "intent": d.Intent}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetDeployment(r.Context(), d.ID)
	httpx.WriteJSON(w, http.StatusOK, toDeploymentJSON(updated, nil))
}

func (a *api) deleteDeployment(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDeployment(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteDeployment(r.Context(), d.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeploymentDeleted, TargetType: "deployment", TargetID: d.ID.String(),
		Details: map[string]any{"app": d.AppName}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setDeploymentAssignments(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDeployment(w, r)
	if !ok {
		return
	}
	as, ok := readAssignments(w, r)
	if !ok {
		return
	}
	err := a.d.Store.SetDeploymentAssignments(r.Context(), d.ID, as, a.d.Now().UTC())
	if errors.Is(err, store.ErrBadReference) {
		badRequest(w, "Algún grupo indicado ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeploymentUpdated, TargetType: "deployment", TargetID: d.ID.String(),
		Details: map[string]any{"app": d.AppName, "assignments": len(as)}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetDeployment(r.Context(), d.ID)
	httpx.WriteJSON(w, http.StatusOK, toDeploymentJSON(updated, nil))
}

type deploymentRowJSON struct {
	DeviceID       string     `json:"device_id"`
	Hostname       string     `json:"hostname"`
	DeploymentID   string     `json:"deployment_id"`
	AppName        string     `json:"app_name"`
	Intent         string     `json:"intent"`
	State          string     `json:"state"`
	Message        string     `json:"message"`
	Attempts       int        `json:"attempts"`
	ExitCode       *int       `json:"exit_code"`
	RebootRequired bool       `json:"reboot_required"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

func toRowJSON(r store.DeploymentStatusRow) deploymentRowJSON {
	return deploymentRowJSON{DeviceID: r.DeviceID.String(), Hostname: r.Hostname, DeploymentID: r.DeploymentID.String(), AppName: r.AppName,
		Intent: r.Intent, State: r.State, Message: r.Message, Attempts: r.Attempts, ExitCode: r.ExitCode, RebootRequired: r.RebootRequired, UpdatedAt: r.UpdatedAt}
}

func (a *api) deploymentStatus(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDeployment(w, r)
	if !ok {
		return
	}
	rows, err := a.deploymentRows(r, d)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := struct {
		Counts  map[string]int      `json:"counts"`
		Devices []deploymentRowJSON `json:"devices"`
	}{Counts: map[string]int{}, Devices: []deploymentRowJSON{}}
	for _, st := range deploymentStates {
		out.Counts[st] = 0
	}
	for _, row := range rows {
		out.Counts[row.State]++
		out.Devices = append(out.Devices, toRowJSON(row))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) retryDeployment(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDeployment(w, r)
	if !ok {
		return
	}
	var in struct {
		DeviceID *string `json:"device_id"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	var dev *uuid.UUID
	if in.DeviceID != nil {
		id, err := uuid.Parse(*in.DeviceID)
		if err != nil {
			badRequest(w, "El equipo indicado no es válido.")
			return
		}
		dev = &id
	}
	n, err := a.d.Store.RetryDeployment(r.Context(), d.ID, dev, a.d.Now().UTC())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeploymentRetried, TargetType: "deployment", TargetID: d.ID.String(),
		Details: map[string]any{"app": d.AppName, "devices": n}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

// deviceDeployments devuelve los despliegues que aplican al equipo con su estado.
func (a *api) deviceDeployments(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	saved, err := a.d.Store.DeviceDeploymentStatus(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	byDep := map[string]store.DeploymentStatusRow{}
	for _, s := range saved {
		byDep[s.DeploymentID.String()] = s
	}
	out := []deploymentRowJSON{}
	for _, app := range eff.Apps {
		s, ok := byDep[app.DeploymentID]
		if !ok {
			id, _ := uuid.Parse(app.DeploymentID)
			s = store.DeploymentStatusRow{DeviceID: row.ID, Hostname: row.Hostname, DeploymentID: id, State: protocol.AppPending}
		}
		s.AppName, s.Intent = app.AppName, app.Intent
		out = append(out, toRowJSON(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// readAssignments lee {assignments: [{group_id, exclude}]} como en las políticas.
func readAssignments(w http.ResponseWriter, r *http.Request) ([]policy.Assignment, bool) {
	var in struct {
		Assignments []struct {
			GroupID *string `json:"group_id"`
			Exclude bool    `json:"exclude"`
		} `json:"assignments"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return nil, false
	}
	var as []policy.Assignment
	for _, x := range in.Assignments {
		var gid *uuid.UUID
		if x.GroupID != nil {
			id, err := uuid.Parse(*x.GroupID)
			if err != nil {
				badRequest(w, "Algún grupo indicado no es válido.")
				return nil, false
			}
			gid = &id
		} else if x.Exclude {
			badRequest(w, "«Todos los equipos» solo se puede incluir, no excluir.")
			return nil, false
		}
		as = append(as, policy.Assignment{GroupID: gid, Exclude: x.Exclude})
	}
	return as, true
}
