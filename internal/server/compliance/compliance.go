// Package compliance calcula si un equipo cumple: sus ajustes de política más
// las reglas que evalúa el servidor. Es lógica pura.
package compliance

import (
	"fmt"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/rules"
	"mdm/internal/server/settings"
)

const (
	Compliant    = "compliant"
	Noncompliant = "noncompliant"
	Unknown      = "unknown"
)

// reportTTL es el tiempo tras el cual un informe deja de valer.
const reportTTL = 24 * time.Hour

type DeviceInfo struct {
	Platform     string
	LastSeenAt   *time.Time
	AgentVersion string
	OSVersion    string
	// OldestSecurityUpdate es desde cuándo tiene pendiente la actualización de
	// seguridad aprobada más antigua (nil si ninguna).
	OldestSecurityUpdate *time.Time
	// Móviles: nivel de parche (AAAA-MM-DD) y veredicto de integridad de Google.
	SecurityPatchLevel string
	Integrity          string
}

type Input struct {
	Effective  protocol.EffectivePolicy
	ReportHash string
	ReportedAt *time.Time
	Results    []protocol.SettingResult
	Device     DeviceInfo
	Settings   settings.Values
	Now        time.Time
}

type Result struct {
	State string
	// Reasons son los motivos de las reglas del servidor. Los ajustes que no
	// se cumplen se ven en sus propios resultados.
	Reasons []string
}

// ServerReasons evalúa las reglas del servidor y devuelve los motivos de incumplimiento.
func ServerReasons(d DeviceInfo, v settings.Values, now time.Time) []string {
	var out []string
	if d.LastSeenAt != nil && v.MaxOfflineDays > 0 {
		days := int(now.Sub(*d.LastSeenAt).Hours() / 24)
		if days > v.MaxOfflineDays {
			out = append(out, fmt.Sprintf("Lleva %d días sin conectar (máximo %d).", days, v.MaxOfflineDays))
		}
	}
	if v.MinAgentVersion != "" && d.AgentVersion != "" && rules.CompareVersions(d.AgentVersion, v.MinAgentVersion) < 0 {
		out = append(out, fmt.Sprintf("La versión del agente (%s) es anterior a la mínima (%s).", d.AgentVersion, v.MinAgentVersion))
	}
	if min := v.MinOSVersion[d.Platform]; min != "" && d.OSVersion != "" && rules.CompareVersions(d.OSVersion, min) < 0 {
		out = append(out, fmt.Sprintf("La versión del sistema (%s) es anterior a la mínima (%s).", d.OSVersion, min))
	}
	if v.MinAndroidPatchLevel != "" && d.SecurityPatchLevel != "" && d.SecurityPatchLevel < v.MinAndroidPatchLevel {
		out = append(out, fmt.Sprintf("El parche de seguridad de Android (%s) es anterior al mínimo (%s).", d.SecurityPatchLevel, v.MinAndroidPatchLevel))
	}
	if d.Integrity == "POTENTIALLY_COMPROMISED" || d.Integrity == "AT_RISK" {
		out = append(out, "Google no confía en la integridad del dispositivo (posiblemente rooteado o modificado).")
	}
	if v.MaxSecurityUpdateAgeDays > 0 && d.OldestSecurityUpdate != nil {
		days := int(now.Sub(*d.OldestSecurityUpdate).Hours() / 24)
		if days > v.MaxSecurityUpdateAgeDays {
			out = append(out, fmt.Sprintf("Tiene actualizaciones de seguridad pendientes desde hace %d días (máximo %d).", days, v.MaxSecurityUpdateAgeDays))
		}
	}
	return out
}

// Compute calcula el estado. Las reglas del servidor hacen incumplir en todo
// caso; si se cumplen, decide el último informe del agente, que solo vale si
// es de la huella vigente, tiene menos de 24 horas y cubre todos los ajustes.
func Compute(in Input) Result {
	if reasons := ServerReasons(in.Device, in.Settings, in.Now); len(reasons) > 0 {
		return Result{State: Noncompliant, Reasons: reasons}
	}
	if len(in.Effective.Settings) == 0 || in.ReportedAt == nil || in.ReportHash != in.Effective.Hash ||
		in.Now.Sub(*in.ReportedAt) > reportTTL {
		return Result{State: Unknown}
	}
	type id struct{ policy, key string }
	got := map[id]protocol.SettingResult{}
	for _, r := range in.Results {
		got[id{r.PolicyID, r.Key}] = r
	}
	failing := false
	for _, s := range in.Effective.Settings {
		r, ok := got[id{s.PolicyID, s.Key}]
		if !ok {
			return Result{State: Unknown}
		}
		failing = failing || r.Status == protocol.StatusNoncompliant || r.Status == protocol.StatusError
	}
	if failing {
		return Result{State: Noncompliant}
	}
	return Result{State: Compliant}
}

// NextSince devuelve la fecha desde la que el equipo no cumple tras pasar del
// estado prev al next.
func NextSince(prev, next string, prevSince *time.Time, now time.Time) *time.Time {
	if next != Noncompliant {
		return nil
	}
	if prev == Noncompliant && prevSince != nil {
		return prevSince
	}
	return &now
}
