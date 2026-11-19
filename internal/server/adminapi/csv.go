package adminapi

import (
	"encoding/csv"
	"net/http"
	"strings"
	"time"

	"mdm/internal/server/audit"
	"mdm/internal/server/store"
)

var csvHeader = []string{"Nombre", "Plataforma", "Sistema", "Versión", "Serie", "Fabricante", "Modelo",
	"Estado", "Grupo", "Usuario", "Etiquetas", "Última conexión", "Inscrito"}

var statusLabels = map[store.Status]string{
	store.StatusOnline: "En línea", store.StatusOffline: "Desconectado",
	store.StatusInactive: "Inactivo", store.StatusRevoked: "Revocado",
}

var platformLabels = map[string]string{"windows": "Windows", "linux": "Linux", "android": "Android"}

// csvSafe evita que una hoja de cálculo interprete la celda como fórmula: los
// valores que empiezan por =, +, - o @ (o por tabulador o retorno de carro,
// que algunas aplicaciones descartan antes de evaluar) se anteponen con un
// apóstrofo.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

const csvTime = "2006-01-02 15:04"

func csvDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(csvTime)
}

// exportDevices devuelve todos los dispositivos que cumplen el filtro como
// CSV en UTF-8 con BOM, que es lo que Excel necesita para leer bien los acentos.
func (a *api) exportDevices(w http.ResponseWriter, r *http.Request) {
	f, ok := a.deviceFilter(w, r)
	if !ok {
		return
	}
	f.All = true
	rows, _, err := a.d.Store.ListDevices(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	now := a.d.Now()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dispositivos-`+now.Format("2006-01-02")+`.csv"`)
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	_ = cw.Write(csvHeader)
	for _, row := range rows {
		d := row.Device
		enrolled := d.EnrolledAt
		rec := []string{d.Hostname, platformLabels[d.Platform], d.OSName, d.OSVersion, d.Serial, d.Manufacturer, d.Model,
			statusLabels[store.StatusOf(d, now, a.d.CheckinIntervalS)], row.GroupName, d.AssignedUser,
			strings.Join(d.Tags, ", "), csvDate(d.LastSeenAt), csvDate(&enrolled)}
		for i := range rec {
			rec[i] = csvSafe(rec[i])
		}
		_ = cw.Write(rec)
	}
	cw.Flush()

	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DevicesExport,
		Details: map[string]any{"count": len(rows), "filters": r.URL.RawQuery}, IP: a.ip(r)})
}
