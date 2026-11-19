// Package httpx reúne las utilidades HTTP que comparten las dos API.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

// Códigos de error de la API.
const (
	CodeInvalidRequest = "invalid_request"
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"
	CodeNotFound       = "not_found"
	CodeConflict       = "conflict"
	CodeTooLarge       = "too_large"
	CodeRateLimited    = "rate_limited"
	CodeUnavailable    = "unavailable"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError responde con el formato de error común. El mensaje va en español
// y listo para mostrarse.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// DecodeJSON lee un único objeto JSON de como mucho maxBytes y rechaza campos
// desconocidos. Si falla, responde con el error y devuelve false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if isTooLarge(err) {
			WriteError(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "La petición es demasiado grande.")
			return false
		}
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "El cuerpo de la petición no es JSON válido.")
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		if isTooLarge(err) {
			WriteError(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "La petición es demasiado grande.")
			return false
		}
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "El cuerpo de la petición no es JSON válido.")
		return false
	}
	return true
}

func isTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}

// ClientIP devuelve la IP del cliente. Con un proxy inverso de confianza
// delante, toma el último valor de X-Forwarded-For: es el que añade el propio
// proxy, mientras que los anteriores los puede falsificar el cliente.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			last := strings.TrimSpace(parts[len(parts)-1])
			if ip := net.ParseIP(last); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
