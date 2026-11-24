package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mdm/internal/protocol"
)

var ctx = context.Background()

const ubuntuRoot = "testdata/linux/ubuntu"

// fakeRun responde a cada orden con el fichero grabado en cmd/<orden>; las
// órdenes listadas en fail devuelven error, como si no estuvieran instaladas.
func fakeRun(root string, fail ...string) runner {
	return func(_ context.Context, name string, _ ...string) ([]byte, error) {
		for _, f := range fail {
			if f == name {
				return nil, errors.New("orden no encontrada: " + name)
			}
		}
		b, err := os.ReadFile(filepath.Join(root, "cmd", name))
		if err != nil {
			return nil, errors.New("orden no encontrada: " + name)
		}
		return b, nil
	}
}

func testCollector(fail ...string) *linuxCollector {
	c := newLinux(ubuntuRoot, fakeRun(ubuntuRoot, fail...))
	c.statfs = func(path string) (uint64, uint64, error) { return 100 << 30, 40 << 30, nil }
	c.netifs = func() []protocol.NetIf {
		return []protocol.NetIf{{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"10.0.0.5"}, IPv6: []string{}}}
	}
	c.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
	return c
}

func TestLinuxCollect_FullTree(t *testing.T) {
	inv := testCollector().Collect(ctx)
	if len(inv.Unavailable) != 0 {
		t.Errorf("secciones no disponibles: %v", inv.Unavailable)
	}
	s := inv.System
	if s.Hostname != "srv-web-01" || s.OSName != "Ubuntu" || s.OSVersion != "24.04" || s.Kernel != "6.8.0-48-generic" ||
		s.Timezone != "Europe/Madrid" || s.LoggedInUser != "ana" || s.Arch == "" {
		t.Errorf("sistema: %+v", s)
	}
	if s.BootTime != "2026-11-12T12:00:00Z" {
		t.Errorf("hora de arranque: %s", s.BootTime)
	}
	h := inv.Hardware
	if h.Manufacturer != "Dell Inc." || h.Model != "PowerEdge R640" || h.Serial != "ABC1234" || h.BIOSVersion != "2.19.1" ||
		h.CPUCores != 8 || h.CPUThreads != 16 || h.MemoryBytes != 16699576320 || !strings.Contains(h.CPUModel, "Xeon") ||
		h.Chassis != protocol.ChassisServer {
		t.Errorf("hardware: %+v", h)
	}
	if len(inv.Disks) != 4 {
		t.Fatalf("discos: %+v", inv.Disks)
	}
	root := inv.Disks[0]
	if root.Mount != "/" || root.Device != "/dev/mapper/vg-root" || root.Filesystem != "ext4" || !root.Encrypted ||
		root.SizeBytes != 100<<30 || root.FreeBytes != 40<<30 {
		t.Errorf("raíz: %+v", root)
	}
	if inv.Disks[1].Encrypted || inv.Disks[3].Mount != "/mnt/datos compartidos" {
		t.Errorf("otros discos: %+v", inv.Disks)
	}
	sec := inv.Security
	if sec.DiskEncryption != "partial" || sec.Firewall != "on" || sec.Antivirus != "unknown" || sec.SecureBoot != "unknown" {
		t.Errorf("seguridad: %+v", sec)
	}
	if len(inv.Software) != 3 || inv.Software[0].Name != "bash" {
		t.Errorf("software: %+v", inv.Software)
	}
	if len(inv.Network) != 1 || inv.Network[0].Name != "eth0" {
		t.Errorf("red: %+v", inv.Network)
	}
	if inv.Hash == "" || inv.Hash != protocol.InventoryHash(inv) {
		t.Errorf("huella: %q", inv.Hash)
	}
	if !inv.CollectedAt.Equal(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("fecha: %v", inv.CollectedAt)
	}
}

func TestLinuxCollect_PartialFailure(t *testing.T) {
	inv := testCollector("dpkg-query", "rpm").Collect(ctx)
	if len(inv.Unavailable) != 1 || inv.Unavailable[0] != "software" {
		t.Fatalf("no disponibles: %v", inv.Unavailable)
	}
	if inv.System.Hostname == "" || inv.Hardware.Model == "" || len(inv.Disks) == 0 {
		t.Errorf("el resto debería estar poblado: %+v", inv)
	}
	if inv.Software == nil {
		t.Error("software debe ser una lista vacía, no nula")
	}
}

func TestLinuxCollect_FallsBackToRPM(t *testing.T) {
	c := testCollector("dpkg-query")
	base := c.run
	c.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "rpm" {
			return []byte("bash\t5.1.8-9.el9\tRocky\t1718000000\n"), nil
		}
		return base(ctx, name, args...)
	}
	inv := c.Collect(ctx)
	if len(inv.Unavailable) != 0 || len(inv.Software) != 1 || inv.Software[0].Version != "5.1.8-9.el9" {
		t.Fatalf("rpm: %v %+v", inv.Unavailable, inv.Software)
	}
}

func TestLinuxCollect_LsblkMissing(t *testing.T) {
	inv := testCollector("lsblk").Collect(ctx)
	if inv.Security.DiskEncryption != "unknown" || len(inv.Disks) != 4 {
		t.Fatalf("sin lsblk el cifrado es desconocido, no «sin cifrar»: %+v", inv.Security)
	}
}

func TestLinuxCollect_NoFirewallTools(t *testing.T) {
	if inv := testCollector("ufw", "firewall-cmd", "nft").Collect(ctx); inv.Security.Firewall != "unknown" {
		t.Fatalf("cortafuegos: %s", inv.Security.Firewall)
	}
}

func TestLinuxCollect_MissingTreeMarksSections(t *testing.T) {
	c := newLinux(t.TempDir(), func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no") })
	c.netifs = func() []protocol.NetIf { return nil }
	inv := c.Collect(ctx)
	got := strings.Join(inv.Unavailable, ",")
	for _, sec := range []string{"system", "hardware", "disks", "software"} {
		if !strings.Contains(got, sec) {
			t.Errorf("falta %s en no disponibles: %s", sec, got)
		}
	}
	if inv.Disks == nil || inv.Network == nil || inv.Software == nil {
		t.Error("las listas deben ser vacías, no nulas")
	}
}

func TestLinuxCollect_HashStableAcrossRuns(t *testing.T) {
	a := testCollector()
	h1 := a.Collect(ctx).Hash
	a.now = func() time.Time { return time.Date(2026, 11, 13, 18, 0, 0, 0, time.UTC) }
	a.statfs = func(string) (uint64, uint64, error) { return 100 << 30, 39 << 30, nil }
	if h2 := a.Collect(ctx).Hash; h1 != h2 {
		t.Fatal("la huella cambió solo por el paso del tiempo y el uso del disco")
	}
}

func TestLinuxIdentify(t *testing.T) {
	id := testCollector().Identify(ctx)
	if id.Platform != "linux" || id.Hostname != "srv-web-01" || id.OSName != "Ubuntu" || id.OSVersion != "24.04" ||
		id.Serial != "ABC1234" || id.Manufacturer != "Dell Inc." || id.Model != "PowerEdge R640" || id.Arch == "" {
		t.Errorf("identidad: %+v", id)
	}
	if id.HardwareID != "4c4c4544-0042-3510-8054-b4c04f564433" {
		t.Errorf("hardware_id: %s", id.HardwareID)
	}
	if id.Token != "" || id.AgentVersion != "" {
		t.Error("Identify no debe rellenar token ni versión")
	}
}

func TestLinuxIdentify_GenericUUIDFallsBackToMachineID(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "sys/class/dmi/id"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "sys/class/dmi/id/product_uuid"), []byte("03000200-0400-0500-0006-000700080009\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "etc/machine-id"), []byte("0123456789abcdef0123456789abcdef\n"), 0o644)
	c := newLinux(root, func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no") })
	if id := c.Identify(ctx); id.HardwareID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("hardware_id: %q", id.HardwareID)
	}
}
