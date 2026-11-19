package reports

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mdm/internal/server/schedule"
	"mdm/internal/server/store"
)

func TestScheduler_WeeklyMondayEight(t *testing.T) {
	s, _, _ := seed(t)
	ch, _ := s.CreateChannel(ctx, store.Channel{Name: "IT", Type: "email", Recipients: []string{"it@ejemplo.es"}})
	madrid, _ := time.LoadLocation("Europe/Madrid")
	created := time.Date(2026, 11, 18, 12, 0, 0, 0, madrid) // miércoles
	sc, err := s.CreateReportSchedule(ctx, store.ReportSchedule{Report: "software_by_app", Filters: json.RawMessage(`{}`),
		Spec: schedule.Spec{Frequency: schedule.Weekly, Time: "08:00", Day: 1}, ChannelID: ch.ID, Enabled: true}, created)
	if err != nil {
		t.Fatal(err)
	}
	now := created
	sch := &Scheduler{Store: s, Now: func() time.Time { return now }, Location: func() *time.Location { return madrid }, PublicURL: "https://mdm.ejemplo.es"}
	count := func() int {
		var n int
		_ = s.Pool().QueryRow(ctx, `SELECT count(*) FROM deliveries`).Scan(&n)
		return n
	}
	sch.RunDue(ctx)
	if count() != 0 {
		t.Fatal("al crearla no se envía (la creación cuenta como última ejecución)")
	}
	now = time.Date(2026, 11, 23, 7, 59, 0, 0, madrid) // lunes 07:59
	sch.RunDue(ctx)
	if count() != 0 {
		t.Fatal("antes de las 8:00 no")
	}
	now = time.Date(2026, 11, 23, 8, 3, 0, 0, madrid)
	sch.RunDue(ctx)
	if count() != 1 {
		t.Fatal("el lunes a las 8:00 se envía")
	}
	var msg string
	_ = s.Pool().QueryRow(ctx, `SELECT message::text FROM deliveries`).Scan(&msg)
	if !strings.Contains(msg, "software_by_app-2026-11-23.csv") || !strings.Contains(msg, "Software por programa") {
		t.Fatalf("adjunto: %s", msg[:min(300, len(msg))])
	}
	now = now.Add(30 * time.Minute)
	sch.RunDue(ctx)
	if count() != 1 {
		t.Fatal("una vez por semana")
	}
	got, _ := s.GetReportSchedule(ctx, sc.ID)
	if got.LastStatus != "Enviado: 2 filas." || got.LastRunAt == nil {
		t.Fatalf("%+v", got)
	}
}

func TestScheduler_WebhookGetsLink(t *testing.T) {
	s, _, _ := seed(t)
	ch, _ := s.CreateChannel(ctx, store.Channel{Name: "hook", Type: "webhook", URL: "https://hooks.ejemplo.es/x"})
	madrid, _ := time.LoadLocation("Europe/Madrid")
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, madrid)
	_, _ = s.CreateReportSchedule(ctx, store.ReportSchedule{Report: "inactive", Filters: json.RawMessage(`{"days":30}`),
		Spec: schedule.Spec{Frequency: schedule.Daily, Time: "07:00"}, ChannelID: ch.ID, Enabled: true}, now.Add(-24*time.Hour))
	(&Scheduler{Store: s, Now: func() time.Time { return now }, Location: func() *time.Location { return madrid }, PublicURL: "https://mdm.ejemplo.es"}).RunDue(ctx)
	var msg string
	_ = s.Pool().QueryRow(ctx, `SELECT message::text FROM deliveries`).Scan(&msg)
	if !strings.Contains(msg, `"report.ready"`) || !strings.Contains(msg, "https://mdm.ejemplo.es/informes/inactive") || strings.Contains(msg, "Attachment\": {") {
		t.Fatalf("%s", msg)
	}
}
