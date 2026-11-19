package agentapi

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"mdm/internal/protocol"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

// assignToAll crea una política de la plataforma con los ajustes dados y la asigna a todos.
func (a *agentTest) assignToAll(platform string, settings ...policy.Setting) store.Policy {
	a.t.Helper()
	a.n++
	p, err := a.store.CreatePolicy(ctx, store.NewPolicy{Name: fmt.Sprintf("P%d", a.n), Platform: platform, Settings: settings})
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.store.SetAssignments(ctx, p.ID, []policy.Assignment{{}}, a.clock.Now()); err != nil {
		a.t.Fatal(err)
	}
	return p
}

func (c *agentClient) effective() protocol.EffectivePolicy {
	c.a.t.Helper()
	resp := c.a.get("/policy", c.authz)
	var eff protocol.EffectivePolicy
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || json.Unmarshal(b, &eff) != nil {
		c.a.t.Fatalf("GET /policy: %d %s", resp.StatusCode, b)
	}
	return eff
}

func (c *agentClient) report(hash string, results ...protocol.SettingResult) int {
	return c.a.post("/compliance", "", c.authz, protocol.ComplianceReport{PolicyHash: hash, Results: results}, false).StatusCode
}

func fw() policy.Setting {
	return policy.Setting{Key: "windows.firewall", Mode: "enforce", Params: json.RawMessage(`{}`)}
}

func TestCheckin_ReturnsPolicyHashAndStoresCapabilities(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	_, out := c.checkin(protocol.CheckinRequest{Capabilities: []string{"policy.v1", "commands.v1"}})
	empty, _ := policy.Effective(nil, nil, "windows", nil)
	if out.PolicyHash != empty.Hash {
		t.Fatalf("huella sin políticas: %s", out.PolicyHash)
	}
	if d := a.device(c.id.String()); strings.Join(d.Capabilities, ",") != "policy.v1,commands.v1" {
		t.Fatalf("capacidades: %v", d.Capabilities)
	}
	a.assignToAll("windows", fw())
	_, out2 := c.checkin(protocol.CheckinRequest{})
	if out2.PolicyHash == out.PolicyHash || out2.PolicyHash != c.effective().Hash {
		t.Fatal("la huella del check-in debe seguir a la política efectiva")
	}
	if d := a.device(c.id.String()); len(d.Capabilities) != 0 {
		t.Fatalf("un agente sin capacidades debe quedar sin ellas: %v", d.Capabilities)
	}
}

func TestGetPolicy_ReturnsEffective(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	p := a.assignToAll("windows", fw())
	a.assignToAll("linux", policy.Setting{Key: "linux.firewall", Mode: "enforce", Params: json.RawMessage(`{}`)})
	eff := c.effective()
	if len(eff.Settings) != 1 || eff.Settings[0].PolicyID != p.ID.String() || eff.EvaluateIntervalS != 900 || eff.EscrowedKeyIDs == nil {
		t.Fatalf("efectiva: %+v", eff)
	}
	resp := a.get("/policy", "")
	if resp.StatusCode != 401 {
		t.Fatalf("sin credencial: %d", resp.StatusCode)
	}
}

func TestPostCompliance_Validates(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	p := a.assignToAll("windows", fw())
	hash := c.effective().Hash
	if code := c.report(hash, protocol.SettingResult{PolicyID: p.ID.String(), Key: "windows.firewall", Status: "quizá"}); code != 400 {
		t.Errorf("estado desconocido: %d", code)
	}
	if code := c.report(hash, protocol.SettingResult{PolicyID: "no-uuid", Key: "windows.firewall", Status: "compliant"}); code != 400 {
		t.Errorf("política inválida: %d", code)
	}
	big := protocol.SettingResult{PolicyID: p.ID.String(), Key: "windows.firewall", Status: "compliant", Message: strings.Repeat("x", 300*1024)}
	if code := c.report(hash, big); code != 413 {
		t.Errorf("300 KB: %d", code)
	}
	long := protocol.SettingResult{PolicyID: p.ID.String(), Key: "windows.firewall", Status: "noncompliant", Message: strings.Repeat("ñ", 5000) + "\x00"}
	if code := c.report(hash, long); code != 204 {
		t.Fatalf("mensaje largo: %d", code)
	}
	_, rows, _ := a.store.Compliance(ctx, c.id)
	if len(rows) != 1 || len([]rune(rows[0].Message)) != 1024 || strings.ContainsRune(rows[0].Message, 0) {
		t.Fatalf("mensaje guardado: %d runas", len([]rune(rows[0].Message)))
	}
}

func TestPostCompliance_UpdatesState(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	c.checkin(protocol.CheckinRequest{})
	p := a.assignToAll("windows", fw())
	hash := c.effective().Hash
	ok := protocol.SettingResult{PolicyID: p.ID.String(), Key: "windows.firewall", Status: "compliant"}
	if code := c.report(hash, ok); code != 204 {
		t.Fatalf("informe: %d", code)
	}
	rec, _, _ := a.store.Compliance(ctx, c.id)
	if rec.State != "compliant" {
		t.Fatalf("tras cumplir: %+v", rec)
	}
	bad := ok
	bad.Status = "noncompliant"
	c.report(hash, bad)
	rec, _, _ = a.store.Compliance(ctx, c.id)
	if rec.State != "noncompliant" || rec.NoncompliantSince == nil {
		t.Fatalf("tras incumplir: %+v", rec)
	}
	// Review Focus 4: la política cambia; un informe con la huella anterior no da «cumple».
	a.assignToAll("windows", policy.Setting{Key: "windows.smb1", Mode: "enforce", Params: json.RawMessage(`{}`)})
	c.report(hash, ok)
	rec, _, _ = a.store.Compliance(ctx, c.id)
	if rec.State != "unknown" {
		t.Fatalf("informe de una huella antigua: %+v", rec)
	}
}
