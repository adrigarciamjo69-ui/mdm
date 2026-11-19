package adminapi

import (
	"encoding/json"
	"strings"
	"testing"

	"mdm/internal/server/auth"
)

type policyResp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Priority int    `json:"priority"`
	Settings []struct {
		Key    string          `json:"key"`
		Mode   string          `json:"mode"`
		Params json.RawMessage `json:"params"`
	} `json:"settings"`
	Assignments []struct {
		GroupID   *string `json:"group_id"`
		GroupName string  `json:"group_name"`
		Exclude   bool    `json:"exclude"`
	} `json:"assignments"`
}

func newPolicy(name, platform string, settings ...map[string]any) map[string]any {
	if settings == nil {
		settings = []map[string]any{}
	}
	return map[string]any{"name": name, "platform": platform, "settings": settings}
}

func setting(key, mode string, params map[string]any) map[string]any {
	return map[string]any{"key": key, "mode": mode, "params": params}
}

func TestPoliciesAPI_CRUDAndRoles(t *testing.T) {
	a := newTestAPI(t)
	body := newPolicy("Seguridad básica", "windows", setting("windows.screen_lock", "enforce", map[string]any{"minutes": 10}), setting("windows.bitlocker", "enforce", nil))
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/policies", body), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	resp := admin.do("POST", "/policies", body)
	wantStatus(t, resp, 201)
	p := decode[policyResp](t, resp)
	if p.Priority != 1 || len(p.Settings) != 2 || p.Platform != "windows" {
		t.Fatalf("creada: %+v", p)
	}
	for _, s := range p.Settings {
		if s.Key == "windows.bitlocker" && string(s.Params) != `{"escrow_key":true}` {
			t.Fatalf("no se aplicaron los valores por defecto: %s", s.Params)
		}
	}
	wantError(t, admin.do("POST", "/policies", newPolicy("SEGURIDAD BÁSICA", "linux")), 409, "conflict")
	resp = admin.do("POST", "/policies", newPolicy("Mal", "windows", setting("windows.screen_lock", "enforce", map[string]any{"minutes": 0})))
	if resp.StatusCode != 400 || !strings.Contains(bodyString(resp), "entre 1 y 60") {
		t.Fatalf("ajuste inválido: %d", resp.StatusCode)
	}
	wantError(t, admin.do("POST", "/policies", newPolicy("Plataforma", "macos")), 400, "invalid_request")
	resp = admin.do("POST", "/policies", newPolicy("Dos iguales", "linux", setting("linux.firewall", "enforce", nil), setting("linux.firewall", "audit", nil)))
	wantError(t, resp, 400, "invalid_request")

	// Lectura para todos.
	list := decode[[]policyResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/policies", nil))
	if len(list) != 1 {
		t.Fatalf("listado: %+v", list)
	}
	// Editar: la plataforma no cambia.
	resp = admin.do("PATCH", "/policies/"+p.ID, map[string]any{"name": "Seguridad", "description": "", "settings": []map[string]any{setting("windows.firewall", "audit", nil)}})
	wantStatus(t, resp, 200)
	if got := decode[policyResp](t, resp); got.Name != "Seguridad" || len(got.Settings) != 1 || got.Platform != "windows" {
		t.Fatalf("editada: %+v", got)
	}
	wantError(t, admin.do("PATCH", "/policies/"+p.ID, map[string]any{"name": "x", "platform": "linux", "settings": []any{}}), 400, "invalid_request")
	wantStatus(t, admin.do("DELETE", "/policies/"+p.ID, nil), 204)
	wantError(t, admin.do("GET", "/policies/"+p.ID, nil), 404, "not_found")
}

func TestPoliciesAPI_Assignments(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	g, _ := a.store.CreateGroup(ctx, "Portátiles", "")
	srv, _ := a.store.CreateGroup(ctx, "Servidores", "")
	p := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("P", "windows", setting("windows.firewall", "enforce", nil))))
	resp := admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{
		{"group_id": g.ID, "exclude": false}, {"group_id": srv.ID, "exclude": true}}})
	wantStatus(t, resp, 200)
	got := decode[policyResp](t, resp)
	if len(got.Assignments) != 2 || got.Assignments[0].GroupName != "Portátiles" || !got.Assignments[1].Exclude {
		t.Fatalf("asignaciones: %+v", got.Assignments)
	}
	// Todos los equipos (group_id nulo) solo como inclusión.
	wantError(t, admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil, "exclude": true}}}), 400, "invalid_request")
	wantError(t, admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": "00000000-0000-4000-8000-000000000000"}}}), 400, "invalid_request")
	wantStatus(t, admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}}), 200)
}

func TestPoliciesAPI_Reorder(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	p1 := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("Uno", "linux")))
	p2 := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("Dos", "linux")))
	p3 := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("Tres", "windows")))
	wantStatus(t, admin.do("POST", "/policies/reorder", map[string]any{"ids": []string{p3.ID, p1.ID, p2.ID}}), 204)
	list := decode[[]policyResp](t, admin.do("GET", "/policies", nil))
	if list[0].Name != "Tres" || list[0].Priority != 1 || list[2].Name != "Dos" {
		t.Fatalf("orden: %+v", list)
	}
	wantError(t, admin.do("POST", "/policies/reorder", map[string]any{"ids": []string{p1.ID, p2.ID}}), 409, "conflict")
	// Una política nueva se coloca al final.
	p4 := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("Cuatro", "linux")))
	if p4.Priority != 4 {
		t.Fatalf("prioridad de la nueva: %d", p4.Priority)
	}
}

func TestPoliciesAPI_Audit(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	p := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("P", "linux")))
	admin.do("PATCH", "/policies/"+p.ID, map[string]any{"name": "P2", "description": "", "settings": []any{}})
	admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}})
	admin.do("DELETE", "/policies/"+p.ID, nil)
	for _, act := range []string{"policy.created", "policy.updated", "policy.assigned", "policy.deleted"} {
		if e := a.findAudit(act); e == nil || e.TargetID != p.ID {
			t.Errorf("falta %s: %+v", act, e)
		}
	}
}

func TestPolicyCatalogEndpoint(t *testing.T) {
	a := newTestAPI(t)
	type def struct {
		Key       string `json:"key"`
		Title     string `json:"title"`
		AuditOnly bool   `json:"audit_only"`
		Fields    []struct {
			Name string `json:"name"`
		} `json:"fields"`
	}
	resp := a.loginAs(auth.RoleReadonly).do("GET", "/policy-catalog?platform=linux", nil)
	body := bodyString(resp)
	if strings.Contains(body, `"fields":null`) {
		t.Fatal("un ajuste sin parámetros debe tener fields: [], no null (la consola los recorre)")
	}
	list := decode[[]def](t, a.loginAs(auth.RoleReadonly).do("GET", "/policy-catalog?platform=linux", nil))
	if len(list) != 6 {
		t.Fatalf("catálogo de Linux: %d", len(list))
	}
	wantError(t, a.loginAs(auth.RoleReadonly).do("GET", "/policy-catalog?platform=macos", nil), 400, "invalid_request")
}

func TestPolicyDevicesEndpoint(t *testing.T) {
	a := newTestAPI(t)
	srv, _ := a.store.CreateGroup(ctx, "Servidores", "")
	a.seedDevice(seed{Hostname: "srv-1", Platform: "linux", Group: &srv.ID})
	a.seedDevice(seed{Hostname: "srv-2", Platform: "linux"})
	a.seedDevice(seed{Hostname: "srv-3", Platform: "linux", Group: &srv.ID, Revoked: true})
	a.seedDevice(seed{Hostname: "win-1", Platform: "windows", Group: &srv.ID})
	admin := a.loginAs(auth.RoleAdmin)
	p := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("P", "linux")))
	admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": srv.ID}}})
	type dev struct {
		Hostname string `json:"hostname"`
	}
	got := decode[[]dev](t, a.loginAs(auth.RoleReadonly).do("GET", "/policies/"+p.ID+"/devices", nil))
	if len(got) != 1 || got[0].Hostname != "srv-1" {
		t.Fatalf("equipos: %+v", got)
	}
}

func TestEffectivePolicyFor(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Platform: "linux"})
	admin := a.loginAs(auth.RoleAdmin)
	p := decode[policyResp](t, admin.do("POST", "/policies", newPolicy("P", "linux", setting("linux.ssh", "enforce", map[string]any{"root_login": false}))))
	eff, _, _ := a.store.EffectivePolicyFor(ctx, id)
	if len(eff.Settings) != 0 {
		t.Fatal("sin asignar no debe aplicar")
	}
	admin.do("PUT", "/policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}})
	eff, _, _ = a.store.EffectivePolicyFor(ctx, id)
	var params map[string]bool
	if len(eff.Settings) == 1 {
		_ = json.Unmarshal(eff.Settings[0].Params, &params)
	}
	if len(eff.Settings) != 1 || eff.Settings[0].Key != "linux.ssh" || len(params) != 2 || params["root_login"] || params["password_auth"] {
		t.Fatalf("efectiva: %+v (%s)", eff, eff.Settings[0].Params)
	}
}
