package adminapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

type dynGroupResp struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Rule        json.RawMessage `json:"rule"`
	DeviceCount int             `json:"device_count"`
}

func linuxRule() map[string]any {
	return map[string]any{"match": "all", "conditions": []map[string]any{{"field": "platform", "op": "eq", "value": "linux"}}}
}

func (a *testAPI) members(groupID string) []string {
	a.t.Helper()
	p := decode[pageResp[deviceResp]](a.t, a.loginAs(auth.RoleReadonly).do("GET", "/devices?per_page=200&group_id="+groupID, nil))
	return strings.Split(names(p), ",")
}

func TestDynamicGroup_MembershipComputedOnSave(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "win-1", Platform: "windows"})
	a.seedDevice(seed{Hostname: "srv-1", Platform: "linux"})
	a.seedDevice(seed{Hostname: "srv-2", Platform: "linux"})
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/groups", map[string]any{"name": "Linux", "kind": "dynamic", "rule": linuxRule()})
	wantStatus(t, resp, 201)
	g := decode[dynGroupResp](t, resp)
	if g.Kind != "dynamic" || len(g.Rule) == 0 {
		t.Fatalf("grupo creado: %+v", g)
	}
	if got := strings.Join(a.members(g.ID), ","); got != "srv-1,srv-2" {
		t.Fatalf("miembros: %s", got)
	}
	list := decode[[]dynGroupResp](t, op.do("GET", "/groups", nil))
	if list[0].DeviceCount != 2 || list[0].Kind != "dynamic" {
		t.Fatalf("listado: %+v", list)
	}
	// Cambiar la regla recalcula al momento.
	rule := map[string]any{"match": "all", "conditions": []map[string]any{{"field": "hostname", "op": "eq", "value": "srv-2"}}}
	wantStatus(t, op.do("PATCH", "/groups/"+g.ID, map[string]any{"name": "Linux", "rule": rule}), 200)
	if got := strings.Join(a.members(g.ID), ","); got != "srv-2" {
		t.Fatalf("miembros tras cambiar la regla: %s", got)
	}
}

func TestDynamicGroup_RecomputeOnInventory(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Hostname: "pc-1"})
	op := a.loginAs(auth.RoleOperator)
	rule := map[string]any{"match": "all", "conditions": []map[string]any{{"field": "software", "op": "installed", "value": "firefox"}}}
	g := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "Con Firefox", "kind": "dynamic", "rule": rule}))
	if got := a.members(g.ID); got[0] != "" {
		t.Fatalf("antes del inventario: %v", got)
	}
	inv := invWith(50, "on")
	inv.Software = []protocol.Software{{Name: "Mozilla Firefox (x64 es-ES)", Version: "132"}}
	if err := a.store.SaveInventory(ctx, id, *inv, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.RecomputeDevice(ctx, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := a.members(g.ID); got[0] != "pc-1" {
		t.Fatalf("tras el inventario: %v", got)
	}
}

func TestDynamicGroup_RecomputeOnDevicePatch(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Hostname: "pc-1"})
	op := a.loginAs(auth.RoleOperator)
	rule := map[string]any{"match": "all", "conditions": []map[string]any{{"field": "tag", "op": "has", "value": "vip"}}}
	g := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "VIP", "kind": "dynamic", "rule": rule}))
	wantStatus(t, op.do("PATCH", "/devices/"+id.String(), map[string]any{"tags": []string{"vip"}}), 200)
	jobs, _, _ := a.store.ListJobs(ctx, "pending", 1, 50)
	found := false
	for _, j := range jobs {
		found = found || (j.Kind == "groups.recompute" && strings.Contains(string(j.Payload), id.String()))
	}
	if !found {
		t.Fatalf("no se encoló el recálculo del equipo: %+v", jobs)
	}
	_, _ = a.store.RecomputeDevice(ctx, id, time.Now())
	if got := a.members(g.ID); got[0] != "pc-1" {
		t.Fatalf("miembros: %v", got)
	}
}

func TestDynamicGroup_CannotBePrimary(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{})
	op := a.loginAs(auth.RoleOperator)
	g := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "Linux", "kind": "dynamic", "rule": linuxRule()}))
	wantError(t, op.do("PATCH", "/devices/"+id.String(), map[string]any{"group_id": g.ID}), 400, "invalid_request")
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "x", "group_id": g.ID}), 400, "invalid_request")
}

func TestDynamicGroup_KindImmutableAndRuleOnlyDynamic(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	g := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "Linux", "kind": "dynamic", "rule": linuxRule()}))
	wantError(t, op.do("PATCH", "/groups/"+g.ID, map[string]any{"name": "Linux", "kind": "static"}), 400, "invalid_request")
	s := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "Ventas"}))
	if s.Kind != "static" {
		t.Fatalf("el tipo por defecto debe ser estático: %+v", s)
	}
	wantError(t, op.do("PATCH", "/groups/"+s.ID, map[string]any{"name": "Ventas", "rule": linuxRule()}), 400, "invalid_request")
	wantError(t, op.do("POST", "/groups", map[string]any{"name": "Sin regla", "kind": "dynamic"}), 400, "invalid_request")
	wantError(t, op.do("POST", "/groups", map[string]any{"name": "Raro", "kind": "magico"}), 400, "invalid_request")
}

func TestDynamicGroup_InvalidRule(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	rule := map[string]any{"match": "all", "conditions": []map[string]any{{"field": "color", "op": "eq", "value": "rojo"}}}
	resp := op.do("POST", "/groups", map[string]any{"name": "Mal", "kind": "dynamic", "rule": rule})
	if resp.StatusCode != 400 || !strings.Contains(bodyString(resp), "«color» no existe") {
		t.Fatalf("regla inválida: %d", resp.StatusCode)
	}
}

func TestGroupsPreview(t *testing.T) {
	a := newTestAPI(t)
	for _, h := range []string{"srv-c", "srv-a", "srv-b", "win-1"} {
		p := "linux"
		if strings.HasPrefix(h, "win") {
			p = "windows"
		}
		a.seedDevice(seed{Hostname: h, Platform: p})
	}
	op := a.loginAs(auth.RoleOperator)
	type preview struct {
		Count  int `json:"count"`
		Sample []struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"sample"`
	}
	resp := op.do("POST", "/groups/preview", map[string]any{"rule": linuxRule()})
	wantStatus(t, resp, 200)
	p := decode[preview](t, resp)
	if p.Count != 3 || len(p.Sample) != 3 || p.Sample[0].Hostname != "srv-a" {
		t.Fatalf("vista previa: %+v", p)
	}
	if list := decode[[]dynGroupResp](t, op.do("GET", "/groups", nil)); len(list) != 0 {
		t.Fatal("la vista previa no debe crear nada")
	}
	wantError(t, op.do("POST", "/groups/preview", map[string]any{"rule": map[string]any{"match": "all"}}), 400, "invalid_request")
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/groups/preview", map[string]any{"rule": linuxRule()}), 403, "forbidden")
}

func TestDevicesFilterByDynamicGroup(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "srv-1", Platform: "linux"})
	a.seedDevice(seed{Hostname: "win-1", Platform: "windows"})
	g := decode[dynGroupResp](t, a.loginAs(auth.RoleOperator).do("POST", "/groups", map[string]any{"name": "L", "kind": "dynamic", "rule": linuxRule()}))
	if got := strings.Join(a.members(g.ID), ","); got != "srv-1" {
		t.Fatalf("filtro: %s", got)
	}
}

func TestRecomputeAll_RemovesStaleMembers(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Hostname: "pc-1", Tags: []string{"vip"}})
	op := a.loginAs(auth.RoleOperator)
	rule := map[string]any{"match": "all", "conditions": []map[string]any{{"field": "tag", "op": "has", "value": "vip"}}}
	g := decode[dynGroupResp](t, op.do("POST", "/groups", map[string]any{"name": "VIP", "kind": "dynamic", "rule": rule}))
	empty := []string{}
	if err := a.store.UpdateDevice(ctx, id, storeTags(&empty)); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RecomputeAll(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := a.members(g.ID); got[0] != "" {
		t.Fatalf("debería haber salido del grupo: %v", got)
	}
}

func TestDeviceGroupIDs(t *testing.T) {
	a := newTestAPI(t)
	st, _ := a.store.CreateGroup(ctx, "Ventas", "")
	id := a.seedDevice(seed{Platform: "linux", Group: &st.ID})
	g := decode[dynGroupResp](t, a.loginAs(auth.RoleOperator).do("POST", "/groups", map[string]any{"name": "L", "kind": "dynamic", "rule": linuxRule()}))
	ids, err := a.store.DeviceGroupIDs(ctx, id)
	if err != nil || len(ids) != 2 {
		t.Fatalf("grupos: %v %v", ids, err)
	}
	want := map[uuid.UUID]bool{st.ID: true, uuid.MustParse(g.ID): true}
	for _, x := range ids {
		if !want[x] {
			t.Fatalf("grupo inesperado %s", x)
		}
	}
}
