// Package patches decide qué actualizaciones del sistema puede instalar cada
// equipo. Es lógica pura.
package patches

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// Missing es una actualización que le falta a un equipo.
type Missing struct {
	ID             string
	KB             string
	Classification string
	Security       bool
	PublishedAt    *time.Time
	FirstSeenAt    time.Time
}

// Policy son los campos de la política de parches que influyen en la aprobación.
type Policy struct {
	Platform    string
	AutoApprove []string
	DeferDays   int
	ExcludedKBs []string
	LinuxScope  string
}

// Approved devuelve, ordenados, los identificadores aprobados para instalar.
//
// Windows: una KB excluida nunca se aprueba; una rechazada a mano tampoco;
// una aprobada a mano sí, y si no hay decisión, decide la clasificación. En
// todos los casos se espera defer_days desde la publicación (o desde que se
// vio por primera vez, si no hay fecha). Linux: decide linux_scope
// (seguridad o todo) con el mismo aplazamiento; no hay decisiones por paquete.
func Approved(missing []Missing, pol Policy, decisions map[string]string, now time.Time) []string {
	excluded := map[string]bool{}
	for _, kb := range pol.ExcludedKBs {
		excluded[strings.ToUpper(strings.TrimSpace(kb))] = true
	}
	out := []string{}
	for _, m := range missing {
		since := m.FirstSeenAt
		if m.PublishedAt != nil {
			since = *m.PublishedAt
		}
		if now.Before(since.Add(time.Duration(pol.DeferDays) * 24 * time.Hour)) {
			continue
		}
		if pol.Platform == "linux" {
			if pol.LinuxScope == "all" || m.Security {
				out = append(out, m.ID)
			}
			continue
		}
		if m.KB != "" && excluded[strings.ToUpper(m.KB)] {
			continue
		}
		switch decisions[m.ID] {
		case "declined":
			continue
		case "approved":
			out = append(out, m.ID)
			continue
		}
		if slices.Contains(pol.AutoApprove, m.Classification) {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out
}
