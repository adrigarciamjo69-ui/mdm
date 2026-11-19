package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

var updateClasses = []string{"security", "critical", "definition", "feature_pack", "update", "driver", "other"}

type patchPolicyJSON struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Platform      string           `json:"platform"`
	Priority      int              `json:"priority"`
	AutoApprove   []string         `json:"auto_approve"`
	DeferDays     int              `json:"defer_days"`
	Window        protocol.Window  `json:"window"`
	Reboot        string           `json:"reboot"`
	NotifyMinutes int              `json:"notify_minutes"`
	LinuxScope    string           `json:"linux_scope"`
	ExcludedKBs   []string         `json:"excluded_kbs"`
	Assignments   []assignmentJSON `json:"assignments"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func toPatchPolicyJSON(p store.PatchPolicy) patchPolicyJSON {
	out := patchPolicyJSON{ID: p.ID.String(), Name: p.Name, Platform: p.Platform, Priority: p.Priority, AutoApprove: p.AutoApprove,
		DeferDays: p.DeferDays, Window: p.Window, Reboot: p.Reboot, NotifyMinutes: p.NotifyMinutes, LinuxScope: p.LinuxScope,
		ExcludedKBs: p.ExcludedKBs, Assignments: []assignmentJSON{}, UpdatedAt: p.UpdatedAt}
	for _, a := range p.Assignments {
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

func readPatchPolicy(w http.ResponseWriter, r *http.Request) (store.PatchPolicy, bool) {
	var in struct {
		Name          string          `json:"name"`
		Platform      string          `json:"platform"`
		AutoApprove   []string        `json:"auto_approve"`
		DeferDays     int             `json:"defer_days"`
		Window        protocol.Window `json:"window"`
		Reboot        string          `json:"reboot"`
		NotifyMinutes int             `json:"notify_minutes"`
		LinuxScope    string          `json:"linux_scope"`
		ExcludedKBs   []string        `json:"excluded_kbs"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.PatchPolicy{}, false
	}
	p := store.PatchPolicy{Platform: in.Platform, AutoApprove: in.AutoApprove, DeferDays: in.DeferDays, Window: in.Window,
		Reboot: in.Reboot, NotifyMinutes: in.NotifyMinutes, LinuxScope: in.LinuxScope}
	var ok bool
	if p.Name, ok = validAppText(in.Name, 100); !ok {
		badRequest(w, "El nombre es obligatorio y no puede superar los 100 caracteres.")
		return p, false
	}
	for _, c := range p.AutoApprove {
		if !slices.Contains(updateClasses, c) {
			badRequest(w, "Clasificación desconocida: «"+c+"».")
			return p, false
		}
	}
	if p.DeferDays < 0 || p.DeferDays > 60 {
		badRequest(w, "Los días de espera deben estar entre 0 y 60.")
		return p, false
	}
	if err := p.Window.Validate(); err != nil {
		badRequest(w, err.Error())
		return p, false
	}
	if p.Reboot != "never" && p.Reboot != "notify" && p.Reboot != "in_window" {
		badRequest(w, "El reinicio debe ser «never», «notify» o «in_window».")
		return p, false
	}
	if p.NotifyMinutes == 0 {
		p.NotifyMinutes = 15
	}
	if p.NotifyMinutes < 5 || p.NotifyMinutes > 120 {
		badRequest(w, "El aviso previo al reinicio debe estar entre 5 y 120 minutos.")
		return p, false
	}
	if p.LinuxScope == "" {
		p.LinuxScope = "security"
	}
	if p.LinuxScope != "security" && p.LinuxScope != "all" {
		badRequest(w, "En Linux se instalan las de seguridad («security») o todas («all»).")
		return p, false
	}
	for _, kb := range in.ExcludedKBs {
		kb = strings.ToUpper(strings.TrimSpace(kb))
		if kb != "" {
			p.ExcludedKBs = append(p.ExcludedKBs, kb)
		}
	}
	return p, true
}

func (a *api) listPatchPolicies(w http.ResponseWriter, r *http.Request) {
	ps, err := a.d.Store.ListPatchPolicies(r.Context(), r.URL.Query().Get("platform"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]patchPolicyJSON, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPatchPolicyJSON(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createPatchPolicy(w http.ResponseWriter, r *http.Request) {
	p, ok := readPatchPolicy(w, r)
	if !ok {
		return
	}
	if p.Platform != protocol.PlatformWindows && p.Platform != protocol.PlatformLinux {
		badRequest(w, "La plataforma debe ser windows o linux.")
		return
	}
	created, err := a.d.Store.CreatePatchPolicy(r.Context(), p)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una política de parches con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PatchPolicyCreated, TargetType: "patch_policy", TargetID: created.ID.String(),
		Details: map[string]any{"name": created.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toPatchPolicyJSON(created))
}

func (a *api) loadPatchPolicy(w http.ResponseWriter, r *http.Request) (store.PatchPolicy, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.PatchPolicy{}, false
	}
	p, err := a.d.Store.GetPatchPolicy(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta política de parches no existe.")
		return p, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return p, false
	}
	return p, true
}

func (a *api) getPatchPolicy(w http.ResponseWriter, r *http.Request) {
	if p, ok := a.loadPatchPolicy(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toPatchPolicyJSON(p))
	}
}

func (a *api) updatePatchPolicy(w http.ResponseWriter, r *http.Request) {
	cur, ok := a.loadPatchPolicy(w, r)
	if !ok {
		return
	}
	p, ok := readPatchPolicy(w, r)
	if !ok {
		return
	}
	if p.Platform != "" && p.Platform != cur.Platform {
		badRequest(w, "No se puede cambiar la plataforma de una política de parches.")
		return
	}
	p.ID, p.Platform = cur.ID, cur.Platform
	err := a.d.Store.UpdatePatchPolicy(r.Context(), p, a.d.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una política de parches con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PatchPolicyUpdated, TargetType: "patch_policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetPatchPolicy(r.Context(), p.ID)
	httpx.WriteJSON(w, http.StatusOK, toPatchPolicyJSON(updated))
}

func (a *api) deletePatchPolicy(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPatchPolicy(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeletePatchPolicy(r.Context(), p.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PatchPolicyDeleted, TargetType: "patch_policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setPatchPolicyAssignments(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPatchPolicy(w, r)
	if !ok {
		return
	}
	as, ok := readAssignments(w, r)
	if !ok {
		return
	}
	err := a.d.Store.SetPatchPolicyAssignments(r.Context(), p.ID, as, a.d.Now().UTC())
	if errors.Is(err, store.ErrBadReference) {
		badRequest(w, "Algún grupo indicado ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PatchPolicyUpdated, TargetType: "patch_policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name, "assignments": len(as)}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetPatchPolicy(r.Context(), p.ID)
	httpx.WriteJSON(w, http.StatusOK, toPatchPolicyJSON(updated))
}

func (a *api) reorderPatchPolicies(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	ids := make([]uuid.UUID, 0, len(in.IDs))
	for _, s := range in.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			badRequest(w, "Algún identificador no es válido.")
			return
		}
		ids = append(ids, id)
	}
	if err := a.d.Store.ReorderPatchPolicies(r.Context(), ids); errors.Is(err, store.ErrConflict) {
		conflict(w, "La lista debe contener todas las políticas de parches, una sola vez cada una.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateJSON struct {
	ID             string     `json:"id"`
	Platform       string     `json:"platform"`
	KB             string     `json:"kb"`
	Title          string     `json:"title"`
	Classification string     `json:"classification"`
	Severity       string     `json:"severity"`
	SizeBytes      int64      `json:"size_bytes"`
	PublishedAt    *time.Time `json:"published_at"`
	Security       bool       `json:"security"`
	Missing        int        `json:"missing"`
	Installed      int        `json:"installed"`
	Failed         int        `json:"failed"`
	Decision       *string    `json:"decision"`
}

func (a *api) listUpdates(w http.ResponseWriter, r *http.Request) {
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	list, total, err := a.d.Store.ListUpdates(r.Context(), store.UpdateFilter{Platform: q.Get("platform"), Classification: q.Get("classification"),
		Severity: q.Get("severity"), Query: strings.TrimSpace(q.Get("q")), Page: page, PerPage: perPage})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]updateJSON, 0, len(list))
	for _, u := range list {
		items = append(items, updateJSON{ID: u.ID, Platform: u.Platform, KB: u.KB, Title: u.Title, Classification: u.Classification,
			Severity: u.Severity, SizeBytes: u.SizeBytes, PublishedAt: u.PublishedAt, Security: u.Security,
			Missing: u.Missing, Installed: u.Installed, Failed: u.Failed, Decision: u.Decision})
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[updateJSON]{Items: items, Total: total, Page: page, PerPage: perPage})
}

// setUpdateApproval guarda una decisión manual sobre una actualización de
// Windows; null vuelve a dejar decidir a la regla automática.
func (a *api) setUpdateApproval(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in struct {
		Decision json.RawMessage `json:"decision"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	var decision string
	if len(in.Decision) > 0 && string(in.Decision) != "null" {
		if err := json.Unmarshal(in.Decision, &decision); err != nil || (decision != "approved" && decision != "declined") {
			badRequest(w, "La decisión debe ser «approved», «declined» o null.")
			return
		}
	}
	admin := adminFrom(r.Context())
	if err := a.d.Store.SetUpdateDecision(r.Context(), store.CleanText(id, 200), decision, admin.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	action := audit.UpdateApproved
	switch decision {
	case "declined":
		action = audit.UpdateDeclined
	case "":
		action = audit.UpdateReset
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: action, TargetType: "update", TargetID: id, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

// deviceUpdates devuelve el estado de parches y las actualizaciones del equipo.
func (a *api) deviceUpdates(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	ups, st, err := a.d.Store.DeviceUpdates(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	approved := map[string]bool{}
	if eff.Patches != nil {
		for _, id := range eff.Patches.ApprovedUpdateIDs {
			approved[id] = true
		}
	}
	type item struct {
		ID             string     `json:"id"`
		KB             string     `json:"kb"`
		Title          string     `json:"title"`
		Classification string     `json:"classification"`
		Severity       string     `json:"severity"`
		Security       bool       `json:"security"`
		State          string     `json:"state"`
		Approved       bool       `json:"approved"`
		FirstSeenAt    time.Time  `json:"first_seen_at"`
		InstalledAt    *time.Time `json:"installed_at"`
		LastError      string     `json:"last_error"`
	}
	out := struct {
		State struct {
			LastScanAt    *time.Time       `json:"last_scan_at"`
			LastInstallAt *time.Time       `json:"last_install_at"`
			RebootPending bool             `json:"reboot_pending"`
			Window        *protocol.Window `json:"window"`
		} `json:"state"`
		Updates []item `json:"updates"`
	}{Updates: []item{}}
	out.State.LastScanAt, out.State.LastInstallAt, out.State.RebootPending = st.LastScanAt, st.LastInstallAt, st.RebootPending
	if eff.Patches != nil {
		w := eff.Patches.Window
		out.State.Window = &w
	}
	for _, u := range ups {
		out.Updates = append(out.Updates, item{ID: u.ID, KB: u.KB, Title: u.Title, Classification: u.Classification, Severity: u.Severity,
			Security: u.Security, State: u.State, Approved: approved[u.ID], FirstSeenAt: u.FirstSeenAt, InstalledAt: u.InstalledAt, LastError: u.LastError})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
