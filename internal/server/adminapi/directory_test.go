package adminapi

import (
	"strings"
	"testing"

	"mdm/internal/server/auth"
	"mdm/internal/server/directory/fake"
	"mdm/internal/server/store"
)

type dirUserResp struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Status      string  `json:"status"`
	AssetCount  int     `json:"asset_count"`
	DeletedAt   *string `json:"deleted_at"`
}

func (a *testAPI) withGraph(t *testing.T) *fake.Server {
	f := fake.New()
	t.Cleanup(f.Close)
	a.rebuild(func(d *Deps) { d.AuthBase, d.GraphBase = f.AuthBase(), f.GraphBase() })
	return f
}

func TestEntraConfig_SecretMaskedAndTest(t *testing.T) {
	a := newTestAPI(t)
	f := a.withGraph(t)
	f.Users = []fake.User{{ID: "u1", DisplayName: "Ana", Mail: "ana@electrans.es", AccountEnabled: true}}
	admin, op := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleOperator)
	body := map[string]any{"tenant_id": "electrans.onmicrosoft.com", "client_id": "11111111-2222-3333-4444-555555555555",
		"client_secret": "secreto", "secret_expires": "2027-06-30"}
	wantError(t, op.do("PUT", "/integrations/entra", body), 403, "forbidden")
	wantError(t, admin.do("PUT", "/integrations/entra", map[string]any{"tenant_id": "", "client_id": "x", "client_secret": "s"}), 400, "invalid_request")
	wantStatus(t, admin.do("PUT", "/integrations/entra", body), 200)
	got := bodyString(admin.do("GET", "/integrations/entra", nil))
	if strings.Contains(got, `"secreto"`) || !strings.Contains(got, `"client_secret":"********"`) || !strings.Contains(got, `"configured":true`) {
		t.Fatalf("configuración: %s", got)
	}
	// La prueba con la máscara usa el secreto guardado.
	body["client_secret"] = "********"
	resp := admin.do("POST", "/integrations/entra/test", body)
	wantStatus(t, resp, 200)
	if r := decode[struct {
		Users int `json:"users"`
	}](t, resp); r.Users != 1 {
		t.Fatalf("prueba: %+v", r)
	}
	body["client_secret"] = "malo"
	wantError(t, admin.do("POST", "/integrations/entra/test", body), 400, "invalid_request")
	// Guardar con la máscara conserva el secreto.
	body["client_secret"] = "********"
	wantStatus(t, admin.do("PUT", "/integrations/entra", body), 200)
	wantStatus(t, op.do("POST", "/directory/sync", nil), 200)
	page := decode[pageResp[dirUserResp]](t, op.do("GET", "/directory/users", nil))
	if page.Total != 1 || page.Items[0].Email != "ana@electrans.es" || page.Items[0].Status != "active" {
		t.Fatalf("usuarios: %+v", page)
	}
}

func TestDirectoryUsers_ListDetailAndLeavers(t *testing.T) {
	a := newTestAPI(t)
	op, ro := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleReadonly)
	ana := a.dirUser("e1", "Ana Pérez", "ana@electrans.es")
	a.dirUser("e2", "Luis Gil", "luis@electrans.es")
	a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Portátil Ana", "owner_id": ana.ID.String(), "status": "en_uso"})

	page := decode[pageResp[dirUserResp]](t, ro.do("GET", "/directory/users?q=ana", nil))
	if page.Total != 1 || page.Items[0].AssetCount != 1 {
		t.Fatalf("buscar: %+v", page)
	}
	detail := decode[struct {
		dirUserResp
		Assets []assetResp `json:"assets"`
	}](t, ro.do("GET", "/directory/users/"+ana.ID.String(), nil))
	if detail.DisplayName != "Ana Pérez" || len(detail.Assets) != 1 || detail.Assets[0].Name != "Portátil Ana" {
		t.Fatalf("ficha: %+v", detail)
	}
	// Ana desaparece de Entra: aparece entre las bajas con material.
	luis, _ := a.store.DirectoryUserByEntraID(ctx, "e2")
	if err := a.store.ApplyDirectorySync(ctx, []storeUser{luis}, a.clock.Now()); err != nil {
		t.Fatal(err)
	}
	leavers := decode[pageResp[dirUserResp]](t, ro.do("GET", "/directory/users?status=leavers", nil))
	if leavers.Total != 1 || leavers.Items[0].Status != "deleted" {
		t.Fatalf("bajas con material: %+v", leavers)
	}
	wantError(t, ro.do("GET", "/directory/users?status=raro", nil), 400, "invalid_request")
	wantError(t, ro.do("PATCH", "/directory/users/"+ana.ID.String(), map[string]string{"notes": "x"}), 403, "forbidden")
	wantStatus(t, op.do("PATCH", "/directory/users/"+ana.ID.String(), map[string]string{"notes": "Devolver portátil"}), 200)
}

func TestDirectorySync_NotConfigured(t *testing.T) {
	a := newTestAPI(t)
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/directory/sync", nil), 409, "conflict")
	st := decode[struct {
		Configured bool `json:"configured"`
	}](t, a.loginAs(auth.RoleReadonly).do("GET", "/directory/status", nil))
	if st.Configured {
		t.Fatal("configurado")
	}
}

type storeUser = store.DirectoryUser
