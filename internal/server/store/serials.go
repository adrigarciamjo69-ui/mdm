package store

import "strings"

// junkSerials son números de serie de relleno que ponen fabricantes y
// máquinas virtuales: no identifican a un equipo.
var junkSerials = map[string]bool{
	"TO BE FILLED BY O.E.M.": true, "DEFAULT STRING": true, "SYSTEM SERIAL NUMBER": true,
	"CHASSIS SERIAL NUMBER": true, "NOT SPECIFIED": true, "NONE": true, "N/A": true,
	"0123456789": true, "NOT APPLICABLE": true, "INVALID": true, "UNKNOWN": true,
}

// SerialKey normaliza un número de serie para unir activos con equipos.
// Devuelve "" si el número no sirve para identificar un equipo.
func SerialKey(serial string) string {
	k := strings.ToUpper(strings.TrimSpace(serial))
	if len(k) < 4 || junkSerials[k] || strings.HasPrefix(k, "VMWARE-") || strings.Contains(k, "PARALLELS") {
		return ""
	}
	if strings.Count(k, k[:1]) == len(k) {
		return ""
	}
	return k
}
