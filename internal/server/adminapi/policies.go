package adminapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

var policyPlatforms = map[string]bool{protocol.PlatformWindows: true, protocol.PlatformLinux: true, "android": true}

// secretMask sustituye a los parámetros secretos en las respuestas de la API.
const secretMask = "********"

// sealSecrets cifra los parámetros secretos de los ajustes. Si llega el
// marcador, se conserva el valor cifrado anterior del mismo ajuste.
func (a *api) sealSecrets(platform string, settings []policy.Setting, previous []policy.Setting) ([]policy.Setting, error) {
	prev := map[string]map[string]any{}
	for _, s := range previous {
		var m map[string]any
		_ = json.Unmarshal(s.Params, &m)
		prev[s.Key] = m
	}
	for i, s := range settings {
		d, _ := policy.Find(platform, s.Key)
		var params map[string]any
		_ = json.Unmarshal(s.Params, &params)
		changed := false
		for _, f := range d.Fields {
			if f.Type != policy.TypeSecret {
				continue
			}
			v, _ := params[f.Name].(string)
			switch {
			case v == secretMask:
				old, _ := prev[s.Key][f.Name].(string)
				if old == "" {
					return nil, fmt.Errorf("Escribe la contraseña de «%s».", d.Title)
				}
				params[f.Name] = old
			case strings.HasPrefix(v, "v1:"):
				// ya cifrada (no debería llegar de la consola, pero no se cifra dos veces)
			default:
				if a.d.Secrets == nil {
					return nil, errors.New("El servidor no tiene configurada la clave de cifrado.")
				}
				sealed, err := a.d.Secrets.Seal([]byte(v))
				if err != nil {
					return nil, err
				}
				params[f.Name] = sealed
			}
			changed = true
		}
		if changed {
			b, _ := json.Marshal(params)
			settings[i].Params = b
		}
	}
	return settings, nil
}

// maskSecrets oculta los parámetros secretos antes de devolver una política.
func maskSecrets(platform string, settings []policy.Setting) []policy.Setting {
	out := make([]policy.Setting, len(settings))
	for i, s := range settings {
		out[i] = s
		d, _ := policy.Find(platform, s.Key)
		var params map[string]any
		_ = json.Unmarshal(s.Params, &params)
		masked := false
		for _, f := range d.Fields {
			if f.Type == policy.TypeSecret && params[f.Name] != nil {
				params[f.Name] = secretMask
				masked = true
			}
		}
		if masked {
			b, _ := json.Marshal(params)
			out[i].Params = b
		}
	}
	return out
}

type assignmentJSON struct {
	GroupID   *string `json:"group_id"`
	GroupName string  `json:"group_name"`
	Exclude   bool    `json:"exclude"`
}

type policyJSON struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Platform    string           `json:"platform"`
	Priority    int              `json:"priority"`
	Settings    []policy.Setting `json:"settings"`
	Assignments []assignmentJSON `json:"assignments"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func toPolicyJSON(p store.Policy) policyJSON {
	out := policyJSON{ID: p.ID.String(), Name: p.Name, Description: p.Description, Platform: p.Platform, Priority: p.Priority,
		Settings: maskSecrets(p.Platform, p.Settings), Assignments: []assignmentJSON{}, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if out.Settings == nil {
		out.Settings = []policy.Setting{}
	}
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

// normalizeSettings valida y normaliza los ajustes de una política. Una clave
// solo puede aparecer una vez por política.
func normalizeSettings(platform string, in []policy.Setting) ([]policy.Setting, error) {
	out := make([]policy.Setting, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		if seen[s.Key] {
			d, _ := policy.Find(platform, s.Key)
			title := s.Key
			if d.Title != "" {
				title = d.Title
			}
			return nil, fmt.Errorf("«%s» aparece dos veces en la política.", title)
		}
		seen[s.Key] = true
		n, err := policy.Normalize(platform, s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func validPolicyName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && utf8.RuneCountInString(name) <= 100
}

func (a *api) listPolicies(w http.ResponseWriter, r *http.Request) {
	ps, err := a.d.Store.ListPolicies(r.Context(), r.URL.Query().Get("platform"))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]policyJSON, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPolicyJSON(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) loadPolicy(w http.ResponseWriter, r *http.Request) (store.Policy, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.Policy{}, false
	}
	p, err := a.d.Store.GetPolicy(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta política ya no existe.")
		return p, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return p, false
	}
	return p, true
}

func (a *api) getPolicy(w http.ResponseWriter, r *http.Request) {
	if p, ok := a.loadPolicy(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toPolicyJSON(p))
	}
}

func (a *api) createPolicy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		Platform    string           `json:"platform"`
		Settings    []policy.Setting `json:"settings"`
	}
	if !httpx.DecodeJSON(w, r, 1<<20, &in) {
		return
	}
	name, ok := validPolicyName(in.Name)
	if !ok {
		badRequest(w, "El nombre de la política es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	if !policyPlatforms[in.Platform] {
		badRequest(w, "La plataforma debe ser windows, linux o android.")
		return
	}
	settings, err := normalizeSettings(in.Platform, in.Settings)
	if err == nil {
		settings, err = a.sealSecrets(in.Platform, settings, nil)
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	admin := adminFrom(r.Context())
	p, err := a.d.Store.CreatePolicy(r.Context(), store.NewPolicy{Name: name, Description: strings.TrimSpace(in.Description),
		Platform: in.Platform, Settings: settings, CreatedBy: admin.ID})
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una política con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PolicyCreated, TargetType: "policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name, "platform": p.Platform, "settings": len(settings)}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toPolicyJSON(p))
}

func (a *api) updatePolicy(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPolicy(w, r)
	if !ok {
		return
	}
	var in struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		Settings    []policy.Setting `json:"settings"`
		Platform    *string          `json:"platform"`
	}
	if !httpx.DecodeJSON(w, r, 1<<20, &in) {
		return
	}
	if in.Platform != nil && *in.Platform != p.Platform {
		badRequest(w, "No se puede cambiar la plataforma de una política.")
		return
	}
	name, ok := validPolicyName(in.Name)
	if !ok {
		badRequest(w, "El nombre de la política es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	settings, err := normalizeSettings(p.Platform, in.Settings)
	if err == nil {
		settings, err = a.sealSecrets(p.Platform, settings, p.Settings)
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	err = a.d.Store.UpdatePolicy(r.Context(), p.ID, name, strings.TrimSpace(in.Description), settings, a.d.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una política con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PolicyUpdated, TargetType: "policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": name, "settings": len(settings)}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetPolicy(r.Context(), p.ID)
	httpx.WriteJSON(w, http.StatusOK, toPolicyJSON(updated))
}

func (a *api) deletePolicy(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPolicy(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeletePolicy(r.Context(), p.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PolicyDeleted, TargetType: "policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setPolicyAssignments(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPolicy(w, r)
	if !ok {
		return
	}
	var in struct {
		Assignments []struct {
			GroupID *string `json:"group_id"`
			Exclude bool    `json:"exclude"`
		} `json:"assignments"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	var as []policy.Assignment
	for _, x := range in.Assignments {
		var gid *uuid.UUID
		if x.GroupID != nil {
			id, err := uuid.Parse(*x.GroupID)
			if err != nil {
				badRequest(w, "Algún grupo indicado no es válido.")
				return
			}
			gid = &id
		} else if x.Exclude {
			badRequest(w, "«Todos los equipos» solo se puede incluir, no excluir.")
			return
		}
		as = append(as, policy.Assignment{GroupID: gid, Exclude: x.Exclude})
	}
	err := a.d.Store.SetAssignments(r.Context(), p.ID, as, a.d.Now().UTC())
	if errors.Is(err, store.ErrBadReference) {
		badRequest(w, "Algún grupo indicado ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PolicyAssigned, TargetType: "policy", TargetID: p.ID.String(),
		Details: map[string]any{"name": p.Name, "assignments": len(as)}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetPolicy(r.Context(), p.ID)
	httpx.WriteJSON(w, http.StatusOK, toPolicyJSON(updated))
}

func (a *api) reorderPolicies(w http.ResponseWriter, r *http.Request) {
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
	err := a.d.Store.ReorderPolicies(r.Context(), ids)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "La lista debe contener todas las políticas, una sola vez cada una. Recarga la página e inténtalo de nuevo.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.PolicyUpdated, TargetType: "policy",
		Details: map[string]any{"reordered": len(ids)}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) policyCatalog(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if !policyPlatforms[platform] {
		badRequest(w, "Indica la plataforma: windows o linux.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, policy.ForPlatform(platform))
}

func (a *api) policyDevices(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadPolicy(w, r)
	if !ok {
		return
	}
	devs, err := a.d.Store.PolicyDevices(r.Context(), p.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type item struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
	}
	out := make([]item, 0, len(devs))
	for _, d := range devs {
		out = append(out, item{ID: d.ID.String(), Hostname: d.Hostname, Status: string(store.StatusOf(d.Device, a.d.Now(), a.d.CheckinIntervalS))})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
