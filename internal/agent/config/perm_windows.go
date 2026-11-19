//go:build windows

package config

import (
	"fmt"
	"os/exec"
)

// restrict quita los permisos heredados del directorio y lo deja accesible
// solo para SYSTEM (S-1-5-18) y Administradores (S-1-5-32-544). Se usan los
// identificadores y no los nombres porque estos cambian con el idioma del sistema.
var restrict = func(dir string) error {
	out, err := exec.Command("icacls", dir, "/inheritance:r", "/grant:r",
		"*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls: %w: %s", err, out)
	}
	return nil
}
