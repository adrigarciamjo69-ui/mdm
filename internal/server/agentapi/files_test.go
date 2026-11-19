package agentapi

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/policy"
	"mdm/internal/server/store"
)

func TestAgentDownload_RangeAndAuth(t *testing.T) {
	a := newAgentAPI(t)
	c := a.enrolled()
	content := bytes.Repeat([]byte("0123456789"), 100)
	sha, size, err := a.files.Put(context.Background(), bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a.deployFileToAll(sha, size)
	get := func(authz, rng string) (int, []byte, string) {
		r := httptest.NewRequest("GET", "/files/"+sha, nil)
		r.RemoteAddr = "203.0.113.50:40000"
		if authz != "" {
			r.Header.Set("Authorization", authz)
		}
		if rng != "" {
			r.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		a.handler.ServeHTTP(w, r)
		b, _ := io.ReadAll(w.Result().Body)
		return w.Code, b, w.Header().Get("ETag")
	}
	if code, _, _ := get("", ""); code != 401 {
		t.Fatalf("sin credencial: %d", code)
	}
	code, body, etag := get(c.authz, "")
	if code != 200 || !bytes.Equal(body, content) || etag != `"`+sha+`"` {
		t.Fatalf("descarga: %d %d bytes etag %s", code, len(body), etag)
	}
	code, body, _ = get(c.authz, "bytes=100-")
	if code != 206 || !bytes.Equal(body, content[100:]) {
		t.Fatalf("Range: %d %d bytes", code, len(body))
	}
	r := httptest.NewRequest("GET", "/files/"+string(bytes.Repeat([]byte("a"), 64)), nil)
	r.Header.Set("Authorization", c.authz)
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("sha desconocido: %d", w.Code)
	}
}

// Un equipo solo puede descargar los instaladores de los despliegues que le aplican.
func TestAgentDownload_OnlyOwnDeployments(t *testing.T) {
	a := newAgentAPI(t)
	mine := a.enrolled()
	other := a.enrolled()
	sha, size, _ := a.files.Put(context.Background(), bytes.NewReader([]byte("instalador con licencia")), 1<<20)
	_ = a.store.SaveFile(ctx, store.File{SHA256: sha, Size: size, UploadedAt: time.Now()})
	app, _ := a.store.CreateApp(ctx, store.App{Name: "Conta", Platform: "windows", Source: "upload"}, a.admin.ID)
	_, _ = a.store.AddAppVersion(ctx, store.AppVersion{AppID: app.ID, Version: "1", FileSHA256: sha, InstallerType: "msi",
		InstallCommand: "x", UninstallCommand: "y", SuccessCodes: []int{0}, Detection: protocol.Detection{Type: "inventory", NameContains: "Conta"}}, true)
	g, _ := a.store.CreateGroup(ctx, "Contabilidad", "")
	gid := &g.ID
	_ = a.store.UpdateDevice(ctx, mine.id, store.DevicePatch{GroupID: &gid})
	dep, _ := a.store.CreateDeployment(ctx, store.Deployment{AppID: app.ID, Intent: "required", Reboot: "never"}, a.admin.ID)
	_ = a.store.SetDeploymentAssignments(ctx, dep.ID, []policy.Assignment{{GroupID: &g.ID}}, time.Now())
	get := func(c *agentClient) int {
		r := httptest.NewRequest("GET", "/files/"+sha, nil)
		r.Header.Set("Authorization", c.authz)
		w := httptest.NewRecorder()
		a.handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := get(mine); code != 200 {
		t.Fatalf("el equipo del despliegue: %d", code)
	}
	if code := get(other); code != 404 {
		t.Fatalf("un equipo sin ese despliegue no debe poder descargarlo: %d", code)
	}
}

// deployFileToAll crea una aplicación con ese fichero y la despliega en todos los equipos.
func (a *agentTest) deployFileToAll(sha string, size int64) {
	a.t.Helper()
	_ = a.store.SaveFile(ctx, store.File{SHA256: sha, Size: size, UploadedAt: time.Now()})
	app, _ := a.store.CreateApp(ctx, store.App{Name: "App " + sha[:6], Platform: "windows", Source: "upload"}, a.admin.ID)
	_, _ = a.store.AddAppVersion(ctx, store.AppVersion{AppID: app.ID, Version: "1", FileSHA256: sha, InstallerType: "msi",
		InstallCommand: "x", UninstallCommand: "y", SuccessCodes: []int{0}, Detection: protocol.Detection{Type: "inventory", NameContains: "x"}}, true)
	dep, _ := a.store.CreateDeployment(ctx, store.Deployment{AppID: app.ID, Intent: "required", Reboot: "never"}, a.admin.ID)
	_ = a.store.SetDeploymentAssignments(ctx, dep.ID, []policy.Assignment{{}}, time.Now())
}
