package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	maxTags       = 20
	maxTagLen     = 32
	maxUserLen    = 200
	activityRange = 90 * 24 * time.Hour
)

type deviceJSON struct {
	ID           string     `json:"id"`
	Hostname     string     `json:"hostname"`
	Platform     string     `json:"platform"`
	OSName       string     `json:"os_name"`
	OSVersion    string     `json:"os_version"`
	Arch         string     `json:"arch"`
	Serial       string     `json:"serial"`
	Manufacturer string     `json:"manufacturer"`
	Model        string     `json:"model"`
	AgentVersion string     `json:"agent_version"`
	Status       string     `json:"status"`
	Compliance   string     `json:"compliance"`
	GroupID      *string    `json:"group_id"`
	GroupName    string     `json:"group_name"`
	AssignedUser string     `json:"assigned_user"`
	Tags         []string   `json:"tags"`
	EnrolledAt   time.Time  `json:"enrolled_at"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	LastIP       string     `json:"last_ip"`
	InventoryAt  *time.Time `json:"inventory_at"`
}

type deviceDetailJSON struct {
	deviceJSON
	Inventory            json.RawMessage `json:"inventory"`
	InventoryCollectedAt *time.Time      `json:"inventory_collected_at"`
}

type pageJSON[T any] struct {
	Items   []T `json:"items"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

func (a *api) deviceToJSON(r store.DeviceRow) deviceJSON {
	d := r.Device
	out := deviceJSON{
		ID: d.ID.String(), Hostname: d.Hostname, Platform: d.Platform, OSName: d.OSName, OSVersion: d.OSVersion,
		Arch: d.Arch, Serial: d.Serial, Manufacturer: d.Manufacturer, Model: d.Model, AgentVersion: d.AgentVersion,
		Status:     string(store.StatusOf(d, a.d.Now(), a.d.CheckinIntervalS)),
		Compliance: r.Compliance,
		GroupName:  r.GroupName, AssignedUser: d.AssignedUser, Tags: d.Tags,
		EnrolledAt: d.EnrolledAt, LastSeenAt: d.LastSeenAt, LastIP: d.LastIP, InventoryAt: d.InventoryAt,
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if d.GroupID != nil {
		s := d.GroupID.String()
		out.GroupID = &s
	}
	return out
}

func (a *api) devicesToJSON(rows []store.DeviceRow) []deviceJSON {
	out := make([]deviceJSON, 0, len(rows))
	for _, r := range rows {
		out = append(out, a.deviceToJSON(r))
	}
	return out
}

// pageParams lee page y per_page. Un valor no numérico es un error; un valor
// fuera de rango se ajusta.
func pageParams(w http.ResponseWriter, r *http.Request) (page, perPage int, ok bool) {
	q := r.URL.Query()
	for name, dst := range map[string]*int{"page": &page, "per_page": &perPage} {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				badRequest(w, "El parámetro "+name+" debe ser un número.")
				return 0, 0, false
			}
			*dst = n
		}
	}
	page, perPage = store.NormalizePage(page, perPage)
	return page, perPage, true
}

// deviceFilter construye el filtro del listado a partir de la consulta.
func (a *api) deviceFilter(w http.ResponseWriter, r *http.Request) (store.DeviceFilter, bool) {
	q := r.URL.Query()
	f := store.DeviceFilter{
		Q: strings.TrimSpace(q.Get("q")), Platform: q.Get("platform"), OSVersion: q.Get("os_version"),
		Tag: strings.ToLower(strings.TrimSpace(q.Get("tag"))), Status: store.Status(q.Get("status")), Issue: q.Get("issue"),
		Compliance: q.Get("compliance"),
		Sort:       q.Get("sort"), Order: strings.ToLower(q.Get("order")),
		Now: a.d.Now(), CheckinIntervalS: a.d.CheckinIntervalS,
	}
	switch f.Platform {
	case "", protocol.PlatformWindows, protocol.PlatformLinux, "android":
	default:
		badRequest(w, "Plataforma desconocida.")
		return f, false
	}
	switch f.Status {
	case "", store.StatusOnline, store.StatusOffline, store.StatusInactive, store.StatusRevoked:
	default:
		badRequest(w, "Estado desconocido.")
		return f, false
	}
	switch f.Compliance {
	case "", "compliant", "noncompliant", "unknown":
	default:
		badRequest(w, "Estado de cumplimiento desconocido.")
		return f, false
	}
	switch f.Issue {
	case "", store.IssueLowDisk, store.IssueUnencrypted, store.IssueStale:
	default:
		badRequest(w, "Filtro de problema desconocido.")
		return f, false
	}
	if f.Sort != "" && !store.ValidDeviceSort(f.Sort) {
		badRequest(w, "No se puede ordenar por ese campo.")
		return f, false
	}
	if f.Order != "" && f.Order != "asc" && f.Order != "desc" {
		badRequest(w, "El orden debe ser asc o desc.")
		return f, false
	}
	// group_id puede repetirse: el equipo está en cualquiera de ellos.
	for _, v := range q["group_id"] {
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			badRequest(w, "El grupo indicado no es válido.")
			return f, false
		}
		f.GroupIDs = append(f.GroupIDs, id)
	}
	return f, true
}

func (a *api) listDevices(w http.ResponseWriter, r *http.Request) {
	f, ok := a.deviceFilter(w, r)
	if !ok {
		return
	}
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	rows, total, err := a.d.Store.ListDevices(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[deviceJSON]{Items: a.devicesToJSON(rows), Total: total, Page: f.Page, PerPage: f.PerPage})
}

// loadDevice lee el dispositivo de la ruta. Responde y devuelve false si el
// identificador no es válido o el dispositivo no existe.
func (a *api) loadDevice(w http.ResponseWriter, r *http.Request) (store.DeviceRow, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.DeviceRow{}, false
	}
	row, err := a.d.Store.DeviceRowByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este dispositivo ya no existe.")
		return store.DeviceRow{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.DeviceRow{}, false
	}
	return row, true
}

func (a *api) writeDetail(w http.ResponseWriter, r *http.Request, row store.DeviceRow) {
	out := deviceDetailJSON{deviceJSON: a.deviceToJSON(row), Inventory: json.RawMessage("null")}
	raw, at, err := a.d.Store.DeviceInventory(r.Context(), row.ID)
	switch {
	case err == nil:
		out.Inventory, out.InventoryCollectedAt = raw, &at
	case !errors.Is(err, store.ErrNotFound):
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) getDevice(w http.ResponseWriter, r *http.Request) {
	if row, ok := a.loadDevice(w, r); ok {
		a.writeDetail(w, r, row)
	}
}

func (a *api) deviceSoftware(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	items, total, err := a.d.Store.ListSoftware(r.Context(), row.ID, strings.TrimSpace(r.URL.Query().Get("q")), page, perPage)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[protocol.Software]{Items: items, Total: total, Page: page, PerPage: perPage})
}

func (a *api) deviceActivity(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	points, err := a.d.Store.DeviceActivity(r.Context(), row.ID, a.d.Now().Add(-activityRange))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type pointJSON struct {
		Hour     time.Time `json:"hour"`
		Checkins int       `json:"checkins"`
	}
	out := make([]pointJSON, 0, len(points))
	for _, p := range points {
		out = append(out, pointJSON{Hour: p.Hour, Checkins: p.Checkins})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// normalizeTags pasa las etiquetas a minúsculas, recorta espacios y elimina
// vacías y duplicadas conservando el orden.
func normalizeTags(in []string) ([]string, string) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if utf8.RuneCountInString(t) > maxTagLen {
			return nil, "Cada etiqueta puede tener como máximo 32 caracteres."
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > maxTags {
		return nil, "Un dispositivo puede tener como máximo 20 etiquetas."
	}
	return out, ""
}

// patchDevice cambia grupo, usuario asignado y etiquetas. Solo se tocan los
// campos presentes en el cuerpo; group_id null quita el grupo.
func (a *api) patchDevice(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	var patch store.DevicePatch
	var changed []string
	for key, raw := range body {
		switch key {
		case "group_id":
			var v *string
			if err := json.Unmarshal(raw, &v); err != nil {
				badRequest(w, "El grupo indicado no es válido.")
				return
			}
			var gid *uuid.UUID
			if v != nil && *v != "" {
				id, err := uuid.Parse(*v)
				if err != nil {
					badRequest(w, "El grupo indicado no es válido.")
					return
				}
				if _, ok := a.requireStaticGroup(w, r, id); !ok {
					return
				}
				gid = &id
			}
			patch.GroupID = &gid
		case "assigned_user":
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				badRequest(w, "El usuario asignado debe ser un texto.")
				return
			}
			v = strings.TrimSpace(store.CleanString(v))
			if utf8.RuneCountInString(v) > maxUserLen {
				badRequest(w, "El usuario asignado no puede superar los 200 caracteres.")
				return
			}
			patch.AssignedUser = &v
		case "tags":
			var v []string
			if err := json.Unmarshal(raw, &v); err != nil {
				badRequest(w, "Las etiquetas deben ser una lista de textos.")
				return
			}
			tags, msg := normalizeTags(v)
			if msg != "" {
				badRequest(w, msg)
				return
			}
			patch.Tags = &tags
		default:
			badRequest(w, "No se puede modificar el campo «"+key+"».")
			return
		}
		changed = append(changed, key)
	}
	if len(changed) == 0 {
		badRequest(w, "No se ha indicado ningún cambio.")
		return
	}
	err := a.d.Store.UpdateDevice(r.Context(), row.ID, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este dispositivo ya no existe.")
		return
	case errors.Is(err, store.ErrBadReference):
		badRequest(w, "El grupo elegido ya no existe.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	if err := store.EnqueueRecomputeDevice(r.Context(), a.d.Store.Pool(), row.ID, a.d.Now()); err != nil {
		logError(r, err) // la tarea horaria lo corregirá
	}
	sort.Strings(changed)
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeviceUpdated, TargetType: "device",
		TargetID: row.ID.String(), Details: map[string]any{"hostname": row.Hostname, "fields": changed}, IP: a.ip(r)})

	updated, err := a.d.Store.DeviceRowByID(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.writeDetail(w, r, updated)
}

func (a *api) revokeDevice(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.RevokeDevice(r.Context(), row.ID, a.d.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "Este dispositivo ya no existe.")
			return
		}
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeviceRevoked, TargetType: "device",
		TargetID: row.ID.String(), Details: map[string]any{"hostname": row.Hostname}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deleteDevice(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteDevice(r.Context(), row.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			notFound(w, "Este dispositivo ya no existe.")
			return
		}
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeviceDeleted, TargetType: "device",
		TargetID: row.ID.String(), Details: map[string]any{"hostname": row.Hostname, "serial": row.Serial}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
