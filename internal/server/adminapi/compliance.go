package adminapi

import (
	"net/http"
	"time"

	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
)

type settingResultJSON struct {
	PolicyID    string    `json:"policy_id"`
	PolicyName  string    `json:"policy_name"`
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	Applied     bool      `json:"applied"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

type conflictJSON struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	WinnerID   string `json:"winner_id"`
	WinnerName string `json:"winner_name"`
	LoserID    string `json:"loser_id"`
	LoserName  string `json:"loser_name"`
}

// deviceCompliance devuelve el cumplimiento de un equipo con el detalle por
// ajuste, los conflictos entre políticas y los motivos del servidor.
func (a *api) deviceCompliance(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	rec, results, err := a.d.Store.Compliance(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	eff, conflicts, err := a.d.Store.EffectivePolicyFor(r.Context(), row.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	policies, err := a.d.Store.ListPolicies(r.Context(), row.Platform)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	names := map[string]string{}
	for _, p := range policies {
		names[p.ID.String()] = p.Name
	}
	title := func(key string) string {
		if d, ok := policy.Find(row.Platform, key); ok {
			return d.Title
		}
		return key
	}
	out := struct {
		State             string              `json:"state"`
		NoncompliantSince *time.Time          `json:"noncompliant_since"`
		EvaluatedAt       *time.Time          `json:"evaluated_at"`
		ReportedAt        *time.Time          `json:"reported_at"`
		UpToDate          bool                `json:"up_to_date"`
		ServerReasons     []string            `json:"server_reasons"`
		Results           []settingResultJSON `json:"results"`
		Conflicts         []conflictJSON      `json:"conflicts"`
	}{State: rec.State, NoncompliantSince: rec.NoncompliantSince, EvaluatedAt: rec.EvaluatedAt, ReportedAt: rec.ReportedAt,
		UpToDate: rec.ReportHash != "" && rec.ReportHash == eff.Hash, ServerReasons: rec.Reasons,
		Results: []settingResultJSON{}, Conflicts: []conflictJSON{}}
	for _, res := range results {
		out.Results = append(out.Results, settingResultJSON{PolicyID: res.PolicyID, PolicyName: names[res.PolicyID], Key: res.Key,
			Title: title(res.Key), Status: res.Status, Message: res.Message, Applied: res.Applied, EvaluatedAt: res.EvaluatedAt})
	}
	for _, c := range conflicts {
		out.Conflicts = append(out.Conflicts, conflictJSON{Key: c.Key, Title: title(c.Key),
			WinnerID: c.Winner.String(), WinnerName: names[c.Winner.String()], LoserID: c.Loser.String(), LoserName: names[c.Loser.String()]})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
