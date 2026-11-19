package inventory

import (
	"os"
	"testing"
	"time"

	"mdm/internal/protocol"
)

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseOSRelease(t *testing.T) {
	name, ver := parseOSRelease(read(t, "testdata/linux/ubuntu/etc/os-release"))
	if name != "Ubuntu" || ver != "24.04" {
		t.Errorf("ubuntu: %q %q", name, ver)
	}
	name, ver = parseOSRelease([]byte("NAME=\"Rocky Linux\"\nVERSION_ID='9.4'\nID=rocky\n"))
	if name != "Rocky Linux" || ver != "9.4" {
		t.Errorf("rocky: %q %q", name, ver)
	}
	name, ver = parseOSRelease([]byte("# comentario\nNAME=Debian\nVERSION_ID=12\n\nBASURA\n"))
	if name != "Debian" || ver != "12" {
		t.Errorf("sin comillas: %q %q", name, ver)
	}
	if name, ver = parseOSRelease(nil); name != "Linux" || ver != "" {
		t.Errorf("vacío: %q %q", name, ver)
	}
}

func TestParseCPUInfo(t *testing.T) {
	model, cores, threads := parseCPUInfo(read(t, "testdata/linux/ubuntu/proc/cpuinfo"))
	if model != "Intel(R) Xeon(R) Gold 6248 CPU @ 2.50GHz" || cores != 8 || threads != 16 {
		t.Errorf("x86: %q %d %d", model, cores, threads)
	}
	arm := "processor\t: 0\nBogoMIPS\t: 108.00\nCPU part\t: 0xd0b\n\nprocessor\t: 1\nCPU part\t: 0xd0b\n\nprocessor\t: 2\nCPU part\t: 0xd0b\n\nprocessor\t: 3\nCPU part\t: 0xd0b\n\nHardware\t: Raspberry Pi 5\n"
	model, cores, threads = parseCPUInfo([]byte(arm))
	if model != "Raspberry Pi 5" || cores != 4 || threads != 4 {
		t.Errorf("arm con Hardware: %q %d %d", model, cores, threads)
	}
	model, _, _ = parseCPUInfo([]byte("processor\t: 0\nCPU part\t: 0xd0b\n"))
	if model != "ARM (CPU part 0xd0b)" {
		t.Errorf("arm sin Hardware: %q", model)
	}
	if model, cores, threads = parseCPUInfo(nil); model != "" || cores != 0 || threads != 0 {
		t.Errorf("vacío: %q %d %d", model, cores, threads)
	}
}

func TestParseMemTotal(t *testing.T) {
	if got := parseMemTotal(read(t, "testdata/linux/ubuntu/proc/meminfo")); got != 16699576320 {
		t.Errorf("got %d", got)
	}
	if got := parseMemTotal([]byte("MemFree: 1 kB\n")); got != 0 {
		t.Errorf("sin MemTotal: %d", got)
	}
}

func TestParseDpkg(t *testing.T) {
	sw := parseDpkg(read(t, "testdata/linux/ubuntu/cmd/dpkg-query"))
	if len(sw) != 3 {
		t.Fatalf("paquetes: %+v", sw)
	}
	if sw[0] != (protocol.Software{Name: "bash", Version: "5.2.21-2ubuntu4", Publisher: "Ubuntu Developers"}) {
		t.Errorf("primer paquete: %+v", sw[0])
	}
	for _, s := range sw {
		if s.Name == "viejo" {
			t.Error("incluye un paquete desinstalado")
		}
	}
}

func TestParseRPM(t *testing.T) {
	out := "bash\t5.1.8-9.el9\tRocky Enterprise Software Foundation\t1718000000\nkernel\t5.14.0-427.el9\t(none)\t1718000100\nlinea rota\n"
	sw := parseRPM([]byte(out))
	if len(sw) != 2 || sw[0].Name != "bash" || sw[0].Version != "5.1.8-9.el9" || sw[0].Publisher != "Rocky Enterprise Software Foundation" {
		t.Fatalf("paquetes: %+v", sw)
	}
	if sw[0].InstalledAt == nil || !sw[0].InstalledAt.Equal(time.Unix(1718000000, 0)) {
		t.Errorf("fecha de instalación: %v", sw[0].InstalledAt)
	}
	if sw[1].Publisher != "" {
		t.Errorf("(none) debe quedar vacío: %q", sw[1].Publisher)
	}
}

func TestParseLsblkCrypt(t *testing.T) {
	m, err := parseLsblkCrypt(read(t, "testdata/linux/ubuntu/cmd/lsblk"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/": true, "/var/snap/x": true, "/boot": false, "/boot/efi": false, "/mnt/datos compartidos": false}
	for mount, enc := range want {
		if got, ok := m[mount]; !ok || got != enc {
			t.Errorf("%s: cifrado=%v presente=%v", mount, got, ok)
		}
	}
	// Formato antiguo de lsblk: "mountpoint" en singular.
	old := `{"blockdevices":[{"name":"sda","type":"disk","mountpoint":null,"children":[{"name":"c","type":"crypt","mountpoint":"/home"}]}]}`
	if m, _ := parseLsblkCrypt([]byte(old)); !m["/home"] {
		t.Errorf("formato antiguo: %v", m)
	}
	if _, err := parseLsblkCrypt([]byte("no json")); err == nil {
		t.Error("una salida ilegible debe dar error")
	}
}

func TestParseMounts(t *testing.T) {
	ms := parseMounts(read(t, "testdata/linux/ubuntu/proc/mounts"))
	if len(ms) != 4 {
		t.Fatalf("montajes: %+v", ms)
	}
	want := []mount{
		{Device: "/dev/mapper/vg-root", Mount: "/", FS: "ext4"},
		{Device: "/dev/nvme0n1p2", Mount: "/boot", FS: "ext4"},
		{Device: "/dev/nvme0n1p1", Mount: "/boot/efi", FS: "vfat"},
		{Device: "/dev/sdb1", Mount: "/mnt/datos compartidos", FS: "xfs"},
	}
	for i, w := range want {
		if ms[i] != w {
			t.Errorf("montaje %d: %+v, se esperaba %+v", i, ms[i], w)
		}
	}
}

func TestFirewallState(t *testing.T) {
	cases := []struct {
		name                string
		ufw, firewalld, nft []byte
		want                string
	}{
		{"ufw activo", []byte("Status: active\n"), nil, nil, "on"},
		{"ufw inactivo", []byte("Status: inactive\n"), nil, nil, "off"},
		{"firewalld en marcha", nil, []byte("running\n"), nil, "on"},
		{"firewalld parado", nil, []byte("not running\n"), nil, "off"},
		{"nft con reglas", nil, nil, []byte("table inet filter {\n\tchain input {\n\t\ttype filter hook input priority 0; policy drop;\n\t}\n}\n"), "on"},
		{"nft vacío", nil, nil, []byte("\n"), "off"},
		{"ufw inactivo pero nft con reglas", []byte("Status: inactive\n"), nil, []byte("table ip filter {\n chain INPUT { type filter hook input priority 0; }\n}"), "on"},
		{"sin herramientas", nil, nil, nil, "unknown"},
	}
	for _, c := range cases {
		if got := firewallState(c.ufw, c.firewalld, c.nft); got != c.want {
			t.Errorf("%s: %s, se esperaba %s", c.name, got, c.want)
		}
	}
}

func TestEncryptionSummary(t *testing.T) {
	d := func(enc ...bool) []protocol.Disk {
		var out []protocol.Disk
		for _, e := range enc {
			out = append(out, protocol.Disk{Encrypted: e})
		}
		return out
	}
	cases := []struct {
		disks []protocol.Disk
		want  string
	}{{d(true, true), "on"}, {d(false, false), "off"}, {d(true, false), "partial"}, {nil, "unknown"}}
	for _, c := range cases {
		if got := encryptionSummary(c.disks); got != c.want {
			t.Errorf("%v: %s, se esperaba %s", c.disks, got, c.want)
		}
	}
}
