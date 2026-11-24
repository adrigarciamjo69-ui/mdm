package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mdm/internal/protocol"
)

// runner ejecuta una orden del sistema y devuelve su salida.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// linuxCollector recoge el inventario leyendo /proc, /sys y /etc bajo root y
// ejecutando órdenes con run. Ambos se sustituyen en las pruebas.
type linuxCollector struct {
	root   string
	run    runner
	statfs func(path string) (size, free uint64, err error)
	netifs func() []protocol.NetIf
	now    func() time.Time
}

func newLinux(root string, run runner) *linuxCollector {
	return &linuxCollector{
		root:   root,
		run:    run,
		statfs: func(string) (uint64, uint64, error) { return 0, 0, errors.New("statfs no disponible") },
		netifs: collectNetwork,
		now:    time.Now,
	}
}

func (c *linuxCollector) read(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(c.root, rel))
}

// readString lee un fichero de una línea; devuelve "" si no existe.
func (c *linuxCollector) readString(rel string) string {
	b, _ := c.read(rel)
	return strings.TrimSpace(string(b))
}

func (c *linuxCollector) dmi(name string) string {
	return c.readString("sys/class/dmi/id/" + name)
}

func (c *linuxCollector) hostname() string {
	if h := c.readString("proc/sys/kernel/hostname"); h != "" {
		return h
	}
	h, _ := os.Hostname()
	return h
}

func (c *linuxCollector) Identify(ctx context.Context) protocol.EnrollRequest {
	osRelease, _ := c.read("etc/os-release")
	name, version := parseOSRelease(osRelease)
	return protocol.EnrollRequest{
		HardwareID:   chooseHardwareID(c.dmi("product_uuid"), c.readString("etc/machine-id")),
		Hostname:     c.hostname(),
		Platform:     protocol.PlatformLinux,
		OSName:       name,
		OSVersion:    version,
		Arch:         runtime.GOARCH,
		Serial:       c.dmi("product_serial"),
		Manufacturer: c.dmi("sys_vendor"),
		Model:        c.dmi("product_name"),
	}
}

func (c *linuxCollector) Collect(ctx context.Context) protocol.Inventory {
	inv := protocol.Inventory{CollectedAt: c.now().UTC()}
	fail := func(section string) { inv.Unavailable = append(inv.Unavailable, section) }

	if sys, err := c.system(ctx); err != nil {
		fail(protocol.SectionSystem)
	} else {
		inv.System = sys
	}
	if hw, err := c.hardware(); err != nil {
		fail(protocol.SectionHardware)
	} else {
		inv.Hardware = hw
	}
	disks, encryptionKnown, err := c.disks(ctx)
	if err != nil {
		fail(protocol.SectionDisks)
	} else {
		inv.Disks = disks
	}
	inv.Network = c.netifs()
	inv.Security = c.security(ctx, inv.Disks, encryptionKnown)
	if sw, err := c.software(ctx); err != nil {
		fail(protocol.SectionSoftware)
	} else {
		inv.Software = sw
	}
	finish(&inv)
	return inv
}

func (c *linuxCollector) system(ctx context.Context) (protocol.System, error) {
	osRelease, err := c.read("etc/os-release")
	if err != nil {
		return protocol.System{}, err
	}
	name, version := parseOSRelease(osRelease)
	s := protocol.System{
		Hostname:  c.hostname(),
		OSName:    name,
		OSVersion: version,
		Kernel:    c.readString("proc/sys/kernel/osrelease"),
		Arch:      runtime.GOARCH,
		Timezone:  c.timezone(),
	}
	if f := strings.Fields(c.readString("proc/uptime")); len(f) > 0 {
		if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
			boot := c.now().Add(-time.Duration(secs * float64(time.Second)))
			s.BootTime = boot.UTC().Round(time.Second).Format(time.RFC3339)
		}
	}
	if out, err := c.run(ctx, "who"); err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			s.LoggedInUser = f[0]
		}
	}
	return s, nil
}

func (c *linuxCollector) timezone() string {
	if tz := c.readString("etc/timezone"); tz != "" {
		return tz
	}
	if target, err := os.Readlink(filepath.Join(c.root, "etc/localtime")); err == nil {
		if _, tz, ok := strings.Cut(target, "zoneinfo/"); ok {
			return tz
		}
	}
	return ""
}

func (c *linuxCollector) hardware() (protocol.Hardware, error) {
	cpuinfo, err := c.read("proc/cpuinfo")
	if err != nil {
		return protocol.Hardware{}, err
	}
	meminfo, err := c.read("proc/meminfo")
	if err != nil {
		return protocol.Hardware{}, err
	}
	model, cores, threads := parseCPUInfo(cpuinfo)
	// Los datos DMI pueden faltar (placas ARM, algunos contenedores): se dejan vacíos.
	return protocol.Hardware{
		Manufacturer: c.dmi("sys_vendor"), Model: c.dmi("product_name"), Serial: c.dmi("product_serial"),
		BIOSVendor: c.dmi("bios_vendor"), BIOSVersion: c.dmi("bios_version"), BIOSDate: c.dmi("bios_date"),
		CPUModel: model, CPUCores: cores, CPUThreads: threads, MemoryBytes: parseMemTotal(meminfo),
		Chassis: chassisFromDMI(c.dmi("chassis_type")),
	}, nil
}

// chassisFromDMI traduce el código de /sys/class/dmi/id/chassis_type.
func chassisFromDMI(raw string) string {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return protocol.ChassisUnknown
	}
	return protocol.ChassisFromSMBIOS([]int{n})
}

// disks devuelve los volúmenes montados. encryptionKnown es falso si no se
// pudo consultar lsblk, en cuyo caso el cifrado se desconoce.
func (c *linuxCollector) disks(ctx context.Context) (disks []protocol.Disk, encryptionKnown bool, err error) {
	raw, err := c.read("proc/mounts")
	if err != nil {
		return nil, false, err
	}
	crypt := c.cryptMap(ctx)
	disks = []protocol.Disk{}
	for _, m := range parseMounts(raw) {
		d := protocol.Disk{Device: m.Device, Mount: m.Mount, Filesystem: m.FS, Encrypted: crypt[m.Mount]}
		if size, free, err := c.statfs(m.Mount); err == nil {
			d.SizeBytes, d.FreeBytes = size, free
		}
		disks = append(disks, d)
	}
	return disks, crypt != nil, nil
}

// cryptMap consulta lsblk; devuelve nil si no está disponible o no se entiende.
func (c *linuxCollector) cryptMap(ctx context.Context) map[string]bool {
	out, err := c.run(ctx, "lsblk", "-J", "-o", "NAME,TYPE,MOUNTPOINTS")
	if err != nil { // lsblk antiguo: no conoce la columna MOUNTPOINTS
		out, err = c.run(ctx, "lsblk", "-J", "-o", "NAME,TYPE,MOUNTPOINT")
	}
	if err != nil {
		return nil
	}
	m, err := parseLsblkCrypt(out)
	if err != nil {
		return nil
	}
	return m
}

func (c *linuxCollector) security(ctx context.Context, disks []protocol.Disk, encryptionKnown bool) protocol.Security {
	tool := func(name string, args ...string) []byte {
		out, err := c.run(ctx, name, args...)
		if err != nil {
			return nil
		}
		if out == nil {
			out = []byte{}
		}
		return out
	}
	sec := protocol.Security{
		DiskEncryption: protocol.StateUnknown,
		Firewall:       firewallState(tool("ufw", "status"), tool("firewall-cmd", "--state"), tool("nft", "list", "ruleset")),
		// En Linux no hay un registro estándar de antivirus.
		Antivirus:  protocol.StateUnknown,
		SecureBoot: c.secureBoot(),
	}
	if encryptionKnown {
		sec.DiskEncryption = encryptionSummary(disks)
	}
	return sec
}

// secureBoot lee la variable EFI SecureBoot: su último byte es 1 si está
// activado. Sin EFI no se puede saber.
func (c *linuxCollector) secureBoot() string {
	matches, _ := filepath.Glob(filepath.Join(c.root, "sys/firmware/efi/efivars/SecureBoot-*"))
	if len(matches) == 0 {
		return protocol.StateUnknown
	}
	b, err := os.ReadFile(matches[0])
	if err != nil || len(b) == 0 {
		return protocol.StateUnknown
	}
	if b[len(b)-1] == 1 {
		return protocol.StateOn
	}
	return protocol.StateOff
}

// software prueba dpkg y, si no da resultado, rpm.
func (c *linuxCollector) software(ctx context.Context) ([]protocol.Software, error) {
	if out, err := c.run(ctx, "dpkg-query", "-W", "-f="+dpkgFormat); err == nil {
		if sw := parseDpkg(out); len(sw) > 0 {
			return sw, nil
		}
	}
	if out, err := c.run(ctx, "rpm", "-qa", "--qf", rpmFormat); err == nil {
		if sw := parseRPM(out); len(sw) > 0 {
			return sw, nil
		}
	}
	return nil, errors.New("no se pudo consultar dpkg ni rpm")
}
