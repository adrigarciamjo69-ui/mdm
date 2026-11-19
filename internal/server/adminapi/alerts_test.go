package adminapi

import (
	"encoding/json"
	"testing"
	"time"

	"mdm/internal/server/alerts"
	"mdm/internal/server/auth"
)

type ruleResp struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Kind     string          `json:"kind"`
	Params   json.RawMessage `json:"params"`
	Severity string          `json:"severity"`
	Enabled  bool            `json:"enabled"`
}

type alertResp struct {
	ID       int64  `json:"id"`
	State    string `json:"state"`
	Title    string `json:"title"`
	Hostname string `json:"hostname"`
	Severity string `json:"severity"`
}

func TestAlertRules_CRUDAndValidation(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	body := map[string]any{"name": "Servidores sin conexión", "type": "device.offline", "params": map[string]any{"hours": 1}, "severity": "critical",
		"channels": []string{}, "emails": []string{"guardia@ejemplo.es"}, "notify_resolved": true, "enabled": true,
		"assignments": []map[string]any{{"group_id": nil}}}
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/alert-rules", body), 403, "forbidden")
	resp := admin.do("POST", "/alert-rules", body)
	wantStatus(t, resp, 201)
	r := decode[ruleResp](t, resp)
	if r.Kind != "state" || string(r.Params) != `{"hours":1}` {
		t.Fatalf("%+v", r)
	}
	bad := map[string]any{"name": "x", "type": "device.offline", "params": map[string]any{"hours": 0}, "severity": "critical"}
	wantError(t, admin.do("POST", "/alert-rules", bad), 400, "invalid_request")
	bad = map[string]any{"name": "y", "type": "device.offline", "params": map[string]any{"hours": 2}, "severity": "urgente"}
	wantError(t, admin.do("POST", "/alert-rules", bad), 400, "invalid_request")
	bad = map[string]any{"name": "z", "type": "device.offline", "params": map[string]any{"hours": 2}, "severity": "info", "emails": []string{"no-es"}}
	wantError(t, admin.do("POST", "/alert-rules", bad), 400, "invalid_request")
	body["enabled"] = false
	wantStatus(t, admin.do("PATCH", "/alert-rules/"+r.ID, body), 200)
	wantStatus(t, admin.do("DELETE", "/alert-rules/"+r.ID, nil), 204)
	for _, act := range []string{"alert_rule.created", "alert_rule.updated", "alert_rule.deleted"} {
		if a.findAudit(act) == nil {
			t.Errorf("falta %s", act)
		}
	}
}

func TestAlerts_ListAckResolve(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	a.seedDevice(seed{Hostname: "srv-db-01", SeenAgo: 3 * time.Hour})
	r := decode[ruleResp](t, admin.do("POST", "/alert-rules", map[string]any{"name": "Sin conexión", "type": "device.offline",
		"params": map[string]any{"hours": 1}, "severity": "critical", "enabled": true, "assignments": []map[string]any{{"group_id": nil}}}))
	_ = r
	alerts.New(alerts.Deps{Store: a.store, Now: a.clock.Now}).EvaluateState(ctx)
	list := decode[pageResp[alertResp]](t, a.loginAs(auth.RoleReadonly).do("GET", "/alerts?state=unresolved", nil))
	if list.Total != 1 || list.Items[0].Hostname != "srv-db-01" || list.Items[0].State != "open" {
		t.Fatalf("%+v", list)
	}
	id := list.Items[0].ID
	counts := decode[map[string]int](t, a.loginAs(auth.RoleReadonly).do("GET", "/alerts/counts", nil))
	if counts["critical"] != 1 {
		t.Fatalf("%+v", counts)
	}
	path := "/alerts/" + jsonNum(id)
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", path+"/acknowledge", nil), 403, "forbidden")
	wantStatus(t, a.loginAs(auth.RoleOperator).do("POST", path+"/acknowledge", nil), 204)
	if got := decode[alertResp](t, admin.do("GET", path, nil)); got.State != "acknowledged" {
		t.Fatalf("%+v", got)
	}
	wantStatus(t, a.loginAs(auth.RoleOperator).do("POST", path+"/resolve", nil), 204)
	if got := decode[alertResp](t, admin.do("GET", path, nil)); got.State != "resolved" {
		t.Fatalf("%+v", got)
	}
	if a.findAudit("alert.acknowledged") == nil || a.findAudit("alert.resolved") == nil {
		t.Fatal("falta la auditoría")
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }
