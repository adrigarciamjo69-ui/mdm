package auth

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

var errBadCredential = errors.New("credencial de dispositivo mal formada")

// FormatDeviceCredential une identificador y secreto en la credencial que guarda el agente.
func FormatDeviceCredential(deviceID, secret string) string {
	return deviceID + "." + secret
}

// ParseDeviceCredential extrae identificador y secreto del valor completo de
// la cabecera Authorization ("Bearer <id>.<secreto>").
func ParseDeviceCredential(header string) (uuid.UUID, string, error) {
	rest, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return uuid.Nil, "", errBadCredential
	}
	idPart, secret, ok := strings.Cut(rest, ".")
	if !ok || secret == "" {
		return uuid.Nil, "", errBadCredential
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return uuid.Nil, "", errBadCredential
	}
	return id, secret, nil
}
