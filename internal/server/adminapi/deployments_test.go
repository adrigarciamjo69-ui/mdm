package adminapi

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

type deploymentResp struct {
	ID          string         `json:"id"`
	AppID       string         `json:"app_id"`
	AppName     string         `json:"app_name"`
	Intent      string         `json:"intent"`
	KeepUpdated bool           `json:"keep_updated"`
	Counts      map[string]int `json:"counts"`
}

type statusResp struct {
	Counts  map[string]int `json:"counts"`
	Devices []struct {
		DeviceID string `json:"device_id"`
		Hostname string `json:"hostname"`
		State    string `json:"state"`
		Message  string `json:"message"`
		Attempts int    `json:"attempts"`
	} `json:"devices"`
}

func (a *testAPI) deployAll(appID, intent string, extra map[string]any) deploymentResp {
	a.t.Helper()
	admin := a.loginAs(auth.RoleAdmin)
	body := map[string]any{"app_id": appID, "intent": intent, "reboot": "never"}
	for k, v := range extra {
		body[k] = v
	}
	resp := admin.do("POST", "/deployments", body)
	wantStatus(a.t, resp, 201)
	d := decode[deploymentResp](a.t, resp)
	wantStatus(a.t, admin.do("PUT", "/deployments/"+d.ID+"/assignments", map[string]any{"assignments": []map[string]any{{"group_id": nil}}}), 200)
	return d
}

func TestEffectivePolicy_HashChangesWithDeployment(t *testing.T) {
	a := newTestAPI(t)
	dev := a.seedDevice(seed{Platform: "linux"})
	before, _, _ := a.store.EffectivePolicyFor(ctx, dev)
	app := decode[appResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")))
	a.deployAll(app.ID, "required", map[string]any{"keep_updated": true})
	after, _, _ := a.store.EffectivePolicyFor(ctx, dev)
	if after.Hash == before.Hash || len(after.Apps) != 1 || after.Apps[0].PackageID != "nginx" || !after.Apps[0].KeepUpdated || after.Apps[0].Source != "apt" {
		t.Fatalf("efectiva: %+v", after)
	}
	if after.Hash != protocol.PolicyHashFull(after) {
		t.Fatal("la huella debe ser la completa")
	}
}

func TestEffectivePolicy_UploadUsesCurrentVersion(t *testing.T) {
	a := newTestAPI(t)
	dev := a.seedDevice(seed{Platform: "windows"})
	admin := a.loginAs(auth.RoleAdmin)
	app := decode[appResp](t, admin.do("POST", "/apps", newApp("Conta", "windows", "upload", "")))
	a.deployAll(app.ID, "required", nil)
	eff, _, _ := a.store.EffectivePolicyFor(ctx, dev)
	if len(eff.Apps) != 0 {
		t.Fatal("sin versión vigente no hay nada que instalar")
	}
	sha := a.uploadedFile("msi-1")
	admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "1.0", "file_sha256": sha, "installer_type": "msi"})
	eff, _, _ = a.store.EffectivePolicyFor(ctx, dev)
	if len(eff.Apps) != 1 || eff.Apps[0].FileSHA256 != sha || eff.Apps[0].Version != "1.0" || eff.Apps[0].Size != 5 || eff.Apps[0].InstallerType != "msi" ||
		eff.Apps[0].InstallCommand == "" || eff.Apps[0].Detection == nil || len(eff.Apps[0].SuccessCodes) != 3 {
		t.Fatalf("despliegue con instalador: %+v", eff.Apps)
	}
}

func TestDeployments_Validation(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	up := decode[appResp](t, admin.do("POST", "/apps", newApp("Conta", "windows", "upload", "")))
	apt := decode[appResp](t, admin.do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")))
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": up.ID, "intent": "required", "keep_updated": true, "reboot": "never"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": apt.ID, "intent": "instalar", "reboot": "never"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": apt.ID, "intent": "required", "reboot": "ya"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": uuid.NewString(), "intent": "required", "reboot": "never"}), 400, "invalid_request")
	admin.do("POST", "/apps/"+apt.ID+"/archive", nil)
	wantError(t, admin.do("POST", "/deployments", map[string]any{"app_id": apt.ID, "intent": "required", "reboot": "never"}), 400, "invalid_request")
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/deployments", map[string]any{"app_id": up.ID, "intent": "required", "reboot": "never"}), 403, "forbidden")
}

func TestAppsStatusAndDeploymentStatus(t *testing.T) {
	a := newTestAPI(t)
	d1 := a.seedDevice(seed{Hostname: "srv-1", Platform: "linux"})
	d2 := a.seedDevice(seed{Hostname: "srv-2", Platform: "linux"})
	a.seedDevice(seed{Hostname: "win-1", Platform: "windows"})
	app := decode[appResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")))
	dep := a.deployAll(app.ID, "required", nil)
	code := 100
	_ = a.store.SaveAppStatus(ctx, d1, []protocol.AppStatus{{DeploymentID: dep.ID, State: "failed", Message: "E: Unable to locate package nginx", Attempts: 4, ExitCode: &code, UpdatedAt: time.Now()}})
	st := decode[statusResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/deployments/"+dep.ID+"/status", nil))
	if len(st.Devices) != 2 || st.Counts["failed"] != 1 || st.Counts["pending"] != 1 {
		t.Fatalf("estado: %+v", st)
	}
	if st.Devices[0].Hostname != "srv-1" || st.Devices[0].Message != "E: Unable to locate package nginx" {
		t.Fatalf("fila: %+v", st.Devices[0])
	}
	list := decode[[]deploymentResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/deployments", nil))
	if len(list) != 1 || list[0].AppName != "nginx" || list[0].Counts["failed"] != 1 {
		t.Fatalf("listado: %+v", list)
	}
	type devDep struct {
		AppName string `json:"app_name"`
		State   string `json:"state"`
	}
	dd := decode[[]devDep](t, a.loginAs(auth.RoleReadonly).do("GET", "/devices/"+d2.String()+"/deployments", nil))
	if len(dd) != 1 || dd[0].AppName != "nginx" || dd[0].State != "pending" {
		t.Fatalf("despliegues del equipo: %+v", dd)
	}
}

func TestRetry_ResetsAndChangesHash(t *testing.T) {
	a := newTestAPI(t)
	d1 := a.seedDevice(seed{Platform: "linux"})
	app := decode[appResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")))
	dep := a.deployAll(app.ID, "required", nil)
	_ = a.store.SaveAppStatus(ctx, d1, []protocol.AppStatus{{DeploymentID: dep.ID, State: "failed", Attempts: 4, UpdatedAt: time.Now()}})
	before, _, _ := a.store.EffectivePolicyFor(ctx, d1)
	wantError(t, a.loginAs(auth.RoleReadonly).do("POST", "/deployments/"+dep.ID+"/retry", map[string]any{}), 403, "forbidden")
	wantStatus(t, a.loginAs(auth.RoleOperator).do("POST", "/deployments/"+dep.ID+"/retry", map[string]any{}), 204)
	after, _, _ := a.store.EffectivePolicyFor(ctx, d1)
	if after.Hash == before.Hash || after.Apps[0].RetryEpoch != 1 {
		t.Fatalf("reintentar debe cambiar la huella: %+v", after.Apps)
	}
	st := decode[statusResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/deployments/"+dep.ID+"/status", nil))
	if st.Devices[0].State != "pending" || st.Devices[0].Attempts != 0 {
		t.Fatalf("tras reintentar: %+v", st.Devices[0])
	}
	if a.findAudit("deployment.retried") == nil {
		t.Fatal("falta la auditoría")
	}
}
