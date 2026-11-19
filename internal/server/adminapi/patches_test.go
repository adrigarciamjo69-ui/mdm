package adminapi

import (
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
	"mdm/internal/server/compliance"
)

type patchPolicyResp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

func newPatchPolicy(name, platform string) map[string]any {
	return map[string]any{"name": name, "platform": platform, "auto_approve": []string{"security", "critical"}, "defer_days": 0,
		"window": map[string]any{"days": []int{0, 1, 2, 3, 4, 5, 6}, "start": "22:00", "end": "05:00"},
		"reboot": "in_window", "notify_minutes": 15, "linux_scope": "security", "excluded_kbs": []string{}}
}

func (a *testAPI) scanned(dev string, updates ...protocol.UpdateInfo) {
	a.t.Helper()
	if err := a.store.SaveUpdateScan(ctx, mustUUID(dev), "windows", protocol.UpdateScan{ScannedAt: time.Now(), Updates: updates}, time.Now()); err != nil {
		a.t.Fatal(err)
	}
}

func TestPatchPolicies_EffectiveAndPriority(t *testing.T) {
	a := newTestAPI(t)
	dev := a.seedDevice(seed{Platform: "windows"})
	pub := time.Now().Add(-48 * time.Hour)
	a.scanned(dev.String(), protocol.UpdateInfo{ID: "u-sec", KB: "KB5046617", Classification: "security", Security: true, PublishedAt: &pub},
		protocol.UpdateInfo{ID: "u-drv", KB: "KB9", Classification: "driver", PublishedAt: &pub})
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/patch-policies", newPatchPolicy("X", "windows")), 403, "forbidden")
	p1 := decode[patchPolicyResp](t, admin.do("POST", "/patch-policies", newPatchPolicy("Estaciones", "windows")))
	wantStatus(t, admin.do("PUT", "/patch-policies/"+p1.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}}), 200)
	eff, _, _ := a.store.EffectivePolicyFor(ctx, dev)
	if eff.Patches == nil || eff.Patches.Reboot != "in_window" || len(eff.Patches.ApprovedUpdateIDs) != 1 || eff.Patches.ApprovedUpdateIDs[0] != "u-sec" {
		t.Fatalf("política de parches efectiva: %+v", eff.Patches)
	}
	// Una segunda de más prioridad tras reordenar.
	strict := newPatchPolicy("Prudente", "windows")
	strict["defer_days"] = 7
	p2 := decode[patchPolicyResp](t, admin.do("POST", "/patch-policies", strict))
	admin.do("PUT", "/patch-policies/"+p2.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}})
	wantStatus(t, admin.do("POST", "/patch-policies/reorder", map[string]any{"ids": []string{p2.ID, p1.ID}}), 204)
	eff, _, _ = a.store.EffectivePolicyFor(ctx, dev)
	if len(eff.Patches.ApprovedUpdateIDs) != 0 {
		t.Fatalf("con 7 días de espera no se aprueba nada publicado hace 2: %+v", eff.Patches)
	}
	bad := newPatchPolicy("Mala", "windows")
	bad["window"] = map[string]any{"days": []int{}, "start": "22:00", "end": "05:00"}
	wantError(t, admin.do("POST", "/patch-policies", bad), 400, "invalid_request")
	bad = newPatchPolicy("Mala 2", "windows")
	bad["auto_approve"] = []string{"todo"}
	wantError(t, admin.do("POST", "/patch-policies", bad), 400, "invalid_request")
}

func TestUpdatesAPI_CountsAndApproval(t *testing.T) {
	a := newTestAPI(t)
	d1 := a.seedDevice(seed{Platform: "windows"})
	d2 := a.seedDevice(seed{Platform: "windows"})
	a.scanned(d1.String(), protocol.UpdateInfo{ID: "u1", KB: "KB1", Title: "Acumulativa", Classification: "security", Security: true})
	a.scanned(d2.String(), protocol.UpdateInfo{ID: "u1", KB: "KB1", Title: "Acumulativa", Classification: "security", Security: true},
		protocol.UpdateInfo{ID: "u2", KB: "KB2", Title: "Controlador", Classification: "driver"})
	type upd struct {
		ID       string  `json:"id"`
		KB       string  `json:"kb"`
		Missing  int     `json:"missing"`
		Decision *string `json:"decision"`
	}
	list := decode[pageResp[upd]](t, a.loginAs(auth.RoleReadonly).do("GET", "/updates?platform=windows", nil))
	if list.Total != 2 || list.Items[0].ID != "u1" || list.Items[0].Missing != 2 || list.Items[0].Decision != nil {
		t.Fatalf("listado: %+v", list)
	}
	wantError(t, a.loginAs(auth.RoleOperator).do("PUT", "/updates/u2/approval", map[string]any{"decision": "approved"}), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	wantStatus(t, admin.do("PUT", "/updates/u2/approval", map[string]any{"decision": "approved"}), 204)
	list = decode[pageResp[upd]](t, admin.do("GET", "/updates?platform=windows&classification=driver", nil))
	if list.Total != 1 || list.Items[0].Decision == nil || *list.Items[0].Decision != "approved" {
		t.Fatalf("tras aprobar: %+v", list)
	}
	wantStatus(t, admin.do("PUT", "/updates/u2/approval", map[string]any{"decision": nil}), 204)
	wantError(t, admin.do("PUT", "/updates/u2/approval", map[string]any{"decision": "quizá"}), 400, "invalid_request")
	if a.findAudit("update.approved") == nil {
		t.Fatal("falta la auditoría")
	}
	type devUpd struct {
		State struct {
			RebootPending bool `json:"reboot_pending"`
		} `json:"state"`
		Updates []struct {
			ID       string `json:"id"`
			Approved bool   `json:"approved"`
		} `json:"updates"`
	}
	du := decode[devUpd](t, admin.do("GET", "/devices/"+d2.String()+"/updates", nil))
	if len(du.Updates) != 2 {
		t.Fatalf("actualizaciones del equipo: %+v", du)
	}
}

func TestBatches_ScanUpdatesOperator(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Platform: "windows"})
	for _, typ := range []string{"scan_updates", "install_updates_now"} {
		wantStatus(t, a.loginAs(auth.RoleOperator).do("POST", "/batches", map[string]any{"type": typ, "device_ids": []string{d.String()}}), 201)
	}
}

func TestCompliance_SecurityUpdateAge(t *testing.T) {
	a := newTestAPI(t)
	dev := a.seedDevice(seed{Platform: "windows", SeenAgo: time.Minute})
	admin := a.loginAs(auth.RoleAdmin)
	p := decode[patchPolicyResp](t, admin.do("POST", "/patch-policies", newPatchPolicy("Todo", "windows")))
	admin.do("PUT", "/patch-policies/"+p.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}})
	a.scanned(dev.String(), protocol.UpdateInfo{ID: "u1", KB: "KB1", Classification: "security", Security: true})
	// Primera vez vista hace 40 días.
	if _, err := a.store.Pool().Exec(ctx, `UPDATE device_updates SET first_seen_at = now() - interval '40 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := compliance.Recompute(ctx, a.store, dev, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := a.store.Compliance(ctx, dev)
	if rec.State != "noncompliant" || len(rec.Reasons) != 1 {
		t.Fatalf("40 días con una de seguridad pendiente (máximo 30): %+v", rec)
	}
	admin.do("PUT", "/settings", map[string]any{"max_security_update_age_days": 0})
	_, _ = compliance.Recompute(ctx, a.store, dev, time.Now())
	if rec, _, _ := a.store.Compliance(ctx, dev); len(rec.Reasons) != 0 {
		t.Fatalf("con la regla desactivada no hay motivo: %+v", rec)
	}
}

func TestSummary_PatchesAndDeployments(t *testing.T) {
	a := newTestAPI(t)
	d1 := a.seedDevice(seed{Platform: "windows"})
	a.seedDevice(seed{Platform: "windows"})
	a.scanned(d1.String(), protocol.UpdateInfo{ID: "u1", Classification: "security", Security: true}, protocol.UpdateInfo{ID: "u2", Classification: "driver"})
	app := decode[appResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/apps", newApp("Firefox", "windows", "winget", "Mozilla.Firefox")))
	dep := a.deployAll(app.ID, "required", nil)
	_ = a.store.SaveAppStatus(ctx, d1, []protocol.AppStatus{{DeploymentID: dep.ID, State: "failed", UpdatedAt: time.Now()}})
	type summary struct {
		SecurityUpdatesPending int `json:"security_updates_pending"`
		DeploymentFailures     int `json:"deployment_failures"`
	}
	s := decode[summary](t, a.loginAs(auth.RoleReadonly).do("GET", "/summary", nil))
	if s.SecurityUpdatesPending != 1 || s.DeploymentFailures != 1 {
		t.Fatalf("%+v", s)
	}
}
