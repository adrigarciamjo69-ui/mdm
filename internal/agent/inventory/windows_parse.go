package inventory

import (
	"sort"
	"strings"
	"time"

	"mdm/internal/protocol"
)

// Este fichero reúne la lógica de interpretación del inventario de Windows.
// No lleva etiqueta de compilación para poder probarse en cualquier sistema;
// la lectura real de WMI y del registro está en windows.go.

// uninstallEntry es una clave de «Uninstall» del registro de Windows.
type uninstallEntry struct {
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallDate     string
	SystemComponent uint64
	ParentKeyName   string
}

// softwareFromUninstall convierte las claves de desinstalación en la lista de
// programas que vería un usuario en «Aplicaciones instaladas»: omite las
// entradas sin nombre, los componentes del sistema y las actualizaciones
// (las que cuelgan de otro programa), elimina duplicados entre las ramas de
// 64 y 32 bits y ordena por nombre.
func softwareFromUninstall(entries []uninstallEntry) []protocol.Software {
	out := []protocol.Software{}
	seen := map[string]bool{}
	for _, e := range entries {
		name := strings.TrimSpace(e.DisplayName)
		if name == "" || e.SystemComponent == 1 || strings.TrimSpace(e.ParentKeyName) != "" {
			continue
		}
		version := strings.TrimSpace(e.DisplayVersion)
		key := name + "\x00" + version
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, protocol.Software{
			Name: name, Version: version, Publisher: strings.TrimSpace(e.Publisher),
			InstalledAt: parseInstallDate(e.InstallDate),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// parseInstallDate interpreta la fecha de instalación del registro (AAAAMMDD).
func parseInstallDate(s string) *time.Time {
	t, err := time.Parse("20060102", strings.TrimSpace(s))
	if err != nil || t.Year() < 1990 {
		return nil
	}
	return &t
}

// bitlockerState resume el ProtectionStatus de BitLocker de cada volumen fijo
// (1 = protegido; 0 o 2 = sin protección o desconocido).
func bitlockerState(protectionStatus []uint32) string {
	if len(protectionStatus) == 0 {
		return protocol.StateUnknown
	}
	on := 0
	for _, s := range protectionStatus {
		if s == 1 {
			on++
		}
	}
	switch on {
	case len(protectionStatus):
		return protocol.StateOn
	case 0:
		return protocol.StateOff
	}
	return protocol.StatePartial
}

// firewallFromProfiles resume el valor EnableFirewall de los perfiles de
// dominio, privado y público. Un perfil nulo no se pudo leer y no cuenta.
func firewallFromProfiles(domain, private, public *uint32) string {
	known, on := 0, 0
	for _, p := range []*uint32{domain, private, public} {
		if p == nil {
			continue
		}
		known++
		if *p != 0 {
			on++
		}
	}
	switch {
	case known == 0:
		return protocol.StateUnknown
	case on == known:
		return protocol.StateOn
	case on == 0:
		return protocol.StateOff
	}
	return protocol.StatePartial
}

// avProduct es un antivirus registrado en el Centro de seguridad de Windows.
type avProduct struct {
	DisplayName  string
	ProductState uint32
}

// antivirusFromProducts indica si hay algún antivirus activado y cuál. En
// ProductState, el segundo cuarteto del byte central vale 1 cuando la
// protección en tiempo real está activada. Con varios productos basta uno
// activado; si ninguno lo está, se devuelve el primero.
func antivirusFromProducts(products []avProduct) (state, name string) {
	if len(products) == 0 {
		return protocol.StateUnknown, ""
	}
	for _, p := range products {
		if (p.ProductState>>12)&0xF == 1 {
			return protocol.StateOn, strings.TrimSpace(p.DisplayName)
		}
	}
	return protocol.StateOff, strings.TrimSpace(products[0].DisplayName)
}

// windowsOSName quita el prefijo «Microsoft» del nombre que da WMI.
func windowsOSName(caption string) string {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(caption), "Microsoft "))
	if name == "" {
		return "Windows"
	}
	return name
}
