//go:build !windows

package config

import "os"

// restrict deja el directorio accesible solo para su propietario (root).
var restrict = func(dir string) error { return os.Chmod(dir, 0o700) }
