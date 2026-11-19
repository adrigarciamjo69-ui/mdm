package adminapi

import (
	"testing"

	"mdm/internal/server/auth"
)

func autoBody(actions []map[string]any) map[string]any {
	return map[string]any{"name": "Limpieza nocturna", "enabled": true, "max_devices": 500,
		"trigger":     map[string]any{"type": "schedule", "frequency": "daily", "time": "02:00"},
		"actions":     actions,
		"assignments": []map[string]any{{"group_id": nil}}}
}

func TestAutomations_RolesSimulateRuns(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "PC-1"})
	a.seedDevice(seed{Hostname: "PC-2"})
	op := a.loginAs(auth.RoleOperator)
	wantError(t, op.do("POST", "/automations", autoBody([]map[string]any{{"type": "reboot"}})), 403, "forbidden")
	resp := op.do("POST", "/automations", autoBody([]map[string]any{{"type": "add_tag", "tag": "revisado"}, {"type": "refresh_inventory"}}))
	wantStatus(t, resp, 201)
	au := decode[struct {
		ID        string  `json:"id"`
		NextRunAt *string `json:"next_run_at"`
	}](t, resp)
	if au.NextRunAt == nil {
		t.Fatal("próxima ejecución")
	}
	sim := decode[struct {
		DeviceCount int      `json:"device_count"`
		Actions     []string `json:"actions"`
	}](t, a.loginAs(auth.RoleReadonly).do("POST", "/automations/"+au.ID+"/simulate", nil))
	if sim.DeviceCount != 2 || len(sim.Actions) != 2 {
		t.Fatalf("%+v", sim)
	}
	if runs := decode[[]map[string]any](t, a.loginAs(auth.RoleReadonly).do("GET", "/automations/"+au.ID+"/runs", nil)); len(runs) != 0 {
		t.Fatalf("simular no deja historial: %+v", runs)
	}
	// Un operador no puede convertirla en una que reinicia.
	wantError(t, op.do("PATCH", "/automations/"+au.ID, autoBody([]map[string]any{{"type": "reboot"}})), 403, "forbidden")
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("PATCH", "/automations/"+au.ID, autoBody([]map[string]any{{"type": "reboot"}})), 200)
	// Y una vez que reinicia, el operador ya no puede tocarla.
	wantError(t, op.do("DELETE", "/automations/"+au.ID, nil), 403, "forbidden")
	body := autoBody([]map[string]any{{"type": "reboot"}})
	body["max_devices"] = 2000
	wantError(t, a.loginAs(auth.RoleAdmin).do("PATCH", "/automations/"+au.ID, body), 400, "invalid_request")
	for _, act := range []string{"automation.created", "automation.updated"} {
		if a.findAudit(act) == nil {
			t.Errorf("falta %s", act)
		}
	}
}

func TestAutomations_GetOne(t *testing.T) {
	a := newTestAPI(t)
	au := decode[struct {
		ID string `json:"id"`
	}](t, a.loginAs(auth.RoleOperator).do("POST", "/automations", autoBody([]map[string]any{{"type": "add_tag", "tag": "x"}})))
	got := decode[struct {
		Name    string           `json:"name"`
		Actions []map[string]any `json:"actions"`
	}](t, a.loginAs(auth.RoleReadonly).do("GET", "/automations/"+au.ID, nil))
	if got.Name != "Limpieza nocturna" || len(got.Actions) != 1 {
		t.Fatalf("%+v", got)
	}
}
