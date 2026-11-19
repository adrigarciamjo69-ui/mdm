package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/reports"
	"mdm/internal/server/schedule"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

func (a *api) listReports(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, reports.All())
}

// reportFilters lee los filtros de la consulta (?group=&platform=&q=&days=).
func reportFilters(name string, r *http.Request) (reports.Filters, error) {
	q := r.URL.Query()
	m := map[string]any{"group_id": q.Get("group"), "platform": q.Get("platform"), "q": q.Get("q")}
	if d := q.Get("days"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil {
			return reports.Filters{}, errors.New("Los días deben ser un número.")
		}
		m["days"] = n
	}
	b, _ := json.Marshal(m)
	return reports.ParseFilters(name, b)
}

func (a *api) runReport(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	def, ok := reports.Find(name)
	if !ok {
		notFound(w, "Este informe no existe.")
		return
	}
	f, err := reportFilters(name, r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		data, total, err := reports.CSV(r.Context(), a.d.Store, name, f)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		admin := adminFrom(r.Context())
		a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ReportExported, TargetType: "report", TargetID: name,
			Details: map[string]any{"title": def.Title, "rows": total}, IP: a.ip(r)})
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+"-"+a.d.Now().Format("2006-01-02")+`.csv"`)
		_, _ = w.Write(data)
		return
	}
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	res, err := reports.Run(r.Context(), a.d.Store, name, f, page, perPage)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

type reportScheduleJSON struct {
	ID          string          `json:"id"`
	Report      string          `json:"report"`
	Title       string          `json:"title"`
	Filters     json.RawMessage `json:"filters"`
	Frequency   string          `json:"frequency"`
	Time        string          `json:"time"`
	Day         int             `json:"day"`
	ChannelID   string          `json:"channel_id"`
	ChannelName string          `json:"channel_name"`
	Enabled     bool            `json:"enabled"`
	LastRunAt   *time.Time      `json:"last_run_at"`
	LastStatus  string          `json:"last_status"`
	NextRunAt   *time.Time      `json:"next_run_at"`
}

func (a *api) scheduleLocation(r *http.Request) *time.Location {
	v, err := settings.Load(r.Context(), a.d.Store)
	if err != nil {
		return settings.Defaults().Location()
	}
	return v.Location()
}

func (a *api) toReportScheduleJSON(r *http.Request, s store.ReportSchedule) reportScheduleJSON {
	def, _ := reports.Find(s.Report)
	out := reportScheduleJSON{ID: s.ID.String(), Report: s.Report, Title: def.Title, Filters: s.Filters, Frequency: s.Spec.Frequency, Time: s.Spec.Time,
		Day: s.Spec.Day, ChannelID: s.ChannelID.String(), ChannelName: s.ChannelName, Enabled: s.Enabled, LastRunAt: s.LastRunAt, LastStatus: s.LastStatus}
	if s.Enabled {
		from := a.d.Now()
		if s.LastRunAt != nil {
			from = *s.LastRunAt
		}
		next := schedule.Next(s.Spec, from, a.scheduleLocation(r))
		out.NextRunAt = &next
	}
	return out
}

func (a *api) readReportSchedule(w http.ResponseWriter, r *http.Request) (store.ReportSchedule, bool) {
	var in struct {
		Report    string          `json:"report"`
		Filters   json.RawMessage `json:"filters"`
		Frequency string          `json:"frequency"`
		Time      string          `json:"time"`
		Day       int             `json:"day"`
		ChannelID string          `json:"channel_id"`
		Enabled   bool            `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.ReportSchedule{}, false
	}
	if _, ok := reports.Find(in.Report); !ok {
		badRequest(w, "Informe desconocido.")
		return store.ReportSchedule{}, false
	}
	f, err := reports.ParseFilters(in.Report, in.Filters)
	if err != nil {
		badRequest(w, err.Error())
		return store.ReportSchedule{}, false
	}
	spec := schedule.Spec{Frequency: in.Frequency, Time: in.Time, Day: in.Day}
	if err := spec.Validate(); err != nil || spec.Frequency == schedule.Hourly {
		if err == nil {
			err = errors.New("Un informe se programa a diario, cada semana o cada mes.")
		}
		badRequest(w, err.Error())
		return store.ReportSchedule{}, false
	}
	chID, err := uuid.Parse(in.ChannelID)
	if err != nil {
		badRequest(w, "Elige el canal por el que se enviará.")
		return store.ReportSchedule{}, false
	}
	if _, err := a.d.Store.GetChannel(r.Context(), chID); err != nil {
		badRequest(w, "El canal indicado no existe.")
		return store.ReportSchedule{}, false
	}
	fb, _ := json.Marshal(f)
	return store.ReportSchedule{Report: in.Report, Filters: fb, Spec: spec, ChannelID: chID, Enabled: in.Enabled}, true
}

func (a *api) listReportSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := a.d.Store.ListReportSchedules(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]reportScheduleJSON, 0, len(list))
	for _, s := range list {
		out = append(out, a.toReportScheduleJSON(r, s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createReportSchedule(w http.ResponseWriter, r *http.Request) {
	s, ok := a.readReportSchedule(w, r)
	if !ok {
		return
	}
	created, err := a.d.Store.CreateReportSchedule(r.Context(), s, a.d.Now().UTC())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ReportScheduleCreated, TargetType: "report_schedule", TargetID: created.ID.String(),
		Details: map[string]any{"report": created.Report, "frequency": created.Spec.Frequency}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, a.toReportScheduleJSON(r, created))
}

func (a *api) updateReportSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	s, ok := a.readReportSchedule(w, r)
	if !ok {
		return
	}
	s.ID = id
	if err := a.d.Store.UpdateReportSchedule(r.Context(), s); errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta programación no existe.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ReportScheduleUpdated, TargetType: "report_schedule", TargetID: id.String(),
		Details: map[string]any{"report": s.Report, "enabled": s.Enabled}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetReportSchedule(r.Context(), id)
	httpx.WriteJSON(w, http.StatusOK, a.toReportScheduleJSON(r, updated))
}

func (a *api) deleteReportSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := a.d.Store.DeleteReportSchedule(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta programación no existe.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ReportScheduleDeleted, TargetType: "report_schedule", TargetID: id.String(), IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
