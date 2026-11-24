package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

type licenseResp struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	HasKey          bool   `json:"has_key"`
	Seats           int    `json:"seats"`
	Status          string `json:"status"`
	EffectiveStatus string `json:"effective_status"`
	Assigned        int    `json:"assigned"`
	Installed       int    `json:"installed"`
	Over            bool   `json:"over"`
	Assignments     []struct {
		ID    string `json:"id"`
		Asset *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"asset"`
		User *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"user"`
	} `json:"assignments"`
}

func (a *testAPI) newLicense(c *testClient, body map[string]any) licenseResp {
	a.t.Helper()
	for k, v := range map[string]any{"license_type": "suscripcion", "status": "activa", "seats": 0} {
		if _, ok := body[k]; !ok {
			body[k] = v
		}
	}
	resp := c.do("POST", "/licenses", body)
	wantStatus(a.t, resp, 201)
	return decode[licenseResp](a.t, resp)
}

func TestLicenses_KeySealedAndMasked(t *testing.T) {
	a := newTestAPI(t)
	op, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleAdmin)
	l := a.newLicense(op, map[string]any{"name": "Office 2021", "license_key": "AAAAA-BBBBB-CCCCC"})
	if !l.HasKey {
		t.Fatal("sin clave")
	}
	for _, path := range []string{"/licenses", "/licenses/" + l.ID} {
		if body := bodyString(op.do("GET", path, nil)); strings.Contains(body, "AAAAA") {
			t.Fatalf("%s expone la clave: %s", path, body)
		}
	}
	if raw, _ := a.store.LicenseKeySealed(ctx, mustUUID(l.ID)); strings.Contains(raw, "AAAAA") || raw == "" {
		t.Fatalf("la clave no está cifrada: %q", raw)
	}
	wantError(t, op.do("POST", "/licenses/"+l.ID+"/key/reveal", nil), 403, "forbidden")
	resp := admin.do("POST", "/licenses/"+l.ID+"/key/reveal", nil)
	wantStatus(t, resp, 200)
	if k := decode[struct {
		Key string `json:"key"`
	}](t, resp); k.Key != "AAAAA-BBBBB-CCCCC" {
		t.Fatalf("clave: %q", k.Key)
	}
	found := false
	for _, e := range a.auditEntries() {
		if e.Action == "license.key_viewed" && e.TargetID == l.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("ver la clave no está en la auditoría")
	}
	// Editar sin license_key conserva la clave; "" la quita.
	base := map[string]any{"name": "Office 2021", "license_type": "perpetua", "status": "activa", "seats": 5}
	wantStatus(t, op.do("PATCH", "/licenses/"+l.ID, base), 200)
	if got := decode[licenseResp](t, op.do("GET", "/licenses/"+l.ID, nil)); !got.HasKey || got.Seats != 5 {
		t.Fatalf("tras editar: %+v", got)
	}
	base["license_key"] = ""
	wantStatus(t, op.do("PATCH", "/licenses/"+l.ID, base), 200)
	if got := decode[licenseResp](t, op.do("GET", "/licenses/"+l.ID, nil)); got.HasKey {
		t.Fatal("la clave no se ha quitado")
	}
}

func TestLicenses_Validation(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	for _, b := range []map[string]any{
		{"name": "", "license_type": "perpetua", "status": "activa"},
		{"name": "X", "license_type": "pirata", "status": "activa"},
		{"name": "X", "license_type": "perpetua", "status": "rota"},
		{"name": "X", "license_type": "perpetua", "status": "activa", "seats": -1},
		{"name": "X", "license_type": "perpetua", "status": "activa", "expiry_date": "31/12/2026"},
	} {
		wantError(t, op.do("POST", "/licenses", b), 400, "invalid_request")
	}
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/licenses", map[string]any{"name": "X", "license_type": "perpetua", "status": "activa"}), 403, "forbidden")
	l := a.newLicense(op, map[string]any{"name": "X"})
	wantError(t, op.do("DELETE", "/licenses/"+l.ID, nil), 403, "forbidden")
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("DELETE", "/licenses/"+l.ID, nil), 204)
}

func TestLicenses_CountsAndInstallations(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	inv := &protocol.Inventory{CollectedAt: time.Now(), Software: []protocol.Software{{Name: "Microsoft Office Professional Plus 2021", Version: "16.0"}}}
	dev := a.seedDevice(seed{Hostname: "PC-OFFICE", Inventory: inv})
	a.seedDevice(seed{Hostname: "PC-SIN", Inventory: &protocol.Inventory{CollectedAt: time.Now(), Software: []protocol.Software{{Name: "7-Zip"}}}})
	devAsset, _ := a.store.AssetByDevice(ctx, dev)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	m := a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Portátil manual"})

	l := a.newLicense(op, map[string]any{"name": "Office", "seats": 1, "match_pattern": "office professional"})
	wantStatus(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{"asset_id": devAsset.ID.String()}), 201)
	wantStatus(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{"asset_id": m.ID}), 201)
	wantStatus(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{"user_id": ana.ID.String()}), 201)
	wantError(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{"asset_id": m.ID}), 409, "conflict")
	wantError(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{"asset_id": m.ID, "user_id": ana.ID.String()}), 400, "invalid_request")
	wantError(t, op.do("POST", "/licenses/"+l.ID+"/assignments", map[string]string{}), 400, "invalid_request")

	got := decode[licenseResp](t, op.do("GET", "/licenses/"+l.ID, nil))
	if got.Assigned != 3 || got.Installed != 1 || !got.Over || len(got.Assignments) != 3 {
		t.Fatalf("recuento: %+v", got)
	}
	inst := decode[[]struct {
		Hostname string `json:"hostname"`
	}](t, op.do("GET", "/licenses/"+l.ID+"/installations", nil))
	if len(inst) != 1 || inst[0].Hostname != "PC-OFFICE" {
		t.Fatalf("instalaciones: %+v", inst)
	}
	// Las licencias del activo y de la persona.
	al := decode[[]licenseResp](t, op.do("GET", "/assets/"+m.ID+"/licenses", nil))
	if len(al) != 1 || al[0].Name != "Office" {
		t.Fatalf("licencias del activo: %+v", al)
	}
	user := decode[struct {
		Licenses []licenseResp `json:"licenses"`
	}](t, op.do("GET", "/directory/users/"+ana.ID.String(), nil))
	if len(user.Licenses) != 1 {
		t.Fatalf("licencias de la persona: %+v", user)
	}
	wantStatus(t, op.do("DELETE", "/licenses/"+l.ID+"/assignments/"+got.Assignments[0].ID, nil), 204)
	if got := decode[licenseResp](t, op.do("GET", "/licenses/"+l.ID, nil)); got.Assigned != 2 {
		t.Fatalf("tras quitar: %d", got.Assigned)
	}
}

func TestLicenses_ExpiryAndFilters(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	today := a.clock.Now()
	a.newLicense(op, map[string]any{"name": "Caducada", "expiry_date": today.AddDate(0, 0, -1).Format("2006-01-02")})
	a.newLicense(op, map[string]any{"name": "Pronto", "expiry_date": today.AddDate(0, 0, 10).Format("2006-01-02")})
	a.newLicense(op, map[string]any{"name": "Lejos", "expiry_date": today.AddDate(1, 0, 0).Format("2006-01-02")})
	a.newLicense(op, map[string]any{"name": "Perpetua", "license_type": "perpetua"})
	list := decode[pageResp[licenseResp]](t, op.do("GET", "/licenses", nil))
	byName := map[string]licenseResp{}
	for _, l := range list.Items {
		byName[l.Name] = l
	}
	if list.Total != 4 || byName["Caducada"].EffectiveStatus != "expirada" || byName["Pronto"].EffectiveStatus != "activa" {
		t.Fatalf("estados: %+v", list.Items)
	}
	if p := decode[pageResp[licenseResp]](t, op.do("GET", "/licenses?expiring=30", nil)); p.Total != 1 || p.Items[0].Name != "Pronto" {
		t.Fatalf("caducan en 30 días: %+v", p.Items)
	}
	if p := decode[pageResp[licenseResp]](t, op.do("GET", "/licenses?status=expirada", nil)); p.Total != 1 || p.Items[0].Name != "Caducada" {
		t.Fatalf("expiradas: %+v", p.Items)
	}
	if p := decode[pageResp[licenseResp]](t, op.do("GET", "/licenses?q=perp", nil)); p.Total != 1 {
		t.Fatalf("buscar: %+v", p.Items)
	}
}
