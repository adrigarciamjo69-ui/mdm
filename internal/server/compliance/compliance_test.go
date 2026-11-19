package compliance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/settings"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func eff(keys ...string) protocol.EffectivePolicy {
	var s []protocol.PolicySetting
	for _, k := range keys {
		s = append(s, protocol.PolicySetting{PolicyID: "p1", Key: k, Mode: "enforce", Params: json.RawMessage(`{}`)})
	}
	return protocol.EffectivePolicy{Hash: protocol.PolicyHash(s, nil), Settings: s}
}

func res(key, status string) protocol.SettingResult {
	return protocol.SettingResult{PolicyID: "p1", Key: key, Status: status}
}

func input(e protocol.EffectivePolicy, results ...protocol.SettingResult) Input {
	seen := now.Add(-time.Minute)
	reported := now.Add(-10 * time.Minute)
	return Input{Effective: e, ReportHash: e.Hash, ReportedAt: &reported, Results: results,
		Device:   DeviceInfo{Platform: "windows", LastSeenAt: &seen, AgentVersion: "1.1.0", OSVersion: "10.0.26100"},
		Settings: settings.Defaults(), Now: now}
}

func TestCompute_AllCompliant(t *testing.T) {
	r := Compute(input(eff("a", "b"), res("a", "compliant"), res("b", "compliant")))
	if r.State != Compliant || len(r.Reasons) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestCompute_NotApplicableCounts(t *testing.T) {
	if r := Compute(input(eff("a", "b"), res("a", "compliant"), res("b", "not_applicable"))); r.State != Compliant {
		t.Fatalf("%+v", r)
	}
}

func TestCompute_ErrorAndNoncompliant(t *testing.T) {
	for _, st := range []string{"noncompliant", "error"} {
		r := Compute(input(eff("a", "b"), res("a", "compliant"), res("b", st)))
		if r.State != Noncompliant || len(r.Reasons) != 0 {
			t.Errorf("%s: %+v", st, r)
		}
	}
}

func TestCompute_StaleHashIsUnknown(t *testing.T) {
	in := input(eff("a"), res("a", "compliant"))
	in.ReportHash = "sha256:anterior"
	if r := Compute(in); r.State != Unknown {
		t.Fatalf("informe de una huella anterior: %+v", r)
	}
}

func TestCompute_MissingResultIsUnknown(t *testing.T) {
	if r := Compute(input(eff("a", "b"), res("a", "compliant"))); r.State != Unknown {
		t.Fatalf("falta el resultado de un ajuste: %+v", r)
	}
}

func TestCompute_NoReportIn24hIsUnknown(t *testing.T) {
	in := input(eff("a"), res("a", "compliant"))
	old := now.Add(-25 * time.Hour)
	in.ReportedAt = &old
	if r := Compute(in); r.State != Unknown {
		t.Fatalf("%+v", r)
	}
	in.ReportedAt = nil
	if r := Compute(in); r.State != Unknown {
		t.Fatalf("sin informe: %+v", r)
	}
}

func TestCompute_NoPoliciesIsUnknown(t *testing.T) {
	if r := Compute(input(eff())); r.State != Unknown {
		t.Fatalf("%+v", r)
	}
}

func TestCompute_ServerRules(t *testing.T) {
	base := func() Input { return input(eff("a"), res("a", "compliant")) }

	in := base()
	old := now.Add(-15 * 24 * time.Hour)
	in.Device.LastSeenAt = &old
	if r := Compute(in); r.State != Noncompliant || !strings.Contains(r.Reasons[0], "15 días") {
		t.Errorf("sin conexión: %+v", r)
	}

	in = base()
	in.Settings.MinAgentVersion = "1.2.0"
	if r := Compute(in); r.State != Noncompliant || !strings.Contains(r.Reasons[0], "agente") {
		t.Errorf("versión del agente: %+v", r)
	}

	in = base()
	in.Settings.MinOSVersion = map[string]string{"windows": "10.0.26200"}
	if r := Compute(in); r.State != Noncompliant || !strings.Contains(r.Reasons[0], "sistema") {
		t.Errorf("versión del sistema: %+v", r)
	}
	in.Device.Platform = "linux"
	if r := Compute(in); r.State != Compliant {
		t.Errorf("el mínimo de Windows no aplica a Linux: %+v", r)
	}

	in = input(eff())
	in.Device.LastSeenAt = &old
	if r := Compute(in); r.State != Noncompliant {
		t.Errorf("sin políticas pero fuera de plazo: %+v", r)
	}
}

func TestNextSince(t *testing.T) {
	prev := now.Add(-3 * time.Hour)
	if got := NextSince(Noncompliant, Noncompliant, &prev, now); got == nil || !got.Equal(prev) {
		t.Errorf("sigue sin cumplir: %v", got)
	}
	if got := NextSince(Compliant, Noncompliant, nil, now); got == nil || !got.Equal(now) {
		t.Errorf("deja de cumplir: %v", got)
	}
	if got := NextSince(Noncompliant, Compliant, &prev, now); got != nil {
		t.Errorf("vuelve a cumplir: %v", got)
	}
	if got := NextSince(Noncompliant, Unknown, &prev, now); got != nil {
		t.Errorf("desconocido: %v", got)
	}
}
