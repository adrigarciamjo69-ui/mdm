package metrics

import (
	"context"
	"testing"
	"time"

	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

func TestSnapshot_OncePerDayAfterFive(t *testing.T) {
	s := storetest.New(t)
	a := storetest.SeedDevice(t, s, "PC-1", "windows")
	storetest.SeedDevice(t, s, "PC-2", "linux")
	_, _ = s.Pool().Exec(ctx, `UPDATE devices SET last_seen_at = now()`)
	now := time.Now()
	_, _ = s.SaveComplianceState(ctx, a, "noncompliant", []string{"x"}, &now, now)
	madrid, _ := time.LoadLocation("Europe/Madrid")
	clock := time.Date(2026, 11, 18, 0, 3, 0, 0, madrid)
	sn := &Snapshotter{Store: s, Now: func() time.Time { return clock }, Location: func() *time.Location { return madrid }, CheckinIntervalS: 60}
	if took, _ := sn.Run(ctx); took {
		t.Fatal("antes de las 00:05 no")
	}
	clock = clock.Add(3 * time.Minute)
	if took, err := sn.Run(ctx); !took || err != nil {
		t.Fatalf("a las 00:06 sí: %v", err)
	}
	if took, _ := sn.Run(ctx); took {
		t.Fatal("una vez al día")
	}
	series, err := s.Metrics(ctx, []string{"devices.total", "devices.windows", "compliance.percent", "alerts.open.critical"}, clock.AddDate(0, 0, -90))
	if err != nil {
		t.Fatal(err)
	}
	if v := series["devices.total"]; len(v) != 1 || v[0].Value != 2 || v[0].Day != "2026-11-18" {
		t.Fatalf("total: %+v", v)
	}
	if v := series["compliance.percent"]; len(v) != 1 || v[0].Value != 0 {
		t.Fatalf("cumplimiento (1 evaluado, 0 cumple): %+v", v)
	}
	if v := series["alerts.open.critical"]; len(v) != 1 || v[0].Value != 0 {
		t.Fatalf("alertas: %+v", v)
	}
	_ = store.Event{}
}
