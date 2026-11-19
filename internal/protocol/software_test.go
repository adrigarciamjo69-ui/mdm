package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func app(id, version string) AppAssignment {
	return AppAssignment{DeploymentID: id, AppName: "App " + id, Version: version, Intent: "required", Source: "apt", PackageID: "pkg-" + id}
}

func TestPolicyHashFull_IncludesAppsAndPatches(t *testing.T) {
	base := EffectivePolicy{Apps: []AppAssignment{app("a", "1"), app("b", "1")},
		Patches: &PatchPolicy{Window: Window{Days: []int{1}, Start: "22:00", End: "05:00"}, ApprovedUpdateIDs: []string{"u2", "u1"}}}
	h := PolicyHashFull(base)
	swapped := base
	swapped.Apps = []AppAssignment{app("b", "1"), app("a", "1")}
	swapped.Patches = &PatchPolicy{Window: Window{Days: []int{1}, Start: "22:00", End: "05:00"}, ApprovedUpdateIDs: []string{"u1", "u2"}}
	if PolicyHashFull(swapped) != h {
		t.Fatal("el orden de apps o de actualizaciones aprobadas no debe cambiar la huella")
	}
	for name, mod := range map[string]func(p *EffectivePolicy){
		"versión": func(p *EffectivePolicy) { p.Apps = []AppAssignment{app("a", "2"), app("b", "1")} },
		"reintento": func(p *EffectivePolicy) {
			a := app("a", "1")
			a.RetryEpoch = 1
			p.Apps = []AppAssignment{a, app("b", "1")}
		},
		"ventana": func(p *EffectivePolicy) {
			p.Patches = &PatchPolicy{Window: Window{Days: []int{1}, Start: "23:00", End: "05:00"}}
		},
		"sin parches": func(p *EffectivePolicy) { p.Patches = nil },
	} {
		p := base
		mod(&p)
		if PolicyHashFull(p) == h {
			t.Errorf("%s: la huella debería cambiar", name)
		}
	}
	// Con solo ajustes, coincide con la huella de la fase 2: los agentes
	// antiguos no ven cambiar su política al actualizar el servidor.
	s := []PolicySetting{{PolicyID: "p", Key: "k", Mode: "audit"}}
	if PolicyHashFull(EffectivePolicy{Settings: s, EscrowedKeyIDs: []string{"x"}}) != PolicyHash(s, []string{"x"}) {
		t.Fatal("sin apps ni parches la huella debe ser la de la fase 2")
	}
}

func at(day, hhmm string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func TestWindow_Contains(t *testing.T) {
	w := Window{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "05:00"} // lunes a viernes por la noche
	utc := time.UTC
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at("2026-11-16", "23:00", utc), true},  // lunes 23:00
		{at("2026-11-17", "01:30", utc), true},  // martes 01:30: noche del lunes
		{at("2026-11-21", "01:30", utc), true},  // sábado 01:30: noche del viernes
		{at("2026-11-22", "01:30", utc), false}, // domingo 01:30: el sábado no está
		{at("2026-11-16", "01:30", utc), false}, // lunes 01:30: el domingo no está
		{at("2026-11-17", "09:00", utc), false},
		{at("2026-11-17", "05:00", utc), false}, // el final no está incluido
		{at("2026-11-17", "22:00", utc), true},  // el inicio sí
	}
	for _, c := range cases {
		if got := w.Contains(c.t); got != c.want {
			t.Errorf("%s (%s): %v, se esperaba %v", c.t.Format("Mon 15:04"), c.t.Weekday(), got, c.want)
		}
	}
	day := Window{Days: []int{6}, Start: "08:00", End: "14:00"}
	if !day.Contains(at("2026-11-21", "09:00", utc)) || day.Contains(at("2026-11-21", "15:00", utc)) {
		t.Error("ventana dentro del mismo día")
	}
	if (Window{Start: "00:00", End: "23:59"}).Contains(at("2026-11-21", "09:00", utc)) {
		t.Error("una ventana sin días nunca contiene nada")
	}
	all := Window{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "00:00", End: "00:00"}
	if !all.Contains(at("2026-11-21", "13:37", utc)) {
		t.Error("inicio igual al fin significa todo el día")
	}
}

func TestWindow_DSTNight(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("sin base de datos de zonas")
	}
	w := Window{Days: []int{6}, Start: "22:00", End: "05:00"} // sábado por la noche
	// La noche del 28 al 29 de marzo de 2026 la hora salta de 02:00 a 03:00.
	if !w.Contains(at("2026-03-29", "03:15", madrid)) {
		t.Error("03:15 del domingo tras el cambio de hora está en la ventana del sábado")
	}
	if !w.Contains(at("2026-03-29", "01:30", madrid)) || w.Contains(at("2026-03-29", "05:30", madrid)) {
		t.Error("límites en la noche del cambio de hora")
	}
}

func TestWindow_Validate(t *testing.T) {
	ok := Window{Days: []int{1}, Start: "22:00", End: "05:00"}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, w := range []Window{{Days: []int{7}, Start: "01:00", End: "02:00"}, {Days: []int{1}, Start: "25:00", End: "02:00"},
		{Days: []int{1}, Start: "1:00", End: "02:00"}, {Days: []int{}, Start: "01:00", End: "02:00"}} {
		if w.Validate() == nil {
			t.Errorf("debería ser inválida: %+v", w)
		}
	}
}

func TestRequiredCapability_Phase3(t *testing.T) {
	if RequiredCapability(CmdScanUpdates) != CapPatches || RequiredCapability(CmdInstallUpdatesNow) != CapPatches {
		t.Fatal("las órdenes de parches requieren patches.v1")
	}
}

func TestJSONNames_Phase3(t *testing.T) {
	check := func(v any, keys ...string) {
		b, _ := json.Marshal(v)
		for _, k := range keys {
			if !strings.Contains(string(b), `"`+k+`"`) {
				t.Errorf("falta %s en %s", k, b)
			}
		}
	}
	check(EffectivePolicy{Apps: []AppAssignment{{}}, Patches: &PatchPolicy{}}, "apps", "patches")
	check(AppAssignment{Detection: &Detection{}}, "deployment_id", "intent", "source", "package_id", "file_sha256", "install_command",
		"uninstall_command", "success_codes", "detection", "keep_updated", "respect_window", "reboot", "retry_epoch", "start_at")
	check(PatchPolicy{}, "window", "reboot", "notify_minutes", "linux_scope", "approved_update_ids")
	check(AppStatus{}, "deployment_id", "state", "attempts", "exit_code", "reboot_required")
	check(UpdateScan{}, "scanned_at", "reboot_pending", "updates")
	check(UpdateInfo{}, "id", "kb", "title", "classification", "severity", "size_bytes", "published_at", "security")
	b, _ := json.Marshal(EffectivePolicy{})
	if strings.Contains(string(b), "patches") {
		t.Error("sin política de parches no se envía «patches»")
	}
}
