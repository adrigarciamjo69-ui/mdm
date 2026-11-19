package agentapi

import (
	"testing"

	"mdm/internal/protocol"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

func TestAppsStatus_StoredAndIgnoresForeign(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled() // Windows
	app, _ := a.store.CreateApp(ctx, store.App{Name: "Firefox", Platform: "windows", Source: "winget", PackageID: "Mozilla.Firefox"}, a.admin.ID)
	dep, _ := a.store.CreateDeployment(ctx, store.Deployment{AppID: app.ID, Intent: "required", Reboot: "never"}, a.admin.ID)
	_ = a.store.SetDeploymentAssignments(ctx, dep.ID, []policy.Assignment{{}}, a.clock.Now())
	other, _ := a.store.CreateDeployment(ctx, store.Deployment{AppID: app.ID, Intent: "required", Reboot: "never"}, a.admin.ID) // sin asignar
	rep := protocol.AppStatusReport{Items: []protocol.AppStatus{
		{DeploymentID: dep.ID.String(), State: "installed"},
		{DeploymentID: other.ID.String(), State: "installed"},
		{DeploymentID: "no-uuid", State: "installed"},
	}}
	if resp := a.post("/apps/status", "", c.authz, rep, false); resp.StatusCode != 204 {
		t.Fatalf("%d %s", resp.StatusCode, bodyString(resp))
	}
	rows, _ := a.store.DeviceDeploymentStatus(ctx, c.id)
	if len(rows) != 1 || rows[0].State != "installed" {
		t.Fatalf("solo debe guardarse el despliegue que aplica al equipo: %+v", rows)
	}
	bad := protocol.AppStatusReport{Items: []protocol.AppStatus{{DeploymentID: dep.ID.String(), State: "volando"}}}
	wantError(t, a.post("/apps/status", "", c.authz, bad, false), 400, "invalid_request")
}
