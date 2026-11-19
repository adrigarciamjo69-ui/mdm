package adminapi

import (
	"strings"
	"testing"

	"mdm/internal/server/auth"
	"mdm/internal/server/mobile/android/fake"
)

// linkedAndroid deja la integración configurada y vinculada contra el simulador.
func (a *testAPI) linkedAndroid(t *testing.T) *fake.Server {
	f := a.withGoogle(t)
	admin := a.loginAs(auth.RoleAdmin)
	admin.do("PUT", "/integrations/android/key", map[string]string{"key_json": googleKey(t, f.TokenURL())})
	_ = a.store.LinkAndroidEnterprise(ctx, "enterprises/LC0001", "Empresa", nil, a.clock.Now())
	f.Enterprises["enterprises/LC0001"] = map[string]any{"name": "enterprises/LC0001"}
	return f
}

func TestAndroidTokens_CreateListRevoke(t *testing.T) {
	a := newTestAPI(t)
	f := a.linkedAndroid(t)
	g := decode[struct {
		ID string `json:"id"`
	}](t, a.loginAs(auth.RoleOperator).do("POST", "/groups", map[string]string{"name": "Repartidores"}))
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/android/enrollment-tokens", map[string]any{"name": "Furgonetas", "group_id": g.ID, "duration_days": 7, "one_time": false})
	wantStatus(t, resp, 201)
	tok := decode[struct {
		ID        string `json:"id"`
		QRCode    string `json:"qr_code"`
		GroupName string `json:"group_name"`
		ExpiresAt string `json:"expires_at"`
	}](t, resp)
	if tok.QRCode == "" || tok.GroupName != "Repartidores" || len(f.Tokens) != 1 {
		t.Fatalf("%+v", tok)
	}
	wantError(t, op.do("POST", "/android/enrollment-tokens", map[string]any{"name": "x", "duration_days": 91}), 400, "invalid_request")
	list := decode[[]map[string]any](t, a.loginAs(auth.RoleReadonly).do("GET", "/android/enrollment-tokens", nil))
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}
	wantStatus(t, op.do("POST", "/android/enrollment-tokens/"+tok.ID+"/revoke", nil), 204)
	if len(f.Tokens) != 0 || a.findAudit("android.token_created") == nil || a.findAudit("android.token_revoked") == nil {
		t.Fatal("revocar borra el token en Google y queda auditado")
	}
}

func TestAndroidTokens_NotLinked(t *testing.T) {
	a := newTestAPI(t)
	a.withGoogle(t)
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/android/enrollment-tokens", map[string]any{"name": "x", "duration_days": 1}), 409, "conflict")
}

func TestBatches_AndroidCommandsByPlatform(t *testing.T) {
	a := newTestAPI(t)
	phone := a.seedDevice(seed{Hostname: "movil", Platform: "android", SeenAgo: 1})
	pc := a.seedDevice(seed{Hostname: "pc", Platform: "windows", SeenAgo: 1})
	both := []string{phone.String(), pc.String()}
	type batchResp struct {
		Skipped []struct {
			Hostname string `json:"hostname"`
			Reason   string `json:"reason"`
		} `json:"skipped"`
	}
	r := decode[batchResp](t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": "lock", "device_ids": both}))
	if len(r.Skipped) != 1 || r.Skipped[0].Hostname != "pc" || !strings.Contains(r.Skipped[0].Reason, "Android") {
		t.Fatalf("bloquear solo en móviles: %+v", r)
	}
	r = decode[batchResp](t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": "refresh_inventory", "device_ids": both}))
	if len(r.Skipped) != 1 || r.Skipped[0].Hostname != "movil" {
		t.Fatalf("órdenes del agente no van a los móviles: %+v", r)
	}
	r = decode[batchResp](t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": "reboot", "device_ids": both}))
	if len(r.Skipped) != 0 {
		t.Fatalf("reiniciar vale para los dos: %+v", r)
	}
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": "wipe", "device_ids": []string{phone.String()}}), 403, "forbidden")
	wantError(t, a.loginAs(auth.RoleAdmin).do("POST", "/batches", map[string]any{"type": "reset_password", "device_ids": []string{phone.String()},
		"params": map[string]string{"new_password": "12"}}), 400, "invalid_request")
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("POST", "/batches", map[string]any{"type": "reset_password", "device_ids": []string{phone.String()},
		"params": map[string]string{"new_password": "482913"}}), 201)
	wantStatus(t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": "lost_mode_start", "device_ids": []string{phone.String()},
		"params": map[string]string{"message": "Este móvil es de Ejemplo SL", "phone": "+34 900 000 000"}}), 201)
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("POST", "/batches", map[string]any{"type": "wipe", "device_ids": []string{phone.String()},
		"params": map[string]any{"wipe_external_storage": true}}), 201)
}

func TestResetPassword_NotStoredOrShownInClear(t *testing.T) {
	a := newTestAPI(t)
	phone := a.seedDevice(seed{Hostname: "movil", Platform: "android", SeenAgo: 1})
	resp := a.loginAs(auth.RoleAdmin).do("POST", "/batches", map[string]any{"type": "reset_password", "device_ids": []string{phone.String()},
		"params": map[string]string{"new_password": "482913"}})
	wantStatus(t, resp, 201)
	var raw string
	_ = a.store.Pool().QueryRow(ctx, `SELECT payload::text FROM commands`).Scan(&raw)
	if strings.Contains(raw, "482913") || !strings.Contains(raw, "v1:") {
		t.Fatalf("la contraseña se guarda cifrada: %s", raw)
	}
	var cmdID string
	_ = a.store.Pool().QueryRow(ctx, `SELECT id FROM commands`).Scan(&cmdID)
	body := bodyString(a.loginAs(auth.RoleAdmin).do("GET", "/commands/"+cmdID, nil))
	if strings.Contains(body, "482913") || strings.Contains(body, "v1:") {
		t.Fatalf("la API no la muestra: %s", body)
	}
}

func TestAndroidUnlink_RevokesPhones(t *testing.T) {
	a := newTestAPI(t)
	a.linkedAndroid(t)
	phone := a.seedDevice(seed{Hostname: "movil", Platform: "android", SeenAgo: 1})
	pc := a.seedDevice(seed{Hostname: "pc", Platform: "windows", SeenAgo: 1})
	wantStatus(t, a.loginAs(auth.RoleAdmin).do("DELETE", "/integrations/android", nil), 204)
	p, _ := a.store.DeviceByID(ctx, phone)
	w, _ := a.store.DeviceByID(ctx, pc)
	if p.RevokedAt == nil || w.RevokedAt != nil {
		t.Fatal("desvincular revoca los móviles (Google los restablece) y solo los móviles")
	}
}
