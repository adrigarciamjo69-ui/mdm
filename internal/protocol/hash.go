package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// InventoryHash devuelve la huella del inventario. Ignora los campos que
// cambian con el simple paso del tiempo o el uso normal del equipo (fecha de
// recogida, hora de arranque, espacio libre) y no depende del orden de las
// listas, para que el agente solo reenvíe cuando algo cambia de verdad.
func InventoryHash(inv Inventory) string {
	c := inv
	c.CollectedAt = time.Time{}
	c.Hash = ""
	c.System.BootTime = ""

	c.Disks = append([]Disk(nil), inv.Disks...)
	for i := range c.Disks {
		c.Disks[i].FreeBytes = 0
	}
	sort.Slice(c.Disks, func(i, j int) bool { return c.Disks[i].Device < c.Disks[j].Device })

	c.Network = append([]NetIf(nil), inv.Network...)
	sort.Slice(c.Network, func(i, j int) bool { return c.Network[i].Name < c.Network[j].Name })

	c.Software = append([]Software(nil), inv.Software...)
	sort.Slice(c.Software, func(i, j int) bool {
		if c.Software[i].Name != c.Software[j].Name {
			return c.Software[i].Name < c.Software[j].Name
		}
		return c.Software[i].Version < c.Software[j].Version
	})

	c.Unavailable = append([]string(nil), inv.Unavailable...)
	sort.Strings(c.Unavailable)

	b, _ := json.Marshal(c) // los tipos del inventario siempre son serializables
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
