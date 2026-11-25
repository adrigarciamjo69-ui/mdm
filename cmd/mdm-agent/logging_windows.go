//go:build windows

package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"mdm/internal/agent/config"
)

// logOutput devuelve el destino del registro del servicio: un fichero con
// rotación a 5 MB y 3 copias junto a la configuración.
func logOutput(configDir string) io.Writer {
	if configDir == "" {
		configDir = config.Dir()
	}
	return newRotatingWriter(filepath.Join(configDir, "agent.log"), 5<<20, 3)
}

// crashLog guarda en agent-crash.log el detalle de un fallo grave: como
// servicio, la salida de error no la ve nadie.
func crashLog(configDir string) {
	if configDir == "" {
		configDir = config.Dir()
	}
	f, err := os.OpenFile(filepath.Join(configDir, "agent-crash.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	_ = f.Close() // SetCrashOutput conserva su propia copia
}
