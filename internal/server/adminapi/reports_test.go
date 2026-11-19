package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
)

func TestReportsAPI_ViewAndCSV(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "PC-VIEJO", SeenAgo: 40 * 24 * time.Hour})
	a.seedDevice(seed{Hostname: "PC-NUEVO", SeenAgo: time.Minute})
	ro := a.loginAs(auth.RoleReadonly)
	defs := decode[[]struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
	}](t, ro.do("GET", "/reports", nil))
	if len(defs) != 9 {
		t.Fatalf("%d informes", len(defs))
	}
	type res struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
		Total   int        `json:"total"`
	}
	r := decode[res](t, ro.do("GET", "/reports/inactive?days=30", nil))
	if r.Total != 1 || r.Rows[0][0] != "PC-VIEJO" {
		t.Fatalf("%+v", r)
	}
	resp := ro.do("GET", "/reports/inactive?days=30&format=csv", nil)
	wantStatus(t, resp, 200)
	body := bodyString(resp)
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") || !strings.Contains(body, "PC-VIEJO") || !strings.Contains(resp.Header.Get("Content-Disposition"), "inactive-") {
		t.Fatalf("csv: %q", body)
	}
	wantError(t, ro.do("GET", "/reports/inventado", nil), 404, "not_found")
	wantError(t, ro.do("GET", "/reports/inactive?days=0", nil), 400, "invalid_request")
}

func TestReportSchedules_CRUDAndRoles(t *testing.T) {
	a := newTestAPI(t)
	ch := decode[channelResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/channels", map[string]any{"name": "IT", "type": "email", "recipients": []string{"it@ejemplo.es"}}))
	body := map[string]any{"report": "patches_by_device", "filters": map[string]any{}, "frequency": "weekly", "time": "08:00", "day": 1, "channel_id": ch.ID, "enabled": true}
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/report-schedules", body), 403, "forbidden")
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/report-schedules", body)
	wantStatus(t, resp, 201)
	sc := decode[struct {
		ID        string  `json:"id"`
		NextRunAt *string `json:"next_run_at"`
	}](t, resp)
	if sc.NextRunAt == nil {
		t.Fatal("se informa de la próxima ejecución")
	}
	bad := map[string]any{"report": "patches_by_device", "frequency": "weekly", "time": "25:00", "day": 1, "channel_id": ch.ID}
	wantError(t, op.do("POST", "/report-schedules", bad), 400, "invalid_request")
	bad = map[string]any{"report": "inventado", "frequency": "daily", "time": "08:00", "channel_id": ch.ID}
	wantError(t, op.do("POST", "/report-schedules", bad), 400, "invalid_request")
	body["enabled"] = false
	wantStatus(t, op.do("PATCH", "/report-schedules/"+sc.ID, body), 200)
	if list := decode[[]map[string]any](t, a.loginAs(auth.RoleReadonly).do("GET", "/report-schedules", nil)); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	wantStatus(t, op.do("DELETE", "/report-schedules/"+sc.ID, nil), 204)
	if a.findAudit("report_schedule.created") == nil || a.findAudit("report_schedule.deleted") == nil {
		t.Fatal("falta la auditoría")
	}
}
