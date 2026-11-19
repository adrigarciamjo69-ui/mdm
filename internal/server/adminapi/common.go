package adminapi

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/server/httpx"
)

func badRequest(w http.ResponseWriter, msg string) {
	httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, msg)
}

func notFound(w http.ResponseWriter, msg string) {
	httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, msg)
}

func conflict(w http.ResponseWriter, msg string) {
	httpx.WriteError(w, http.StatusConflict, httpx.CodeConflict, msg)
}

// pathID lee un UUID de la ruta. Responde 400 y devuelve false si no lo es.
func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		badRequest(w, "El identificador no es válido.")
		return uuid.Nil, false
	}
	return id, true
}

// cleanName recorta espacios y comprueba que el nombre no esté vacío ni supere max caracteres.
func cleanName(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, s != "" && utf8.RuneCountInString(s) <= max
}
