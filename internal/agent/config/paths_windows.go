//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// Dir es el directorio de configuración del agente.
func Dir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "MDMAgent")
}
