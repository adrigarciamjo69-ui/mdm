package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/secrets"
)

const goodRecovery = "123456-234567-345678-456789-567890-678901-789012-890123"

// seedKey custodia una clave sellada con box.
func (a *testAPI) seedKey(box *secrets.Box, deviceID interface{ String() string }, keyID string) {
	a.t.Helper()
	sealed, _ := box.Seal([]byte(goodRecovery))
	if err := a.store.SaveRecoveryKey(ctx, mustUUID(deviceID.String()), "C:", keyID, sealed, time.Now()); err != nil {
		a.t.Fatal(err)
	}
}

func TestRecoveryKey_ListHidesSecret(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{})
	a.seedKey(a.box, d, "{K1}")
	resp := a.loginAs(auth.RoleReadonly).do("GET", "/devices/"+d.String()+"/recovery-keys", nil)
	body := bodyString(resp)
	if resp.StatusCode != 200 || strings.Contains(body, "123456") || !strings.Contains(body, `"key_id":"{K1}"`) {
		t.Fatalf("listado: %d %s", resp.StatusCode, body)
	}
}

func TestReveal_AdminOnlyAndAudited(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Hostname: "PC-1"})
	a.seedKey(a.box, d, "{K1}")
	path := "/devices/" + d.String() + "/recovery-keys/{K1}/reveal"
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", path, nil), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	got := decode[map[string]string](t, admin.do("POST", path, nil))
	if got["recovery_password"] != goodRecovery || got["volume"] != "C:" {
		t.Fatalf("revelada: %+v", got)
	}
	e := a.findAudit("recovery_key.viewed")
	if e == nil || e.TargetID != d.String() || !strings.Contains(string(e.Details), "{K1}") || strings.Contains(string(e.Details), "123456") {
		t.Fatalf("auditoría: %+v", e)
	}
	wantError(t, admin.do("POST", "/devices/"+d.String()+"/recovery-keys/{NO}/reveal", nil), 404, "not_found")
}

func TestReveal_WrongServerKey(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{})
	a.seedKey(newBox(t), d, "{K1}") // custodiada con otra clave del servidor
	resp := a.loginAs(auth.RoleAdmin).do("POST", "/devices/"+d.String()+"/recovery-keys/{K1}/reveal", nil)
	if resp.StatusCode != 409 || !strings.Contains(bodyString(resp), "no se puede descifrar con la clave actual del servidor") {
		t.Fatalf("clave del servidor distinta: %d", resp.StatusCode)
	}
}
