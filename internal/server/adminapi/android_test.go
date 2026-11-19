package adminapi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mdm/internal/server/auth"
	"mdm/internal/server/mobile/android/fake"
)

func googleKey(t *testing.T, tokenURL string) string {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	b, _ := json.Marshal(map[string]string{"type": "service_account", "project_id": "mdm-empresa",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "mdm@mdm-empresa.iam.gserviceaccount.com", "token_uri": tokenURL})
	return string(b)
}

type androidStatus struct {
	Configured     bool   `json:"configured"`
	ProjectID      string `json:"project_id"`
	ClientEmail    string `json:"client_email"`
	Linked         bool   `json:"linked"`
	EnterpriseName string `json:"enterprise_name"`
	DisplayName    string `json:"enterprise_display_name"`
}

// withGoogle apunta la API de consola al simulador de Google.
func (a *testAPI) withGoogle(t *testing.T) *fake.Server {
	f := fake.New()
	t.Cleanup(f.Close)
	a.rebuild(func(d *Deps) { d.AndroidAPIBase = f.APIBase() })
	return f
}

// callback simula la vuelta del navegador desde Google: sin cookie de sesión.
func (a *testAPI) callback(query string) *http.Response {
	r := httptest.NewRequest("GET", "/integrations/android/callback?"+query, nil)
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w.Result()
}

func TestAndroid_ConfigureKey(t *testing.T) {
	a := newTestAPI(t)
	f := a.withGoogle(t)
	key := googleKey(t, f.TokenURL())
	wantError(t, a.loginAs(auth.RoleOperator).do("PUT", "/integrations/android/key", map[string]string{"key_json": key}), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, admin.do("PUT", "/integrations/android/key", map[string]string{"key_json": `{"type":"authorized_user"}`}), 400, "invalid_request")
	resp := admin.do("PUT", "/integrations/android/key", map[string]string{"key_json": key})
	wantStatus(t, resp, 200)
	st := decode[androidStatus](t, admin.do("GET", "/integrations/android", nil))
	if !st.Configured || st.ProjectID != "mdm-empresa" || st.Linked {
		t.Fatalf("%+v", st)
	}
	var stored string
	_ = a.store.Pool().QueryRow(ctx, `SELECT service_account FROM android_config`).Scan(&stored)
	if strings.Contains(stored, "PRIVATE KEY") || !strings.HasPrefix(stored, "v1:") {
		t.Fatal("la clave debe guardarse cifrada")
	}
	if a.findAudit("android.configured") == nil {
		t.Fatal("falta la auditoría")
	}
	// Una clave que Google rechaza no se guarda.
	f.FailNext("POST /token", 400, 1)
	wantError(t, admin.do("PUT", "/integrations/android/key", map[string]string{"key_json": googleKey(t, f.TokenURL())}), 400, "invalid_request")
}

func TestAndroid_SignupCallbackAndUnlink(t *testing.T) {
	a := newTestAPI(t)
	f := a.withGoogle(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, admin.do("POST", "/integrations/android/signup", nil), 409, "conflict") // sin clave
	admin.do("PUT", "/integrations/android/key", map[string]string{"key_json": googleKey(t, f.TokenURL())})
	resp := admin.do("POST", "/integrations/android/signup", nil)
	wantStatus(t, resp, 200)
	su := decode[struct {
		URL string `json:"url"`
	}](t, resp)
	u, _ := url.Parse(su.URL)
	cb, _ := url.Parse(u.Query().Get("callback"))
	state := cb.Query().Get("state")
	if !strings.HasPrefix(cb.String(), testPublicURL+"/api/v1/integrations/android/callback") || len(state) < 32 {
		t.Fatalf("callback: %s", cb)
	}
	if r := a.callback("state=otro&enterpriseToken=" + fake.GoodEnterpriseToken); r.StatusCode != 400 {
		t.Fatalf("un state distinto debe rechazarse: %d", r.StatusCode)
	}
	r := a.callback("state=" + state + "&enterpriseToken=" + fake.GoodEnterpriseToken)
	if r.StatusCode != 302 || r.Header.Get("Location") != "/integraciones/android?vinculada=1" {
		t.Fatalf("vuelta: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	st := decode[androidStatus](t, admin.do("GET", "/integrations/android", nil))
	if !st.Linked || !strings.HasPrefix(st.EnterpriseName, "enterprises/") || st.DisplayName != "Empresa de prueba" {
		t.Fatalf("%+v", st)
	}
	e := a.findAudit("android.linked")
	if e == nil || e.ActorEmail == "" {
		t.Fatalf("la vinculación queda a nombre de quien la inició: %+v", e)
	}
	if r := a.callback("state=" + state + "&enterpriseToken=" + fake.GoodEnterpriseToken); r.StatusCode != 400 {
		t.Fatal("el state es de un solo uso")
	}
	wantStatus(t, admin.do("DELETE", "/integrations/android", nil), 204)
	if len(f.Enterprises) != 0 {
		t.Fatal("desvincular borra la empresa en Google")
	}
	if st := decode[androidStatus](t, admin.do("GET", "/integrations/android", nil)); st.Linked {
		t.Fatalf("%+v", st)
	}
	if a.findAudit("android.unlinked") == nil {
		t.Fatal("falta la auditoría")
	}
}
