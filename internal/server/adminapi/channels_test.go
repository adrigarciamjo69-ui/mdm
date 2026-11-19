package adminapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mdm/internal/server/auth"
)

type channelResp struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Recipients []string `json:"recipients"`
	URL        string   `json:"url"`
	Secret     string   `json:"secret"`
	LastError  string   `json:"last_error"`
}

func TestEmailConfig_PasswordSealedAndHidden(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, a.loginAs(auth.RoleOperator).do("PUT", "/integrations/email", map[string]any{}), 403, "forbidden")
	body := map[string]any{"host": "smtp.ejemplo.es", "port": 587, "security": "starttls", "username": "mdm", "password": "clave-smtp",
		"from_address": "mdm@ejemplo.es", "from_name": "MDM"}
	resp := admin.do("PUT", "/integrations/email", body)
	wantStatus(t, resp, 200)
	if strings.Contains(bodyString(resp), "clave-smtp") {
		t.Fatal("la contraseña no se devuelve")
	}
	var stored string
	_ = a.store.Pool().QueryRow(ctx, `SELECT password FROM smtp_config`).Scan(&stored)
	if !strings.HasPrefix(stored, "v1:") {
		t.Fatalf("cifrada: %s", stored)
	}
	body["password"] = "********"
	body["host"] = "smtp2.ejemplo.es"
	admin.do("PUT", "/integrations/email", body)
	var again string
	_ = a.store.Pool().QueryRow(ctx, `SELECT password FROM smtp_config`).Scan(&again)
	if again != stored {
		t.Fatal("el marcador conserva la contraseña")
	}
	wantError(t, admin.do("PUT", "/integrations/email", map[string]any{"host": "x", "port": 25, "security": "ssl", "from_address": "a@b.es"}), 400, "invalid_request")
	wantError(t, admin.do("PUT", "/integrations/email", map[string]any{"host": "x", "port": 25, "security": "none", "from_address": "no-es-correo"}), 400, "invalid_request")
	if a.findAudit("email.configured") == nil {
		t.Fatal("falta la auditoría")
	}
}

func TestChannels_WebhookSecretShownOnceAndTest(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	var mu sync.Mutex
	var hits int
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		sig = r.Header.Get("X-MDM-Signature")
		mu.Unlock()
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// httptest escucha en 127.0.0.1: http a una dirección de loopback está permitido.
	resp := admin.do("POST", "/channels", map[string]any{"name": "Teams IT", "type": "webhook", "url": srv.URL + "/hook"})
	wantStatus(t, resp, 201)
	ch := decode[channelResp](t, resp)
	if len(ch.Secret) < 32 {
		t.Fatalf("el secreto se muestra al crear: %+v", ch)
	}
	list := decode[[]channelResp](t, admin.do("GET", "/channels", nil))
	if len(list) != 1 || list[0].Secret != "" {
		t.Fatalf("y nunca más: %+v", list)
	}
	wantStatus(t, admin.do("POST", "/channels/"+ch.ID+"/test", nil), 200)
	if hits != 1 || !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("prueba: %d %s", hits, sig)
	}
	wantError(t, admin.do("POST", "/channels", map[string]any{"name": "Malo", "type": "webhook", "url": "http://ejemplo.es/x"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/channels", map[string]any{"name": "Correo", "type": "email", "recipients": []string{"no-es-correo"}}), 400, "invalid_request")
	wantStatus(t, admin.do("POST", "/channels", map[string]any{"name": "Correo IT", "type": "email", "recipients": []string{"it@ejemplo.es"}}), 201)
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/channels", map[string]any{"name": "x", "type": "email", "recipients": []string{"a@b.es"}}), 403, "forbidden")
	if len(decode[[]channelResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/channels", nil))) != 2 {
		t.Fatal("todos pueden ver los canales (para elegirlos)")
	}
}

func TestChannels_TestReportsFailure(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	ch := decode[channelResp](t, admin.do("POST", "/channels", map[string]any{"name": "Roto", "type": "webhook", "url": srv.URL}))
	resp := admin.do("POST", "/channels/"+ch.ID+"/test", nil)
	if resp.StatusCode != 502 || !strings.Contains(bodyString(resp), "500") {
		t.Fatalf("la prueba explica el fallo: %d", resp.StatusCode)
	}
}
