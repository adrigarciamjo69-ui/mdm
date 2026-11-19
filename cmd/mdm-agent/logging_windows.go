//go:build windows

package main

import (
	"io"
	"path/filepath"

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
