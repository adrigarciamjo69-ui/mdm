package inventory

import (
	"testing"
	"time"
)

func TestSoftwareFromUninstall(t *testing.T) {
	entries := []uninstallEntry{
		{DisplayName: "Mozilla Firefox", DisplayVersion: "132.0", Publisher: "Mozilla", InstallDate: "20260115"},
		{DisplayName: "7-Zip 24.08 (x64)", DisplayVersion: "24.08", Publisher: "Igor Pavlov"},
		{DisplayName: "", DisplayVersion: "1.0"},
		{DisplayName: "Componente interno", SystemComponent: 1},
		{DisplayName: "Actualización KB123", ParentKeyName: "Office"},
		{DisplayName: "Mozilla Firefox", DisplayVersion: "132.0", Publisher: "Mozilla"}, // repetido en la rama de 32 bits
		{DisplayName: "  Git  ", DisplayVersion: " 2.47 ", Publisher: " The Git Development Community "},
	}
	sw := softwareFromUninstall(entries)
	if len(sw) != 3 {
		t.Fatalf("programas: %+v", sw)
	}
	if sw[0].Name != "7-Zip 24.08 (x64)" || sw[1].Name != "Git" || sw[2].Name != "Mozilla Firefox" {
		t.Errorf("orden: %s, %s, %s", sw[0].Name, sw[1].Name, sw[2].Name)
	}
	if sw[1].Version != "2.47" || sw[1].Publisher != "The Git Development Community" {
		t.Errorf("espacios sin recortar: %+v", sw[1])
	}
	if sw[2].InstalledAt == nil || !sw[2].InstalledAt.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("fecha de instalación: %v", sw[2].InstalledAt)
	}
	if got := softwareFromUninstall(nil); got == nil || len(got) != 0 {
		t.Errorf("sin entradas debe dar lista vacía: %v", got)
	}
}

func TestParseInstallDate(t *testing.T) {
	if got := parseInstallDate("20260115"); got == nil || !got.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("fecha válida: %v", got)
	}
	for _, s := range []string{"", "0", "2026-01-15", "20261340", "abcdefgh", "00000000"} {
		if got := parseInstallDate(s); got != nil {
			t.Errorf("%q debería dar nil: %v", s, got)
		}
	}
}

func TestBitlockerState(t *testing.T) {
	cases := []struct {
		in   []uint32
		want string
	}{{[]uint32{1, 1}, "on"}, {[]uint32{0, 0}, "off"}, {[]uint32{1, 0}, "partial"}, {[]uint32{1, 2}, "partial"}, {nil, "unknown"}}
	for _, c := range cases {
		if got := bitlockerState(c.in); got != c.want {
			t.Errorf("%v: %s, se esperaba %s", c.in, got, c.want)
		}
	}
}

func TestFirewallFromProfiles(t *testing.T) {
	on, off := uint32(1), uint32(0)
	cases := []struct {
		d, p, pub *uint32
		want      string
	}{
		{&on, &on, &on, "on"}, {&off, &off, &off, "off"}, {&on, &off, &on, "partial"},
		{nil, nil, nil, "unknown"}, {&on, nil, &on, "on"}, {nil, &off, nil, "off"},
	}
	for i, c := range cases {
		if got := firewallFromProfiles(c.d, c.p, c.pub); got != c.want {
			t.Errorf("caso %d: %s, se esperaba %s", i, got, c.want)
		}
	}
}

func TestAntivirusFromProducts(t *testing.T) {
	if s, n := antivirusFromProducts([]avProduct{{DisplayName: "Microsoft Defender", ProductState: 0x061100}}); s != "on" || n != "Microsoft Defender" {
		t.Errorf("activado: %s %q", s, n)
	}
	if s, n := antivirusFromProducts([]avProduct{{DisplayName: "Otro AV", ProductState: 0x060100}}); s != "off" || n != "Otro AV" {
		t.Errorf("desactivado: %s %q", s, n)
	}
	if s, n := antivirusFromProducts(nil); s != "unknown" || n != "" {
		t.Errorf("sin productos: %s %q", s, n)
	}
	s, n := antivirusFromProducts([]avProduct{
		{DisplayName: "Microsoft Defender", ProductState: 0x060100},
		{DisplayName: "AV de terceros", ProductState: 0x051000},
	})
	if s != "on" || n != "AV de terceros" {
		t.Errorf("varios: %s %q", s, n)
	}
}

func TestWindowsOSName(t *testing.T) {
	cases := map[string]string{
		"Microsoft Windows 11 Pro":                "Windows 11 Pro",
		"Microsoft Windows Server 2022 Standard ": "Windows Server 2022 Standard",
		"Windows 10 Enterprise":                   "Windows 10 Enterprise",
		"":                                        "Windows",
	}
	for in, want := range cases {
		if got := windowsOSName(in); got != want {
			t.Errorf("%q → %q, se esperaba %q", in, got, want)
		}
	}
}
