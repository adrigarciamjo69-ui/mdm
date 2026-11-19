package main

import (
	"testing"
	"time"

	"mdm/internal/protocol"
)

func TestPercentile(t *testing.T) {
	var ds []time.Duration
	for i := 100; i >= 1; i-- { // desordenado a propósito
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	for p, want := range map[float64]time.Duration{50: 50 * time.Millisecond, 95: 95 * time.Millisecond, 99: 99 * time.Millisecond, 100: 100 * time.Millisecond} {
		if got := percentile(ds, p); got != want {
			t.Errorf("p%v = %v, se esperaba %v", p, got, want)
		}
	}
	if got := percentile(nil, 99); got != 0 {
		t.Errorf("lista vacía: %v", got)
	}
	if ds[0] != 100*time.Millisecond {
		t.Error("percentile no debe reordenar la lista de quien llama")
	}
}

func TestVerdict(t *testing.T) {
	cases := []struct {
		p99    time.Duration
		errors int
		want   bool
	}{{199 * time.Millisecond, 0, true}, {200 * time.Millisecond, 0, true}, {201 * time.Millisecond, 0, false}, {10 * time.Millisecond, 1, false}}
	for _, c := range cases {
		if got := verdict(c.p99, c.errors); got != c.want {
			t.Errorf("verdict(%v, %d) = %v", c.p99, c.errors, got)
		}
	}
}

func TestSyntheticInventory(t *testing.T) {
	a, b := syntheticInventory(7), syntheticInventory(7)
	if len(a.Software) != 150 {
		t.Fatalf("programas: %d", len(a.Software))
	}
	if a.Hash == "" || a.Hash != b.Hash || a.Hash != protocol.InventoryHash(a) {
		t.Fatalf("huella inestable o incorrecta: %s %s", a.Hash, b.Hash)
	}
	if syntheticInventory(8).Hash == a.Hash {
		t.Fatal("dos agentes distintos tienen la misma huella")
	}
	if a.System.Hostname != "carga-0007" {
		t.Fatalf("nombre: %s", a.System.Hostname)
	}
}
