// Package schedule calcula cuándo toca ejecutar algo programado (informes,
// automatizaciones) en la zona horaria de las programaciones.
package schedule

import (
	"errors"
	"fmt"
	"regexp"
	"time"
	_ "time/tzdata" // el contenedor corre en UTC y sin base de zonas
)

const (
	Hourly  = "hourly"
	Daily   = "daily"
	Weekly  = "weekly"
	Monthly = "monthly"
)

// Spec es una programación. En Hourly, Time indica el minuto ("00:15" = a y
// cuarto). Day es el día de la semana (0 = domingo) en Weekly y el del mes en
// Monthly (si el mes es más corto, el último día).
type Spec struct {
	Frequency string `json:"frequency"`
	Time      string `json:"time"`
	Day       int    `json:"day"`
}

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (s Spec) Validate() error {
	if !hhmm.MatchString(s.Time) {
		return errors.New("La hora debe tener el formato HH:MM.")
	}
	switch s.Frequency {
	case Hourly, Daily:
	case Weekly:
		if s.Day < 0 || s.Day > 6 {
			return errors.New("El día de la semana va de 0 (domingo) a 6 (sábado).")
		}
	case Monthly:
		if s.Day < 1 || s.Day > 31 {
			return errors.New("El día del mes va de 1 a 31.")
		}
	default:
		return errors.New("La frecuencia debe ser hourly, daily, weekly o monthly.")
	}
	return nil
}

func hm(s string) (int, int) {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h, m
}

func lastDay(y int, m time.Month, loc *time.Location) int {
	return time.Date(y, m+1, 0, 12, 0, 0, 0, loc).Day()
}

// Next devuelve la primera ejecución estrictamente posterior a after. Una
// hora que no existe (la noche del cambio de hora) se ejecuta justo después
// del salto, que es lo que hace time.Date.
func Next(s Spec, after time.Time, loc *time.Location) time.Time {
	a := after.In(loc)
	h, m := hm(s.Time)
	switch s.Frequency {
	case Hourly:
		t := time.Date(a.Year(), a.Month(), a.Day(), a.Hour(), m, 0, 0, loc)
		if !t.After(a) {
			t = t.Add(time.Hour)
		}
		return t
	case Weekly:
		for d := 0; d < 8; d++ {
			day := time.Date(a.Year(), a.Month(), a.Day()+d, h, m, 0, 0, loc)
			if int(day.Weekday()) == s.Day && day.After(a) {
				return day
			}
		}
	case Monthly:
		for k := 0; k < 3; k++ {
			y, mon := a.Year(), a.Month()+time.Month(k)
			first := time.Date(y, mon, 1, 12, 0, 0, 0, loc)
			day := min(s.Day, lastDay(first.Year(), first.Month(), loc))
			t := time.Date(first.Year(), first.Month(), day, h, m, 0, 0, loc)
			if t.After(a) {
				return t
			}
		}
	}
	// Daily (y por defecto).
	t := time.Date(a.Year(), a.Month(), a.Day(), h, m, 0, 0, loc)
	if !t.After(a) {
		t = time.Date(a.Year(), a.Month(), a.Day()+1, h, m, 0, 0, loc)
	}
	return t
}

// Due informa de si toca ejecutar: la siguiente ejecución tras la última ya
// ha llegado. Si el servidor estuvo parado y se perdieron varias, toca una
// sola vez (la última ejecución pasa a ser «ahora»).
func Due(s Spec, last *time.Time, now time.Time, loc *time.Location) bool {
	if last == nil {
		return true
	}
	return !Next(s, *last, loc).After(now)
}
