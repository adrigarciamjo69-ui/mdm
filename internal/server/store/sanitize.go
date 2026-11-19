package store

import (
	"strings"
	"unicode/utf8"

	"mdm/internal/protocol"
)

// MaxStringLen es la longitud máxima, en caracteres, de cualquier cadena que
// llega de un agente.
const MaxStringLen = 512

// CleanString deja una cadena recibida de un agente en condiciones de
// guardarse: elimina los caracteres nulos (PostgreSQL los rechaza), sustituye
// las secuencias que no son UTF-8 válido y recorta a MaxStringLen caracteres.
// El registro de Windows contiene a veces valores corruptos; es preferible
// guardar el dato saneado que rechazar el inventario entero.
func CleanString(s string) string { return CleanText(s, MaxStringLen) }

// CleanText sanea como CleanString pero con un límite de maxRunes caracteres.
func CleanText(s string, maxRunes int) string {
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	if len(s) > maxRunes { // menos bytes que el límite implica menos caracteres
		n := 0
		for i := range s {
			if n == maxRunes {
				return s[:i]
			}
			n++
		}
	}
	return s
}

var knownSections = map[string]bool{
	protocol.SectionSystem: true, protocol.SectionHardware: true, protocol.SectionDisks: true,
	protocol.SectionNetwork: true, protocol.SectionSecurity: true, protocol.SectionSoftware: true,
}

func cleanState(s string) string {
	switch s {
	case protocol.StateOn, protocol.StateOff, protocol.StatePartial:
		return s
	}
	return protocol.StateUnknown
}

func cleanAll(fields ...*string) {
	for _, f := range fields {
		*f = CleanString(*f)
	}
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = CleanString(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// SanitizeInventory deja el inventario de un agente en condiciones de
// guardarse: sanea todas las cadenas, descarta el software sin nombre,
// normaliza los estados de seguridad y las secciones no disponibles a valores
// conocidos, y convierte las listas nulas en vacías.
func SanitizeInventory(inv *protocol.Inventory) {
	inv.Hash = CleanString(inv.Hash)

	unavailable := make([]string, 0, len(inv.Unavailable))
	seen := map[string]bool{}
	for _, s := range inv.Unavailable {
		if knownSections[s] && !seen[s] {
			seen[s] = true
			unavailable = append(unavailable, s)
		}
	}
	inv.Unavailable = unavailable

	s := &inv.System
	cleanAll(&s.Hostname, &s.OSName, &s.OSVersion, &s.Kernel, &s.Arch, &s.BootTime, &s.Timezone, &s.Domain, &s.LoggedInUser)
	h := &inv.Hardware
	cleanAll(&h.Manufacturer, &h.Model, &h.Serial, &h.BIOSVendor, &h.BIOSVersion, &h.BIOSDate, &h.CPUModel)

	if inv.Disks == nil {
		inv.Disks = []protocol.Disk{}
	}
	for i := range inv.Disks {
		d := &inv.Disks[i]
		cleanAll(&d.Device, &d.Mount, &d.Filesystem)
	}
	if inv.Network == nil {
		inv.Network = []protocol.NetIf{}
	}
	for i := range inv.Network {
		n := &inv.Network[i]
		cleanAll(&n.Name, &n.MAC)
		n.IPv4, n.IPv6 = cleanList(n.IPv4), cleanList(n.IPv6)
	}

	sec := &inv.Security
	sec.DiskEncryption, sec.Firewall = cleanState(sec.DiskEncryption), cleanState(sec.Firewall)
	sec.Antivirus, sec.SecureBoot = cleanState(sec.Antivirus), cleanState(sec.SecureBoot)
	sec.AntivirusProduct = CleanString(sec.AntivirusProduct)

	software := make([]protocol.Software, 0, len(inv.Software))
	for _, sw := range inv.Software {
		cleanAll(&sw.Name, &sw.Version, &sw.Publisher)
		if strings.TrimSpace(sw.Name) == "" {
			continue
		}
		software = append(software, sw)
	}
	inv.Software = software
}
