package protocol

import (
	"regexp"
	"testing"
	"time"
)

func TestInventoryHash_IgnoresVolatileFields(t *testing.T) {
	a := sampleInventory()
	b := sampleInventory()
	b.CollectedAt = a.CollectedAt.Add(time.Hour)
	b.Hash = "sha256:otro"
	b.System.BootTime = "2026-01-01T00:00:00Z"
	b.Disks[0].FreeBytes = 1
	if InventoryHash(a) != InventoryHash(b) {
		t.Fatal("la huella cambió con campos volátiles")
	}
}

func TestInventoryHash_ChangesWithSoftware(t *testing.T) {
	a := sampleInventory()
	b := sampleInventory()
	b.Software = append(b.Software, Software{Name: "vim", Version: "9"})
	if InventoryHash(a) == InventoryHash(b) {
		t.Fatal("la huella no cambió al añadir software")
	}
}

func TestInventoryHash_OrderIndependent(t *testing.T) {
	a := sampleInventory()
	b := sampleInventory()
	b.Software[0], b.Software[1] = b.Software[1], b.Software[0]
	b.Disks[0], b.Disks[1] = b.Disks[1], b.Disks[0]
	b.Network[0], b.Network[1] = b.Network[1], b.Network[0]
	a.Unavailable = []string{"security", "software"}
	b.Unavailable = []string{"software", "security"}
	if InventoryHash(a) != InventoryHash(b) {
		t.Fatal("la huella depende del orden")
	}
}

func TestInventoryHash_DoesNotMutateInput(t *testing.T) {
	a := sampleInventory()
	InventoryHash(a)
	if a.Software[0].Name != "curl" || a.Disks[0].FreeBytes != 200<<30 || a.Hash != "sha256:x" {
		t.Fatal("InventoryHash modificó su argumento")
	}
}

func TestInventoryHash_Format(t *testing.T) {
	h := InventoryHash(sampleInventory())
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(h) {
		t.Fatalf("formato inesperado: %s", h)
	}
}
