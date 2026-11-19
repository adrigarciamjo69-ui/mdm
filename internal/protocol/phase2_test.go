package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func set(policy, key, mode, params string) PolicySetting {
	return PolicySetting{PolicyID: policy, Key: key, Mode: mode, Params: json.RawMessage(params)}
}

func TestPolicyHash_OrderIndependent(t *testing.T) {
	a := []PolicySetting{set("p1", "windows.firewall", "enforce", `{}`), set("p2", "windows.screen_lock", "audit", `{"minutes":5}`)}
	b := []PolicySetting{a[1], a[0]}
	if PolicyHash(a, []string{"k2", "k1"}) != PolicyHash(b, []string{"k1", "k2"}) {
		t.Fatal("la huella depende del orden")
	}
	if !strings.HasPrefix(PolicyHash(a, nil), "sha256:") {
		t.Fatal("formato de la huella")
	}
}

func TestPolicyHash_ContentMatters(t *testing.T) {
	base := []PolicySetting{set("p1", "windows.screen_lock", "enforce", `{"minutes":5}`)}
	h := PolicyHash(base, nil)
	for _, other := range [][]PolicySetting{
		{set("p1", "windows.screen_lock", "enforce", `{"minutes":6}`)},
		{set("p1", "windows.screen_lock", "audit", `{"minutes":5}`)},
		{set("p2", "windows.screen_lock", "enforce", `{"minutes":5}`)},
	} {
		if PolicyHash(other, nil) == h {
			t.Errorf("la huella no cambió con %+v", other[0])
		}
	}
	if PolicyHash(base, []string{"k1"}) == h {
		t.Error("custodiar una clave nueva debe cambiar la huella")
	}
	// Los espacios del JSON de parámetros no cambian la huella.
	if PolicyHash([]PolicySetting{set("p1", "windows.screen_lock", "enforce", `{ "minutes" : 5 }`)}, nil) != h {
		t.Error("la huella depende del formato del JSON")
	}
}

func TestPolicyHash_EmptyPolicyStable(t *testing.T) {
	if PolicyHash(nil, nil) != PolicyHash([]PolicySetting{}, []string{}) {
		t.Fatal("nil y vacío deben dar la misma huella")
	}
}

func TestCheckinResponse_OmitsNullUpdate(t *testing.T) {
	b, _ := json.Marshal(CheckinResponse{PolicyHash: "sha256:x"})
	s := string(b)
	if strings.Contains(s, "agent_update") || !strings.Contains(s, `"commands":[]`) || !strings.Contains(s, `"policy_hash":"sha256:x"`) {
		t.Fatalf("respuesta: %s", s)
	}
	b, _ = json.Marshal(CheckinResponse{AgentUpdate: &AgentUpdate{Version: "1.1.0", URL: "/downloads/x", SHA256: "abc"}})
	if !strings.Contains(string(b), `"agent_update":{"version":"1.1.0","url":"/downloads/x","sha256":"abc"}`) {
		t.Fatalf("con actualización: %s", b)
	}
}

func TestRequiredCapability(t *testing.T) {
	if RequiredCapability(CmdRunScript) != CapScripts || RequiredCapability(CmdReboot) != CapCommands || RequiredCapability(CmdRetire) != CapCommands {
		t.Fatal("capacidades requeridas")
	}
}

func TestSettingKeys(t *testing.T) {
	if len(SettingKeys["windows"]) != 10 || len(SettingKeys["linux"]) != 5 {
		t.Fatalf("claves: %d windows, %d linux", len(SettingKeys["windows"]), len(SettingKeys["linux"]))
	}
	if CustomScriptKey != "custom.script" {
		t.Fatal("clave del script personalizado")
	}
}

func TestJSONNames_Phase2(t *testing.T) {
	check := func(v any, keys ...string) {
		b, _ := json.Marshal(v)
		for _, k := range keys {
			if !strings.Contains(string(b), `"`+k+`"`) {
				t.Errorf("falta %s en %s", k, b)
			}
		}
	}
	code := 0
	check(CheckinRequest{}, "policy_hash", "capabilities")
	check(EffectivePolicy{}, "hash", "evaluate_interval_s", "settings", "escrowed_key_ids")
	check(ComplianceReport{}, "policy_hash", "evaluated_at", "results")
	check(SettingResult{}, "policy_id", "key", "status", "message", "applied")
	check(CommandResult{ExitCode: &code}, "status", "exit_code", "stdout", "stderr", "error", "started_at", "finished_at")
	check(RecoveryKey{}, "volume", "key_id", "recovery_password")
	check(Command{}, "expires_at")
	check(ScriptPayload{}, "interpreter", "content", "timeout_s", "env", "script_name", "version")
	check(PowerPayload{}, "delay_s", "message")
}
