package adminapi

import (
	"net/http"
	"time"

	"mdm/internal/server/httpx"
)

type jobJSON struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	Attempts   int        `json:"attempts"`
	LastError  string     `json:"last_error"`
	RunAt      time.Time  `json:"run_at"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
	FailedAt   *time.Time `json:"failed_at"`
}

// listJobs lista tareas en segundo plano por estado (pending, failed, finished).
func (a *api) listJobs(w http.ResponseWriter, r *http.Request) {
	page, perPage, ok := pageParams(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "failed"
	}
	if state != "pending" && state != "failed" && state != "finished" {
		badRequest(w, "El estado debe ser pending, failed o finished.")
		return
	}
	jobs, total, err := a.d.Store.ListJobs(r.Context(), state, page, perPage)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, jobJSON{ID: j.ID, Kind: j.Kind, Attempts: j.Attempts, LastError: j.LastError, RunAt: j.RunAt,
			CreatedAt: j.CreatedAt, FinishedAt: j.FinishedAt, FailedAt: j.FailedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[jobJSON]{Items: items, Total: total, Page: page, PerPage: perPage})
}
