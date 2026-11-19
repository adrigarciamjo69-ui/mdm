package adminapi

import (
	"encoding/json"
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
	"mdm/internal/server/compliance"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

// complyWith asigna a todos una política de la plataforma y registra un
// informe del equipo con el estado dado para cada ajuste.
func (a *testAPI) complyWith(id interface{ String() string }, platform string, settings []policy.Setting, status string) store.Policy {
	a.t.Helper()
	a.n++
	p, err := a.store.CreatePolicy(ctx, store.NewPolicy{Name: "Política " + string(rune('A'+a.n)), Platform: platform, Settings: settings})
	if err != nil {
		a.t.Fatal(err)
	}
	_ = a.store.SetAssignments(ctx, p.ID, []policy.Assignment{{}}, a.clock.Now())
	return p
}

func TestComplianceAcrossAPI(t *testing.T) {
	a := newTestAPI(t)
	ok := a.seedDevice(seed{Hostname: "cumple", Platform: "linux", SeenAgo: time.Minute})
	bad := a.seedDevice(seed{Hostname: "incumple", Platform: "linux", SeenAgo: time.Minute})
	a.seedDevice(seed{Hostname: "sin-informe", Platform: "linux", SeenAgo: time.Minute})
	set := []policy.Setting{{Key: "linux.firewall", Mode: "enforce", Params: json.RawMessage(`{}`)}}
	p := a.complyWith(ok, "linux", set, "")
	report := func(dev interface{ String() string }, status string) {
		id := mustUUID(dev.String())
		eff, _, _ := a.store.EffectivePolicyFor(ctx, id)
		_ = a.store.SaveComplianceReport(ctx, id, protocol.ComplianceReport{PolicyHash: eff.Hash, Results: []protocol.SettingResult{
			{PolicyID: p.ID.String(), Key: "linux.firewall", Status: status, Message: "ufw: inactive"}}}, a.clock.Now())
		if _, err := compliance.Recompute(ctx, a.store, id, a.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	report(ok, "compliant")
	report(bad, "noncompliant")
	ro := a.loginAs(auth.RoleReadonly)

	for q, want := range map[string]string{"compliance=compliant": "cumple", "compliance=noncompliant": "incumple", "compliance=unknown": "sin-informe"} {
		if got := names(list(t, ro, q)); got != want {
			t.Errorf("%s → %q, se esperaba %q", q, got, want)
		}
	}
	wantError(t, ro.do("GET", "/devices?compliance=raro", nil), 400, "invalid_request")
	if d := decode[deviceResp](t, ro.do("GET", "/devices/"+bad.String(), nil)); d.Compliance != "noncompliant" {
		t.Errorf("ficha: %+v", d)
	}

	type detail struct {
		State             string     `json:"state"`
		NoncompliantSince *time.Time `json:"noncompliant_since"`
		UpToDate          bool       `json:"up_to_date"`
		ServerReasons     []string   `json:"server_reasons"`
		Results           []struct {
			PolicyName string `json:"policy_name"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			Message    string `json:"message"`
		} `json:"results"`
		Conflicts []any `json:"conflicts"`
	}
	d := decode[detail](t, ro.do("GET", "/devices/"+bad.String()+"/compliance", nil))
	if d.State != "noncompliant" || d.NoncompliantSince == nil || !d.UpToDate || len(d.Results) != 1 ||
		d.Results[0].Title != "Cortafuegos activado" || d.Results[0].PolicyName != p.Name || d.Results[0].Message != "ufw: inactive" ||
		d.ServerReasons == nil || d.Conflicts == nil {
		t.Fatalf("detalle: %+v", d)
	}

	type summary struct {
		Compliance map[string]int `json:"compliance"`
	}
	s := decode[summary](t, ro.do("GET", "/summary", nil))
	if s.Compliance["compliant"] != 1 || s.Compliance["noncompliant"] != 1 || s.Compliance["unknown"] != 1 {
		t.Fatalf("resumen: %+v", s.Compliance)
	}
}

func TestComplianceConflictsShown(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Platform: "windows"})
	lock := func(m int) []policy.Setting {
		return []policy.Setting{{Key: "windows.screen_lock", Mode: "enforce", Params: json.RawMessage(`{"minutes":` + string(rune('0'+m)) + `}`)}}
	}
	a.complyWith(id, "windows", lock(5), "")
	a.complyWith(id, "windows", lock(9), "")
	type detail struct {
		Conflicts []struct {
			Title      string `json:"title"`
			WinnerName string `json:"winner_name"`
			LoserName  string `json:"loser_name"`
		} `json:"conflicts"`
	}
	d := decode[detail](t, a.loginAs(auth.RoleReadonly).do("GET", "/devices/"+id.String()+"/compliance", nil))
	if len(d.Conflicts) != 1 || d.Conflicts[0].Title != "Bloqueo de pantalla por inactividad" || d.Conflicts[0].WinnerName == "" || d.Conflicts[0].LoserName == "" {
		t.Fatalf("conflictos: %+v", d.Conflicts)
	}
}
