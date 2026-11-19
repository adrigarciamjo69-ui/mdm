// Package inventory recoge el inventario del equipo: sistema, hardware,
// discos, red, seguridad y software instalado.
package inventory

import (
	"context"

	"mdm/internal/protocol"
)

// Collector recoge la identidad y el inventario del equipo. Hay una
// implementación por plataforma; New devuelve la del sistema en curso.
type Collector interface {
	// Identify devuelve los datos de identidad para la inscripción. Deja
	// vacíos Token y AgentVersion, que rellena quien inscribe.
	Identify(ctx context.Context) protocol.EnrollRequest
	// Collect devuelve el inventario completo, con Hash ya calculado. Si una
	// sección no se puede recoger, su nombre figura en Unavailable y el resto
	// se entrega igualmente.
	Collect(ctx context.Context) protocol.Inventory
}

// finish deja el inventario listo para enviarse: listas nunca nulas y huella calculada.
func finish(inv *protocol.Inventory) {
	if inv.Unavailable == nil {
		inv.Unavailable = []string{}
	}
	if inv.Disks == nil {
		inv.Disks = []protocol.Disk{}
	}
	if inv.Network == nil {
		inv.Network = []protocol.NetIf{}
	}
	if inv.Software == nil {
		inv.Software = []protocol.Software{}
	}
	inv.Hash = protocol.InventoryHash(*inv)
}

// encryptionSummary resume el cifrado de los volúmenes: todos, ninguno o parte.
func encryptionSummary(disks []protocol.Disk) string {
	if len(disks) == 0 {
		return protocol.StateUnknown
	}
	enc := 0
	for _, d := range disks {
		if d.Encrypted {
			enc++
		}
	}
	switch enc {
	case len(disks):
		return protocol.StateOn
	case 0:
		return protocol.StateOff
	}
	return protocol.StatePartial
}
