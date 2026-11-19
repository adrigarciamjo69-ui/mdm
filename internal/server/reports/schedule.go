package reports

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"mdm/internal/server/notify"
	"mdm/internal/server/schedule"
	"mdm/internal/server/store"
)

// MaxAttachment es el tamaño máximo del CSV adjunto; si lo supera se envía un enlace.
const MaxAttachment = 10 << 20

// Scheduler ejecuta los informes programados (tarea reports.run).
type Scheduler struct {
	Store     *store.Store
	Now       func() time.Time
	Location  func() *time.Location
	PublicURL string
	Log       *slog.Logger
}

func (sc *Scheduler) log() *slog.Logger {
	if sc.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return sc.Log
}

// ReportURL es la dirección del informe en la consola con sus filtros.
func ReportURL(base, name string, f Filters) string {
	q := url.Values{}
	if f.GroupID != nil {
		q.Set("grupo", f.GroupID.String())
	}
	if f.Platform != "" {
		q.Set("plataforma", f.Platform)
	}
	if f.Q != "" {
		q.Set("q", f.Q)
	}
	if f.Days > 0 {
		q.Set("dias", fmt.Sprint(f.Days))
	}
	u := base + "/informes/" + name
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// RunDue envía los informes cuya hora ha llegado. Si el servidor estuvo
// parado, cada uno se envía una sola vez.
func (sc *Scheduler) RunDue(ctx context.Context) {
	list, err := sc.Store.ListReportSchedules(ctx)
	if err != nil {
		sc.log().Error("informes programados", "err", err)
		return
	}
	loc := sc.Location()
	now := sc.Now()
	for _, r := range list {
		if !r.Enabled || !schedule.Due(r.Spec, r.LastRunAt, now, loc) {
			continue
		}
		status := sc.runOne(ctx, r, now.In(loc))
		_ = sc.Store.SetReportScheduleRun(ctx, r.ID, now, status)
	}
}

func (sc *Scheduler) runOne(ctx context.Context, r store.ReportSchedule, now time.Time) string {
	def, ok := Find(r.Report)
	if !ok {
		return "Error: informe desconocido."
	}
	f, err := ParseFilters(r.Report, r.Filters)
	if err != nil {
		return "Error: " + err.Error()
	}
	ch, err := sc.Store.GetChannel(ctx, r.ChannelID)
	if err != nil {
		return "Error: el canal ya no existe."
	}
	link := ReportURL(sc.PublicURL, r.Report, f)
	file := fmt.Sprintf("%s-%s.csv", r.Report, now.Format("2006-01-02"))
	m := notify.Message{Event: "report.ready", Subject: fmt.Sprintf("[MDM] Informe: %s (%s)", def.Title, now.Format("02/01/2006")),
		Payload: map[string]any{"event": "report.ready", "sent_at": now.UTC(), "report": r.Report, "title": def.Title, "url": link}}
	var rows int
	if ch.Type == "email" {
		data, total, err := CSV(ctx, sc.Store, r.Report, f)
		if err != nil {
			return "Error al generar el informe: " + err.Error()
		}
		rows = total
		m.Text = fmt.Sprintf("Informe «%s»: %d filas.\n\nVer en la consola: %s\n", def.Title, total, link)
		m.HTML = fmt.Sprintf("<p>Informe «%s»: %d filas.</p><p><a href=\"%s\">Ver en la consola</a></p>", def.Title, total, link)
		if len(data) <= MaxAttachment {
			m.Attachment = &notify.Attachment{Name: file, ContentType: "text/csv; charset=utf-8", Data: data}
		} else {
			m.Text += "\nEl fichero supera los 10 MB: descárgalo desde la consola.\n"
			m.HTML += "<p>El fichero supera los 10 MB: descárgalo desde la consola.</p>"
		}
	} else {
		res, err := Run(ctx, sc.Store, r.Report, f, 1, 1)
		if err != nil {
			return "Error al generar el informe: " + err.Error()
		}
		rows = res.Total
		m.Payload["rows"] = res.Total
	}
	if err := notify.Queue(ctx, sc.Store.Pool(), ch.ID, m, nil, nil, now.UTC()); err != nil {
		return "Error al encolar el envío: " + err.Error()
	}
	return fmt.Sprintf("Enviado: %d filas.", rows)
}
