package agentapi

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"mdm/internal/protocol"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

func (a *api) inventory(w http.ResponseWriter, r *http.Request) {
	bad := func(msg string) { httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, msg) }
	tooLarge := func() {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, httpx.CodeTooLarge, "El inventario es demasiado grande.")
	}
	if r.Header.Get("Content-Encoding") != "gzip" {
		bad("El inventario debe enviarse comprimido con gzip.")
		return
	}
	zr, err := gzip.NewReader(http.MaxBytesReader(w, r.Body, maxInventoryCompressed))
	if err != nil {
		if isTooLarge(err) {
			tooLarge()
			return
		}
		bad("El cuerpo no es gzip válido.")
		return
	}
	defer zr.Close()
	// Se lee un byte más del límite para distinguir «justo en el límite» de
	// «lo supera» sin descomprimir una bomba entera.
	raw, err := io.ReadAll(io.LimitReader(zr, maxInventoryDecompressed+1))
	if err != nil {
		if isTooLarge(err) {
			tooLarge()
			return
		}
		bad("El cuerpo no es gzip válido.")
		return
	}
	if len(raw) > maxInventoryDecompressed {
		tooLarge()
		return
	}
	var inv protocol.Inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		bad("El inventario no es JSON válido.")
		return
	}
	store.SanitizeInventory(&inv)

	dev := deviceFrom(r.Context())
	if err := a.d.Store.SaveInventory(r.Context(), dev.ID, inv, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := store.EnqueueRecomputeDevice(r.Context(), a.d.Store.Pool(), dev.ID, a.d.Now()); err != nil {
		slog.Error("encolar recálculo de grupos", "device", dev.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
