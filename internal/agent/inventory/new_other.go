//go:build !linux && !windows

package inventory

// New devuelve un recolector de mejor esfuerzo en plataformas no admitidas,
// para que el agente compile en el equipo de desarrollo.
func New() Collector { return newLinux("/", execRunner) }
