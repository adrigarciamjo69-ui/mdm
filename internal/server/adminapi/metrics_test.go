package adminapi

import (
	"testing"

	"mdm/internal/server/auth"
)

func TestMetricsAndPreferences(t *testing.T) {
	a := newTestAPI(t)
	_ = a.store.SaveMetrics(ctx, a.clock.Now().Format("2006-01-02"), map[string]float64{"devices.total": 42, "compliance.percent": 87.5})
	ro := a.loginAs(auth.RoleReadonly)
	m := decode[map[string][]struct {
		Day   string  `json:"day"`
		Value float64 `json:"value"`
	}](t, ro.do("GET", "/metrics?keys=devices.total,compliance.percent&days=90", nil))
	if len(m["devices.total"]) != 1 || m["devices.total"][0].Value != 42 || m["compliance.percent"][0].Value != 87.5 {
		t.Fatalf("%+v", m)
	}
	wantError(t, ro.do("GET", "/metrics?keys=inventada&days=90", nil), 400, "invalid_request")
	wantError(t, ro.do("GET", "/metrics?keys=devices.total&days=500", nil), 400, "invalid_request")
	type prefs struct {
		SummarySections []string `json:"summary_sections"`
	}
	if p := decode[prefs](t, ro.do("GET", "/me/preferences", nil)); len(p.SummarySections) != 0 {
		t.Fatalf("sin preferencias, vacío (la consola usa su orden): %+v", p)
	}
	wantStatus(t, ro.do("PUT", "/me/preferences", map[string]any{"summary_sections": []string{"alerts", "status", "trends"}}), 200)
	if p := decode[prefs](t, ro.do("GET", "/me/preferences", nil)); len(p.SummarySections) != 3 || p.SummarySections[0] != "alerts" {
		t.Fatalf("%+v", p)
	}
	wantError(t, ro.do("PUT", "/me/preferences", map[string]any{"summary_sections": []string{"inventada"}}), 400, "invalid_request")
	if p := decode[prefs](t, a.loginAs(auth.RoleAdmin).do("GET", "/me/preferences", nil)); len(p.SummarySections) != 0 {
		t.Fatal("las preferencias son de cada administrador")
	}
	wantStatus(t, ro.do("PUT", "/me/preferences", map[string]any{"summary_sections": nil}), 200)
	if p := decode[prefs](t, ro.do("GET", "/me/preferences", nil)); len(p.SummarySections) != 0 {
		t.Fatal("restablecer")
	}
}
