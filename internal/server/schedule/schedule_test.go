package schedule

import (
	"strings"
	"testing"
	"time"
)

func madrid(t *testing.T) *time.Location {
	l, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func at(loc *time.Location, s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", s, loc)
	return t
}

func TestNext_Frequencies(t *testing.T) {
	m := madrid(t)
	cases := []struct {
		s     Spec
		after string
		want  string
	}{
		{Spec{Frequency: Hourly, Time: "00:15"}, "2026-11-18 10:20", "2026-11-18 11:15"},
		{Spec{Frequency: Hourly, Time: "00:15"}, "2026-11-18 10:10", "2026-11-18 10:15"},
		{Spec{Frequency: Daily, Time: "02:00"}, "2026-11-18 10:00", "2026-11-19 02:00"},
		{Spec{Frequency: Daily, Time: "02:00"}, "2026-11-18 01:00", "2026-11-18 02:00"},
		{Spec{Frequency: Weekly, Time: "08:00", Day: 1}, "2026-11-18 10:00", "2026-11-23 08:00"}, // lunes
		{Spec{Frequency: Weekly, Time: "08:00", Day: 3}, "2026-11-18 07:00", "2026-11-18 08:00"}, // hoy es miércoles
		{Spec{Frequency: Monthly, Time: "07:30", Day: 1}, "2026-11-18 10:00", "2026-12-01 07:30"},
		{Spec{Frequency: Monthly, Time: "07:30", Day: 31}, "2026-11-18 10:00", "2026-11-30 07:30"}, // noviembre no tiene 31
	}
	for i, c := range cases {
		got := Next(c.s, at(m, c.after), m)
		if got.Format("2006-01-02 15:04") != c.want || got.Location().String() != "Europe/Madrid" {
			t.Errorf("caso %d: %s, se esperaba %s", i, got.Format("2006-01-02 15:04 MST"), c.want)
		}
	}
}

func TestNext_DST(t *testing.T) {
	m := madrid(t)
	// La noche del 28 al 29 de marzo de 2026 las 02:00 pasan a ser las 03:00.
	got := Next(Spec{Frequency: Daily, Time: "02:30"}, at(m, "2026-03-28 12:00"), m)
	if got.Format("2006-01-02 15:04") != "2026-03-29 03:30" {
		t.Errorf("una hora que no existe se ejecuta justo después del salto: %s", got.Format("2006-01-02 15:04 MST"))
	}
	// Y a las 8:00 en verano es a las 6:00 UTC (en invierno, a las 7:00).
	if g := Next(Spec{Frequency: Daily, Time: "08:00"}, at(m, "2026-07-01 00:00"), m).UTC().Hour(); g != 6 {
		t.Errorf("verano: %d UTC", g)
	}
	if g := Next(Spec{Frequency: Daily, Time: "08:00"}, at(m, "2026-12-01 00:00"), m).UTC().Hour(); g != 7 {
		t.Errorf("invierno: %d UTC", g)
	}
}

func TestDue_MissedRunsOnlyOnce(t *testing.T) {
	m := madrid(t)
	s := Spec{Frequency: Hourly, Time: "00:00"}
	last := at(m, "2026-11-18 01:00")
	// Servidor parado de 01:30 a 06:10: se ejecuta una vez, no cinco.
	if !Due(s, &last, at(m, "2026-11-18 06:10"), m) {
		t.Fatal("se perdió una ejecución: toca")
	}
	now := at(m, "2026-11-18 06:10")
	if Due(s, &now, at(m, "2026-11-18 06:50"), m) {
		t.Fatal("ejecutada a las 06:10, la siguiente es a las 07:00")
	}
	if !Due(s, nil, at(m, "2026-11-18 06:50"), m) {
		t.Fatal("nunca ejecutada: toca")
	}
}

func TestValidate(t *testing.T) {
	for _, s := range []Spec{{Frequency: "cada rato", Time: "02:00"}, {Frequency: Daily, Time: "2:00"}, {Frequency: Weekly, Time: "02:00", Day: 7},
		{Frequency: Monthly, Time: "02:00", Day: 0}, {Frequency: Monthly, Time: "02:00", Day: 32}} {
		if err := s.Validate(); err == nil || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("%+v: %v", s, err)
		}
	}
	if err := (Spec{Frequency: Weekly, Time: "08:00", Day: 1}).Validate(); err != nil {
		t.Fatal(err)
	}
}
