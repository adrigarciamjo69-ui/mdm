package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
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
	ch  uuid.UUID
}

func setup(t *testing.T) *env {
	s := storetest.New(t)
	ch, _ := s.CreateChannel(ctx, store.Channel{Name: "Correo IT", Type: "email", Recipients: []string{"it@ejemplo.es"}})
	en := &env{t: t, s: s, now: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC), ch: ch.ID}
	en.e = New(Deps{Store: s, Now: func() time.Time { return en.now }, PublicURL: "https://mdm.ejemplo.es"})
	return en
}

func (e *env) rule(typ string, params string, assign ...policy.Assignment) store.AlertRule {
	e.t.Helper()
	if assign == nil {
		assign = []policy.Assignment{{}}
	}
	r, err := e.s.CreateAlertRule(ctx, store.AlertRule{Name: typ + " " + uuid.NewString()[:4], Type: typ, Params: json.RawMessage(params),
		Severity: "critical", Channels: []uuid.UUID{e.ch}, NotifyResolved: true, Enabled: true}, assign)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) device(host string, seenAgo time.Duration) uuid.UUID {
	id := storetest.SeedDevice(e.t, e.s, host, "linux")
	seen := e.now.Add(-seenAgo)
	_, _ = e.s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, id, seen)
	return id
}

func (e *env) open() []store.Alert {
	list, _, _ := e.s.ListAlerts(ctx, store.AlertFilter{State: "unresolved", Page: 1, PerPage: 100})
	return list
}

func TestOffline_OpensResolvesAndDedupes(t *testing.T) {
	e := setup(t)
	r := e.rule(TypeDeviceOffline, `{"hours":1}`)
	a := e.device("srv-db-01", 2*time.Hour)
	e.device("srv-web-01", time.Minute)
	e.e.EvaluateState(ctx)
	got := e.open()
	if len(got) != 1 || *got[0].DeviceID != a || !strings.Contains(got[0].Title, "srv-db-01") || got[0].Severity != "critical" || got[0].RuleID == nil || *got[0].RuleID != r.ID {
		t.Fatalf("%+v", got)
	}
	e.e.EvaluateState(ctx)
	if len(e.open()) != 1 {
		t.Fatal("una sola alerta abierta por regla y equipo")
	}
	_, _ = e.s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, a, e.now)
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("se resuelve sola cuando vuelve")
	}
	all, _, _ := e.s.ListAlerts(ctx, store.AlertFilter{Page: 1, PerPage: 10})
	if len(all) != 1 || all[0].State != "resolved" || all[0].ResolvedBy != nil {
		t.Fatalf("%+v", all)
	}
}

func TestScope_GroupsAndExclusion(t *testing.T) {
	e := setup(t)
	srv, _ := e.s.CreateGroup(ctx, "Servidores", "")
	lab, _ := e.s.CreateGroup(ctx, "Laboratorio", "")
	a := e.device("srv-1", 2*time.Hour)
	b := e.device("pc-1", 2*time.Hour)
	c := e.device("srv-lab", 2*time.Hour)
	for _, x := range []struct {
		id uuid.UUID
		g  uuid.UUID
	}{{a, srv.ID}, {c, srv.ID}} {
		g := x.g
		gp := &g
		_ = e.s.UpdateDevice(ctx, x.id, store.DevicePatch{GroupID: &gp})
	}
	_, _ = e.s.Pool().Exec(ctx, `INSERT INTO group_members (group_id, device_id) VALUES ($1, $2)`, lab.ID, c)
	e.rule(TypeDeviceOffline, `{"hours":1}`, policy.Assignment{GroupID: &srv.ID}, policy.Assignment{GroupID: &lab.ID, Exclude: true})
	e.e.EvaluateState(ctx)
	got := e.open()
	if len(got) != 1 || *got[0].DeviceID != a {
		t.Fatalf("solo srv-1 (pc-1 fuera del alcance, srv-lab excluido): %+v (%s %s)", got, b, c)
	}
}

func TestDiskLowComplianceAndPatches(t *testing.T) {
	e := setup(t)
	d := e.device("pc-1", time.Minute)
	inv := protocol.Inventory{Disks: []protocol.Disk{{Mount: "/", SizeBytes: 100 << 30, FreeBytes: 5 << 30}}}
	_ = e.s.SaveInventory(ctx, d, inv, e.now)
	e.rule(TypeDiskLow, `{"percent":10}`)
	_, _ = e.s.SaveComplianceState(ctx, d, "noncompliant", []string{"x"}, &e.now, e.now)
	e.rule(TypeNoncompliant, `{}`)
	e.e.EvaluateState(ctx)
	titles := []string{}
	for _, a := range e.open() {
		titles = append(titles, a.Title)
	}
	if len(titles) != 2 {
		t.Fatalf("%v", titles)
	}
}

func TestEventRules_FilterAndAutoResolve(t *testing.T) {
	e := setup(t)
	d := e.device("pc-1", time.Minute)
	dep := uuid.New()
	other := uuid.New()
	e.rule(TypeDeploymentFailed, fmt.Sprintf(`{"deployment_id":"%s"}`, dep))
	e.e.OnEvent(ctx, store.Event{Type: store.EventDeploymentFailed, DeviceID: &d, At: e.now, Data: map[string]any{"deployment_id": other.String(), "message": "x"}})
	if len(e.open()) != 0 {
		t.Fatal("otro despliegue: no aplica")
	}
	e.e.OnEvent(ctx, store.Event{Type: store.EventDeploymentFailed, DeviceID: &d, At: e.now, Data: map[string]any{"deployment_id": dep.String(), "message": "código 1603"}})
	got := e.open()
	if len(got) != 1 || !strings.Contains(got[0].Title, "pc-1") {
		t.Fatalf("%+v", got)
	}
	e.now = e.now.Add(6 * 24 * time.Hour)
	e.e.EvaluateState(ctx)
	if len(e.open()) != 1 {
		t.Fatal("a los 6 días sigue abierta")
	}
	e.now = e.now.Add(25 * time.Hour)
	e.e.EvaluateState(ctx)
	if len(e.open()) != 0 {
		t.Fatal("las de suceso se cierran solas a los 7 días")
	}
}

func TestLoginFailures_Global(t *testing.T) {
	e := setup(t)
	e.rule(TypeLoginFailures, `{"count":3}`)
	for i := 0; i < 3; i++ {
		_, _ = e.s.Pool().Exec(ctx, `INSERT INTO audit_log (actor_email, action, target_type, target_id, details, ip, at) VALUES ('x@y', 'login.failed', '', '', '{}', '203.0.113.9', $1)`, e.now.Add(-time.Duration(i)*time.Minute))
	}
	e.e.OnEvent(ctx, store.Event{Type: store.EventLoginFailed, At: e.now})
	got := e.open()
	if len(got) != 1 || got[0].DeviceID != nil || !strings.Contains(got[0].Title, "3") {
		t.Fatalf("alerta global: %+v", got)
	}
	e.e.OnEvent(ctx, store.Event{Type: store.EventLoginFailed, At: e.now})
	if len(e.open()) != 1 {
		t.Fatal("una sola global abierta")
	}
}

func TestNotifications_GroupedDigest(t *testing.T) {
	e := setup(t)
	r := e.rule(TypeDeviceOffline, `{"hours":1}`)
	for i := 0; i < 23; i++ {
		e.device(fmt.Sprintf("PC-CONTA-%02d", i+1), 2*time.Hour)
	}
	small := e.rule(TypeDiskLow, `{"percent":10}`)
	_ = small
	e.e.EvaluateState(ctx)
	e.now = e.now.Add(2 * time.Minute)
	if err := e.e.Notify(ctx, r.ID, "opened"); err != nil {
		t.Fatal(err)
	}
	var n int
	var msg []byte
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*), max(message::text) FROM deliveries`).Scan(&n, &msg)
	if n != 1 || !strings.Contains(string(msg), "23 equipos") || !strings.Contains(string(msg), "y 18 más") ||
		!strings.Contains(string(msg), "https://mdm.ejemplo.es/alertas?regla="+r.ID.String()) {
		t.Fatalf("un único aviso agrupado: %d %s", n, msg)
	}
	if err := e.e.Notify(ctx, r.ID, "opened"); err != nil {
		t.Fatal(err)
	}
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*) FROM deliveries`).Scan(&n)
	if n != 1 {
		t.Fatal("no se avisa dos veces de las mismas alertas")
	}
}

func TestNotifications_FewAreIndividualAndResolved(t *testing.T) {
	e := setup(t)
	r := e.rule(TypeDeviceOffline, `{"hours":1}`)
	a := e.device("srv-1", 2*time.Hour)
	e.device("srv-2", 2*time.Hour)
	e.e.EvaluateState(ctx)
	_ = e.e.Notify(ctx, r.ID, "opened")
	var n int
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE event = 'alert.opened'`).Scan(&n)
	if n != 2 {
		t.Fatalf("con 5 o menos, uno por alerta: %d", n)
	}
	_, _ = e.s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = $2 WHERE id = $1`, a, e.now)
	e.e.EvaluateState(ctx)
	_ = e.e.Notify(ctx, r.ID, "resolved")
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE event = 'alert.resolved'`).Scan(&n)
	if n != 1 {
		t.Fatalf("aviso de resuelta: %d", n)
	}
}

func TestDisableRuleResolvesItsAlerts(t *testing.T) {
	e := setup(t)
	r := e.rule(TypeDeviceOffline, `{"hours":1}`)
	e.device("srv-1", 2*time.Hour)
	e.e.EvaluateState(ctx)
	r.Enabled = false
	if err := e.s.UpdateAlertRule(ctx, r, nil, e.now); err != nil {
		t.Fatal(err)
	}
	if len(e.open()) != 0 {
		t.Fatal("desactivar la regla resuelve sus alertas")
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct{ typ, params string }{{TypeDeviceOffline, `{"hours":0}`}, {TypeDeviceOffline, `{"hours":721}`},
		{TypeDiskLow, `{"percent":60}`}, {TypeLoginFailures, `{"count":2}`}, {"inventado", `{}`}, {TypeDeviceOffline, `{"hours":1,"x":2}`}} {
		if _, err := Validate(c.typ, json.RawMessage(c.params)); err == nil {
			t.Errorf("%s %s debería rechazarse", c.typ, c.params)
		}
	}
	if p, err := Validate(TypeDeviceOffline, json.RawMessage(`{"hours":24}`)); err != nil || string(p) != `{"hours":24}` {
		t.Fatalf("%s %v", p, err)
	}
}

func TestNotifications_DeletedChannelDoesNotBlockOthers(t *testing.T) {
	e := setup(t)
	gone, _ := e.s.CreateChannel(ctx, store.Channel{Name: "Se borrará", Type: "email", Recipients: []string{"a@b.es"}})
	r, _ := e.s.CreateAlertRule(ctx, store.AlertRule{Name: "Offline", Type: TypeDeviceOffline, Params: json.RawMessage(`{"hours":1}`), Severity: "warning",
		Channels: []uuid.UUID{gone.ID, e.ch}, Enabled: true}, []policy.Assignment{{}})
	_ = e.s.DeleteChannel(ctx, gone.ID)
	e.device("srv-1", 2*time.Hour)
	e.e.EvaluateState(ctx)
	if err := e.e.Notify(ctx, r.ID, "opened"); err != nil {
		t.Fatalf("un canal borrado no debe impedir avisar por los demás: %v", err)
	}
	var n int
	_ = e.s.Pool().QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE channel_id = $1`, e.ch).Scan(&n)
	if n != 1 {
		t.Fatalf("entregas al canal que sigue existiendo: %d", n)
	}
}
