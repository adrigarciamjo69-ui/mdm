package adminapi

import (
	"net/http"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

const otherPassword = "otra-clave-bien-larga-2"

// sessionFor abre una sesión verificada para una cuenta ya creada.
func (a *testAPI) sessionFor(adm store.Admin) *testClient {
	a.t.Helper()
	secret := "JBSWY3DPEHPK3PXP"
	if err := a.store.SetAdminTOTP(ctx, adm.ID, &secret); err != nil {
		a.t.Fatal(err)
	}
	plain, hash, _ := auth.NewSecret()
	csrf, _, _ := auth.NewSecret()
	now := a.clock.Now()
	if err := a.store.CreateSession(ctx, store.Session{TokenHash: hash, AdminID: adm.ID, CSRFToken: csrf, Verified: true,
		CreatedAt: now, ExpiresAt: now.Add(12 * time.Hour), IP: "192.0.2.1"}); err != nil {
		a.t.Fatal(err)
	}
	return &testClient{api: a, cookie: &http.Cookie{Name: "mdm_session", Value: plain}, csrf: csrf, admin: adm}
}

func TestPassword_ChangeOwn(t *testing.T) {
	a := newTestAPI(t)
	adm := a.createAdmin("lectura@ejemplo.es", goodPassword, auth.RoleReadonly)
	me, other := a.sessionFor(adm), a.sessionFor(adm)

	wantError(t, me.do("POST", "/auth/password", map[string]string{"current_password": "mala-mala-mala-1", "new_password": otherPassword}), 400, "invalid_request")
	wantError(t, me.do("POST", "/auth/password", map[string]string{"current_password": goodPassword, "new_password": "corta"}), 400, "invalid_request")
	wantError(t, me.do("POST", "/auth/password", map[string]string{"current_password": goodPassword, "new_password": goodPassword}), 400, "invalid_request")
	wantStatus(t, me.doNoCSRF("POST", "/auth/password", map[string]string{"current_password": goodPassword, "new_password": otherPassword}), 403)

	wantStatus(t, me.do("POST", "/auth/password", map[string]string{"current_password": goodPassword, "new_password": otherPassword}), 204)
	// Su sesión sigue; la otra se ha cerrado.
	wantStatus(t, me.do("GET", "/auth/me", nil), 200)
	wantStatus(t, other.do("GET", "/auth/me", nil), 401)
	// La contraseña vieja ya no vale y la nueva sí.
	wantStatus(t, login(a.anon(), "lectura@ejemplo.es", goodPassword), 401)
	wantStatus(t, login(a.anon(), "lectura@ejemplo.es", otherPassword), 200)
}

func TestPassword_ChangeOwnRateLimited(t *testing.T) {
	a := newTestAPI(t)
	me := a.sessionFor(a.createAdmin("op@ejemplo.es", goodPassword, auth.RoleOperator))
	for i := 0; i < 5; i++ {
		wantStatus(t, me.do("POST", "/auth/password", map[string]string{"current_password": "mala-mala-mala-1", "new_password": otherPassword}), 400)
	}
	wantStatus(t, me.do("POST", "/auth/password", map[string]string{"current_password": goodPassword, "new_password": otherPassword}), 429)
}

func TestPassword_AdminReset(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	target := a.createAdmin("ana@ejemplo.es", goodPassword, auth.RoleOperator)
	targetSession := a.sessionFor(target)
	path := "/admins/" + target.ID.String() + "/password"

	wantError(t, a.loginAs(auth.RoleOperator).do("POST", path, map[string]string{"password": otherPassword}), 403, "forbidden")
	wantError(t, admin.do("POST", path, map[string]string{"password": "corta"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/admins/"+admin.admin.ID.String()+"/password", map[string]string{"password": otherPassword}), 409, "conflict")
	wantError(t, admin.do("POST", "/admins/00000000-0000-4000-8000-000000000000/password", map[string]string{"password": otherPassword}), 404, "not_found")

	wantStatus(t, admin.do("POST", path, map[string]string{"password": otherPassword}), 204)
	wantStatus(t, targetSession.do("GET", "/auth/me", nil), 401)
	wantStatus(t, login(a.anon(), "ana@ejemplo.es", otherPassword), 200)

	// Queda en la auditoría.
	body := bodyString(admin.do("GET", "/audit?action=password.reset", nil))
	if !contains(body, "ana@ejemplo.es") {
		t.Fatalf("auditoría sin el restablecimiento: %s", body)
	}
}
