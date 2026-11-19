// Package alerts evalúa las reglas de alerta, abre y resuelve alertas y
// prepara los avisos (agrupados si son muchos).
package alerts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Tipos de regla.
const (
	TypeDeviceOffline    = "device.offline"
	TypeDiskLow          = "disk.low"
	TypeNoncompliant     = "compliance.noncompliant"
	TypeAndroidIntegrity = "android.integrity"
	TypeSecurityPending  = "patches.security_pending"
	TypeBitlockerMissing = "bitlocker.key_missing"
	TypeDeploymentFailed = "deployment.failed"
	TypeCommandFailed    = "command.failed"
	TypeDeviceEnrolled   = "device.enrolled"
	TypeLoginFailures    = "console.login_failures"
)

// Kind dice si una regla es de estado (se evalúa periódicamente y se resuelve
// sola) o de suceso.
func Kind(t string) string {
	switch t {
	case TypeDeviceOffline, TypeDiskLow, TypeNoncompliant, TypeAndroidIntegrity, TypeSecurityPending, TypeBitlockerMissing:
		return "state"
	case TypeDeploymentFailed, TypeCommandFailed, TypeDeviceEnrolled, TypeLoginFailures:
		return "event"
	}
	return ""
}

// EventTypes son los tipos de regla de suceso.
var EventTypes = []string{TypeDeploymentFailed, TypeCommandFailed, TypeDeviceEnrolled, TypeLoginFailures}

type intParam struct {
	name     string
	min, max int
}

var intParams = map[string]intParam{
	TypeDeviceOffline:   {"hours", 1, 720},
	TypeDiskLow:         {"percent", 1, 50},
	TypeSecurityPending: {"days", 1, 90},
	TypeLoginFailures:   {"count", 3, 100},
}

var optionalID = map[string]string{TypeNoncompliant: "policy_id", TypeDeploymentFailed: "deployment_id", TypeCommandFailed: "script_id"}

// Validate comprueba los parámetros de un tipo y los devuelve normalizados.
func Validate(typ string, raw json.RawMessage) (json.RawMessage, error) {
	if Kind(typ) == "" {
		return nil, errors.New("Tipo de regla desconocido.")
	}
	in := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&in); err != nil {
			return nil, errors.New("Los parámetros deben ser un objeto JSON.")
		}
	}
	out := map[string]any{}
	if p, ok := intParams[typ]; ok {
		n, ok := in[p.name].(json.Number)
		v, err := n.Int64()
		if !ok || err != nil || int(v) < p.min || int(v) > p.max {
			return nil, fmt.Errorf("«%s» debe ser un número entre %d y %d.", p.name, p.min, p.max)
		}
		out[p.name] = v
		delete(in, p.name)
	}
	if name, ok := optionalID[typ]; ok {
		if v, present := in[name]; present && v != nil && v != "" {
			s, _ := v.(string)
			if _, err := uuid.Parse(s); err != nil {
				return nil, fmt.Errorf("«%s» no es un identificador válido.", name)
			}
			out[name] = s
		}
		delete(in, name)
	}
	for k := range in {
		return nil, fmt.Errorf("Esta regla no admite el parámetro «%s».", k)
	}
	b, _ := json.Marshal(out)
	return b, nil
}
