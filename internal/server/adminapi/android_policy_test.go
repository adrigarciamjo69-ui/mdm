package adminapi

import (
	"encoding/json"
	"strings"
	"testing"

	"mdm/internal/server/auth"
)

func TestAndroidPolicy_WifiPassphraseSealedAndMasked(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wifi := setting("android.wifi", "enforce", map[string]any{"ssid": "Oficina", "passphrase": "secreta123"})
	resp := admin.do("POST", "/policies", newPolicy("Móviles", "android", wifi, setting("android.camera", "enforce", map[string]any{"disabled": true})))
	wantStatus(t, resp, 201)
	body := bodyString(resp)
	if strings.Contains(body, "secreta123") || !strings.Contains(body, `"passphrase":"********"`) {
		t.Fatalf("la API nunca devuelve la contraseña: %s", body)
	}
	var raw string
	_ = a.store.Pool().QueryRow(ctx, `SELECT settings::text FROM policies`).Scan(&raw)
	if strings.Contains(raw, "secreta123") || !strings.Contains(raw, `v1:`) {
		t.Fatalf("en la base de datos va cifrada: %s", raw)
	}
	list := decode[[]policyResp](t, admin.do("GET", "/policies", nil))
	// Guardar sin cambiar la contraseña (llega el marcador) conserva la cifrada.
	resp = admin.do("PATCH", "/policies/"+list[0].ID, map[string]any{"name": "Móviles", "description": "",
		"settings": []map[string]any{setting("android.wifi", "enforce", map[string]any{"ssid": "Oficina 2", "passphrase": "********"})}})
	wantStatus(t, resp, 200)
	var raw2 string
	_ = a.store.Pool().QueryRow(ctx, `SELECT settings::text FROM policies`).Scan(&raw2)
	var before, after []struct {
		Params map[string]string `json:"params"`
	}
	_ = json.Unmarshal([]byte(raw), &before)
	_ = json.Unmarshal([]byte(raw2), &after)
	var oldPass string
	for _, s := range before {
		if s.Params["passphrase"] != "" {
			oldPass = s.Params["passphrase"]
		}
	}
	if after[0].Params["passphrase"] != oldPass || after[0].Params["ssid"] != "Oficina 2" {
		t.Fatalf("el marcador conserva la contraseña anterior: %+v", after)
	}
	plain, err := a.box.Open(oldPass)
	if err != nil || string(plain) != "secreta123" {
		t.Fatalf("se descifra con la clave del servidor: %q %v", plain, err)
	}
	// Un marcador sin contraseña anterior es un error.
	wantError(t, admin.do("POST", "/policies", newPolicy("Otra", "android",
		setting("android.wifi", "enforce", map[string]any{"ssid": "X", "passphrase": "********"}))), 400, "invalid_request")
	wantError(t, admin.do("POST", "/policies", newPolicy("Audit", "android", setting("android.camera", "audit", map[string]any{"disabled": true}))), 400, "invalid_request")
}

func TestApps_GooglePlay(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantStatus(t, admin.do("POST", "/apps", newApp("Teams", "android", "google_play", "com.microsoft.teams")), 201)
	wantError(t, admin.do("POST", "/apps", newApp("Mal", "android", "google_play", "teams")), 400, "invalid_request")
	wantError(t, admin.do("POST", "/apps", newApp("Mal2", "android", "apt", "nginx")), 400, "invalid_request")
	wantError(t, admin.do("POST", "/apps", newApp("Mal3", "windows", "google_play", "com.x.y")), 400, "invalid_request")
	list := decode[[]appResp](t, admin.do("GET", "/apps", nil))
	ok := decode[deploymentResp](t, admin.do("POST", "/deployments", map[string]any{"app_id": list[0].ID, "intent": "available", "reboot": "never"}))
	if ok.Intent != "available" {
		t.Fatalf("%+v", ok)
	}
	win := decode[appResp](t, admin.do("POST", "/apps", newApp("Firefox", "windows", "winget", "Mozilla.Firefox")))
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": win.ID, "intent": "available", "reboot": "never"}), 400, "invalid_request")
}
