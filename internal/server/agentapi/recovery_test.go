package agentapi

import (
	"strings"
	"testing"

	"mdm/internal/protocol"
)

const goodRecovery = "123456-234567-345678-456789-567890-678901-789012-890123"

func TestRecoveryKey_StoredEncryptedAndReplaced(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	send := func(pw string) int {
		return a.post("/recovery-keys", "", c.authz, protocol.RecoveryKey{Volume: "C:", KeyID: "{ABC-1}", RecoveryPassword: pw}, false).StatusCode
	}
	if code := send(goodRecovery); code != 204 {
		t.Fatalf("custodia: %d", code)
	}
	_, sealed, err := a.store.RecoveryKeySecret(ctx, c.id, "{ABC-1}")
	if err != nil || strings.Contains(sealed, "123456") || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("la clave debe guardarse cifrada: %q %v", sealed, err)
	}
	other := strings.Replace(goodRecovery, "123456", "999999", 1)
	send(other)
	_, sealed2, _ := a.store.RecoveryKeySecret(ctx, c.id, "{ABC-1}")
	plain, _ := testBox.Open(sealed2)
	if string(plain) != other {
		t.Fatal("el mismo protector debe sustituir la clave anterior")
	}
	keys, _ := a.store.ListRecoveryKeys(ctx, c.id)
	if len(keys) != 1 {
		t.Fatalf("claves: %d", len(keys))
	}
}

func TestRecoveryKey_ValidatesFormat(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	for _, k := range []protocol.RecoveryKey{
		{Volume: "C:", KeyID: "{x}", RecoveryPassword: "123456"},
		{Volume: "C:", KeyID: "{x}", RecoveryPassword: strings.ReplaceAll(goodRecovery, "-", " ")},
		{Volume: "", KeyID: "{x}", RecoveryPassword: goodRecovery},
		{Volume: "C:", KeyID: "", RecoveryPassword: goodRecovery},
	} {
		wantError(t, a.post("/recovery-keys", "", c.authz, k, false), 400, "invalid_request")
	}
}

func TestEffectivePolicy_EscrowedKeyIDs(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	before := c.effective()
	a.post("/recovery-keys", "", c.authz, protocol.RecoveryKey{Volume: "C:", KeyID: "{K1}", RecoveryPassword: goodRecovery}, false)
	after := c.effective()
	if len(after.EscrowedKeyIDs) != 1 || after.EscrowedKeyIDs[0] != "{K1}" || after.Hash == before.Hash {
		t.Fatalf("custodiar debe aparecer en la política y cambiar la huella: %+v", after)
	}
}
