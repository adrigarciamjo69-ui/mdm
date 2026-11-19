// Package settings define los ajustes globales del servidor, con sus valores
// por defecto y su validación.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"mdm/internal/server/store"
)

// Values son los ajustes globales. Cada campo se guarda en una fila con la
// clave de su etiqueta JSON.
type Values struct {
	AgentAutoUpdate bool              `json:"agent_auto_update"`
	MaxOfflineDays  int               `json:"max_offline_days"`
	MinAgentVersion string            `json:"min_agent_version"`
	MinOSVersion    map[string]string `json:"min_os_version"`
	// MaxSecurityUpdateAgeDays: un equipo no cumple si tiene una actualización
	// de seguridad aprobada pendiente desde hace más días (0 = sin regla).
	MaxSecurityUpdateAgeDays int `json:"max_security_update_age_days"`
	// MinAndroidPatchLevel: un móvil con un parche de seguridad anterior
	// (AAAA-MM-DD) no cumple. Vacío = sin regla.
	MinAndroidPatchLevel string `json:"min_android_patch_level"`
	// ScheduleTimezone es la zona de los informes, automatizaciones e
	// instantáneas programados (las ventanas de parches usan la hora de cada equipo).
	ScheduleTimezone string `json:"schedule_timezone"`
}

// Location devuelve la zona de las programaciones (Europe/Madrid si no es válida).
func (v Values) Location() *time.Location {
	if l, err := time.LoadLocation(v.ScheduleTimezone); err == nil && v.ScheduleTimezone != "" {
		return l
	}
	l, _ := time.LoadLocation("Europe/Madrid")
	return l
}

// Platforms con versión mínima de sistema configurable.
var Platforms = []string{"windows", "linux"}

func Defaults() Values {
	return Values{AgentAutoUpdate: true, MaxOfflineDays: 14, MinOSVersion: map[string]string{"windows": "", "linux": ""}, MaxSecurityUpdateAgeDays: 30, ScheduleTimezone: "Europe/Madrid"}
}

var versionRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([-_+~][0-9A-Za-z.]+)*$`)

// ValidVersion admite cadenas vacías (sin regla) o versiones como 10.0.26100 o 1.0.0-rc1.
func ValidVersion(v string) bool { return v == "" || versionRe.MatchString(v) }

// Validate comprueba los rangos y formatos.
func (v Values) Validate() error {
	if v.MaxOfflineDays < 1 || v.MaxOfflineDays > 365 {
		return errors.New("Los días máximos sin conexión deben estar entre 1 y 365.")
	}
	if v.MaxSecurityUpdateAgeDays < 0 || v.MaxSecurityUpdateAgeDays > 365 {
		return errors.New("La antigüedad máxima de las actualizaciones de seguridad debe estar entre 0 (sin regla) y 365 días.")
	}
	if v.ScheduleTimezone != "" {
		if _, err := time.LoadLocation(v.ScheduleTimezone); err != nil || v.ScheduleTimezone == "Local" {
			return errors.New("La zona horaria no es válida (por ejemplo, Europe/Madrid o Atlantic/Canary).")
		}
	}
	if v.MinAndroidPatchLevel != "" {
		if _, err := time.Parse("2006-01-02", v.MinAndroidPatchLevel); err != nil {
			return errors.New("El nivel mínimo de parche de Android debe ser una fecha AAAA-MM-DD.")
		}
	}
	if !ValidVersion(v.MinAgentVersion) {
		return errors.New("La versión mínima del agente no tiene un formato válido (por ejemplo, 1.2.0).")
	}
	for p, ver := range v.MinOSVersion {
		known := false
		for _, k := range Platforms {
			known = known || k == p
		}
		if !known {
			return fmt.Errorf("Plataforma desconocida en la versión mínima de sistema: %s.", p)
		}
		if !ValidVersion(ver) {
			return fmt.Errorf("La versión mínima de %s no tiene un formato válido.", p)
		}
	}
	return nil
}

// Load devuelve los ajustes guardados fusionados con los valores por defecto.
func Load(ctx context.Context, s *store.Store) (Values, error) {
	raw, err := s.SettingsRaw(ctx)
	if err != nil {
		return Values{}, err
	}
	v := Defaults()
	merged, _ := json.Marshal(v)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(merged, &m)
	for k, val := range raw {
		if _, known := m[k]; known {
			m[k] = val
		}
	}
	b, _ := json.Marshal(m)
	if err := json.Unmarshal(b, &v); err != nil {
		return Values{}, fmt.Errorf("ajustes guardados ilegibles: %w", err)
	}
	for _, p := range Platforms {
		if _, ok := v.MinOSVersion[p]; !ok {
			v.MinOSVersion[p] = ""
		}
	}
	return v, nil
}

// Save valida y guarda todos los ajustes.
func Save(ctx context.Context, s *store.Store, v Values, by uuid.UUID, at time.Time) error {
	if err := v.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(v)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	return s.SaveSettingsRaw(ctx, m, by, at)
}
