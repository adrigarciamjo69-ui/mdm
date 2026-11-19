package adminapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"mdm/internal/server/auth"
)

const goodPassword = "correcta-y-larga-1"

type loginResp struct {
	Next string `json:"next"`
}
type setupResp struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}
type meResp struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CSRFToken string `json:"csrf_token"`
}

func login(c *testClient, email, password string) *http.Response {
	return c.do("POST", "/auth/login", map[string]string{"email": email, "password": password})
}

// fullLogin recorre alta de TOTP y verificación; devuelve el secreto TOTP.
func fullLogin(t *testing.T, c *testClient, email string) string {
	t.Helper()
	wantStatus(t, login(c, email, goodPassword), 200)
	setup := decode[setupResp](t, c.do("POST", "/auth/totp/setup", nil))
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	resp := c.do("POST", "/auth/totp/verify", map[string]string{"code": code})
	wantStatus(t, resp, 200)
	c.csrf = decode[meResp](t, resp).CSRFToken
	return setup.Secret
}

func TestLogin_WrongPasswordAndUnknownEmailLookTheSame(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	r1 := login(a.anon(), "ana@ejemplo.es", "incorrecta-y-larga")
	r2 := login(a.anon(), "nadie@ejemplo.es", "incorrecta-y-larga")
	b1, b2 := bodyString(r1), bodyString(r2)
	if r1.StatusCode != 401 || r2.StatusCode != 401 || b1 != b2 {
		t.Fatalf("%d %s / %d %s", r1.StatusCode, b1, r2.StatusCode, b2)
	}
	if len(r1.Cookies()) != 0 {
		t.Error("un inicio de sesión fallido no debe fijar cookie")
	}
}

func TestLogin_DisabledAdminLooksLikeWrongPassword(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("otro@ejemplo.es", goodPassword, auth.RoleAdmin)
	adm := a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	yes := true
	if err := a.store.UpdateAdmin(ctx, adm.ID, storeAdminPatchDisabled(&yes)); err != nil {
		t.Fatal(err)
	}
	wantError(t, login(a.anon(), "ana@ejemplo.es", goodPassword), 401, "unauthorized")
}

func TestLogin_FirstTimeRequiresTOTPSetup(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	resp := login(a.anon(), "ANA@ejemplo.es", goodPassword)
	wantStatus(t, resp, 200)
	if got := decode[loginResp](t, resp).Next; got != "totp_setup" {
		t.Fatalf("next = %q", got)
	}
}

func TestLogin_PartialSessionCannotAccessAPI(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	c := a.anon()
	wantStatus(t, login(c, "ana@ejemplo.es", goodPassword), 200)
	if c.cookie == nil {
		t.Fatal("no se fijó la cookie de sesión parcial")
	}
	wantError(t, c.do("GET", "/auth/me", nil), 401, "unauthorized")
}

func TestLogin_FullFlow(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleOperator)
	c := a.anon()
	secret := fullLogin(t, c, "ana@ejemplo.es")
	me := decode[meResp](t, c.do("GET", "/auth/me", nil))
	if me.Email != "ana@ejemplo.es" || me.Role != "operator" || me.CSRFToken == "" || me.ID == "" {
		t.Fatalf("me = %+v", me)
	}
	// Segundo acceso: ya tiene TOTP, se le pide el código directamente.
	c2 := a.anon()
	resp := login(c2, "ana@ejemplo.es", goodPassword)
	if got := decode[loginResp](t, resp).Next; got != "totp" {
		t.Fatalf("next = %q", got)
	}
	wantError(t, c2.do("POST", "/auth/totp/setup", nil), 409, "conflict")
	code, _ := totp.GenerateCode(secret, time.Now())
	wantStatus(t, c2.do("POST", "/auth/totp/verify", map[string]string{"code": code}), 200)
}

func TestLogin_SetupNotConfirmedDoesNotLockOut(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	c := a.anon()
	login(c, "ana@ejemplo.es", goodPassword)
	wantStatus(t, c.do("POST", "/auth/totp/setup", nil), 200) // ve el QR y abandona
	resp := login(a.anon(), "ana@ejemplo.es", goodPassword)
	if got := decode[loginResp](t, resp).Next; got != "totp_setup" {
		t.Fatalf("un alta sin confirmar dejó fijado el TOTP: next = %q", got)
	}
}

func TestLogin_BadTOTPCode(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	c := a.anon()
	login(c, "ana@ejemplo.es", goodPassword)
	c.do("POST", "/auth/totp/setup", nil)
	wantError(t, c.do("POST", "/auth/totp/verify", map[string]string{"code": "000000"}), 401, "unauthorized")
	wantError(t, c.do("GET", "/auth/me", nil), 401, "unauthorized")
	if a.findAudit(audit_loginFailed) == nil {
		t.Error("el código incorrecto no quedó en auditoría")
	}
}

func TestLogin_VerifyWithoutSetupRejected(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	c := a.anon()
	login(c, "ana@ejemplo.es", goodPassword)
	wantError(t, c.do("POST", "/auth/totp/verify", map[string]string{"code": "123456"}), 401, "unauthorized")
}

func TestLogin_RateLimited(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	for i := 0; i < 5; i++ {
		wantStatus(t, login(a.anon(), "ana@ejemplo.es", "incorrecta-y-larga"), 401)
	}
	resp := login(a.anon(), "Ana@Ejemplo.es", goodPassword)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("estado %d Retry-After=%q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	// Otra IP no está bloqueada.
	other := a.anon()
	other.ip = "198.51.100.7"
	wantStatus(t, login(other, "ana@ejemplo.es", goodPassword), 200)
	// Pasada la ventana, la primera IP vuelve a poder entrar.
	a.clock.Advance(15*time.Minute + time.Second)
	wantStatus(t, login(a.anon(), "ana@ejemplo.es", goodPassword), 200)
}

func TestLogin_MissingFields(t *testing.T) {
	a := newTestAPI(t)
	wantError(t, a.anon().do("POST", "/auth/login", map[string]string{"email": "", "password": ""}), 400, "invalid_request")
	wantError(t, a.anon().do("POST", "/auth/login", "{"), 400, "invalid_request")
}

func TestCSRF_MissingHeaderRejected(t *testing.T) {
	a := newTestAPI(t)
	c := a.loginAs(auth.RoleReadonly)
	wantError(t, c.doNoCSRF("POST", "/auth/logout", nil), 403, "forbidden")
	wantStatus(t, c.do("GET", "/auth/me", nil), 200) // la sesión sigue viva
	bad := *c
	bad.csrf = "otro-valor"
	wantError(t, bad.do("POST", "/auth/logout", nil), 403, "forbidden")
}

func TestLogout_InvalidatesSession(t *testing.T) {
	a := newTestAPI(t)
	c := a.loginAs(auth.RoleReadonly)
	stolen := *c
	wantStatus(t, c.do("POST", "/auth/logout", nil), 204)
	wantError(t, stolen.do("GET", "/auth/me", nil), 401, "unauthorized")
}

func TestSession_ExpiresAfterTwelveHours(t *testing.T) {
	a := newTestAPI(t)
	c := a.loginAs(auth.RoleReadonly)
	a.clock.Advance(12*time.Hour - time.Minute)
	wantStatus(t, c.do("GET", "/auth/me", nil), 200)
	a.clock.Advance(2 * time.Minute)
	wantError(t, c.do("GET", "/auth/me", nil), 401, "unauthorized")
}

func TestNoCookie_Unauthorized(t *testing.T) {
	a := newTestAPI(t)
	wantError(t, a.anon().do("GET", "/auth/me", nil), 401, "unauthorized")
}

func TestCookieAttributes(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	resp := login(a.anon(), "ana@ejemplo.es", goodPassword)
	var ck *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "mdm_session" {
			ck = c
		}
	}
	if ck == nil {
		t.Fatal("sin cookie mdm_session")
	}
	if !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/" || ck.MaxAge != 12*3600 {
		t.Fatalf("atributos: %+v", ck)
	}
}

func TestAudit_LoginRecorded(t *testing.T) {
	a := newTestAPI(t)
	a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleAdmin)
	login(a.anon(), "ana@ejemplo.es", "incorrecta-y-larga")
	fullLogin(t, a.anon(), "ana@ejemplo.es")
	failed, ok, enrolled := a.findAudit("login.failed"), a.findAudit("login.ok"), a.findAudit("totp.enrolled")
	if failed == nil || failed.ActorEmail != "ana@ejemplo.es" || failed.IP != "192.0.2.1" {
		t.Errorf("login.failed: %+v", failed)
	}
	if ok == nil || ok.ActorEmail != "ana@ejemplo.es" || ok.ActorAdminID == nil {
		t.Errorf("login.ok: %+v", ok)
	}
	if enrolled == nil {
		t.Error("falta totp.enrolled")
	}
}

func TestRole_Enforced(t *testing.T) {
	a := newTestAPI(t)
	h := a.handlerWithProbe(auth.RoleOperator)
	for role, want := range map[auth.Role]int{auth.RoleReadonly: 403, auth.RoleOperator: 200, auth.RoleAdmin: 200} {
		c := a.loginAs(role)
		saved := a.handler
		a.handler = h
		resp := c.do("GET", "/probe", nil)
		a.handler = saved
		if resp.StatusCode != want {
			t.Errorf("%s: %d, se esperaba %d", role, resp.StatusCode, want)
		}
	}
}
