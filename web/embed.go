// Package web incrusta la consola compilada (web/dist) en el binario del servidor.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist devuelve los ficheros de la consola con dist como raíz.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist siempre existe: lo garantiza go:embed al compilar
	}
	return sub
}
