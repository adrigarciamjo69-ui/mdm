package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// downloads sirve los binarios del agente y los scripts de instalación. Solo
// entrega ficheros que estén directamente en dir: sin subdirectorios, sin
// listados y sin rutas que salgan de él.
func downloads(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/downloads/")
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, full)
	}
}
