package agentapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mdm/internal/server/httpx"
)

// downloadTimeout es el plazo de escritura de una descarga: una línea lenta
// puede tardar mucho con un instalador de varios GB.
const downloadTimeout = 3 * time.Hour

// downloadFile sirve un instalador al equipo, con reanudación (Range).
func (a *api) downloadFile(w http.ResponseWriter, r *http.Request) {
	if a.d.Files == nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Este fichero no existe.")
		return
	}
	sha := chi.URLParam(r, "sha256")
	// Solo los instaladores de los despliegues que aplican a este equipo: una
	// credencial robada no da acceso a todo el catálogo.
	eff, _, err := a.d.Store.EffectivePolicyFor(r.Context(), deviceFrom(r.Context()).ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	allowed := false
	for _, app := range eff.Apps {
		allowed = allowed || app.FileSHA256 == sha
	}
	if !allowed {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Este fichero no existe.")
		return
	}
	f, info, err := a.d.Files.Open(sha)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Este fichero no existe.")
		return
	}
	defer f.Close()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(downloadTimeout))
	w.Header().Set("ETag", `"`+sha+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
