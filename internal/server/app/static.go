package app

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// console sirve la consola. Los recursos con huella en el nombre (assets/) se
// guardan en caché indefinidamente; cualquier ruta que no sea un fichero
// devuelve index.html, porque la navegación la resuelve la propia consola.
func console(static fs.FS) http.HandlerFunc {
	files := http.FileServer(http.FS(static))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Método no permitido.", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(static, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			// Un recurso que falta es un 404 de verdad: devolver la página en
			// su lugar haría que el navegador intentase ejecutar HTML como script.
			if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.Error(w, "La consola no está incluida en esta compilación. Ejecuta «make web» y vuelve a compilar el servidor.", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	}
}
