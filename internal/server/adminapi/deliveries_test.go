package adminapi

import (
	"encoding/base64"
	"strings"
	"testing"

	"mdm/internal/server/auth"
)

type deliveryResp struct {
	ID          string `json:"id"`
	Number      string `json:"number"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Responsible string `json:"responsible"`
	User        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	Items []struct {
		AssetID  *string `json:"asset_id"`
		Category string  `json:"category"`
		Brand    string  `json:"brand"`
		Model    string  `json:"model"`
		Serial   string  `json:"serial"`
	} `json:"items"`
}

func (a *testAPI) newDelivery(c *testClient, body map[string]any) deliveryResp {
	a.t.Helper()
	if _, ok := body["date"]; !ok {
		body["date"] = "2026-11-24"
	}
	resp := c.do("POST", "/deliveries", body)
	wantStatus(a.t, resp, 201)
	return decode[deliveryResp](a.t, resp)
}

func TestDelivery_ConfirmEntregaAssigns(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana Pérez", "ana@electrans.es")
	lap := a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Portátil", "brand": "Lenovo", "model": "T14", "serial": "LAP-0001"})
	mon := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor", "serial": "MON-0001"})
	d := a.newDelivery(op, map[string]any{"type": "entrega", "user_id": ana.ID.String(), "recipient_dni": "12345678Z",
		"items": []map[string]any{{"asset_id": lap.ID}, {"asset_id": mon.ID, "observations": "Con cable HDMI"},
			{"category": "Periférico", "model": "Cargador USB-C", "serial": ""}}})
	if d.Status != "borrador" || d.Number != "" || len(d.Items) != 3 || d.Items[0].Brand != "Lenovo" || d.Items[0].Category != "Portátil" ||
		d.Items[2].Model != "Cargador USB-C" || !strings.Contains(d.Responsible, "@ejemplo.es") {
		t.Fatalf("borrador: %+v", d)
	}
	resp := op.do("POST", "/deliveries/"+d.ID+"/confirm", nil)
	wantStatus(t, resp, 200)
	c := decode[deliveryResp](t, resp)
	if c.Status != "confirmada" || c.Number != "ENT-2026-0001" {
		t.Fatalf("confirmada: %+v", c)
	}
	for _, id := range []string{lap.ID, mon.ID} {
		got := decode[assetResp](t, op.do("GET", "/assets/"+id, nil))
		if got.Owner == nil || got.Owner.ID != ana.ID.String() || got.Status != "en_uso" {
			t.Fatalf("activo tras la entrega: %+v", got)
		}
	}
	// Ya no se puede editar ni borrar ni confirmar de nuevo.
	wantError(t, op.do("POST", "/deliveries/"+d.ID+"/confirm", nil), 409, "conflict")
	wantError(t, op.do("PATCH", "/deliveries/"+d.ID, map[string]any{"type": "entrega", "user_id": ana.ID.String(), "date": "2026-11-24"}), 409, "conflict")
	wantError(t, a.loginAs(auth.RoleAdmin).do("DELETE", "/deliveries/"+d.ID, nil), 409, "conflict")
	// Las actas del activo y de la persona.
	if l := decode[[]deliveryResp](t, op.do("GET", "/assets/"+lap.ID+"/deliveries", nil)); len(l) != 1 {
		t.Fatalf("actas del activo: %+v", l)
	}
	user := decode[struct {
		Deliveries []deliveryResp `json:"deliveries"`
	}](t, op.do("GET", "/directory/users/"+ana.ID.String(), nil))
	if len(user.Deliveries) != 1 {
		t.Fatalf("actas de la persona: %+v", user)
	}
}

func TestDelivery_ConfirmDevolucionReturnsToStock(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	lap := a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Portátil", "status": "en_uso", "owner_id": ana.ID.String()})
	d := a.newDelivery(op, map[string]any{"type": "devolucion", "user_id": ana.ID.String(), "items": []map[string]any{{"asset_id": lap.ID}}})
	c := decode[deliveryResp](t, op.do("POST", "/deliveries/"+d.ID+"/confirm", nil))
	if c.Number != "DEV-2026-0001" {
		t.Fatalf("número: %s", c.Number)
	}
	if got := decode[assetResp](t, op.do("GET", "/assets/"+lap.ID, nil)); got.Owner != nil || got.Status != "en_almacen" {
		t.Fatalf("activo tras la devolución: %+v", got)
	}
}

func TestDelivery_NumberingPerTypeAndYear(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	confirm := func(typ, date string) string {
		d := a.newDelivery(op, map[string]any{"type": typ, "user_id": ana.ID.String(), "date": date,
			"items": []map[string]any{{"category": "Otro", "model": "Ratón"}}})
		return decode[deliveryResp](t, op.do("POST", "/deliveries/"+d.ID+"/confirm", nil)).Number
	}
	got := []string{confirm("entrega", "2026-03-01"), confirm("entrega", "2026-05-01"), confirm("devolucion", "2026-05-02"), confirm("entrega", "2027-01-10")}
	want := []string{"ENT-2026-0001", "ENT-2026-0002", "DEV-2026-0001", "ENT-2027-0001"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("números: %v", got)
		}
	}
}

func TestDelivery_ValidationAndTrashedAsset(t *testing.T) {
	a := newTestAPI(t)
	op, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleAdmin)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	old := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Viejo"})
	wantStatus(t, admin.do("DELETE", "/assets/"+old.ID, map[string]string{}), 204)
	for _, b := range []map[string]any{
		{"type": "prestamo", "user_id": ana.ID.String(), "date": "2026-11-24"},
		{"type": "entrega", "user_id": "00000000-0000-4000-8000-000000000000", "date": "2026-11-24"},
		{"type": "entrega", "user_id": ana.ID.String(), "date": "24/11/2026"},
		{"type": "entrega", "user_id": ana.ID.String(), "date": "2026-11-24", "items": []map[string]any{{"asset_id": old.ID}}},
		{"type": "entrega", "user_id": ana.ID.String(), "date": "2026-11-24", "items": []map[string]any{{"observations": "nada"}}},
	} {
		wantError(t, op.do("POST", "/deliveries", b), 400, "invalid_request")
	}
	empty := a.newDelivery(op, map[string]any{"type": "entrega", "user_id": ana.ID.String()})
	wantError(t, op.do("POST", "/deliveries/"+empty.ID+"/confirm", nil), 400, "invalid_request")
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/deliveries", map[string]any{"type": "entrega", "user_id": ana.ID.String(), "date": "2026-11-24"}), 403, "forbidden")
	// El borrador se edita y se borra.
	wantStatus(t, op.do("PATCH", "/deliveries/"+empty.ID, map[string]any{"type": "devolucion", "user_id": ana.ID.String(), "date": "2026-11-25",
		"items": []map[string]any{{"category": "Otro", "model": "Teclado"}}}), 200)
	wantError(t, op.do("DELETE", "/deliveries/"+empty.ID, nil), 403, "forbidden")
	wantStatus(t, admin.do("DELETE", "/deliveries/"+empty.ID, nil), 204)
}

func TestDelivery_CancelAndList(t *testing.T) {
	a := newTestAPI(t)
	op, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleAdmin)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	d := a.newDelivery(op, map[string]any{"type": "entrega", "user_id": ana.ID.String(), "items": []map[string]any{{"category": "Otro", "model": "Ratón"}}})
	wantError(t, admin.do("POST", "/deliveries/"+d.ID+"/cancel", map[string]string{"reason": "x"}), 409, "conflict") // borrador
	op.do("POST", "/deliveries/"+d.ID+"/confirm", nil)
	wantError(t, op.do("POST", "/deliveries/"+d.ID+"/cancel", map[string]string{"reason": "Error"}), 403, "forbidden")
	wantError(t, admin.do("POST", "/deliveries/"+d.ID+"/cancel", map[string]string{"reason": ""}), 400, "invalid_request")
	wantStatus(t, admin.do("POST", "/deliveries/"+d.ID+"/cancel", map[string]string{"reason": "Duplicada"}), 200)
	list := decode[pageResp[deliveryResp]](t, op.do("GET", "/deliveries?status=anulada", nil))
	if list.Total != 1 || list.Items[0].User.Name != "Ana" {
		t.Fatalf("anuladas: %+v", list)
	}
	if l := decode[pageResp[deliveryResp]](t, op.do("GET", "/deliveries?q=ENT-2026", nil)); l.Total != 1 {
		t.Fatalf("buscar por número: %+v", l)
	}
}

func TestDeliverySettings(t *testing.T) {
	a := newTestAPI(t)
	admin, ro := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleReadonly)
	def := decode[map[string]any](t, ro.do("GET", "/delivery-settings", nil))
	clauses := def["clauses"].(map[string]any)
	if len(clauses["entrega"].([]any)) != 5 || len(clauses["devolucion"].([]any)) != 4 {
		t.Fatalf("cláusulas por defecto: %+v", clauses)
	}
	def["company"] = "Electrans S.A."
	def["city"] = "Barcelona"
	wantError(t, ro.do("PUT", "/delivery-settings", def), 403, "forbidden")
	wantStatus(t, admin.do("PUT", "/delivery-settings", def), 200)
	if got := decode[map[string]any](t, ro.do("GET", "/delivery-settings", nil)); got["company"] != "Electrans S.A." {
		t.Fatalf("guardado: %+v", got)
	}
	def["logo"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600<<10)...))
	wantError(t, admin.do("PUT", "/delivery-settings", def), 400, "invalid_request")
	def["logo"] = "data:image/svg+xml;base64,PHN2Zz4="
	wantError(t, admin.do("PUT", "/delivery-settings", def), 400, "invalid_request")
	def["logo"] = ""
	def["colors"] = map[string]any{"entrega": map[string]string{"primary": "rojo", "footer": "#0f172a"}, "devolucion": map[string]string{"primary": "#1c3ca8", "footer": "#0f172a"}}
	wantError(t, admin.do("PUT", "/delivery-settings", def), 400, "invalid_request")
}
