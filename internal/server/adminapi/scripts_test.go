package adminapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/auth"
)

type scriptResp struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Platform      string `json:"platform"`
	Interpreter   string `json:"interpreter"`
	TimeoutS      int    `json:"timeout_s"`
	LatestVersion int    `json:"latest_version"`
	Archived      bool   `json:"archived"`
	Parameters    []struct {
		Name     string `json:"name"`
		Required bool   `json:"required"`
	} `json:"parameters"`
}

type versionResp struct {
	Version int    `json:"version"`
	Content string `json:"content"`
}

func newScript(name, platform, interpreter, content string, params ...map[string]any) map[string]any {
	if params == nil {
		params = []map[string]any{}
	}
	return map[string]any{"name": name, "platform": platform, "interpreter": interpreter, "content": content, "parameters": params}
}

func TestScripts_CreateAndVersion(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	resp := admin.do("POST", "/scripts", newScript("Limpiar temporales", "linux", "bash", "rm -rf /tmp/mdm-*"))
	wantStatus(t, resp, 201)
	s := decode[scriptResp](t, resp)
	if s.LatestVersion != 1 || s.TimeoutS != 300 {
		t.Fatalf("creado: %+v", s)
	}
	wantStatus(t, admin.do("POST", "/scripts/"+s.ID+"/versions", map[string]any{"content": "echo v2"}), 201)
	vs := decode[[]versionResp](t, admin.do("GET", "/scripts/"+s.ID+"/versions", nil))
	if len(vs) != 2 || vs[0].Version != 2 {
		t.Fatalf("versiones: %+v", vs)
	}
	v1 := decode[versionResp](t, admin.do("GET", "/scripts/"+s.ID+"/versions/1", nil))
	if v1.Content != "rm -rf /tmp/mdm-*" {
		t.Fatalf("la versión 1 debe conservarse: %+v", v1)
	}
	got := decode[scriptResp](t, admin.do("GET", "/scripts/"+s.ID, nil))
	if got.LatestVersion != 2 {
		t.Fatalf("versión vigente: %d", got.LatestVersion)
	}
	resp = admin.do("PATCH", "/scripts/"+s.ID, map[string]any{"name": "Limpieza", "description": "Borra temporales", "timeout_s": 120,
		"parameters": []map[string]any{{"name": "RUTA", "required": true}}})
	wantStatus(t, resp, 200)
	if p := decode[scriptResp](t, resp); p.Name != "Limpieza" || p.TimeoutS != 120 || len(p.Parameters) != 1 || p.Interpreter != "bash" {
		t.Fatalf("editado: %+v", p)
	}
	for _, act := range []string{"script.created", "script.updated"} {
		if a.findAudit(act) == nil {
			t.Errorf("falta %s", act)
		}
	}
}

func TestScripts_Validation(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	bad := []map[string]any{
		newScript("", "linux", "sh", "x"),
		newScript("A", "linux", "powershell", "x"),
		newScript("B", "windows", "bash", "x"),
		newScript("C", "macos", "sh", "x"),
		newScript("D", "linux", "sh", ""),
		newScript("E", "linux", "sh", strings.Repeat("x", 257*1024)),
		newScript("F", "linux", "sh", "x", map[string]any{"name": "ruta"}),
		newScript("G", "linux", "sh", "x", map[string]any{"name": "RUTA"}, map[string]any{"name": "RUTA"}),
	}
	for i, b := range bad {
		if resp := admin.do("POST", "/scripts", b); resp.StatusCode != 400 && resp.StatusCode != 413 {
			t.Errorf("caso %d: %d", i, resp.StatusCode)
		}
	}
	for _, timeout := range []int{0, 3601} {
		b := newScript("T", "linux", "sh", "x")
		b["timeout_s"] = timeout
		wantError(t, admin.do("POST", "/scripts", b), 400, "invalid_request")
	}
	wantStatus(t, admin.do("POST", "/scripts", newScript("Único", "linux", "sh", "x")), 201)
	wantError(t, admin.do("POST", "/scripts", newScript("ÚNICO", "linux", "sh", "x")), 409, "conflict")
}

func TestScripts_ReadonlyCanReadNotWrite(t *testing.T) {
	a := newTestAPI(t)
	s := decode[scriptResp](t, a.loginAs(auth.RoleAdmin).do("POST", "/scripts", newScript("S", "linux", "sh", "true")))
	for _, role := range []auth.Role{auth.RoleReadonly, auth.RoleOperator} {
		c := a.loginAs(role)
		wantStatus(t, c.do("GET", "/scripts", nil), 200)
		wantStatus(t, c.do("GET", "/scripts/"+s.ID+"/versions/1", nil), 200)
		wantError(t, c.do("POST", "/scripts", newScript("X", "linux", "sh", "true")), 403, "forbidden")
		wantError(t, c.do("POST", "/scripts/"+s.ID+"/versions", map[string]any{"content": "x"}), 403, "forbidden")
	}
}

func runScript(c *testClient, scriptID string, version int, params map[string]string, ids ...string) (createBatchResp, int) {
	resp := c.do("POST", "/batches", map[string]any{"type": "run_script", "device_ids": ids,
		"params": map[string]any{"script_id": scriptID, "version": version, "params": params}})
	if resp.StatusCode != 201 {
		return createBatchResp{}, resp.StatusCode
	}
	return decode[createBatchResp](c.api.t, resp), 201
}

func TestRunScript_BuildsPayload(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Platform: "linux"})
	admin := a.loginAs(auth.RoleAdmin)
	s := decode[scriptResp](t, admin.do("POST", "/scripts", newScript("Copiar", "linux", "bash", "v1", map[string]any{"name": "RUTA", "required": true})))
	admin.do("POST", "/scripts/"+s.ID+"/versions", map[string]any{"content": "v2"})
	for _, tc := range []struct {
		version int
		content string
		num     int
	}{{0, "v2", 2}, {1, "v1", 1}} {
		out, code := runScript(admin, s.ID, tc.version, map[string]string{"RUTA": "/srv/datos"}, d.String())
		if code != 201 {
			t.Fatalf("versión %d: %d", tc.version, code)
		}
		c, _ := a.store.GetCommand(ctx, uuid.MustParse(out.Batch.Commands[0].ID))
		var p protocol.ScriptPayload
		_ = json.Unmarshal(c.Payload, &p)
		if p.Content != tc.content || p.Version != tc.num || p.Interpreter != "bash" || p.TimeoutS != 300 ||
			p.Env["MDM_PARAM_RUTA"] != "/srv/datos" || p.ScriptName != "Copiar" {
			t.Fatalf("carga v%d: %+v", tc.version, p)
		}
	}
}

func TestRunScript_ParamsNeverInContent(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Platform: "linux"})
	admin := a.loginAs(auth.RoleAdmin)
	s := decode[scriptResp](t, admin.do("POST", "/scripts", newScript("S", "linux", "sh", `echo "$MDM_PARAM_X"`, map[string]any{"name": "X"})))
	evil := "$(rm -rf /)"
	out, _ := runScript(admin, s.ID, 0, map[string]string{"X": evil}, d.String())
	c, _ := a.store.GetCommand(ctx, uuid.MustParse(out.Batch.Commands[0].ID))
	var p protocol.ScriptPayload
	_ = json.Unmarshal(c.Payload, &p)
	if strings.Contains(p.Content, evil) || p.Env["MDM_PARAM_X"] != evil {
		t.Fatalf("el parámetro debe ir solo en el entorno: %+v", p)
	}
	// Y el listado del lote no expone el contenido.
	b := decode[map[string]any](t, admin.do("GET", "/batches/"+out.Batch.ID, nil))
	if raw, _ := json.Marshal(b); strings.Contains(string(raw), "MDM_PARAM_X\\\"") && strings.Contains(string(raw), "echo") {
		t.Fatalf("el lote expone el contenido del script: %s", raw)
	}
}

func TestRunScript_ParamValidation(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Platform: "linux"}).String()
	admin := a.loginAs(auth.RoleAdmin)
	s := decode[scriptResp](t, admin.do("POST", "/scripts", newScript("S", "linux", "sh", "x", map[string]any{"name": "RUTA", "required": true})))
	if _, code := runScript(admin, s.ID, 0, nil, d); code != 400 {
		t.Errorf("sin el obligatorio: %d", code)
	}
	if _, code := runScript(admin, s.ID, 0, map[string]string{"RUTA": "  "}, d); code != 400 {
		t.Errorf("obligatorio vacío: %d", code)
	}
	if _, code := runScript(admin, s.ID, 0, map[string]string{"RUTA": "x", "OTRO": "y"}, d); code != 400 {
		t.Errorf("parámetro desconocido: %d", code)
	}
	if _, code := runScript(admin, s.ID, 9, map[string]string{"RUTA": "x"}, d); code != 400 {
		t.Errorf("versión inexistente: %d", code)
	}
	if _, code := runScript(admin, uuid.NewString(), 0, nil, d); code != 400 {
		t.Errorf("script inexistente: %d", code)
	}
}

func TestRunScript_SkipsOtherPlatform(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Mixto", "")
	a.seedDevice(seed{Hostname: "srv", Platform: "linux", Group: &g.ID})
	a.seedDevice(seed{Hostname: "win", Platform: "windows", Group: &g.ID})
	admin := a.loginAs(auth.RoleAdmin)
	s := decode[scriptResp](t, admin.do("POST", "/scripts", newScript("S", "linux", "sh", "true")))
	resp := admin.do("POST", "/batches", map[string]any{"type": "run_script", "group_id": g.ID, "params": map[string]any{"script_id": s.ID}})
	wantStatus(t, resp, 201)
	out := decode[createBatchResp](t, resp)
	if out.Batch.DeviceCount != 1 || len(out.Skipped) != 1 || out.Skipped[0].Hostname != "win" {
		t.Fatalf("lote mixto: %+v", out)
	}
}

func TestRunScript_ArchivedRejected(t *testing.T) {
	a := newTestAPI(t)
	d := a.seedDevice(seed{Platform: "linux"}).String()
	admin := a.loginAs(auth.RoleAdmin)
	s := decode[scriptResp](t, admin.do("POST", "/scripts", newScript("S", "linux", "sh", "true")))
	wantStatus(t, admin.do("POST", "/scripts/"+s.ID+"/archive", nil), 204)
	if _, code := runScript(admin, s.ID, 0, nil, d); code != 400 {
		t.Fatalf("archivado: %d", code)
	}
	list := decode[[]scriptResp](t, admin.do("GET", "/scripts", nil))
	if len(list) != 0 {
		t.Fatal("los archivados no aparecen por defecto")
	}
	if list := decode[[]scriptResp](t, admin.do("GET", "/scripts?archived=1", nil)); len(list) != 1 || !list[0].Archived {
		t.Fatalf("con archived=1: %+v", list)
	}
}
