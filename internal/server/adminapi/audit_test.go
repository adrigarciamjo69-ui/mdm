package adminapi

import (
	"net/url"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type auditResp struct {
	ID         int64          `json:"id"`
	At         time.Time      `json:"at"`
	ActorEmail string         `json:"actor_email"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Details    map[string]any `json:"details"`
	IP         string         `json:"ip"`
}

func TestAudit_ListFiltersAndPaginates(t *testing.T) {
	a := newTestAPI(t)
	base := a.clock.Now().Add(-10 * time.Hour)
	for i, e := range []store.AuditEntry{
		{ActorEmail: "ana@ejemplo.es", Action: "login.ok"},
		{ActorEmail: "luis@ejemplo.es", Action: "device.revoked", TargetType: "device", TargetID: "d1", Details: []byte(`{"hostname":"PC-1"}`), IP: "10.0.0.9"},
		{ActorEmail: "ana@ejemplo.es", Action: "device.revoked"},
	} {
		e.At = base.Add(time.Duration(i) * time.Hour)
		if err := a.store.InsertAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	admin := a.loginAs(auth.RoleAdmin)
	get := func(q string) pageResp[auditResp] {
		resp := admin.do("GET", "/audit?"+q, nil)
		wantStatus(t, resp, 200)
		return decode[pageResp[auditResp]](t, resp)
	}
	all := get("")
	if all.Total != 3 || !all.Items[0].At.After(all.Items[1].At) {
		t.Fatalf("total %d", all.Total)
	}
	if p := get("per_page=2&page=2"); len(p.Items) != 1 || p.Total != 3 || p.Page != 2 {
		t.Fatalf("paginación: %+v", p)
	}
	rev := get("action=device.revoked")
	if rev.Total != 2 {
		t.Fatalf("por acción: %d", rev.Total)
	}
	one := get("action=device.revoked&actor=luis")
	if one.Total != 1 || one.Items[0].Details["hostname"] != "PC-1" || one.Items[0].IP != "10.0.0.9" || one.Items[0].TargetID != "d1" {
		t.Fatalf("por autor: %+v", one.Items)
	}
	from := url.QueryEscape(base.Add(90 * time.Minute).Format(time.RFC3339))
	to := url.QueryEscape(base.Add(150 * time.Minute).Format(time.RFC3339))
	if p := get("from=" + from + "&to=" + to); p.Total != 1 || p.Items[0].ActorEmail != "ana@ejemplo.es" {
		t.Fatalf("por fechas: %+v", p)
	}
	wantError(t, admin.do("GET", "/audit?from=ayer", nil), 400, "invalid_request")
	wantError(t, admin.do("GET", "/audit?action=inventada", nil), 400, "invalid_request")
}

func TestAudit_NoWriteRoutes(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	for _, m := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		if resp := admin.do(m, "/audit", nil); resp.StatusCode != 405 {
			t.Errorf("%s /audit → %d", m, resp.StatusCode)
		}
	}
}
