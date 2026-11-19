package store

import (
	"testing"
	"time"
)

func TestStatusOf(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		lastSeen time.Duration
		revoked  bool
		want     Status
	}{
		{"recién conectado", 10 * time.Second, false, StatusOnline},
		{"179 s", 179 * time.Second, false, StatusOnline},
		{"180 s", 180 * time.Second, false, StatusOffline},
		{"29 días", 29 * 24 * time.Hour, false, StatusOffline},
		{"30 días", 30 * 24 * time.Hour, false, StatusInactive},
		{"revocado y en línea", 10 * time.Second, true, StatusRevoked},
	}
	for _, c := range cases {
		seen := now.Add(-c.lastSeen)
		d := Device{LastSeenAt: &seen}
		if c.revoked {
			d.RevokedAt = &now
		}
		if got := StatusOf(d, now, 60); got != c.want {
			t.Errorf("%s: %s, se esperaba %s", c.name, got, c.want)
		}
	}
	if got := StatusOf(Device{}, now, 60); got != StatusOffline {
		t.Errorf("sin conexiones: %s", got)
	}
}
