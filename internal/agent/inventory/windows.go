//go:build windows

package inventory

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"

	"mdm/internal/protocol"
)

// Clases de WMI que se consultan. Los nombres de los campos deben coincidir
// con las propiedades de cada clase.
type win32OperatingSystem struct {
	Caption        string
	Version        string
	LastBootUpTime time.Time
}

type win32ComputerSystem struct {
	Manufacturer        string
	Model               string
	Domain              string
	UserName            string
	TotalPhysicalMemory uint64
	PartOfDomain        bool
}

type win32BIOS struct {
	SerialNumber      string
	Manufacturer      string
	SMBIOSBIOSVersion string
	ReleaseDate       *time.Time
}

// ChassisTypes es uint16[] en CIM, pero WMI lo entrega como enteros de 32
// bits con signo; con []uint16 la biblioteca entra en pánico.
type win32SystemEnclosure struct {
	ChassisTypes []int32
}

type win32ComputerSystemProduct struct {
	UUID string
}

type win32Processor struct {
	Name                      string
	NumberOfCores             uint32
	NumberOfLogicalProcessors uint32
}

type win32LogicalDisk struct {
	DeviceID   string
	FileSystem string
	Size       uint64
	FreeSpace  uint64
}

type win32EncryptableVolume struct {
	DriveLetter      string
	ProtectionStatus uint32
}

type antiVirusProduct struct {
	DisplayName  string
	ProductState uint32
}

const (
	nsBitLocker      = `root\CIMV2\Security\MicrosoftVolumeEncryption`
	nsSecurityCenter = `root\SecurityCenter2`

	keyUninstall    = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	keyUninstall32  = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
	keyFirewall     = `SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\`
	keySecureBoot   = `SYSTEM\CurrentControlSet\Control\SecureBoot\State`
	keyCryptography = `SOFTWARE\Microsoft\Cryptography`
)

type windowsCollector struct {
	wmi *wmi.Client
	now func() time.Time
}

// New devuelve el recolector de inventario de Windows.
func New() Collector {
	return &windowsCollector{
		// Las propiedades nulas o ausentes se dejan en su valor cero en lugar
		// de hacer fallar la consulta entera.
		wmi: &wmi.Client{NonePtrZero: true, PtrNil: true, AllowMissingFields: true},
		now: time.Now,
	}
}

// query consulta una clase de WMI en el espacio de nombres por defecto o en ns.
func (c *windowsCollector) query(q string, dst any, ns ...string) error {
	return safeCall(q, func() error {
		if len(ns) > 0 {
			return c.wmi.Query(q, dst, nil, ns[0])
		}
		return c.wmi.Query(q, dst)
	})
}

func regString(root registry.Key, path, name string) string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

func regDWORD(root registry.Key, path, name string) *uint32 {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return nil
	}
	u := uint32(v)
	return &u
}

func (c *windowsCollector) Identify(ctx context.Context) protocol.EnrollRequest {
	req := protocol.EnrollRequest{Platform: protocol.PlatformWindows, Arch: runtime.GOARCH, OSName: "Windows"}
	req.Hostname, _ = os.Hostname()

	var osRows []win32OperatingSystem
	if err := c.query("SELECT Caption, Version, LastBootUpTime FROM Win32_OperatingSystem", &osRows); err == nil && len(osRows) > 0 {
		req.OSName, req.OSVersion = windowsOSName(osRows[0].Caption), osRows[0].Version
	}
	var cs []win32ComputerSystem
	if err := c.query("SELECT Manufacturer, Model, Domain, UserName, TotalPhysicalMemory, PartOfDomain FROM Win32_ComputerSystem", &cs); err == nil && len(cs) > 0 {
		req.Manufacturer, req.Model = strings.TrimSpace(cs[0].Manufacturer), strings.TrimSpace(cs[0].Model)
	}
	var bios []win32BIOS
	if err := c.query("SELECT SerialNumber, Manufacturer, SMBIOSBIOSVersion, ReleaseDate FROM Win32_BIOS", &bios); err == nil && len(bios) > 0 {
		req.Serial = strings.TrimSpace(bios[0].SerialNumber)
	}
	smbios := ""
	var product []win32ComputerSystemProduct
	if err := c.query("SELECT UUID FROM Win32_ComputerSystemProduct", &product); err == nil && len(product) > 0 {
		smbios = product[0].UUID
	}
	req.HardwareID = chooseHardwareID(smbios, regString(registry.LOCAL_MACHINE, keyCryptography, "MachineGuid"))
	return req
}

func (c *windowsCollector) Collect(ctx context.Context) protocol.Inventory {
	inv := protocol.Inventory{CollectedAt: c.now().UTC()}
	fail := func(section string) { inv.Unavailable = append(inv.Unavailable, section) }

	if sys, ok := c.system(); ok {
		inv.System = sys
	} else {
		fail(protocol.SectionSystem)
	}
	if hw, ok := c.hardware(); ok {
		inv.Hardware = hw
	} else {
		fail(protocol.SectionHardware)
	}
	disks, bitlocker, ok := c.disks()
	if ok {
		inv.Disks = disks
	} else {
		fail(protocol.SectionDisks)
	}
	inv.Network = collectNetwork()
	inv.Security = c.security(bitlocker)
	if sw, ok := c.software(); ok {
		inv.Software = sw
	} else {
		fail(protocol.SectionSoftware)
	}
	finish(&inv)
	return inv
}

func (c *windowsCollector) system() (protocol.System, bool) {
	var osRows []win32OperatingSystem
	if err := c.query("SELECT Caption, Version, LastBootUpTime FROM Win32_OperatingSystem", &osRows); err != nil || len(osRows) == 0 {
		return protocol.System{}, false
	}
	o := osRows[0]
	s := protocol.System{OSName: windowsOSName(o.Caption), OSVersion: o.Version, Kernel: o.Version, Arch: runtime.GOARCH}
	s.Hostname, _ = os.Hostname()
	if !o.LastBootUpTime.IsZero() {
		s.BootTime = o.LastBootUpTime.UTC().Round(time.Second).Format(time.RFC3339)
	}
	s.Timezone, _ = c.now().Zone()
	var cs []win32ComputerSystem
	if err := c.query("SELECT Manufacturer, Model, Domain, UserName, TotalPhysicalMemory, PartOfDomain FROM Win32_ComputerSystem", &cs); err == nil && len(cs) > 0 {
		if cs[0].PartOfDomain {
			s.Domain = cs[0].Domain
		}
		s.LoggedInUser = cs[0].UserName
	}
	return s, true
}

func (c *windowsCollector) hardware() (protocol.Hardware, bool) {
	var cs []win32ComputerSystem
	if err := c.query("SELECT Manufacturer, Model, Domain, UserName, TotalPhysicalMemory, PartOfDomain FROM Win32_ComputerSystem", &cs); err != nil || len(cs) == 0 {
		return protocol.Hardware{}, false
	}
	var cpus []win32Processor
	if err := c.query("SELECT Name, NumberOfCores, NumberOfLogicalProcessors FROM Win32_Processor", &cpus); err != nil || len(cpus) == 0 {
		return protocol.Hardware{}, false
	}
	h := protocol.Hardware{
		Manufacturer: strings.TrimSpace(cs[0].Manufacturer), Model: strings.TrimSpace(cs[0].Model),
		MemoryBytes: cs[0].TotalPhysicalMemory, CPUModel: strings.TrimSpace(cpus[0].Name),
	}
	for _, p := range cpus { // un elemento por zócalo
		h.CPUCores += int(p.NumberOfCores)
		h.CPUThreads += int(p.NumberOfLogicalProcessors)
	}
	var bios []win32BIOS
	if err := c.query("SELECT SerialNumber, Manufacturer, SMBIOSBIOSVersion, ReleaseDate FROM Win32_BIOS", &bios); err == nil && len(bios) > 0 {
		h.Serial = strings.TrimSpace(bios[0].SerialNumber)
		h.BIOSVendor, h.BIOSVersion = strings.TrimSpace(bios[0].Manufacturer), strings.TrimSpace(bios[0].SMBIOSBIOSVersion)
		if bios[0].ReleaseDate != nil {
			h.BIOSDate = bios[0].ReleaseDate.Format("2006-01-02")
		}
	}
	var encl []win32SystemEnclosure
	h.Chassis = protocol.ChassisUnknown
	if err := c.query("SELECT ChassisTypes FROM Win32_SystemEnclosure", &encl); err == nil && len(encl) > 0 {
		h.Chassis = protocol.ChassisFromSMBIOS(chassisCodes(encl[0].ChassisTypes))
	}
	return h, true
}

// disks devuelve los volúmenes fijos. bitlocker lleva el ProtectionStatus de
// cada uno, o nil si BitLocker no se pudo consultar (por ejemplo, en
// ediciones de Windows que no lo incluyen).
func (c *windowsCollector) disks() (disks []protocol.Disk, bitlocker []uint32, ok bool) {
	var rows []win32LogicalDisk
	if err := c.query("SELECT DeviceID, FileSystem, Size, FreeSpace FROM Win32_LogicalDisk WHERE DriveType = 3", &rows); err != nil {
		return nil, nil, false
	}
	status := map[string]uint32{}
	var vols []win32EncryptableVolume
	bitlockerKnown := c.query("SELECT DriveLetter, ProtectionStatus FROM Win32_EncryptableVolume", &vols, nsBitLocker) == nil
	for _, v := range vols {
		status[strings.ToUpper(v.DriveLetter)] = v.ProtectionStatus
	}
	disks = []protocol.Disk{}
	for _, r := range rows {
		st, found := status[strings.ToUpper(r.DeviceID)]
		disks = append(disks, protocol.Disk{Device: r.DeviceID, Mount: r.DeviceID, Filesystem: r.FileSystem,
			SizeBytes: r.Size, FreeBytes: r.FreeSpace, Encrypted: found && st == 1})
		if bitlockerKnown {
			bitlocker = append(bitlocker, st) // un volumen que BitLocker no lista cuenta como sin proteger
		}
	}
	return disks, bitlocker, true
}

func (c *windowsCollector) security(bitlocker []uint32) protocol.Security {
	sec := protocol.Security{
		DiskEncryption: bitlockerState(bitlocker),
		Firewall: firewallFromProfiles(
			regDWORD(registry.LOCAL_MACHINE, keyFirewall+"DomainProfile", "EnableFirewall"),
			regDWORD(registry.LOCAL_MACHINE, keyFirewall+"StandardProfile", "EnableFirewall"),
			regDWORD(registry.LOCAL_MACHINE, keyFirewall+"PublicProfile", "EnableFirewall")),
		Antivirus:  protocol.StateUnknown,
		SecureBoot: protocol.StateUnknown,
	}
	// SecurityCenter2 no existe en Windows Server: el antivirus queda como desconocido.
	var av []antiVirusProduct
	if err := c.query("SELECT DisplayName, ProductState FROM AntiVirusProduct", &av, nsSecurityCenter); err == nil {
		products := make([]avProduct, 0, len(av))
		for _, p := range av {
			products = append(products, avProduct{DisplayName: p.DisplayName, ProductState: p.ProductState})
		}
		sec.Antivirus, sec.AntivirusProduct = antivirusFromProducts(products)
	}
	// La clave no existe en equipos con BIOS heredada.
	if v := regDWORD(registry.LOCAL_MACHINE, keySecureBoot, "UEFISecureBootEnabled"); v != nil {
		if *v == 1 {
			sec.SecureBoot = protocol.StateOn
		} else {
			sec.SecureBoot = protocol.StateOff
		}
	}
	return sec
}

// software lee las claves de desinstalación de 64 y 32 bits. Los programas
// instalados solo para un usuario (HKCU) no aparecen: el agente corre como SYSTEM.
func (c *windowsCollector) software() ([]protocol.Software, bool) {
	var entries []uninstallEntry
	read := 0
	for _, path := range []string{keyUninstall, keyUninstall32} {
		es, err := readUninstall(path)
		if err != nil {
			continue
		}
		read++
		entries = append(entries, es...)
	}
	if read == 0 {
		return nil, false
	}
	return softwareFromUninstall(entries), true
}

func readUninstall(path string) ([]uninstallEntry, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	out := make([]uninstallEntry, 0, len(names))
	for _, name := range names {
		sub, err := registry.OpenKey(k, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		var e uninstallEntry
		e.DisplayName, _, _ = sub.GetStringValue("DisplayName")
		e.DisplayVersion, _, _ = sub.GetStringValue("DisplayVersion")
		e.Publisher, _, _ = sub.GetStringValue("Publisher")
		e.InstallDate, _, _ = sub.GetStringValue("InstallDate")
		e.ParentKeyName, _, _ = sub.GetStringValue("ParentKeyName")
		e.SystemComponent, _, _ = sub.GetIntegerValue("SystemComponent")
		sub.Close()
		out = append(out, e)
	}
	return out, nil
}
