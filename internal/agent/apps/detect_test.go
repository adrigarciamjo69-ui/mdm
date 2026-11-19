package apps

import (
	"testing"

	"mdm/internal/protocol"
)

func TestDetect_Inventory(t *testing.T) {
	m := New(Deps{})
	inv := func() []protocol.Software {
		return []protocol.Software{{Name: "Contabilidad Pro 2026 (x64)", Version: "3.10.2"}, {Name: "Otra", Version: "1"}}
	}
	cases := []struct {
		d    protocol.Detection
		want bool
	}{
		{protocol.Detection{Type: "inventory", NameContains: "contabilidad pro"}, true},
		{protocol.Detection{Type: "inventory", NameContains: "contabilidad", MinVersion: "3.9"}, true}, // 3.10 > 3.9 por segmentos
		{protocol.Detection{Type: "inventory", NameContains: "contabilidad", MinVersion: "3.11"}, false},
		{protocol.Detection{Type: "inventory", NameContains: "nómina"}, false},
	}
	for _, c := range cases {
		got, err := m.detectRule(ctx, c.d, inv)
		if err != nil || got != c.want {
			t.Errorf("%+v → %v %v", c.d, got, err)
		}
	}
	m.d.Platform = "linux"
	if _, err := m.detectRule(ctx, protocol.Detection{Type: "registry", RegistryKey: "HKLM\\X"}, inv); err == nil {
		t.Error("la detección por registro no existe en Linux")
	}
}
