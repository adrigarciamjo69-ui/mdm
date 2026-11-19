package inventory

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"mdm/internal/protocol"
)

// parseOSRelease extrae nombre y versión de /etc/os-release.
func parseOSRelease(b []byte) (name, version string) {
	name = "Linux"
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "NAME":
			if v != "" {
				name = v
			}
		case "VERSION_ID":
			version = v
		}
	}
	return name, version
}

// parseCPUInfo extrae modelo, núcleos físicos e hilos de /proc/cpuinfo. En
// ARM no hay «model name» ni identificadores de núcleo: se usa «Hardware» o
// «CPU part» y se cuenta un núcleo por procesador.
func parseCPUInfo(b []byte) (model string, cores, threads int) {
	var hardware, part, physical, core string
	seen := map[string]bool{}
	hasTopology := false
	flush := func() {
		if physical != "" || core != "" {
			hasTopology = true
			seen[physical+"/"+core] = true
		}
		physical, core = "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			if strings.TrimSpace(line) == "" {
				flush()
			}
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "processor":
			threads++
		case "model name":
			if model == "" {
				model = v
			}
		case "Hardware":
			hardware = v
		case "CPU part":
			if part == "" {
				part = v
			}
		case "physical id":
			physical = v
		case "core id":
			core = v
		}
	}
	flush()
	if model == "" {
		switch {
		case hardware != "":
			model = hardware
		case part != "":
			model = "ARM (CPU part " + part + ")"
		}
	}
	cores = threads
	if hasTopology {
		cores = len(seen)
	}
	return model, cores, threads
}

// parseMemTotal devuelve la memoria total en bytes a partir de /proc/meminfo.
func parseMemTotal(b []byte) uint64 {
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			return 0
		}
		kb, _ := strconv.ParseUint(f[0], 10, 64)
		return kb * 1024
	}
	return 0
}

// Formatos que se piden a dpkg-query y rpm: campos separados por tabulador.
const (
	dpkgFormat = `${Package}\t${Version}\t${Maintainer}\t${Status}\n`
	rpmFormat  = `%{NAME}\t%{VERSION}-%{RELEASE}\t%{VENDOR}\t%{INSTALLTIME}\n`
)

// parseDpkg lee la salida de dpkg-query y conserva solo los paquetes instalados.
func parseDpkg(b []byte) []protocol.Software {
	out := []protocol.Software{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[0] == "" || strings.TrimSpace(f[3]) != "install ok installed" {
			continue
		}
		publisher := f[2]
		if i := strings.Index(publisher, " <"); i >= 0 { // «Nombre <correo>»
			publisher = publisher[:i]
		}
		out = append(out, protocol.Software{Name: f[0], Version: f[1], Publisher: strings.TrimSpace(publisher)})
	}
	return out
}

// parseRPM lee la salida de rpm -qa.
func parseRPM(b []byte) []protocol.Software {
	out := []protocol.Software{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[0] == "" {
			continue
		}
		sw := protocol.Software{Name: f[0], Version: f[1], Publisher: f[2]}
		if sw.Publisher == "(none)" {
			sw.Publisher = ""
		}
		if secs, err := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64); err == nil && secs > 0 {
			t := time.Unix(secs, 0).UTC()
			sw.InstalledAt = &t
		}
		out = append(out, sw)
	}
	return out
}

type lsblkDevice struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Mountpoint  *string       `json:"mountpoint"`
	Mountpoints []*string     `json:"mountpoints"`
	Children    []lsblkDevice `json:"children"`
}

// parseLsblkCrypt lee «lsblk -J» y devuelve, por punto de montaje, si el
// volumen está sobre un dispositivo cifrado (tipo crypt en algún nivel
// superior). Admite tanto «mountpoints» como el «mountpoint» de versiones antiguas.
func parseLsblkCrypt(b []byte) (map[string]bool, error) {
	var doc struct {
		Blockdevices []lsblkDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	var walk func(d lsblkDevice, encrypted bool)
	walk = func(d lsblkDevice, encrypted bool) {
		encrypted = encrypted || d.Type == "crypt"
		if d.Mountpoint != nil && *d.Mountpoint != "" {
			out[*d.Mountpoint] = encrypted
		}
		for _, m := range d.Mountpoints {
			if m != nil && *m != "" {
				out[*m] = encrypted
			}
		}
		for _, c := range d.Children {
			walk(c, encrypted)
		}
	}
	for _, d := range doc.Blockdevices {
		walk(d, false)
	}
	return out, nil
}

type mount struct {
	Device, Mount, FS string
}

// pseudoFS son sistemas de ficheros que no corresponden a almacenamiento real.
var pseudoFS = map[string]bool{
	"tmpfs": true, "proc": true, "sysfs": true, "devtmpfs": true, "overlay": true, "squashfs": true,
	"cgroup": true, "cgroup2": true, "devpts": true, "securityfs": true, "debugfs": true, "tracefs": true,
	"pstore": true, "bpf": true, "autofs": true, "mqueue": true, "hugetlbfs": true, "configfs": true,
	"fusectl": true, "efivarfs": true, "ramfs": true, "nsfs": true, "fuse.snapfuse": true, "iso9660": true,
}

// unescapeMount deshace los escapes octales de /proc/mounts (\040 es un espacio).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				sb.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// parseMounts devuelve los montajes de almacenamiento real de /proc/mounts,
// uno por dispositivo (el primero que aparece).
func parseMounts(b []byte) []mount {
	out := []mount{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		dev, mnt, fsType := f[0], unescapeMount(f[1]), f[2]
		if pseudoFS[fsType] || !strings.HasPrefix(dev, "/dev/") || strings.HasPrefix(dev, "/dev/loop") || seen[dev] {
			continue
		}
		seen[dev] = true
		out = append(out, mount{Device: dev, Mount: mnt, FS: fsType})
	}
	return out
}

// firewallState interpreta la salida de «ufw status», «firewall-cmd --state» y
// «nft list ruleset». Un argumento nulo significa que la herramienta no está.
// Basta que una indique reglas activas; si hay herramientas pero ninguna
// activa, el cortafuegos está apagado; sin herramientas no se puede saber.
func firewallState(ufw, firewalld, nft []byte) string {
	if ufw == nil && firewalld == nil && nft == nil {
		return protocol.StateUnknown
	}
	if strings.Contains(string(ufw), "Status: active") ||
		strings.TrimSpace(string(firewalld)) == "running" ||
		strings.Contains(string(nft), "chain ") {
		return protocol.StateOn
	}
	return protocol.StateOff
}

// CryptMounts devuelve, por punto de montaje, si el volumen está cifrado,
// a partir de la salida de «lsblk -J». Lo usan también los ajustes de política.
func CryptMounts(b []byte) (map[string]bool, error) { return parseLsblkCrypt(b) }
