package inventory

import "fmt"

// safeCall ejecuta una consulta y convierte un pánico en error. La biblioteca
// de WMI entra en pánico con algunos tipos de matriz (por ejemplo los
// ChassisTypes de Win32_SystemEnclosure, que Windows entrega como enteros con
// signo): un dato de inventario nunca debe tumbar el agente.
func safeCall(what string, f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("consulta %q: %v", what, r)
		}
	}()
	return f()
}

// chassisCodes pasa los códigos de chasis de WMI a enteros.
func chassisCodes(raw []int32) []int {
	out := make([]int, 0, len(raw))
	for _, t := range raw {
		out = append(out, int(t))
	}
	return out
}
