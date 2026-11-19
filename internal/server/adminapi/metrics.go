package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"mdm/internal/server/httpx"
	"mdm/internal/server/metrics"
	"mdm/internal/server/store"
)

// SummarySections son las secciones del resumen que un administrador puede ordenar u ocultar.
var SummarySections = []string{"status", "alerts", "review", "trends", "recent"}

func (a *api) getMetrics(w http.ResponseWriter, r *http.Request) {
	keys := strings.Split(r.URL.Query().Get("keys"), ",")
	for _, k := range keys {
		if !slices.Contains(metrics.Keys, k) {
			badRequest(w, "Métrica desconocida: «"+k+"».")
			return
		}
	}
	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 || n > 400 {
			badRequest(w, "Los días deben estar entre 1 y 400.")
			return
		}
		days = n
	}
	out, err := a.d.Store.Metrics(r.Context(), keys, a.d.Now().AddDate(0, 0, -days))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type preferences struct {
	SummarySections []string `json:"summary_sections"`
}

func (a *api) getPreferences(w http.ResponseWriter, r *http.Request) {
	out := preferences{SummarySections: []string{}}
	raw, err := a.d.Store.Preference(r.Context(), adminFrom(r.Context()).ID, "summary_sections")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.serverError(w, r, err)
		return
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &out.SummarySections)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) putPreferences(w http.ResponseWriter, r *http.Request) {
	var in preferences
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	seen := map[string]bool{}
	for _, s := range in.SummarySections {
		if !slices.Contains(SummarySections, s) || seen[s] {
			badRequest(w, "Sección desconocida o repetida: «"+s+"».")
			return
		}
		seen[s] = true
	}
	var value json.RawMessage
	if in.SummarySections != nil {
		value, _ = json.Marshal(in.SummarySections)
	}
	if err := a.d.Store.SetPreference(r.Context(), adminFrom(r.Context()).ID, "summary_sections", value); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.getPreferences(w, r)
}
