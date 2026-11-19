package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/rules"
	"mdm/internal/server/store"
)

type groupJSON struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Rule        json.RawMessage `json:"rule"`
	DeviceCount int             `json:"device_count"`
}

func toGroupJSON(g store.Group, count int) groupJSON {
	rule := g.Rule
	if len(rule) == 0 {
		rule = json.RawMessage("null")
	}
	return groupJSON{ID: g.ID.String(), Name: g.Name, Description: g.Description, Kind: g.Kind, Rule: rule, DeviceCount: count}
}

type groupInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	Rule        json.RawMessage `json:"rule"`
}

// parseRule valida la regla y la devuelve normalizada.
func parseRule(w http.ResponseWriter, raw json.RawMessage) (json.RawMessage, bool) {
	var rl rules.Rule
	if err := json.Unmarshal(raw, &rl); err != nil {
		badRequest(w, "La regla no tiene el formato esperado.")
		return nil, false
	}
	if err := rules.Validate(rl); err != nil {
		badRequest(w, err.Error())
		return nil, false
	}
	b, _ := json.Marshal(rl)
	return b, true
}

// readGroupInput valida nombre (1 a 100 caracteres) y descripción (hasta 500).
func readGroupInput(w http.ResponseWriter, r *http.Request) (groupInput, bool) {
	var in groupInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return in, false
	}
	name, ok := cleanName(in.Name, 100)
	if !ok {
		badRequest(w, "El nombre del grupo es obligatorio y no puede superar los 100 caracteres.")
		return in, false
	}
	in.Name = name
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Description) > 500 {
		badRequest(w, "La descripción no puede superar los 500 caracteres.")
		return in, false
	}
	return in, true
}

func (a *api) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := a.d.Store.ListGroups(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]groupJSON, 0, len(groups))
	for _, g := range groups {
		out = append(out, toGroupJSON(g.Group, g.DeviceCount))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createGroup(w http.ResponseWriter, r *http.Request) {
	in, ok := readGroupInput(w, r)
	if !ok {
		return
	}
	var rule json.RawMessage
	switch in.Kind {
	case "", store.GroupStatic:
		in.Kind = store.GroupStatic
		if len(in.Rule) > 0 && string(in.Rule) != "null" {
			badRequest(w, "Solo los grupos dinámicos tienen regla.")
			return
		}
	case store.GroupDynamic:
		if len(in.Rule) == 0 || string(in.Rule) == "null" {
			badRequest(w, "Un grupo dinámico necesita una regla.")
			return
		}
		if rule, ok = parseRule(w, in.Rule); !ok {
			return
		}
	default:
		badRequest(w, "El tipo de grupo debe ser «static» o «dynamic».")
		return
	}
	g, err := a.d.Store.CreateGroupWith(r.Context(), in.Name, in.Description, in.Kind, rule)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe un grupo con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	count := 0
	if g.Kind == store.GroupDynamic {
		if err := a.d.Store.RecomputeGroup(r.Context(), g.ID, a.d.Now()); err != nil {
			a.serverError(w, r, err)
			return
		}
		count, _ = a.groupCount(r, g.ID)
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.GroupCreated, TargetType: "group",
		TargetID: g.ID.String(), Details: map[string]any{"name": g.Name, "kind": g.Kind}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toGroupJSON(g, count))
}

func (a *api) groupCount(r *http.Request, id uuid.UUID) (int, error) {
	groups, err := a.d.Store.ListGroups(r.Context())
	if err != nil {
		return 0, err
	}
	for _, g := range groups {
		if g.ID == id {
			return g.DeviceCount, nil
		}
	}
	return 0, nil
}

func (a *api) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	in, ok := readGroupInput(w, r)
	if !ok {
		return
	}
	current, err := a.d.Store.GroupByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este grupo ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if in.Kind != "" && in.Kind != current.Kind {
		badRequest(w, "No se puede cambiar el tipo de un grupo. Crea uno nuevo del tipo que necesites.")
		return
	}
	hasRule := len(in.Rule) > 0 && string(in.Rule) != "null"
	if hasRule && current.Kind != store.GroupDynamic {
		badRequest(w, "Solo los grupos dinámicos tienen regla.")
		return
	}
	if hasRule {
		rule, ok := parseRule(w, in.Rule)
		if !ok {
			return
		}
		if err := a.d.Store.UpdateGroupRule(r.Context(), id, rule); err != nil {
			a.serverError(w, r, err)
			return
		}
		current.Rule = rule
	}
	err = a.d.Store.UpdateGroup(r.Context(), id, in.Name, in.Description)
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este grupo ya no existe.")
		return
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Ya existe un grupo con ese nombre.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.GroupUpdated, TargetType: "group",
		TargetID: id.String(), Details: map[string]any{"name": in.Name}, IP: a.ip(r)})
	if current.Kind == store.GroupDynamic && hasRule {
		if err := a.d.Store.RecomputeGroup(r.Context(), id, a.d.Now()); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	current.Name, current.Description = in.Name, in.Description
	count, _ := a.groupCount(r, id)
	httpx.WriteJSON(w, http.StatusOK, toGroupJSON(current, count))
}

func (a *api) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	g, err := a.d.Store.GroupByID(r.Context(), id)
	if err == nil {
		err = a.d.Store.DeleteGroup(r.Context(), id)
	}
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este grupo ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.GroupDeleted, TargetType: "group",
		TargetID: id.String(), Details: map[string]any{"name": g.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) previewGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rule json.RawMessage `json:"rule"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	raw, ok := parseRule(w, in.Rule)
	if !ok {
		return
	}
	var rl rules.Rule
	_ = json.Unmarshal(raw, &rl)
	n, sample, err := a.d.Store.PreviewRule(r.Context(), rl, a.d.Now())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type item struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
	}
	out := struct {
		Count  int    `json:"count"`
		Sample []item `json:"sample"`
	}{Count: n, Sample: []item{}}
	for _, d := range sample {
		out.Sample = append(out.Sample, item{ID: d.ID.String(), Hostname: d.Hostname})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// requireStaticGroup comprueba que el grupo exista y sea estático: solo esos
// pueden ser grupo principal de un equipo o destino de un token.
func (a *api) requireStaticGroup(w http.ResponseWriter, r *http.Request, id uuid.UUID) (store.Group, bool) {
	g, err := a.d.Store.GroupByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		badRequest(w, "El grupo elegido ya no existe.")
		return g, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return g, false
	}
	if g.Kind != store.GroupStatic {
		badRequest(w, "Un grupo dinámico no se puede asignar a mano: sus miembros los decide su regla.")
		return g, false
	}
	return g, true
}
