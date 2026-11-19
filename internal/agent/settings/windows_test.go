package settings

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

func TestParseWindowsResult(t *testing.T) {
	r, err := parseWindowsResult("ADVERTENCIA: algo\r\n{\"compliant\":true,\"detail\":\"Activado\",\"not_applicable\":false,\"recovery\":null}\r\n")
	if err != nil || !r.Compliant || r.Detail != "Activado" || r.Recovery != nil {
		t.Fatalf("%+v %v", r, err)
	}
	r, _ = parseWindowsResult(`{"compliant":false,"detail":"x","not_applicable":true}`)
	if !r.NotApplicable {
		t.Fatal("no aplicable")
	}
	r, _ = parseWindowsResult(`{"error":"Acceso denegado"}`)
	if r.Error != "Acceso denegado" {
		t.Fatalf("%+v", r)
	}
	if _, err := parseWindowsResult(""); err == nil || !strings.Contains(err.Error(), "sin resultado") {
		t.Fatalf("salida vacía: %v", err)
	}
	if _, err := parseWindowsResult("Get-Foo : no se reconoce"); err == nil {
		t.Fatal("sin JSON debe dar error")
	}
}

func TestParseRecovery(t *testing.T) {
	r, err := parseWindowsResult(`{"compliant":true,"detail":"","recovery":{"volume":"C:","key_id":"{K1}","recovery_password":"111111-222222-333333-444444-555555-666666-777777-888888"}}`)
	if err != nil || r.Recovery == nil || r.Recovery.KeyID != "{K1}" || r.Recovery.Volume != "C:" {
		t.Fatalf("%+v %v", r, err)
	}
}

type psCall struct {
	env    map[string]string
	script string
}

func winFake(t *testing.T, reply func(mode string) (string, int)) (Deps, *[]psCall) {
	var calls []psCall
	return Deps{Exec: func(_ context.Context, s exec.Spec) exec.Result {
		if s.Interpreter != "powershell" {
			t.Fatalf("intérprete: %s", s.Interpreter)
		}
		calls = append(calls, psCall{env: s.Env, script: s.Content})
		out, code := reply(s.Env["MDM_MODE"])
		return exec.Result{Stdout: out, ExitCode: code}
	}}, &calls
}

func TestWindowsSetting_ParamsAsEnv(t *testing.T) {
	d, calls := winFake(t, func(string) (string, int) { return `{"compliant":true,"detail":"ok"}`, 0 })
	s, err := Build("windows", protocol.PolicySetting{Key: "windows.password_policy", Params: json.RawMessage(`{"min_length":12,"max_age_days":0}`)}, d)
	if err != nil {
		t.Fatal(err)
	}
	ok, detail, err := s.Check(ctx)
	if !ok || detail != "ok" || err != nil {
		t.Fatalf("%v %q %v", ok, detail, err)
	}
	c := (*calls)[0]
	if c.env["MDM_MODE"] != "Test" || c.env["MDM_PARAM_MIN_LENGTH"] != "12" || c.env["MDM_PARAM_MAX_AGE_DAYS"] != "0" {
		t.Fatalf("entorno: %v", c.env)
	}
	if !strings.Contains(c.script, "MDM_MODE") {
		t.Fatal("debe ejecutarse el script incrustado")
	}
}

func TestWindowsSetting_NotApplicableAndErrors(t *testing.T) {
	d, _ := winFake(t, func(string) (string, int) {
		return `{"compliant":false,"detail":"El equipo no tiene TPM.","not_applicable":true}`, 0
	})
	s, _ := Build("windows", protocol.PolicySetting{Key: "windows.bitlocker", Params: json.RawMessage(`{"escrow_key":true}`)}, d)
	if _, detail, err := s.Check(ctx); !errors.Is(err, ErrNotApplicable) || detail != "El equipo no tiene TPM." {
		t.Fatalf("%q %v", detail, err)
	}
	d, _ = winFake(t, func(mode string) (string, int) {
		if mode == "Set" {
			return `{"error":"Acceso denegado"}`, 1
		}
		return `{"compliant":false,"detail":"x"}`, 0
	})
	s, _ = Build("windows", protocol.PolicySetting{Key: "windows.firewall"}, d)
	if err := s.Apply(ctx); err == nil || !strings.Contains(err.Error(), "Acceso denegado") {
		t.Fatalf("Apply: %v", err)
	}
	d, _ = winFake(t, func(string) (string, int) { return "", 1 })
	s, _ = Build("windows", protocol.PolicySetting{Key: "windows.firewall"}, d)
	if _, _, err := s.Check(ctx); err == nil {
		t.Fatal("sin salida debe dar error")
	}
}

func TestBitLockerNeedsEscrow(t *testing.T) {
	reply := `{"compliant":true,"detail":"Protegido","recovery":{"volume":"C:","key_id":"{K1}","recovery_password":"x"}}`
	d, _ := winFake(t, func(string) (string, int) { return reply, 0 })
	d.Escrowed = []string{"{K1}"}
	s, _ := Build("windows", protocol.PolicySetting{Key: "windows.bitlocker", Params: json.RawMessage(`{"escrow_key":true}`)}, d)
	_, _, _ = s.Check(ctx)
	if s.(RecoveryReporter).Recovery() != nil {
		t.Fatal("una clave ya custodiada no se reenvía")
	}
	d.Escrowed = nil
	s, _ = Build("windows", protocol.PolicySetting{Key: "windows.bitlocker", Params: json.RawMessage(`{"escrow_key":true}`)}, d)
	_, _, _ = s.Check(ctx)
	if k := s.(RecoveryReporter).Recovery(); k == nil || k.KeyID != "{K1}" {
		t.Fatalf("una clave nueva debe enviarse: %+v", k)
	}
	s, _ = Build("windows", protocol.PolicySetting{Key: "windows.bitlocker", Params: json.RawMessage(`{"escrow_key":false}`)}, d)
	_, _, _ = s.Check(ctx)
	if s.(RecoveryReporter).Recovery() != nil {
		t.Fatal("sin escrow_key no se custodia")
	}
}

func TestAllWindowsKeysHaveScript(t *testing.T) {
	want := append([]string{}, protocol.SettingKeys["windows"]...)
	var got []string
	entries, _ := windowsScripts.ReadDir("scripts")
	for _, e := range entries {
		got = append(got, strings.TrimSuffix(e.Name(), ".ps1"))
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("scripts:\n %v\n esperados:\n %v", got, want)
	}
	for _, k := range want {
		if _, err := Build("windows", protocol.PolicySetting{Key: k}, Deps{}); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
}
