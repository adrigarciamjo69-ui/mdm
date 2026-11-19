package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/notify"
	"mdm/internal/server/store"
)

const (
	notifyDelay      = 2 * time.Minute
	digestThreshold  = 5
	eventAlertMaxAge = 7 * 24 * time.Hour
	loginWindow      = 15 * time.Minute
)

type Deps struct {
	Store     *store.Store
	Now       func() time.Time
	PublicURL string
	Log       *slog.Logger
}

type Engine struct{ d Deps }

func New(d Deps) *Engine {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Engine{d: d}
}

type ruleParams struct {
	Hours        int    `json:"hours"`
	Percent      int    `json:"percent"`
	Days         int    `json:"days"`
	Count        int    `json:"count"`
	PolicyID     string `json:"policy_id"`
	DeploymentID string `json:"deployment_id"`
	ScriptID     string `json:"script_id"`
}

func params(r store.AlertRule) ruleParams {
	var p ruleParams
	_ = json.Unmarshal(r.Params, &p)
	return p
}

// matches devuelve los equipos que cumplen la condición de una regla de estado.
func (e *Engine) matches(ctx context.Context, r store.AlertRule, now time.Time) ([]store.Match, error) {
	p := params(r)
	switch r.Type {
	case TypeDeviceOffline:
		return e.d.Store.OfflineDevices(ctx, now.Add(-time.Duration(p.Hours)*time.Hour))
	case TypeDiskLow:
		return e.d.Store.LowDiskDevices(ctx, float64(p.Percent)/100)
	case TypeNoncompliant:
		var pid *uuid.UUID
		if id, err := uuid.Parse(p.PolicyID); err == nil {
			pid = &id
		}
		return e.d.Store.NoncompliantDevices(ctx, pid)
	case TypeAndroidIntegrity:
		return e.d.Store.AndroidIntegrityDevices(ctx)
	case TypeSecurityPending:
		return e.d.Store.SecurityPendingDevices(ctx, now.Add(-time.Duration(p.Days)*24*time.Hour))
	case TypeBitlockerMissing:
		return e.d.Store.BitlockerKeyMissing(ctx)
	}
	return nil, nil
}

func stateTitle(r store.AlertRule, host string, m store.Match) string {
	p := params(r)
	switch r.Type {
	case TypeDeviceOffline:
		return fmt.Sprintf("%s lleva más de %d h sin conectar", host, p.Hours)
	case TypeDiskLow:
		if m.Detail != nil {
			return fmt.Sprintf("%s: poco espacio en %v (%v %% libre)", host, m.Detail["mount"], m.Detail["free_percent"])
		}
		return host + ": poco espacio en disco"
	case TypeNoncompliant:
		return host + " no cumple las políticas"
	case TypeAndroidIntegrity:
		return host + ": Google duda de la integridad del dispositivo"
	case TypeSecurityPending:
		return fmt.Sprintf("%s tiene parches de seguridad pendientes desde hace más de %d días", host, p.Days)
	case TypeBitlockerMissing:
		return host + ": la clave de recuperación de BitLocker no está custodiada"
	}
	return host + ": " + r.Name
}

// EvaluateState evalúa las reglas de estado (tarea alerts.evaluate, cada 5
// minutos) y cierra las alertas de suceso de más de 7 días.
func (e *Engine) EvaluateState(ctx context.Context) {
	now := e.d.Now().UTC()
	if _, err := e.d.Store.ResolveOldEventAlerts(ctx, EventTypes, now.Add(-eventAlertMaxAge), now); err != nil {
		e.d.Log.Error("alertas: cerrar antiguas", "err", err)
	}
	rules, err := e.d.Store.ListAlertRules(ctx)
	if err != nil {
		e.d.Log.Error("alertas: leer reglas", "err", err)
		return
	}
	for _, r := range rules {
		if !r.Enabled || Kind(r.Type) != "state" {
			continue
		}
		if err := e.evaluateRule(ctx, r, now); err != nil {
			e.d.Log.Error("alertas: evaluar regla", "rule", r.Name, "err", err)
		}
	}
}

func (e *Engine) evaluateRule(ctx context.Context, r store.AlertRule, now time.Time) error {
	scope, err := e.d.Store.DevicesInScope(ctx, r.Assignments)
	if err != nil {
		return err
	}
	matches, err := e.matches(ctx, r, now)
	if err != nil {
		return err
	}
	open, err := e.d.Store.UnresolvedAlerts(ctx, r.ID)
	if err != nil {
		return err
	}
	isOpen := map[uuid.UUID]bool{}
	for _, a := range open {
		if a.DeviceID != nil {
			isOpen[*a.DeviceID] = true
		}
	}
	current := map[uuid.UUID]bool{}
	opened, resolved := false, false
	for _, m := range matches {
		dev, ok := scope[m.DeviceID]
		if !ok {
			continue
		}
		current[m.DeviceID] = true
		if isOpen[m.DeviceID] {
			continue
		}
		details, _ := json.Marshal(m.Detail)
		id := m.DeviceID
		rid := r.ID
		if _, ok, err := e.d.Store.OpenAlert(ctx, store.Alert{RuleID: &rid, RuleName: r.Name, DeviceID: &id, Severity: r.Severity,
			Title: stateTitle(r, dev.Hostname, m), Details: details, OpenedAt: now}); err != nil {
			return err
		} else if ok {
			opened = true
		}
	}
	for _, a := range open {
		if a.DeviceID != nil && !current[*a.DeviceID] {
			if ok, err := e.d.Store.ResolveAlert(ctx, a.ID, nil, now); err != nil {
				return err
			} else if ok {
				resolved = true
			}
		}
	}
	if opened {
		e.scheduleNotify(ctx, r.ID, "opened", now)
	}
	if resolved && r.NotifyResolved {
		e.scheduleNotify(ctx, r.ID, "resolved", now)
	}
	return nil
}

// scheduleNotify programa el aviso a los 2 minutos; las alertas que se abran
// mientras tanto entran en el mismo aviso (clave única por regla y tipo).
func (e *Engine) scheduleNotify(ctx context.Context, ruleID uuid.UUID, kind string, now time.Time) {
	payload := map[string]string{"rule_id": ruleID.String(), "kind": kind}
	if err := store.EnqueueJob(ctx, e.d.Store.Pool(), store.NewJob{Kind: "alerts.notify", Payload: payload, RunAt: now.Add(notifyDelay),
		UniqueKey: "alerts.notify:" + ruleID.String() + ":" + kind}); err != nil {
		e.d.Log.Error("alertas: programar aviso", "err", err)
	}
}

// OnEvent abre alertas de las reglas de suceso que correspondan.
func (e *Engine) OnEvent(ctx context.Context, ev store.Event) {
	ruleType := map[string]string{store.EventDeploymentFailed: TypeDeploymentFailed, store.EventCommandFailed: TypeCommandFailed,
		store.EventDeviceEnrolled: TypeDeviceEnrolled, store.EventLoginFailed: TypeLoginFailures}[ev.Type]
	if ruleType == "" {
		return
	}
	rules, err := e.d.Store.ListAlertRules(ctx)
	if err != nil {
		e.d.Log.Error("alertas: leer reglas", "err", err)
		return
	}
	now := e.d.Now().UTC()
	for _, r := range rules {
		if !r.Enabled || r.Type != ruleType {
			continue
		}
		if err := e.onEventRule(ctx, r, ev, now); err != nil {
			e.d.Log.Error("alertas: suceso", "rule", r.Name, "err", err)
		}
	}
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func (e *Engine) onEventRule(ctx context.Context, r store.AlertRule, ev store.Event, now time.Time) error {
	p := params(r)
	rid := r.ID
	if r.Type == TypeLoginFailures {
		n, err := e.d.Store.CountAuditSince(ctx, "login.failed", now.Add(-loginWindow))
		if err != nil || n < p.Count {
			return err
		}
		_, ok, err := e.d.Store.OpenAlert(ctx, store.Alert{RuleID: &rid, RuleName: r.Name, Severity: r.Severity, OpenedAt: now,
			Title: fmt.Sprintf("%d inicios de sesión fallidos en la consola en 15 minutos", n)})
		if ok {
			e.scheduleNotify(ctx, r.ID, "opened", now)
		}
		return err
	}
	if ev.DeviceID == nil {
		return nil
	}
	scope, err := e.d.Store.DevicesInScope(ctx, r.Assignments)
	if err != nil {
		return err
	}
	dev, ok := scope[*ev.DeviceID]
	if !ok {
		return nil
	}
	var title string
	switch r.Type {
	case TypeDeploymentFailed:
		if p.DeploymentID != "" && str(ev.Data, "deployment_id") != p.DeploymentID {
			return nil
		}
		app := "software"
		if id, err := uuid.Parse(str(ev.Data, "deployment_id")); err == nil {
			if d, err := e.d.Store.GetDeployment(ctx, id); err == nil {
				app = "«" + d.AppName + "»"
			}
		}
		title = fmt.Sprintf("%s: falló la instalación de %s", dev.Hostname, app)
	case TypeCommandFailed:
		if p.ScriptID != "" && str(ev.Data, "script_id") != p.ScriptID {
			return nil
		}
		title = fmt.Sprintf("%s: falló la orden «%s»", dev.Hostname, str(ev.Data, "type"))
	case TypeDeviceEnrolled:
		title = dev.Hostname + " se ha inscrito"
	}
	details, _ := json.Marshal(ev.Data)
	_, opened, err := e.d.Store.OpenAlert(ctx, store.Alert{RuleID: &rid, RuleName: r.Name, DeviceID: ev.DeviceID, Severity: r.Severity,
		Title: title, Details: details, OpenedAt: now})
	if opened {
		e.scheduleNotify(ctx, r.ID, "opened", now)
	}
	return err
}

var severityLabel = map[string]string{"critical": "Crítica", "warning": "Aviso", "info": "Información"}

func (e *Engine) alertURL(id int64) string { return fmt.Sprintf("%s/alertas/%d", e.d.PublicURL, id) }

func (e *Engine) alertPayload(event string, a store.Alert, now time.Time) map[string]any {
	al := map[string]any{"id": a.ID, "rule": a.RuleName, "severity": a.Severity, "title": a.Title, "opened_at": a.OpenedAt, "url": e.alertURL(a.ID)}
	if a.DeviceID != nil {
		al["device"] = map[string]any{"id": a.DeviceID.String(), "hostname": a.Hostname, "platform": a.Platform}
	}
	if a.ResolvedAt != nil {
		al["resolved_at"] = a.ResolvedAt
	}
	return map[string]any{"event": event, "sent_at": now, "alert": al}
}

// Notify envía los avisos pendientes de una regla (tarea alerts.notify).
func (e *Engine) Notify(ctx context.Context, ruleID uuid.UUID, kind string) error {
	r, err := e.d.Store.GetAlertRule(ctx, ruleID)
	if err != nil {
		return nil // regla borrada: nada que avisar
	}
	pending, err := e.d.Store.PendingNotifications(ctx, ruleID, kind)
	if err != nil || len(pending) == 0 {
		return err
	}
	now := e.d.Now().UTC()
	var msgs []notify.Message
	var groups [][]int64
	sev := severityLabel[r.Severity]
	if kind == "opened" && len(pending) > digestThreshold {
		names := []string{}
		for _, a := range pending {
			if a.Hostname != "" {
				names = append(names, a.Hostname)
			}
		}
		shown := names
		more := ""
		if len(names) > 5 {
			shown, more = names[:5], fmt.Sprintf("… y %d más", len(names)-5)
		}
		url := fmt.Sprintf("%s/alertas?regla=%s", e.d.PublicURL, r.ID)
		summary := fmt.Sprintf("%d equipos: %s%s", len(pending), strings.Join(shown, ", "), more)
		ids := make([]int64, len(pending))
		for i, a := range pending {
			ids[i] = a.ID
		}
		msgs = append(msgs, notify.Message{Event: "alerts.digest",
			Subject: fmt.Sprintf("[MDM] %s: %s — %d equipos", sev, r.Name, len(pending)),
			Text:    fmt.Sprintf("Regla «%s».\n%s\n\nVer en la consola: %s\n", r.Name, summary, url),
			HTML:    fmt.Sprintf("<p>Regla «%s».</p><p>%s</p><p><a href=\"%s\">Ver en la consola</a></p>", html.EscapeString(r.Name), html.EscapeString(summary), url),
			Payload: map[string]any{"event": "alerts.digest", "sent_at": now, "rule": r.Name, "severity": r.Severity, "count": len(pending),
				"devices": names, "url": url}})
		groups = append(groups, ids)
	} else {
		for _, a := range pending {
			event, verb := "alert.opened", sev
			if kind == "resolved" {
				event, verb = "alert.resolved", "Resuelta"
			}
			url := e.alertURL(a.ID)
			msgs = append(msgs, notify.Message{Event: event, Subject: fmt.Sprintf("[MDM] %s: %s", verb, a.Title),
				Text:    fmt.Sprintf("%s\nRegla: %s\nGravedad: %s\n\nVer en la consola: %s\n", a.Title, r.Name, sev, url),
				HTML:    fmt.Sprintf("<p><strong>%s</strong></p><p>Regla: %s · Gravedad: %s</p><p><a href=\"%s\">Ver en la consola</a></p>", html.EscapeString(a.Title), html.EscapeString(r.Name), sev, url),
				Payload: e.alertPayload(event, a, now)})
			groups = append(groups, []int64{a.ID})
		}
	}
	// Un canal borrado después de crear la regla se salta: no debe impedir
	// avisar por los demás.
	var channels []uuid.UUID
	for _, ch := range r.Channels {
		if _, err := e.d.Store.GetChannel(ctx, ch); err == nil {
			channels = append(channels, ch)
		}
	}
	err = e.d.Store.Tx(ctx, func(q store.Querier) error {
		for i, m := range msgs {
			for _, ch := range channels {
				if err := notify.Queue(ctx, q, ch, m, r.Emails, groups[i], now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	all := []int64{}
	for _, g := range groups {
		all = append(all, g...)
	}
	return e.d.Store.MarkNotified(ctx, all, kind)
}

// HandleNotify es el manejador de la tarea alerts.notify.
func (e *Engine) HandleNotify(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		RuleID string `json:"rule_id"`
		Kind   string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	id, err := uuid.Parse(p.RuleID)
	if err != nil {
		return nil
	}
	return e.Notify(ctx, id, p.Kind)
}
