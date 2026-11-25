package tempadmin

import (
	"context"
	"os/exec"
	"time"
)

// ExecRunner ejecuta la orden con un tiempo límite y devuelve su salida
// combinada. A diferencia del ejecutor del inventario, un código de salida
// distinto de cero es siempre un error: aquí importa si se cambió el grupo.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
