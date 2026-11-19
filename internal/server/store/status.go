package store

import "time"

type Status string

const (
	StatusOnline   Status = "online"
	StatusOffline  Status = "offline"
	StatusInactive Status = "inactive"
	StatusRevoked  Status = "revoked"
)

// Statuses lista todos los estados posibles.
var Statuses = []Status{StatusOnline, StatusOffline, StatusInactive, StatusRevoked}

const (
	// onlineIntervals es el número de intervalos de check-in sin noticias a
	// partir del cual un equipo deja de considerarse en línea.
	onlineIntervals = 3
	inactiveAfter   = 30 * 24 * time.Hour
	// mobileOnline: un móvil informa con mucha menos frecuencia que un PC.
	mobileOnline = 24 * time.Hour
	staleAfter   = 7 * 24 * time.Hour
	lowDiskRatio = 0.10
)

// thresholds traduce «ahora» a las fechas de corte que usan tanto StatusOf
// como los filtros SQL, para que ambos coincidan siempre.
type thresholds struct {
	online, mobileOnline, inactive, stale time.Time
}

func cutoffs(now time.Time, checkinIntervalS int) thresholds {
	return thresholds{
		online:       now.Add(-time.Duration(onlineIntervals*checkinIntervalS) * time.Second),
		mobileOnline: now.Add(-mobileOnline),
		inactive:     now.Add(-inactiveAfter),
		stale:        now.Add(-staleAfter),
	}
}

// StatusOf calcula el estado del dispositivo. No se almacena: así nunca queda
// desfasado ni hace falta un proceso que marque equipos como desconectados.
func StatusOf(d Device, now time.Time, checkinIntervalS int) Status {
	if d.RevokedAt != nil {
		return StatusRevoked
	}
	if d.LastSeenAt == nil {
		return StatusOffline
	}
	c := cutoffs(now, checkinIntervalS)
	online := c.online
	if d.Platform == "android" {
		online = c.mobileOnline
	}
	switch {
	case d.LastSeenAt.After(online):
		return StatusOnline
	case !d.LastSeenAt.After(c.inactive):
		return StatusInactive
	}
	return StatusOffline
}

// onlineCutoffSQL devuelve la fecha de corte de «en línea» según la plataforma
// de la fila, para que el SQL coincida con StatusOf.
func onlineCutoffSQL(pc, mobile string) string {
	return "(CASE WHEN d.platform = 'android' THEN " + mobile + "::timestamptz ELSE " + pc + "::timestamptz END)"
}
