package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type tokenResp struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Token     string     `json:"token"`
	State     string     `json:"state"`
	GroupID   *string    `json:"group_id"`
	GroupName string     `json:"group_name"`
	ExpiresAt *time.Time `json:"expires_at"`
	MaxUses   *int       `json:"max_uses"`
	Uses      int        `json:"uses"`
	Install   struct {
		Linux   string `json:"linux"`
		Windows string `json:"windows"`
	} `json:"install"`
}

func TestTokens_CreateReturnsPlainOnce(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	resp := op.do("POST", "/enrollment-tokens", map[string]any{"name": "Oficina", "max_uses": 50})
	wantStatus(t, resp, 201)
	tok := decode[tokenResp](t, resp)
	if len(tok.Token) != 43 || tok.State != "active" || tok.Uses != 0 || tok.MaxUses == nil || *tok.MaxUses != 50 {
		t.Fatalf("creado: %+v", tok)
	}
	body := bodyString(op.do("GET", "/enrollment-tokens", nil))
	if strings.Contains(body, tok.Token) || strings.Contains(body, auth.HashSecret(tok.Token)) || strings.Contains(body, "token_hash") {
		t.Fatalf("el listado expone el token o su hash: %s", body)
	}
	if !strings.Contains(body, `"name":"Oficina"`) {
		t.Fatalf("el listado no incluye el token creado: %s", body)
	}
}

func TestTokens_InstallCommands(t *testing.T) {
	a := newTestAPI(t)
	tok := decode[tokenResp](t, a.loginAs(auth.RoleOperator).do("POST", "/enrollment-tokens", map[string]any{"name": "Oficina"}))
	wantLinux := "curl -fsSL " + testPublicURL + "/downloads/install.sh | sudo sh -s -- --server " + testPublicURL + " --token " + tok.Token
	if tok.Install.Linux != wantLinux {
		t.Errorf("linux:\n got %s\nwant %s", tok.Install.Linux, wantLinux)
	}
	wantWin := `powershell -ExecutionPolicy Bypass -Command "& ([scriptblock]::Create((irm ` + testPublicURL + `/downloads/install.ps1))) -Server ` + testPublicURL + ` -Token ` + tok.Token + `"`
	if tok.Install.Windows != wantWin {
		t.Errorf("windows:\n got %s\nwant %s", tok.Install.Windows, wantWin)
	}
}

func TestTokens_ReadonlyForbidden(t *testing.T) {
	a := newTestAPI(t)
	ro := a.loginAs(auth.RoleReadonly)
	wantError(t, ro.do("GET", "/enrollment-tokens", nil), 403, "forbidden")
	wantError(t, ro.do("POST", "/enrollment-tokens", map[string]any{"name": "X"}), 403, "forbidden")
}

func TestTokens_Revoke(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	tok := decode[tokenResp](t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "Oficina"}))
	wantStatus(t, op.do("POST", "/enrollment-tokens/"+tok.ID+"/revoke", nil), 204)
	list := decode[[]tokenResp](t, op.do("GET", "/enrollment-tokens", nil))
	if len(list) != 1 || list[0].State != "revoked" {
		t.Fatalf("tras revocar: %+v", list)
	}
	wantStatus(t, op.do("POST", "/enrollment-tokens/"+tok.ID+"/revoke", nil), 204) // repetir no falla
	wantError(t, op.do("POST", "/enrollment-tokens/00000000-0000-4000-8000-000000000000/revoke", nil), 404, "not_found")
}

func TestTokenState(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Hour)
	five := 5
	cases := []struct {
		name string
		tok  store.EnrollmentToken
		want string
	}{
		{"activo sin límites", store.EnrollmentToken{}, "active"},
		{"activo con límites", store.EnrollmentToken{ExpiresAt: &future, MaxUses: &five, Uses: 4}, "active"},
		{"caducado", store.EnrollmentToken{ExpiresAt: &past}, "expired"},
		{"caduca justo ahora", store.EnrollmentToken{ExpiresAt: &now}, "expired"},
		{"agotado", store.EnrollmentToken{MaxUses: &five, Uses: 5}, "exhausted"},
		{"revocado prevalece", store.EnrollmentToken{RevokedAt: &past, ExpiresAt: &past, MaxUses: &five, Uses: 5}, "revoked"},
	}
	for _, c := range cases {
		if got := store.TokenState(c.tok, now); got != c.want {
			t.Errorf("%s: %s, se esperaba %s", c.name, got, c.want)
		}
	}
}

func TestTokens_Validation(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": ""}), 400, "invalid_request")
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "X", "max_uses": 0}), 400, "invalid_request")
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "X", "expires_at": a.clock.Now().Add(-time.Hour)}), 400, "invalid_request")
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "X", "group_id": "00000000-0000-4000-8000-000000000000"}), 400, "invalid_request")
	wantError(t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "X", "group_id": "no-uuid"}), 400, "invalid_request")
}

func TestTokens_WithGroupAndExpiry(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	g := decode[groupResp](t, op.do("POST", "/groups", map[string]string{"name": "Ventas"}))
	exp := a.clock.Now().Add(7 * 24 * time.Hour)
	tok := decode[tokenResp](t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "X", "group_id": g.ID, "expires_at": exp}))
	if tok.GroupID == nil || *tok.GroupID != g.ID || tok.GroupName != "Ventas" || tok.ExpiresAt == nil || !tok.ExpiresAt.Equal(exp) {
		t.Fatalf("token: %+v", tok)
	}
	// Borrar el grupo no borra el token: lo deja sin grupo.
	wantStatus(t, op.do("DELETE", "/groups/"+g.ID, nil), 204)
	list := decode[[]tokenResp](t, op.do("GET", "/enrollment-tokens", nil))
	if len(list) != 1 || list[0].GroupID != nil {
		t.Fatalf("tras borrar el grupo: %+v", list)
	}
	a.clock.Advance(8 * 24 * time.Hour)
	list = decode[[]tokenResp](t, a.loginAs(auth.RoleOperator).do("GET", "/enrollment-tokens", nil))
	if list[0].State != "expired" {
		t.Fatalf("estado tras caducar: %s", list[0].State)
	}
}

func TestAudit_TokenAndGroupActions(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	g := decode[groupResp](t, op.do("POST", "/groups", map[string]string{"name": "Ventas"}))
	op.do("PATCH", "/groups/"+g.ID, map[string]string{"name": "Comercial"})
	op.do("DELETE", "/groups/"+g.ID, nil)
	tok := decode[tokenResp](t, op.do("POST", "/enrollment-tokens", map[string]any{"name": "Oficina"}))
	op.do("POST", "/enrollment-tokens/"+tok.ID+"/revoke", nil)
	for _, action := range []string{"group.created", "group.updated", "group.deleted", "token.created", "token.revoked"} {
		e := a.findAudit(action)
		if e == nil {
			t.Errorf("falta %s", action)
			continue
		}
		if e.ActorEmail != op.admin.Email || e.TargetID == "" {
			t.Errorf("%s: %+v", action, e)
		}
		if strings.Contains(string(e.Details), tok.Token) {
			t.Errorf("%s: la auditoría contiene el token en claro", action)
		}
	}
}
