package adminapi

import (
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type appResp struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Platform       string `json:"platform"`
	Source         string `json:"source"`
	PackageID      string `json:"package_id"`
	CurrentVersion *struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"current_version"`
	Versions []versionAppResp `json:"versions"`
}

type versionAppResp struct {
	ID               string `json:"id"`
	Version          string `json:"version"`
	InstallerType    string `json:"installer_type"`
	InstallCommand   string `json:"install_command"`
	UninstallCommand string `json:"uninstall_command"`
	SuccessCodes     []int  `json:"success_codes"`
	IsCurrent        bool   `json:"is_current"`
	Detection        struct {
		Type         string `json:"type"`
		NameContains string `json:"name_contains"`
		MinVersion   string `json:"min_version"`
	} `json:"detection"`
}

func (a *testAPI) uploadedFile(content string) string {
	a.t.Helper()
	sha, size, err := a.files.Put(ctx, strings.NewReader(content), 1<<20)
	if err != nil {
		a.t.Fatal(err)
	}
	_ = a.store.SaveFile(ctx, store.File{SHA256: sha, Size: size, UploadedAt: time.Now()})
	return sha
}

func newApp(name, platform, source, pkg string) map[string]any {
	return map[string]any{"name": name, "platform": platform, "source": source, "package_id": pkg}
}

func TestApps_SourcePlatformRules(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantStatus(t, admin.do("POST", "/apps", newApp("Firefox", "windows", "winget", "Mozilla.Firefox")), 201)
	wantStatus(t, admin.do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")), 201)
	wantStatus(t, admin.do("POST", "/apps", newApp("Contabilidad", "windows", "upload", "")), 201)
	for _, bad := range []map[string]any{
		newApp("A", "linux", "winget", "x.y"), newApp("B", "windows", "apt", "x"), newApp("C", "windows", "dnf", "x"),
		newApp("D", "linux", "apt", ""), newApp("E", "linux", "tienda", "x"), newApp("F", "macos", "upload", ""),
		newApp("", "linux", "apt", "x"),
	} {
		wantError(t, admin.do("POST", "/apps", bad), 400, "invalid_request")
	}
	wantError(t, admin.do("POST", "/apps", newApp("FIREFOX", "windows", "winget", "Mozilla.Firefox")), 409, "conflict")
}

func TestApps_PackageIDFormat(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	wantError(t, admin.do("POST", "/apps", newApp("X", "linux", "apt", "rm -rf /")), 400, "invalid_request")
	wantError(t, admin.do("POST", "/apps", newApp("Y", "windows", "winget", "a;b")), 400, "invalid_request")
	wantStatus(t, admin.do("POST", "/apps", newApp("Z", "linux", "dnf", "python3-pip")), 201)
	wantStatus(t, admin.do("POST", "/apps", newApp("W", "windows", "winget", "Microsoft.VisualStudioCode")), 201)
}

func TestAppVersions_DefaultsAndExe(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	app := decode[appResp](t, admin.do("POST", "/apps", newApp("Contabilidad", "windows", "upload", "")))
	sha := a.uploadedFile("msi")
	resp := admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "3.2.1", "file_sha256": sha, "installer_type": "msi"})
	wantStatus(t, resp, 201)
	v := decode[versionAppResp](t, resp)
	if v.InstallCommand != `msiexec /i "{file}" /qn /norestart` || v.UninstallCommand != `msiexec /x "{file}" /qn /norestart` ||
		len(v.SuccessCodes) != 3 || !v.IsCurrent || v.Detection.Type != "inventory" || v.Detection.NameContains != "Contabilidad" || v.Detection.MinVersion != "3.2.1" {
		t.Fatalf("valores por defecto: %+v", v)
	}
	exe := a.uploadedFile("exe")
	wantError(t, admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "3.3", "file_sha256": exe, "installer_type": "exe"}), 400, "invalid_request")
	resp = admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "3.3", "file_sha256": exe, "installer_type": "exe",
		"install_command": `"{file}" /S`, "uninstall_command": `"C:\Program Files\Conta\uninst.exe" /S`})
	wantStatus(t, resp, 201)
	got := decode[appResp](t, admin.do("GET", "/apps/"+app.ID, nil))
	if got.CurrentVersion == nil || got.CurrentVersion.Version != "3.3" || len(got.Versions) != 2 {
		t.Fatalf("la última versión pasa a vigente: %+v", got)
	}
	wantStatus(t, admin.do("POST", "/apps/"+app.ID+"/versions/"+v.ID+"/current", nil), 204)
	if got := decode[appResp](t, admin.do("GET", "/apps/"+app.ID, nil)); got.CurrentVersion.Version != "3.2.1" {
		t.Fatalf("vigente tras cambiarla: %+v", got.CurrentVersion)
	}
	wantError(t, admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "3.3", "file_sha256": exe, "installer_type": "exe",
		"install_command": "x", "uninstall_command": "y"}), 409, "conflict")
	wantError(t, admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "4", "file_sha256": strings.Repeat("a", 64), "installer_type": "msi"}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "5", "file_sha256": sha, "installer_type": "deb"}), 400, "invalid_request")
	winget := decode[appResp](t, admin.do("POST", "/apps", newApp("Firefox", "windows", "winget", "Mozilla.Firefox")))
	wantError(t, admin.do("POST", "/apps/"+winget.ID+"/versions", map[string]any{"version": "1", "file_sha256": sha, "installer_type": "msi"}), 400, "invalid_request")
}

func TestAppVersions_DetectionValidation(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	lin := decode[appResp](t, admin.do("POST", "/apps", newApp("Agente X", "linux", "upload", "")))
	deb := a.uploadedFile("deb")
	add := func(det map[string]any) int {
		return admin.do("POST", "/apps/"+lin.ID+"/versions", map[string]any{"version": "1." + string(rune('0'+len(det))), "file_sha256": deb, "installer_type": "deb", "detection": det}).StatusCode
	}
	if c := add(map[string]any{"type": "file", "path": "/opt/x"}); c != 400 {
		t.Errorf("file solo en Windows: %d", c)
	}
	if c := add(map[string]any{"type": "inventory"}); c != 400 {
		t.Errorf("inventory sin nombre: %d", c)
	}
	if c := add(map[string]any{"type": "script", "interpreter": "powershell", "script": "x"}); c != 400 {
		t.Errorf("intérprete de otra plataforma: %d", c)
	}
	if c := add(map[string]any{"type": "script", "interpreter": "sh", "script": "dpkg -s agente-x"}); c != 201 {
		t.Errorf("script válido: %d", c)
	}
	v := decode[appResp](t, admin.do("GET", "/apps/"+lin.ID, nil)).Versions[0]
	if !strings.Contains(v.UninstallCommand, "dpkg-deb -f") {
		t.Errorf("desinstalación por defecto de un deb: %q", v.UninstallCommand)
	}
}

func TestApps_RolesAndAudit(t *testing.T) {
	a := newTestAPI(t)
	wantError(t, a.loginAs(auth.RoleOperator).do("POST", "/apps", newApp("x", "linux", "apt", "x")), 403, "forbidden")
	admin := a.loginAs(auth.RoleAdmin)
	app := decode[appResp](t, admin.do("POST", "/apps", newApp("nginx", "linux", "apt", "nginx")))
	wantStatus(t, admin.do("PATCH", "/apps/"+app.ID, map[string]any{"name": "Servidor web", "publisher": "F5", "description": ""}), 200)
	wantStatus(t, admin.do("POST", "/apps/"+app.ID+"/archive", nil), 204)
	if list := decode[[]appResp](t, a.loginAs(auth.RoleReadonly).do("GET", "/apps", nil)); len(list) != 0 {
		t.Fatal("los archivados no aparecen por defecto")
	}
	for _, act := range []string{"app.created", "app.updated", "app.archived"} {
		if a.findAudit(act) == nil {
			t.Errorf("falta %s", act)
		}
	}
}

func TestFiles_ReferencedNotPurged(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	app := decode[appResp](t, admin.do("POST", "/apps", newApp("Conta", "windows", "upload", "")))
	used := a.uploadedFile("usado")
	unused := a.uploadedFile("sin uso")
	admin.do("POST", "/apps/"+app.ID+"/versions", map[string]any{"version": "1", "file_sha256": used, "installer_type": "msi"})
	shas, _ := a.store.UnreferencedFiles(ctx, time.Now().Add(time.Hour))
	if len(shas) != 1 || shas[0] != unused {
		t.Fatalf("sin referencias: %v", shas)
	}
}
