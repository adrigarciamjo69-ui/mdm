package adminapi

import (
	"net/http"

	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type osCountJSON struct {
	Platform  string `json:"platform"`
	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`
	Count     int    `json:"count"`
}

type summaryJSON struct {
	Total             int            `json:"total"`
	ByStatus          map[string]int `json:"by_status"`
	ByPlatform        map[string]int `json:"by_platform"`
	OSVersions        []osCountJSON  `json:"os_versions"`
	LowDisk           int            `json:"low_disk"`
	Unencrypted       int            `json:"unencrypted"`
	Stale             int            `json:"stale"`
	RecentEnrollments []deviceJSON   `json:"recent_enrollments"`
	Compliance        map[string]int `json:"compliance"`
	SecurityPending   int            `json:"security_updates_pending"`
	DeployFailures    int            `json:"deployment_failures"`
}

func (a *api) summary(w http.ResponseWriter, r *http.Request) {
	s, err := a.d.Store.Summary(r.Context(), a.d.Now(), a.d.CheckinIntervalS)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := summaryJSON{Total: s.Total, ByStatus: map[string]int{}, ByPlatform: s.ByPlatform,
		OSVersions: make([]osCountJSON, 0, len(s.OSVersions)),
		LowDisk:    s.LowDisk, Unencrypted: s.Unencrypted, Stale: s.Stale,
		RecentEnrollments: a.devicesToJSON(s.RecentEnrollments), Compliance: s.Compliance,
		SecurityPending: s.SecurityUpdatesPending, DeployFailures: s.DeploymentFailures}
	for _, st := range store.Statuses {
		out.ByStatus[string(st)] = s.ByStatus[st]
	}
	for _, o := range s.OSVersions {
		out.OSVersions = append(out.OSVersions, osCountJSON{Platform: o.Platform, OSName: o.OSName, OSVersion: o.OSVersion, Count: o.Count})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
