package adminapi

import (
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

func TestJobsAPI_FailedListAdminOnly(t *testing.T) {
	a := newTestAPI(t)
	now := time.Now()
	_ = store.EnqueueJob(ctx, a.store.Pool(), store.NewJob{Kind: "android.sync", RunAt: now.Add(-time.Minute)})
	j, _ := a.store.ClaimJob(ctx, now, time.Minute)
	_ = a.store.FailJob(ctx, j.ID, "credenciales de Google no válidas", now)
	_ = store.EnqueueJob(ctx, a.store.Pool(), store.NewJob{Kind: "cleanup", RunAt: now.Add(time.Hour)})
	wantError(t, a.loginAs(auth.RoleOperator).do("GET", "/jobs?state=failed", nil), 403, "forbidden")
	type job struct {
		Kind      string `json:"kind"`
		Attempts  int    `json:"attempts"`
		LastError string `json:"last_error"`
		FailedAt  string `json:"failed_at"`
	}
	p := decode[pageResp[job]](t, a.loginAs(auth.RoleAdmin).do("GET", "/jobs?state=failed", nil))
	if p.Total != 1 || p.Items[0].Kind != "android.sync" || p.Items[0].LastError != "credenciales de Google no válidas" || p.Items[0].FailedAt == "" {
		t.Fatalf("fallidas: %+v", p)
	}
	wantError(t, a.loginAs(auth.RoleAdmin).do("GET", "/jobs?state=raro", nil), 400, "invalid_request")
}
