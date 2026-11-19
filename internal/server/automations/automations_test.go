package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/policy"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

type env struct {
	t   *testing.T
	s   *store.Store
	e   *Engine
	now time.Time
	adm uuid.UUID
	loc *time.Location
}

func setup(t *testing.T) *env {
	s := storetest.New(t)
	loc, _ := time.LoadLocation("Europe/Madrid")
	adm, _ := s.CreateAdmin(ctx, "ana@ejemplo.es", "x", "admin")
	en := &env{t: t, s: s, now: time.Date(2026, 11, 18, 12, 0, 0, 0, loc), adm: adm.ID, loc: loc}
	en.e = New(Deps{Store: s, Now: func() time.Time { return en.now }, Location: func() *time.Location { return en.loc }})
	return en
}

func (e *env) automation(trigger, actions string, max int, assign ...policy.Assignment) store.Automation {
	e.t.Helper()
	if assign == nil {
		assign = []policy.Assignment{{}}
	}
	tr, acts, err := Validate(json.RawMessage(trigger), json.RawMessage(actions))
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := e.s.CreateAutomation(ctx, store.Automation{Name: "Auto " + uuid.NewString()[:4], Enabled: true, Trigger: tr, Actions: acts,
		MaxDevices: max, CreatedBy: e.adm}, assign, e.now)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *env) commands(typ string) int {
	var n int
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*) FROM commands WHERE type = $1`, typ).Scan(&n)
	return n
}

func TestValidate(t *testing.T) {
	bad := []struct{ trigger, actions string }{
		{`{"type":"schedule","frequency":"daily","time":"25:00"}`, `[{"type":"reboot"}]`},
		{`{"type":"event","event":"cualquiera"}`, `[{"type":"reboot"}]`},
		{`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[]`},
		{`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"formatear"}]`},
		{`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"add_tag","tag":""}]`},
		{`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"run_script"}]`},
	}
	for i, c := range bad {
		if _, _, err := Validate(json.RawMessage(c.trigger), json.RawMessage(c.actions)); err == nil || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("caso %d: %v", i, err)
		}
	}
	if !NeedsAdmin([]Action{{Type: "reboot"}}) || NeedsAdmin([]Action{{Type: "add_tag", Tag: "x"}}) || !NeedsAdmin([]Action{{Type: "set_group"}}) {
		t.Fatal("reboot, run_script y set_group exigen administrador")
	}
}

func TestSchedule_RunsWhenDueWithTagsAndBatch(t *testing.T) {
	e := setup(t)
	g, _ := e.s.CreateGroup(ctx, "Contabilidad", "")
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		id := storetest.SeedDevice(t, e.s, fmt.Sprintf("PC-CONTA-%d", i), "windows")
		gp := &g.ID
		_ = e.s.UpdateDevice(ctx, id, store.DevicePatch{GroupID: &gp})
		ids = append(ids, id)
	}
	storetest.SeedDevice(t, e.s, "PC-OTRO", "windows")
	a := e.automation(`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"refresh_inventory"},{"type":"add_tag","tag":"limpiado"}]`, 500,
		policy.Assignment{GroupID: &g.ID})
	e.e.RunDue(ctx)
	if e.commands("refresh_inventory") != 0 {
		t.Fatal("al crearla no se ejecuta")
	}
	e.now = time.Date(2026, 11, 19, 2, 1, 0, 0, e.loc)
	e.e.RunDue(ctx)
	if e.commands("refresh_inventory") != 3 {
		t.Fatalf("lote para los 3 del grupo: %d", e.commands("refresh_inventory"))
	}
	d, _ := e.s.DeviceByID(ctx, ids[0])
	if len(d.Tags) != 1 || d.Tags[0] != "limpiado" {
		t.Fatalf("etiqueta: %v", d.Tags)
	}
	runs, _ := e.s.AutomationRuns(ctx, a.ID, 10)
	if len(runs) != 1 || runs[0].Status != "succeeded" || runs[0].DeviceCount != 3 || len(runs[0].BatchIDs) != 1 {
		t.Fatalf("historial: %+v", runs)
	}
	e.e.RunDue(ctx)
	if r, _ := e.s.AutomationRuns(ctx, a.ID, 10); len(r) != 1 {
		t.Fatal("una vez al día")
	}
}

func TestBlockedOverMaxDevices(t *testing.T) {
	e := setup(t)
	for i := 0; i < 3; i++ {
		storetest.SeedDevice(t, e.s, fmt.Sprintf("PC-%d", i), "windows")
	}
	a := e.automation(`{"type":"schedule","frequency":"hourly","time":"00:00"}`, `[{"type":"reboot"}]`, 2)
	e.now = e.now.Add(time.Hour)
	e.e.RunDue(ctx)
	if e.commands("reboot") != 0 {
		t.Fatal("con más equipos que el máximo no se ejecuta nada")
	}
	runs, _ := e.s.AutomationRuns(ctx, a.ID, 10)
	if len(runs) != 1 || runs[0].Status != "blocked" || runs[0].DeviceCount != 3 {
		t.Fatalf("%+v", runs)
	}
	alerts, _, _ := e.s.ListAlerts(ctx, store.AlertFilter{State: "unresolved", Page: 1, PerPage: 10})
	if len(alerts) != 1 || alerts[0].Severity != "info" || !strings.Contains(alerts[0].Title, a.Name) {
		t.Fatalf("alerta informativa: %+v", alerts)
	}
}

func TestSimulate_NoSideEffects(t *testing.T) {
	e := setup(t)
	storetest.SeedDevice(t, e.s, "PC-1", "windows")
	storetest.SeedDevice(t, e.s, "SRV-1", "linux")
	a := e.automation(`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"evaluate_policies"},{"type":"add_tag","tag":"x"}]`, 500)
	sim, err := e.e.Simulate(ctx, a)
	if err != nil || sim.DeviceCount != 2 || len(sim.Devices) != 2 || len(sim.Actions) != 2 || sim.Blocked {
		t.Fatalf("%+v %v", sim, err)
	}
	if e.commands("evaluate_policies") != 0 {
		t.Fatal("simular no ejecuta nada")
	}
	if r, _ := e.s.AutomationRuns(ctx, a.ID, 10); len(r) != 0 {
		t.Fatal("ni deja historial")
	}
}

func TestEvent_DeviceEnrolledWithDebounce(t *testing.T) {
	e := setup(t)
	a := e.automation(`{"type":"event","event":"device.enrolled"}`, `[{"type":"add_tag","tag":"nuevo"}]`, 500)
	d := storetest.SeedDevice(t, e.s, "PC-1", "windows")
	ev := store.Event{Type: store.EventDeviceEnrolled, DeviceID: &d, At: e.now}
	e.e.OnEvent(ctx, ev)
	e.now = e.now.Add(5 * time.Minute)
	e.e.OnEvent(ctx, ev)
	if r, _ := e.s.AutomationRuns(ctx, a.ID, 10); len(r) != 1 {
		t.Fatalf("antirrebote de 10 minutos: %d ejecuciones", len(r))
	}
	e.now = e.now.Add(6 * time.Minute)
	e.e.OnEvent(ctx, ev)
	if r, _ := e.s.AutomationRuns(ctx, a.ID, 10); len(r) != 2 {
		t.Fatal("pasados 10 minutos, sí")
	}
	dev, _ := e.s.DeviceByID(ctx, d)
	if len(dev.Tags) != 1 || dev.Tags[0] != "nuevo" {
		t.Fatalf("%v", dev.Tags)
	}
}

func TestEvent_ComplianceOnlyToNoncompliantAndScope(t *testing.T) {
	e := setup(t)
	g, _ := e.s.CreateGroup(ctx, "Portátiles", "")
	a := e.automation(`{"type":"event","event":"compliance.changed"}`, `[{"type":"evaluate_policies"}]`, 500, policy.Assignment{GroupID: &g.ID})
	in := storetest.SeedDevice(t, e.s, "PORT-1", "windows")
	out := storetest.SeedDevice(t, e.s, "PC-1", "windows")
	gp := &g.ID
	_ = e.s.UpdateDevice(ctx, in, store.DevicePatch{GroupID: &gp})
	e.e.OnEvent(ctx, store.Event{Type: store.EventComplianceChanged, DeviceID: &in, At: e.now, Data: map[string]any{"from": "noncompliant", "to": "compliant"}})
	e.e.OnEvent(ctx, store.Event{Type: store.EventComplianceChanged, DeviceID: &out, At: e.now, Data: map[string]any{"from": "compliant", "to": "noncompliant"}})
	if r, _ := e.s.AutomationRuns(ctx, a.ID, 10); len(r) != 0 {
		t.Fatal("solo al pasar a «no cumple» y dentro del alcance")
	}
	e.e.OnEvent(ctx, store.Event{Type: store.EventComplianceChanged, DeviceID: &in, At: e.now, Data: map[string]any{"from": "compliant", "to": "noncompliant"}})
	if e.commands("evaluate_policies") != 1 {
		t.Fatal("el equipo del suceso recibe la orden")
	}
}

func TestRunScriptFailureStopsAndRecords(t *testing.T) {
	e := setup(t)
	storetest.SeedDevice(t, e.s, "PC-1", "windows")
	a := e.automation(`{"type":"schedule","frequency":"hourly","time":"00:00"}`,
		fmt.Sprintf(`[{"type":"run_script","script_id":"%s"},{"type":"add_tag","tag":"x"}]`, uuid.New()), 500)
	e.now = e.now.Add(time.Hour)
	e.e.RunDue(ctx)
	runs, _ := e.s.AutomationRuns(ctx, a.ID, 10)
	if len(runs) != 1 || runs[0].Status != "failed" || !strings.Contains(runs[0].Error, "script") {
		t.Fatalf("%+v", runs)
	}
}

func TestSimulate_NotesAndroidSkipped(t *testing.T) {
	e := setup(t)
	storetest.SeedDevice(t, e.s, "PC-1", "windows")
	_, _, _ = e.s.UpsertAndroidDevice(ctx, store.AndroidDevice{AmapiName: "enterprises/x/devices/1", Hostname: "Pixel 8a", EnrolledAt: e.now})
	a := e.automation(`{"type":"schedule","frequency":"daily","time":"02:00"}`, `[{"type":"refresh_inventory"},{"type":"add_tag","tag":"x"}]`, 500)
	sim, _ := e.e.Simulate(ctx, a)
	if !strings.Contains(sim.Actions[0], "no se envía a 1 móvil") || strings.Contains(sim.Actions[1], "móvil") {
		t.Fatalf("%v", sim.Actions)
	}
}

func TestNotify_EscapesHTML(t *testing.T) {
	e := setup(t)
	storetest.SeedDevice(t, e.s, "PC-1", "windows")
	ch, _ := e.s.CreateChannel(ctx, store.Channel{Name: "IT", Type: "email", Recipients: []string{"it@ejemplo.es"}})
	e.automation(`{"type":"schedule","frequency":"hourly","time":"00:00"}`, fmt.Sprintf(`[{"type":"notify","channel_id":"%s","message":"<img src=x onerror=alert(1)>"}]`, ch.ID), 500)
	e.now = e.now.Add(time.Hour)
	e.e.RunDue(ctx)
	var msg string
	_ = e.s.Pool().QueryRow(ctx, `SELECT message->>'HTML' FROM deliveries`).Scan(&msg)
	if strings.Contains(msg, "<img") || !strings.Contains(msg, "&lt;img") {
		t.Fatalf("el mensaje del operador va escapado en el HTML: %s", msg)
	}
}
