package adminapi

import (
	"testing"

	"mdm/internal/server/auth"
)

func TestAgentPins_AdminOnlyAndStaticGroups(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Piloto", "")
	dyn, _ := a.store.CreateGroupWith(ctx, "Linux", "", "dynamic", []byte(`{"match":"all","conditions":[{"field":"platform","op":"eq","value":"linux"}]}`))
	path := "/agent-pins/" + g.ID.String()
	wantError(t, a.loginAs(auth.RoleOperator).do("PUT", path, map[string]string{"version": "1.0.0"}), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, admin.do("PUT", path, map[string]string{"version": "uno"}), 400, "invalid_request")
	wantError(t, admin.do("PUT", "/agent-pins/"+dyn.ID.String(), map[string]string{"version": "1.0.0"}), 400, "invalid_request")
	wantStatus(t, admin.do("PUT", path, map[string]string{"version": "1.0.0"}), 200)
	type rel struct {
		Pins []struct {
			GroupName string `json:"group_name"`
			Version   string `json:"version"`
		} `json:"pins"`
		Binaries []string `json:"binaries"`
	}
	r := decode[rel](t, a.loginAs(auth.RoleReadonly).do("GET", "/agent-releases", nil))
	if len(r.Pins) != 1 || r.Pins[0].GroupName != "Piloto" || r.Pins[0].Version != "1.0.0" || r.Binaries == nil {
		t.Fatalf("versiones: %+v", r)
	}
	wantStatus(t, admin.do("DELETE", path, nil), 204)
	if r := decode[rel](t, admin.do("GET", "/agent-releases", nil)); len(r.Pins) != 0 {
		t.Fatalf("tras descongelar: %+v", r)
	}
	if a.findAudit("agent.pinned") == nil {
		t.Fatal("falta la auditoría agent.pinned")
	}
}
