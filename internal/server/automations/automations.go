// Package automations ejecuta acciones programadas o disparadas por sucesos
// sobre los equipos de un alcance, con salvaguardas: simulación, máximo de
// equipos, antirrebote e historial.
package automations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/batches"
	"mdm/internal/server/notify"
	"mdm/internal/server/schedule"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

const debounce = 10 * time.Minute

type Trigger struct {
	Type        string `json:"type"` // schedule, event
	Frequency   string `json:"frequency,omitempty"`
	Time        string `json:"time,omitempty"`
	Day         int    `json:"day,omitempty"`
	Event       string `json:"event,omitempty"` // device.enrolled, compliance.changed, alert.opened
	AlertRuleID string `json:"alert_rule_id,omitempty"`
}

type Action struct {
	Type      string            `json:"type"`
	ScriptID  string            `json:"script_id,omitempty"`
	Version   int               `json:"version,omitempty"` // 0 = la vigente al ejecutarse
	Params    map[string]string `json:"params,omitempty"`
	Tag       string            `json:"tag,omitempty"`
	GroupID   string            `json:"group_id,omitempty"`
	ChannelID string            `json:"channel_id,omitempty"`
	Message   string            `json:"message,omitempty"`
}

var batchActions = map[string]bool{protocol.CmdRunScript: true, protocol.CmdReboot: true, protocol.CmdRefreshInventory: true,
	protocol.CmdScanUpdates: true, protocol.CmdEvaluatePolicies: true}

var actionLabel = map[string]string{"run_script": "Ejecutar script", "reboot": "Reiniciar", "refresh_inventory": "Actualizar inventario",
	"scan_updates": "Buscar actualizaciones", "evaluate_policies": "Reevaluar políticas", "add_tag": "Añadir etiqueta",
	"remove_tag": "Quitar etiqueta", "set_group": "Cambiar de grupo", "notify": "Avisar"}

// Validate comprueba el disparador y las acciones y los devuelve normalizados.
func Validate(rawTrigger, rawActions json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	var t Trigger
	dec := json.NewDecoder(bytes.NewReader(rawTrigger))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, nil, errors.New("El disparador no es válido.")
	}
	switch t.Type {
	case "schedule":
		if err := (schedule.Spec{Frequency: t.Frequency, Time: t.Time, Day: t.Day}).Validate(); err != nil {
			return nil, nil, err
		}
		t.Event, t.AlertRuleID = "", ""
	case "event":
		switch t.Event {
		case store.EventDeviceEnrolled, store.EventComplianceChanged:
			t.AlertRuleID = ""
		case store.EventAlertOpened:
			if t.AlertRuleID != "" {
				if _, err := uuid.Parse(t.AlertRuleID); err != nil {
					return nil, nil, errors.New("La regla de alerta del disparador no es válida.")
				}
			}
		default:
			return nil, nil, errors.New("El suceso debe ser device.enrolled, compliance.changed o alert.opened.")
		}
		t.Frequency, t.Time, t.Day = "", "", 0
	default:
		return nil, nil, errors.New("El disparador debe ser «schedule» (programado) o «event» (por suceso).")
	}
	var acts []Action
	dec = json.NewDecoder(bytes.NewReader(rawActions))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&acts); err != nil {
		return nil, nil, errors.New("Las acciones no son válidas.")
	}
	if len(acts) == 0 || len(acts) > 10 {
		return nil, nil, errors.New("Indica entre 1 y 10 acciones.")
	}
	for i, a := range acts {
		switch {
		case a.Type == "run_script":
			if _, err := uuid.Parse(a.ScriptID); err != nil {
				return nil, nil, fmt.Errorf("La acción %d necesita el script.", i+1)
			}
		case batchActions[a.Type]:
		case a.Type == "add_tag" || a.Type == "remove_tag":
			if strings.TrimSpace(a.Tag) == "" || utf8.RuneCountInString(a.Tag) > 50 {
				return nil, nil, fmt.Errorf("La acción %d necesita una etiqueta (como mucho 50 caracteres).", i+1)
			}
			acts[i].Tag = strings.TrimSpace(a.Tag)
		case a.Type == "set_group":
			if _, err := uuid.Parse(a.GroupID); err != nil {
				return nil, nil, fmt.Errorf("La acción %d necesita el grupo.", i+1)
			}
		case a.Type == "notify":
			if _, err := uuid.Parse(a.ChannelID); err != nil {
				return nil, nil, fmt.Errorf("La acción %d necesita el canal.", i+1)
			}
			if utf8.RuneCountInString(a.Message) > 1000 {
				return nil, nil, fmt.Errorf("El mensaje de la acción %d es demasiado largo.", i+1)
			}
		default:
			return nil, nil, fmt.Errorf("Acción desconocida: «%s».", a.Type)
		}
	}
	tb, _ := json.Marshal(t)
	ab, _ := json.Marshal(acts)
	return tb, ab, nil
}

// NeedsAdmin informa de si crear o editar estas acciones exige administrador.
func NeedsAdmin(acts []Action) bool {
	for _, a := range acts {
		if a.Type == "run_script" || a.Type == "reboot" || a.Type == "set_group" {
			return true
		}
	}
	return false
}

func Parse(a store.Automation) (Trigger, []Action) {
	var t Trigger
	var acts []Action
	_ = json.Unmarshal(a.Trigger, &t)
	_ = json.Unmarshal(a.Actions, &acts)
	return t, acts
}

type Deps struct {
	Store    *store.Store
	Secrets  *secrets.Box
	Now      func() time.Time
	Location func() *time.Location
	Log      *slog.Logger
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

// RunDue ejecuta las automatizaciones programadas cuya hora ha llegado
// (tarea automations.run). Las perdidas con el servidor parado, una vez.
func (e *Engine) RunDue(ctx context.Context) {
	list, err := e.d.Store.ListAutomations(ctx)
	if err != nil {
		e.d.Log.Error("automatizaciones", "err", err)
		return
	}
	now := e.d.Now()
	for _, a := range list {
		t, _ := Parse(a)
		if !a.Enabled || t.Type != "schedule" {
			continue
		}
		if !schedule.Due(schedule.Spec{Frequency: t.Frequency, Time: t.Time, Day: t.Day}, a.LastRunAt, now, e.d.Location()) {
			continue
		}
		_ = e.d.Store.SetAutomationLastRun(ctx, a.ID, now)
		scope, err := e.d.Store.DevicesInScope(ctx, a.Assignments)
		if err != nil {
			e.d.Log.Error("automatización: alcance", "name", a.Name, "err", err)
			continue
		}
		e.run(ctx, a, ids(scope), map[string]any{"type": "schedule", "at": now.UTC()})
	}
}

func ids(m map[uuid.UUID]store.ScopedDevice) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

// OnEvent ejecuta las automatizaciones por suceso sobre el equipo del suceso.
func (e *Engine) OnEvent(ctx context.Context, ev store.Event) {
	if ev.DeviceID == nil {
		return
	}
	switch ev.Type {
	case store.EventDeviceEnrolled, store.EventAlertOpened:
	case store.EventComplianceChanged:
		if to, _ := ev.Data["to"].(string); to != "noncompliant" {
			return
		}
	default:
		return
	}
	list, err := e.d.Store.ListAutomations(ctx)
	if err != nil {
		e.d.Log.Error("automatizaciones", "err", err)
		return
	}
	now := e.d.Now()
	for _, a := range list {
		t, _ := Parse(a)
		if !a.Enabled || t.Type != "event" || t.Event != ev.Type {
			continue
		}
		if t.Event == store.EventAlertOpened && t.AlertRuleID != "" {
			if rid, _ := ev.Data["rule_id"].(string); rid != t.AlertRuleID {
				continue
			}
		}
		scope, err := e.d.Store.DevicesInScope(ctx, a.Assignments)
		if err != nil {
			continue
		}
		if _, ok := scope[*ev.DeviceID]; !ok {
			continue
		}
		if ok, err := e.d.Store.TryFire(ctx, a.ID, *ev.DeviceID, ev.Type, now, debounce); err != nil || !ok {
			continue
		}
		e.run(ctx, a, []uuid.UUID{*ev.DeviceID}, map[string]any{"type": "event", "event": ev.Type, "device_id": ev.DeviceID.String()})
	}
}

type SimDevice struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Platform string `json:"platform"`
}

type Simulation struct {
	DeviceCount int         `json:"device_count"`
	Devices     []SimDevice `json:"devices"`
	Actions     []string    `json:"actions"`
	Blocked     bool        `json:"blocked"`
	MaxDevices  int         `json:"max_devices"`
}

// Simulate dice qué equipos se verían afectados ahora y qué se crearía, sin
// ejecutar nada. Para una automatización por suceso, el alcance es el
// conjunto de equipos que podrían dispararla.
func (e *Engine) Simulate(ctx context.Context, a store.Automation) (Simulation, error) {
	scope, err := e.d.Store.DevicesInScope(ctx, a.Assignments)
	if err != nil {
		return Simulation{}, err
	}
	_, acts := Parse(a)
	sim := Simulation{DeviceCount: len(scope), Devices: []SimDevice{}, MaxDevices: a.MaxDevices, Blocked: len(scope) > a.MaxDevices}
	for _, d := range scope {
		if len(sim.Devices) < 200 {
			sim.Devices = append(sim.Devices, SimDevice{ID: d.ID.String(), Hostname: d.Hostname, Platform: d.Platform})
		}
	}
	phones := 0
	for _, d := range scope {
		if d.Platform == "android" {
			phones++
		}
	}
	for _, x := range acts {
		desc := e.describe(ctx, x)
		if batchActions[x.Type] && phones > 0 && !protocol.AndroidCommands[x.Type] {
			if phones == 1 {
				desc += " (no se envía a 1 móvil Android)"
			} else {
				desc += fmt.Sprintf(" (no se envía a %d móviles Android)", phones)
			}
		}
		sim.Actions = append(sim.Actions, desc)
	}
	return sim, nil
}

func (e *Engine) describe(ctx context.Context, x Action) string {
	label := actionLabel[x.Type]
	switch x.Type {
	case "run_script":
		if id, err := uuid.Parse(x.ScriptID); err == nil {
			if s, err := e.d.Store.GetScript(ctx, id); err == nil {
				v := "versión vigente"
				if x.Version > 0 {
					v = fmt.Sprintf("versión %d", x.Version)
				}
				return fmt.Sprintf("%s «%s» (%s)", label, s.Name, v)
			}
		}
	case "add_tag", "remove_tag":
		return fmt.Sprintf("%s «%s»", label, x.Tag)
	case "set_group":
		if id, err := uuid.Parse(x.GroupID); err == nil {
			if g, err := e.d.Store.GroupByID(ctx, id); err == nil {
				return fmt.Sprintf("%s: «%s»", label, g.Name)
			}
		}
	case "notify":
		if id, err := uuid.Parse(x.ChannelID); err == nil {
			if c, err := e.d.Store.GetChannel(ctx, id); err == nil {
				return fmt.Sprintf("%s por «%s»", label, c.Name)
			}
		}
	}
	return label
}

// run ejecuta las acciones en orden sobre los equipos y deja la ejecución en el historial.
func (e *Engine) run(ctx context.Context, a store.Automation, devices []uuid.UUID, trigger map[string]any) {
	now := e.d.Now().UTC()
	info, _ := json.Marshal(trigger)
	run := store.AutomationRun{AutomationID: a.ID, TriggerInfo: info, StartedAt: now, DeviceCount: len(devices)}
	finish := func(status, msg string) {
		t := e.d.Now().UTC()
		run.FinishedAt, run.Status, run.Error = &t, status, msg
		if _, err := e.d.Store.AddAutomationRun(ctx, run); err != nil {
			e.d.Log.Error("automatización: historial", "err", err)
		}
	}
	if len(devices) > a.MaxDevices {
		finish("blocked", fmt.Sprintf("%d equipos superan el máximo de %d: no se ha ejecutado nada.", len(devices), a.MaxDevices))
		_, _, _ = e.d.Store.OpenAlert(ctx, store.Alert{RuleName: "Automatizaciones", Severity: "info", OpenedAt: now,
			Title: fmt.Sprintf("La automatización «%s» no se ejecutó: %d equipos superan su máximo de %d", a.Name, len(devices), a.MaxDevices)})
		return
	}
	if len(devices) == 0 {
		finish("succeeded", "Ningún equipo en el alcance.")
		return
	}
	_, acts := Parse(a)
	done := 0
	var summary []string
	for i, x := range acts {
		var err error
		switch {
		case batchActions[x.Type]:
			var id *uuid.UUID
			id, err = e.batch(ctx, a, x, devices, now)
			if id != nil {
				run.BatchIDs = append(run.BatchIDs, *id)
			}
		case x.Type == "add_tag" || x.Type == "remove_tag":
			err = e.tags(ctx, x, devices)
		case x.Type == "set_group":
			err = e.group(ctx, x, devices)
		case x.Type == "notify":
			err = e.notify(ctx, a, x, devices, summary, now)
		}
		if err != nil {
			status := "failed"
			if done > 0 {
				status = "partial"
			}
			finish(status, fmt.Sprintf("Acción %d (%s): %s", i+1, actionLabel[x.Type], err.Error()))
			return
		}
		done++
		summary = append(summary, e.describe(ctx, x))
	}
	finish("succeeded", "")
}

func (e *Engine) batch(ctx context.Context, a store.Automation, x Action, devices []uuid.UUID, now time.Time) (*uuid.UUID, error) {
	var params json.RawMessage
	if x.Type == "run_script" {
		params, _ = json.Marshal(map[string]any{"script_id": x.ScriptID, "version": x.Version, "params": nonNil(x.Params)})
	}
	var rows []store.DeviceRow
	for _, id := range devices {
		if r, err := e.d.Store.DeviceRowByID(ctx, id); err == nil && r.RevokedAt == nil {
			rows = append(rows, r)
		}
	}
	rows, _ = batches.FilterPlatform(x.Type, rows)
	payload, keep, _, err := batches.Build(ctx, e.d.Store, e.d.Secrets, x.Type, params, rows)
	if err != nil {
		return nil, err
	}
	if len(keep) == 0 {
		return nil, nil
	}
	targets := make([]uuid.UUID, len(keep))
	for i, r := range keep {
		targets[i] = r.ID
	}
	b, err := e.d.Store.CreateBatch(ctx, store.NewBatch{Type: x.Type, Params: payload, CreatedBy: a.CreatedBy, CreatedByEmail: "Automatización «" + a.Name + "»",
		TargetDescription: fmt.Sprintf("Automatización «%s» (%d equipos)", a.Name, len(targets)), At: now, ExpiresIn: 7 * 24 * time.Hour}, targets)
	if err != nil {
		return nil, err
	}
	return &b.ID, nil
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (e *Engine) tags(ctx context.Context, x Action, devices []uuid.UUID) error {
	for _, id := range devices {
		d, err := e.d.Store.DeviceByID(ctx, id)
		if err != nil {
			continue
		}
		tags := []string{}
		has := false
		for _, t := range d.Tags {
			if strings.EqualFold(t, x.Tag) {
				has = true
				if x.Type == "remove_tag" {
					continue
				}
			}
			tags = append(tags, t)
		}
		if x.Type == "add_tag" && !has {
			tags = append(tags, x.Tag)
		}
		if err := e.d.Store.UpdateDevice(ctx, id, store.DevicePatch{Tags: &tags}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) group(ctx context.Context, x Action, devices []uuid.UUID) error {
	gid, _ := uuid.Parse(x.GroupID)
	g, err := e.d.Store.GroupByID(ctx, gid)
	if err != nil {
		return errors.New("el grupo ya no existe")
	}
	if g.Kind == "dynamic" {
		return errors.New("un grupo dinámico no puede ser el grupo principal")
	}
	p := &gid
	for _, id := range devices {
		if err := e.d.Store.UpdateDevice(ctx, id, store.DevicePatch{GroupID: &p}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) notify(ctx context.Context, a store.Automation, x Action, devices []uuid.UUID, done []string, now time.Time) error {
	cid, _ := uuid.Parse(x.ChannelID)
	if _, err := e.d.Store.GetChannel(ctx, cid); err != nil {
		return errors.New("el canal ya no existe")
	}
	text := fmt.Sprintf("Automatización «%s»: %d equipos.", a.Name, len(devices))
	if len(done) > 0 {
		text += " Acciones: " + strings.Join(done, "; ") + "."
	}
	if x.Message != "" {
		text = x.Message + "\n\n" + text
	}
	m := notify.Message{Event: "automation.notify", Subject: "[MDM] Automatización: " + a.Name, Text: text,
		HTML:    "<p>" + strings.ReplaceAll(html.EscapeString(text), "\n", "<br>") + "</p>",
		Payload: map[string]any{"event": "automation.notify", "sent_at": now, "automation": a.Name, "devices": len(devices), "actions": done, "message": x.Message}}
	return notify.Queue(ctx, e.d.Store.Pool(), cid, m, nil, nil, now)
}
