//go:build !windows

package main

import (
	"io"
	"os"
)

// logOutput devuelve el destino del registro del servicio: la salida estándar,
// que systemd recoge en journald.
func logOutput(string) io.Writer { return os.Stdout }
