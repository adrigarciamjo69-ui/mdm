package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/server/alerts"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

type alertRuleJSON struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Type           string           `json:"type"`
	Kind           string           `json:"kind"`
	Params         json.RawMessage  `json:"params"`
	Severity       string           `json:"severity"`
	Channels       []string         `json:"channels"`
	Emails         []string         `json:"emails"`
	NotifyResolved bool             `json:"notify_resolved"`
	Enabled        bool             `json:"enabled"`
	Assignments    []assignmentJSON `json:"assignments"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

func toAlertRuleJSON(r store.AlertRule) alertRuleJSON {
	out := alertRuleJSON{ID: r.ID.String(), Name: r.Name, Type: r.Type, Kind: alerts.Kind(r.Type), Params: r.Params, Severity: r.Severity,
		Channels: []string{}, Emails: r.Emails, NotifyResolved: r.NotifyResolved, Enabled: r.Enabled, Assignments: []assignmentJSON{}, UpdatedAt: r.UpdatedAt}
	for _, c := range r.Channels {
		out.Channels = append(out.Channels, c.String())
	}
	for _, a := range r.Assignments {
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

type alertRuleInput struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Params         json.RawMessage `json:"params"`
	Severity       string          `json:"severity"`
	Channels       []string        `json:"channels"`
	Emails         []string        `json:"emails"`
	NotifyResolved bool            `json:"notify_resolved"`
	Enabled        bool            `json:"enabled"`
	Assignments    []struct {
		GroupID *string `json:"group_id"`
		Exclude bool    `json:"exclude"`
	} `json:"assignments"`
}

func (a *api) readAlertRule(w http.ResponseWriter, r *http.Request, existingType string) (store.AlertRule, []policy.Assignment, bool) {
	var in alertRuleInput
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.AlertRule{}, nil, false
	}
	if existingType != "" {
		in.Type = existingType
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		badRequest(w, "El nombre es obligatorio (como mucho 100 caracteres).")
		return store.AlertRule{}, nil, false
	}
	params, err := alerts.Validate(in.Type, in.Params)
	if err != nil {
		badRequest(w, err.Error())
		return store.AlertRule{}, nil, false
	}
	if in.Severity != "info" && in.Severity != "warning" && in.Severity != "critical" {
		badRequest(w, "La gravedad debe ser info, warning o critical.")
		return store.AlertRule{}, nil, false
	}
	rule := store.AlertRule{Name: name, Type: in.Type, Params: params, Severity: in.Severity, NotifyResolved: in.NotifyResolved, Enabled: in.Enabled}
	for _, c := range in.Channels {
		id, err := uuid.Parse(c)
		if err != nil {
			badRequest(w, "Algún canal no es válido.")
			return rule, nil, false
		}
		if _, err := a.d.Store.GetChannel(r.Context(), id); err != nil {
			badRequest(w, "Algún canal indicado no existe.")
			return rule, nil, false
		}
		rule.Channels = append(rule.Channels, id)
	}
	if len(in.Emails) > 20 {
		badRequest(w, "Como mucho 20 direcciones adicionales.")
		return rule, nil, false
	}
	for _, e := range in.Emails {
		if !validEmail(e) {
			badRequest(w, "La dirección «"+e+"» no es válida.")
			return rule, nil, false
		}
	}
	rule.Emails = in.Emails
	var as []policy.Assignment
	for _, x := range in.Assignments {
		var gid *uuid.UUID
		if x.GroupID != nil {
			id, err := uuid.Parse(*x.GroupID)
			if err != nil {
				badRequest(w, "Algún grupo indicado no es válido.")
				return rule, nil, false
			}
			gid = &id
		}
		as = append(as, policy.Assignment{GroupID: gid, Exclude: x.Exclude})
	}
	if as == nil {
		as = []policy.Assignment{}
	}
	return rule, as, true
}

func (a *api) listAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := a.d.Store.ListAlertRules(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]alertRuleJSON, 0, len(rules))
	for _, x := range rules {
		out = append(out, toAlertRuleJSON(x))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createAlertRule(w http.ResponseWriter, r *http.Request) {
	rule, as, ok := a.readAlertRule(w, r, "")
	if !ok {
		return
	}
	created, err := a.d.Store.CreateAlertRule(r.Context(), rule, as)
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Ya hay una regla con ese nombre.")
		return
	case errors.Is(err, store.ErrBadReference):
		badRequest(w, "Algún grupo indicado ya no existe.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AlertRuleCreated, TargetType: "alert_rule", TargetID: created.ID.String(),
		Details: map[string]any{"name": created.Name, "type": created.Type}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toAlertRuleJSON(created))
}

func (a *api) loadAlertRule(w http.ResponseWriter, r *http.Request) (store.AlertRule, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.AlertRule{}, false
	}
	rule, err := a.d.Store.GetAlertRule(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta regla no existe.")
		return rule, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return rule, false
	}
	return rule, true
}

func (a *api) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	cur, ok := a.loadAlertRule(w, r)
	if !ok {
		return
	}
	rule, as, ok := a.readAlertRule(w, r, cur.Type)
	if !ok {
		return
	}
	rule.ID = cur.ID
	err := a.d.Store.UpdateAlertRule(r.Context(), rule, as, a.d.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya hay una regla con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AlertRuleUpdated, TargetType: "alert_rule", TargetID: rule.ID.String(),
		Details: map[string]any{"name": rule.Name, "enabled": rule.Enabled}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetAlertRule(r.Context(), rule.ID)
	httpx.WriteJSON(w, http.StatusOK, toAlertRuleJSON(updated))
}

func (a *api) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	rule, ok := a.loadAlertRule(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteAlertRule(r.Context(), rule.ID, a.d.Now().UTC()); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AlertRuleDeleted, TargetType: "alert_rule", TargetID: rule.ID.String(),
		Details: map[string]any{"name": rule.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

type alertJSON struct {
	ID             int64           `json:"id"`
	RuleID         *string         `json:"rule_id"`
	RuleName       string          `json:"rule_name"`
	DeviceID       *string         `json:"device_id"`
	Hostname       string          `json:"hostname"`
	State          string          `json:"state"`
	Severity       string          `json:"severity"`
	Title          string          `json:"title"`
	Details        json.RawMessage `json:"details"`
	OpenedAt       time.Time       `json:"opened_at"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	ResolvedByHand bool            `json:"resolved_by_hand"`
}

func toAlertJSON(x store.Alert) alertJSON {
	out := alertJSON{ID: x.ID, RuleName: x.RuleName, Hostname: x.Hostname, State: x.State, Severity: x.Severity, Title: x.Title, Details: x.Details,
		OpenedAt: x.OpenedAt, AcknowledgedAt: x.AcknowledgedAt, ResolvedAt: x.ResolvedAt, ResolvedByHand: x.ResolvedBy != nil}
	if x.RuleID != nil {
		s := x.RuleID.String()
		out.RuleID = &s
	}
	if x.DeviceID != nil {
		s := x.DeviceID.String()
		out.DeviceID = &s
	}
	return out
}

func (a *api) listAlerts(w http.ResponseWriter, r *http.Request) {
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := store.AlertFilter{State: q.Get("state"), Severity: q.Get("severity"), Page: page, PerPage: perPage}
	if id, err := uuid.Parse(q.Get("rule")); err == nil {
		f.RuleID = &id
	}
	if id, err := uuid.Parse(q.Get("device")); err == nil {
		f.DeviceID = &id
	}
	list, total, err := a.d.Store.ListAlerts(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]alertJSON, 0, len(list))
	for _, x := range list {
		items = append(items, toAlertJSON(x))
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[alertJSON]{Items: items, Total: total, Page: page, PerPage: perPage})
}

func (a *api) alertCounts(w http.ResponseWriter, r *http.Request) {
	c, err := a.d.Store.OpenAlertCounts(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

func (a *api) loadAlert(w http.ResponseWriter, r *http.Request) (store.Alert, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		notFound(w, "Esta alerta no existe.")
		return store.Alert{}, false
	}
	x, err := a.d.Store.GetAlert(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta alerta no existe.")
		return x, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return x, false
	}
	return x, true
}

func (a *api) getAlert(w http.ResponseWriter, r *http.Request) {
	if x, ok := a.loadAlert(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toAlertJSON(x))
	}
}

func (a *api) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	x, ok := a.loadAlert(w, r)
	if !ok {
		return
	}
	admin := adminFrom(r.Context())
	if _, err := a.d.Store.AcknowledgeAlert(r.Context(), x.ID, admin.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AlertAcknowledged, TargetType: "alert", TargetID: strconv.FormatInt(x.ID, 10),
		Details: map[string]any{"title": x.Title}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) resolveAlert(w http.ResponseWriter, r *http.Request) {
	x, ok := a.loadAlert(w, r)
	if !ok {
		return
	}
	admin := adminFrom(r.Context())
	if _, err := a.d.Store.ResolveAlert(r.Context(), x.ID, &admin.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AlertResolved, TargetType: "alert", TargetID: strconv.FormatInt(x.ID, 10),
		Details: map[string]any{"title": x.Title}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
