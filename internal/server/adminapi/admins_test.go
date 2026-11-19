package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
)

type adminResp struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Disabled    bool       `json:"disabled"`
	HasTOTP     bool       `json:"has_totp"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

func TestAdmins_OperatorForbidden(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	wantError(t, op.do("GET", "/admins", nil), 403, "forbidden")
	wantError(t, op.do("POST", "/admins", map[string]string{"email": "x@ejemplo.es", "password": goodPassword, "role": "readonly"}), 403, "forbidden")
	wantError(t, op.do("PATCH", "/admins/"+op.admin.ID.String(), map[string]any{"role": "admin"}), 403, "forbidden")
	wantError(t, op.do("GET", "/audit", nil), 403, "forbidden")
}

func TestAdmins_Create(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	resp := admin.do("POST", "/admins", map[string]string{"email": " Nueva@Ejemplo.es ", "password": goodPassword, "role": "operator"})
	wantStatus(t, resp, 201)
	created := decode[adminResp](t, resp)
	if created.Email != "Nueva@Ejemplo.es" || created.Role != "operator" || created.HasTOTP || created.Disabled {
		t.Fatalf("creado: %+v", created)
	}
	// La cuenta nueva puede iniciar sesión con esa contraseña.
	if got := decode[loginResp](t, login(a.anon(), "nueva@ejemplo.es", goodPassword)).Next; got != "totp_setup" {
		t.Fatalf("inicio de sesión de la cuenta nueva: %q", got)
	}
	cases := []map[string]string{
		{"email": "a@ejemplo.es", "password": "12345678901", "role": "operator"},
		{"email": "a@ejemplo.es", "password": goodPassword, "role": "root"},
		{"email": "sin-arroba", "password": goodPassword, "role": "operator"},
		{"email": "", "password": goodPassword, "role": "operator"},
	}
	for _, c := range cases {
		wantError(t, admin.do("POST", "/admins", c), 400, "invalid_request")
	}
	wantError(t, admin.do("POST", "/admins", map[string]string{"email": "NUEVA@ejemplo.es", "password": goodPassword, "role": "operator"}), 409, "conflict")
}

func TestAdmins_ListHidesSecrets(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	a.createAdmin("sin-totp@ejemplo.es", goodPassword, auth.RoleReadonly)
	resp := admin.do("GET", "/admins", nil)
	wantStatus(t, resp, 200)
	body := bodyString(resp)
	for _, leak := range []string{"password_hash", "totp_secret", "argon2id", "JBSWY3DPEHPK3PXP"} {
		if strings.Contains(body, leak) {
			t.Errorf("el listado expone %s", leak)
		}
	}
	if !strings.Contains(body, `"has_totp":true`) || !strings.Contains(body, `"has_totp":false`) {
		t.Errorf("falta has_totp: %s", body)
	}
}

func TestAdmins_CannotDisableOrDemoteSelf(t *testing.T) {
	a := newTestAPI(t)
	admin, _ := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleAdmin)
	path := "/admins/" + admin.admin.ID.String()
	wantError(t, admin.do("PATCH", path, map[string]any{"disabled": true}), 409, "conflict")
	wantError(t, admin.do("PATCH", path, map[string]any{"role": "operator"}), 409, "conflict")
	wantStatus(t, admin.do("GET", "/auth/me", nil), 200)
}

func TestAdmins_CannotRemoveLastAdmin(t *testing.T) {
	a := newTestAPI(t)
	first, second := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleAdmin)
	// first desactiva a second: quedan uno. Luego second no puede actuar y first no puede quitarse.
	wantStatus(t, first.do("PATCH", "/admins/"+second.admin.ID.String(), map[string]any{"disabled": true}), 200)
	// Reactivarlo como operador y que intente degradar al último admin no es posible (403), así que
	// se comprueba la regla del almacén a través de un segundo admin que degrada al primero.
	wantStatus(t, first.do("PATCH", "/admins/"+second.admin.ID.String(), map[string]any{"disabled": false}), 200)
	third := a.loginAs(auth.RoleAdmin)
	wantStatus(t, third.do("PATCH", "/admins/"+second.admin.ID.String(), map[string]any{"role": "readonly"}), 200)
	wantStatus(t, third.do("PATCH", "/admins/"+first.admin.ID.String(), map[string]any{"role": "readonly"}), 200)
	// third es ahora el único administrador: nadie más puede tocarlo y él no puede quitarse.
	wantError(t, third.do("PATCH", "/admins/"+third.admin.ID.String(), map[string]any{"disabled": true}), 409, "conflict")
	list := decode[[]adminResp](t, third.do("GET", "/admins", nil))
	admins := 0
	for _, x := range list {
		if x.Role == "admin" && !x.Disabled {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("administradores activos: %d", admins)
	}
}

func TestAdmins_DisableKillsSessions(t *testing.T) {
	a := newTestAPI(t)
	admin, victim := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleOperator)
	wantStatus(t, victim.do("GET", "/auth/me", nil), 200)
	resp := admin.do("PATCH", "/admins/"+victim.admin.ID.String(), map[string]any{"disabled": true})
	wantStatus(t, resp, 200)
	if got := decode[adminResp](t, resp); !got.Disabled {
		t.Fatalf("respuesta: %+v", got)
	}
	wantError(t, victim.do("GET", "/auth/me", nil), 401, "unauthorized")
}

func TestAdmins_PatchValidation(t *testing.T) {
	a := newTestAPI(t)
	admin, other := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleReadonly)
	path := "/admins/" + other.admin.ID.String()
	wantError(t, admin.do("PATCH", path, map[string]any{"role": "root"}), 400, "invalid_request")
	wantError(t, admin.do("PATCH", path, map[string]any{}), 400, "invalid_request")
	wantError(t, admin.do("PATCH", "/admins/00000000-0000-4000-8000-000000000000", map[string]any{"disabled": true}), 404, "not_found")
	wantError(t, admin.do("PATCH", "/admins/no-uuid", map[string]any{"disabled": true}), 400, "invalid_request")
}

func TestAdmins_AuditRecorded(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	created := decode[adminResp](t, admin.do("POST", "/admins", map[string]string{"email": "nueva@ejemplo.es", "password": goodPassword, "role": "readonly"}))
	admin.do("PATCH", "/admins/"+created.ID, map[string]any{"role": "operator"})
	c := a.findAudit("admin.created")
	if c == nil || c.TargetID != created.ID || !strings.Contains(string(c.Details), "nueva@ejemplo.es") || strings.Contains(string(c.Details), goodPassword) {
		t.Errorf("admin.created: %+v", c)
	}
	u := a.findAudit("admin.updated")
	if u == nil || u.TargetID != created.ID || !strings.Contains(string(u.Details), "operator") {
		t.Errorf("admin.updated: %+v", u)
	}
}
