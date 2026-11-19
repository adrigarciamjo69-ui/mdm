package adminapi

import (
	"strings"
	"testing"

	"mdm/internal/server/auth"
)

type settingsResp struct {
	AgentAutoUpdate bool              `json:"agent_auto_update"`
	MaxOfflineDays  int               `json:"max_offline_days"`
	MinAgentVersion string            `json:"min_agent_version"`
	MinOSVersion    map[string]string `json:"min_os_version"`
}

func TestSettingsAPI_Roles(t *testing.T) {
	a := newTestAPI(t)
	ro := a.loginAs(auth.RoleReadonly)
	if got := decode[settingsResp](t, ro.do("GET", "/settings", nil)); !got.AgentAutoUpdate || got.MaxOfflineDays != 14 {
		t.Fatalf("GET: %+v", got)
	}
	wantError(t, ro.do("PUT", "/settings", map[string]any{"max_offline_days": 7}), 403, "forbidden")
	wantError(t, a.loginAs(auth.RoleOperator).do("PUT", "/settings", map[string]any{"max_offline_days": 7}), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	resp := admin.do("PUT", "/settings", map[string]any{"max_offline_days": 7})
	wantStatus(t, resp, 200)
	if got := decode[settingsResp](t, resp); got.MaxOfflineDays != 7 {
		t.Fatalf("PUT: %+v", got)
	}
	e := a.findAudit("settings.updated")
	if e == nil || !strings.Contains(string(e.Details), "max_offline_days") || strings.Contains(string(e.Details), "agent_auto_update") {
		t.Fatalf("auditoría: %+v", e)
	}
}

func TestSettingsAPI_PartialUpdate(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	admin.do("PUT", "/settings", map[string]any{"min_agent_version": "1.1.0"})
	got := decode[settingsResp](t, admin.do("PUT", "/settings", map[string]any{"agent_auto_update": false}))
	if got.MinAgentVersion != "1.1.0" || got.AgentAutoUpdate || got.MaxOfflineDays != 14 {
		t.Fatalf("actualización parcial: %+v", got)
	}
	got = decode[settingsResp](t, admin.do("PUT", "/settings", map[string]any{"min_os_version": map[string]string{"linux": "22.04"}}))
	if got.MinOSVersion["linux"] != "22.04" {
		t.Fatalf("versión mínima de Linux: %+v", got)
	}
}

func TestSettingsAPI_Invalid(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, admin.do("PUT", "/settings", map[string]any{"max_offline_days": 0}), 400, "invalid_request")
	wantError(t, admin.do("PUT", "/settings", map[string]any{"min_agent_version": "uno"}), 400, "invalid_request")
	wantError(t, admin.do("PUT", "/settings", map[string]any{"desconocido": 1}), 400, "invalid_request")
}
