package agentapi

import (
	"compress/gzip"
	"io"
	"net/http"

	"mdm/internal/protocol"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const maxUpdatesBody = 1 << 20

func cleanUpdate(u *protocol.UpdateInfo) {
	u.ID = store.CleanText(u.ID, 200)
	u.KB = store.CleanText(u.KB, 50)
	u.Title = store.CleanText(u.Title, 500)
	u.Classification = store.CleanText(u.Classification, 50)
	u.Severity = store.CleanText(u.Severity, 50)
	if u.Classification == "" {
		u.Classification = "other"
	}
}

// postUpdates recibe el resultado de una búsqueda de actualizaciones.
func (a *api) postUpdates(w http.ResponseWriter, r *http.Request) {
	var scan protocol.UpdateScan
	limit := int64(maxUpdatesBody)
	// El agente la envía comprimida con gzip, como el inventario.
	if r.Header.Get("Content-Encoding") == "gzip" {
		limit = 8 * maxUpdatesBody
		zr, err := gzip.NewReader(http.MaxBytesReader(w, r.Body, maxUpdatesBody))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "El cuerpo no es gzip válido.")
			return
		}
		r.Body = io.NopCloser(io.LimitReader(zr, 8*maxUpdatesBody))
		r.Header.Del("Content-Encoding")
	}
	if !httpx.DecodeJSON(w, r, limit, &scan) {
		return
	}
	valid := scan.Updates[:0]
	for _, u := range scan.Updates {
		cleanUpdate(&u)
		if u.ID != "" {
			valid = append(valid, u)
		}
	}
	scan.Updates = valid
	dev := deviceFrom(r.Context())
	if err := a.d.Store.SaveUpdateScan(r.Context(), dev.ID, dev.Platform, scan, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// postUpdateResults recibe el resultado de una instalación de actualizaciones.
func (a *api) postUpdateResults(w http.ResponseWriter, r *http.Request) {
	var res protocol.UpdateResults
	if !httpx.DecodeJSON(w, r, maxUpdatesBody, &res) {
		return
	}
	for _, x := range res.Results {
		if x.Status != "installed" && x.Status != "failed" {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "El resultado de cada actualización debe ser installed o failed.")
			return
		}
	}
	if err := a.d.Store.SaveUpdateResults(r.Context(), deviceFrom(r.Context()).ID, res, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
