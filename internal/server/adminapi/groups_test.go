package adminapi

import (
	"testing"

	"mdm/internal/server/auth"
)

type groupResp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DeviceCount int    `json:"device_count"`
}

func TestGroups_CRUD(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/groups", map[string]string{"name": "  Ventas ", "description": "Planta 2"})
	wantStatus(t, resp, 201)
	g := decode[groupResp](t, resp)
	if g.ID == "" || g.Name != "Ventas" || g.Description != "Planta 2" {
		t.Fatalf("creado: %+v", g)
	}
	list := decode[[]groupResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/groups", nil))
	if len(list) != 1 || list[0].DeviceCount != 0 || list[0].Name != "Ventas" {
		t.Fatalf("listado: %+v", list)
	}
	resp = op.do("PATCH", "/groups/"+g.ID, map[string]string{"name": "Comercial", "description": ""})
	wantStatus(t, resp, 200)
	if got := decode[groupResp](t, resp); got.Name != "Comercial" || got.Description != "" {
		t.Fatalf("editado: %+v", got)
	}
	wantStatus(t, op.do("DELETE", "/groups/"+g.ID, nil), 204)
	if list := decode[[]groupResp](t, op.do("GET", "/groups", nil)); len(list) != 0 {
		t.Fatalf("tras borrar: %+v", list)
	}
}

func TestGroups_EmptyListIsArray(t *testing.T) {
	a := newTestAPI(t)
	if body := bodyString(a.loginAs(auth.RoleReadonly).do("GET", "/groups", nil)); body != "[]\n" {
		t.Fatalf("listado vacío: %q", body)
	}
}

func TestGroups_DuplicateName(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	wantStatus(t, op.do("POST", "/groups", map[string]string{"name": "Ventas"}), 201)
	wantError(t, op.do("POST", "/groups", map[string]string{"name": "ventas"}), 409, "conflict")
	other := decode[groupResp](t, op.do("POST", "/groups", map[string]string{"name": "Otro"}))
	wantError(t, op.do("PATCH", "/groups/"+other.ID, map[string]string{"name": "VENTAS"}), 409, "conflict")
}

func TestGroups_ReadonlyCannotWrite(t *testing.T) {
	a := newTestAPI(t)
	g := decode[groupResp](t, a.loginAs(auth.RoleOperator).do("POST", "/groups", map[string]string{"name": "Ventas"}))
	ro := a.loginAs(auth.RoleReadonly)
	wantError(t, ro.do("POST", "/groups", map[string]string{"name": "X"}), 403, "forbidden")
	wantError(t, ro.do("PATCH", "/groups/"+g.ID, map[string]string{"name": "X"}), 403, "forbidden")
	wantError(t, ro.do("DELETE", "/groups/"+g.ID, nil), 403, "forbidden")
}

func TestGroups_Validation(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	wantError(t, op.do("POST", "/groups", map[string]string{"name": "   "}), 400, "invalid_request")
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	wantError(t, op.do("POST", "/groups", map[string]string{"name": string(long)}), 400, "invalid_request")
	wantError(t, op.do("PATCH", "/groups/no-es-uuid", map[string]string{"name": "X"}), 400, "invalid_request")
	wantError(t, op.do("PATCH", "/groups/00000000-0000-4000-8000-000000000000", map[string]string{"name": "X"}), 404, "not_found")
	wantError(t, op.do("DELETE", "/groups/00000000-0000-4000-8000-000000000000", nil), 404, "not_found")
}
