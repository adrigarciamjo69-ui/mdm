package inventory

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

// execRunner ejecuta órdenes reales con un tiempo límite y salida en inglés,
// para que los analizadores no dependan del idioma del sistema. Una orden que
// termina con error pero produce salida (por ejemplo «firewall-cmd --state»
// cuando el servicio está parado) devuelve esa salida.
// ExecRunner es execRunner para otros paquetes del agente.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execRunner(ctx, name, args...)
}

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(out) > 0 {
		return out, nil
	}
	return out, err
}
