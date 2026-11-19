package inventory

import (
	"regexp"
	"strings"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// genericUUIDs son valores de SMBIOS que algunos fabricantes dejan sin
// personalizar y que por tanto comparten muchos equipos.
var genericUUIDs = map[string]bool{
	"00000000-0000-0000-0000-000000000000": true,
	"ffffffff-ffff-ffff-ffff-ffffffffffff": true,
	"03000200-0400-0500-0006-000700080009": true,
}

// chooseHardwareID devuelve el UUID de SMBIOS en minúsculas si es un UUID real
// y no uno genérico de fábrica; en otro caso, el identificador alternativo del
// sistema operativo.
func chooseHardwareID(smbios, fallback string) string {
	id := strings.ToLower(strings.TrimSpace(smbios))
	if uuidRe.MatchString(id) && !genericUUIDs[id] {
		return id
	}
	return strings.ToLower(strings.TrimSpace(fallback))
}
