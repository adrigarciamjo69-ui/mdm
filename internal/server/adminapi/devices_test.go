package adminapi

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type deviceResp struct {
	ID                   string         `json:"id"`
	Hostname             string         `json:"hostname"`
	Platform             string         `json:"platform"`
	OSName               string         `json:"os_name"`
	OSVersion            string         `json:"os_version"`
	Serial               string         `json:"serial"`
	Status               string         `json:"status"`
	Compliance           string         `json:"compliance"`
	GroupID              *string        `json:"group_id"`
	GroupName            string         `json:"group_name"`
	AssignedUser         string         `json:"assigned_user"`
	Tags                 []string       `json:"tags"`
	LastSeenAt           *time.Time     `json:"last_seen_at"`
	Inventory            map[string]any `json:"inventory"`
	InventoryCollectedAt *time.Time     `json:"inventory_collected_at"`
}

type pageResp[T any] struct {
	Items   []T `json:"items"`
	Total   int `json:"total"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

func list(t *testing.T, c *testClient, query string) pageResp[deviceResp] {
	t.Helper()
	resp := c.do("GET", "/devices?"+query, nil)
	wantStatus(t, resp, 200)
	return decode[pageResp[deviceResp]](t, resp)
}

func names(p pageResp[deviceResp]) string {
	var out []string
	for _, d := range p.Items {
		out = append(out, d.Hostname)
	}
	return strings.Join(out, ",")
}

func TestListDevices_Pagination(t *testing.T) {
	a := newTestAPI(t)
	for i := 0; i < 30; i++ {
		a.seedDevice(seed{})
	}
	ro := a.loginAs(auth.RoleReadonly)
	p1 := list(t, ro, "")
	if len(p1.Items) != 25 || p1.Total != 30 || p1.Page != 1 || p1.PerPage != 25 {
		t.Fatalf("página 1: %d elementos, total %d, page %d, per_page %d", len(p1.Items), p1.Total, p1.Page, p1.PerPage)
	}
	if p2 := list(t, ro, "page=2"); len(p2.Items) != 5 || p2.Total != 30 {
		t.Fatalf("página 2: %d elementos", len(p2.Items))
	}
	if p9 := list(t, ro, "page=9"); len(p9.Items) != 0 || p9.Total != 30 {
		t.Fatalf("página fuera de rango: %d elementos", len(p9.Items))
	}
}

func TestListDevices_EmptyIsArray(t *testing.T) {
	a := newTestAPI(t)
	body := bodyString(a.loginAs(auth.RoleReadonly).do("GET", "/devices", nil))
	if !strings.Contains(body, `"items":[]`) {
		t.Fatalf("listado vacío: %s", body)
	}
}

func TestListDevices_PerPageCapped(t *testing.T) {
	a := newTestAPI(t)
	ro := a.loginAs(auth.RoleReadonly)
	if p := list(t, ro, "per_page=500"); p.PerPage != 200 {
		t.Errorf("per_page=500 → %d", p.PerPage)
	}
	if p := list(t, ro, "per_page=0"); p.PerPage != 25 {
		t.Errorf("per_page=0 → %d", p.PerPage)
	}
	wantError(t, ro.do("GET", "/devices?page=abc", nil), 400, "invalid_request")
}

func TestListDevices_Search(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "PC-CONTA-07", Serial: "ABC123", User: "ana.garcia"})
	a.seedDevice(seed{Hostname: "srv-web", Serial: "ZZZ999", User: "luis"})
	ro := a.loginAs(auth.RoleReadonly)
	for q, want := range map[string]string{"conta": "PC-CONTA-07", "abc1": "PC-CONTA-07", "GARCIA": "PC-CONTA-07", "web": "srv-web", "nada": ""} {
		if got := names(list(t, ro, "q="+q)); got != want {
			t.Errorf("q=%s → %q, se esperaba %q", q, got, want)
		}
	}
}

func TestListDevices_SearchEscapesWildcards(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "PC-1"})
	a.seedDevice(seed{Hostname: "100%_real"})
	ro := a.loginAs(auth.RoleReadonly)
	if got := names(list(t, ro, "q="+url.QueryEscape("%"))); got != "100%_real" {
		t.Errorf("q=%% → %q", got)
	}
	if got := names(list(t, ro, "q=_")); got != "100%_real" {
		t.Errorf("q=_ → %q", got)
	}
}

func TestListDevices_Filters(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	a.seedDevice(seed{Hostname: "online", Platform: "windows", OSVersion: "11", SeenAgo: 10 * time.Second, Group: &g.ID, Tags: []string{"vip"}})
	a.seedDevice(seed{Hostname: "offline", Platform: "linux", OSVersion: "24.04", SeenAgo: time.Hour})
	a.seedDevice(seed{Hostname: "inactive", Platform: "linux", OSVersion: "22.04", SeenAgo: 31 * 24 * time.Hour})
	a.seedDevice(seed{Hostname: "revoked", Platform: "windows", OSVersion: "10", SeenAgo: 10 * time.Second, Revoked: true})
	a.seedDevice(seed{Hostname: "never", Platform: "windows", OSVersion: "11"})
	ro := a.loginAs(auth.RoleReadonly)
	cases := map[string]string{
		"platform=linux":                "inactive,offline",
		"platform=windows":              "never,online,revoked",
		"group_id=" + g.ID.String():     "online",
		"os_version=11":                 "never,online",
		"tag=vip":                       "online",
		"status=online":                 "online",
		"status=offline":                "never,offline",
		"status=inactive":               "inactive",
		"status=revoked":                "revoked",
		"platform=linux&status=offline": "offline",
	}
	for q, want := range cases {
		if got := names(list(t, ro, q)); got != want {
			t.Errorf("%s → %q, se esperaba %q", q, got, want)
		}
	}
	// El estado de cada fila coincide con el filtro que la devuelve.
	for _, d := range list(t, ro, "").Items {
		want := d.Hostname
		if d.Hostname == "never" {
			want = "offline"
		}
		if d.Status != want {
			t.Errorf("%s: status %s", d.Hostname, d.Status)
		}
	}
	if d := list(t, ro, "status=online").Items[0]; d.GroupName != "Ventas" || d.GroupID == nil || len(d.Tags) != 1 {
		t.Errorf("grupo o etiquetas en el listado: %+v", d)
	}
	for _, bad := range []string{"platform=macos", "status=raro", "group_id=no-uuid"} {
		wantError(t, ro.do("GET", "/devices?"+bad, nil), 400, "invalid_request")
	}
}

func TestListDevices_Sort(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "beta", SeenAgo: time.Minute})
	a.seedDevice(seed{Hostname: "Alfa", SeenAgo: time.Hour})
	a.seedDevice(seed{Hostname: "gamma"})
	ro := a.loginAs(auth.RoleReadonly)
	cases := map[string]string{
		"":                             "Alfa,beta,gamma",
		"sort=hostname&order=desc":     "gamma,beta,Alfa",
		"sort=last_seen_at&order=desc": "beta,Alfa,gamma",
		"sort=last_seen_at&order=asc":  "Alfa,beta,gamma",
		"sort=enrolled_at&order=asc":   "beta,Alfa,gamma",
	}
	for q, want := range cases {
		if got := names(list(t, ro, q)); got != want {
			t.Errorf("%q → %q, se esperaba %q", q, got, want)
		}
	}
	wantError(t, ro.do("GET", "/devices?sort=password", nil), 400, "invalid_request")
	wantError(t, ro.do("GET", "/devices?order=lateral", nil), 400, "invalid_request")
}

func TestListDevices_IssueFilters(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: "lleno", SeenAgo: time.Minute, Inventory: invWith(5, "on")})
	a.seedDevice(seed{Hostname: "sin-cifrar", SeenAgo: time.Minute, Inventory: invWith(50, "off")})
	a.seedDevice(seed{Hostname: "ausente", SeenAgo: 8 * 24 * time.Hour, Inventory: invWith(50, "on")})
	a.seedDevice(seed{Hostname: "bien", SeenAgo: time.Minute, Inventory: invWith(50, "on")})
	ro := a.loginAs(auth.RoleReadonly)
	for q, want := range map[string]string{"issue=low_disk": "lleno", "issue=unencrypted": "sin-cifrar", "issue=stale": "ausente"} {
		if got := names(list(t, ro, q)); got != want {
			t.Errorf("%s → %q, se esperaba %q", q, got, want)
		}
	}
	wantError(t, ro.do("GET", "/devices?issue=otra", nil), 400, "invalid_request")
}

func TestDevice_Detail(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Hostname: "PC-1", SeenAgo: time.Minute, Inventory: invWith(50, "on")})
	bare := a.seedDevice(seed{Hostname: "PC-2"})
	ro := a.loginAs(auth.RoleReadonly)
	resp := ro.do("GET", "/devices/"+id.String(), nil)
	wantStatus(t, resp, 200)
	d := decode[deviceResp](t, resp)
	if d.Hostname != "PC-1" || d.Status != "online" || d.Inventory == nil || d.InventoryCollectedAt == nil {
		t.Fatalf("ficha: %+v", d)
	}
	if sys, _ := d.Inventory["system"].(map[string]any); sys["kernel"] != "6.8" {
		t.Errorf("inventario: %v", d.Inventory)
	}
	body := bodyString(ro.do("GET", "/devices/"+bare.String(), nil))
	if !strings.Contains(body, `"inventory":null`) || strings.Contains(body, "credential") {
		t.Errorf("ficha sin inventario: %s", body)
	}
	wantError(t, ro.do("GET", "/devices/"+uuid.NewString(), nil), 404, "not_found")
	wantError(t, ro.do("GET", "/devices/no-uuid", nil), 400, "invalid_request")
}

func TestDevice_Software(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{Inventory: invWith(50, "on")})
	ro := a.loginAs(auth.RoleReadonly)
	type sw struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	p := decode[pageResp[sw]](t, ro.do("GET", "/devices/"+id.String()+"/software?per_page=2", nil))
	if p.Total != 3 || len(p.Items) != 2 || p.Items[0].Name != "7-Zip" || p.Items[1].Name != "Firefox" {
		t.Fatalf("página: %+v", p)
	}
	p = decode[pageResp[sw]](t, ro.do("GET", "/devices/"+id.String()+"/software?q=fire", nil))
	if p.Total != 1 || p.Items[0].Name != "Firefox" {
		t.Fatalf("búsqueda: %+v", p)
	}
	wantError(t, ro.do("GET", "/devices/"+uuid.NewString()+"/software", nil), 404, "not_found")
}

func TestDevice_Activity(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{})
	now := a.clock.Now()
	for _, ago := range []time.Duration{3 * time.Hour, 3 * time.Hour, time.Minute, 91 * 24 * time.Hour} {
		_ = a.store.RecordCheckin(ctx, id, store.CheckinParams{At: now.Add(-ago)})
	}
	type point struct {
		Hour     time.Time `json:"hour"`
		Checkins int       `json:"checkins"`
	}
	pts := decode[[]point](t, a.loginAs(auth.RoleReadonly).do("GET", "/devices/"+id.String()+"/activity", nil))
	if len(pts) != 2 || !pts[0].Hour.Before(pts[1].Hour) || pts[0].Checkins != 2 || pts[1].Checkins != 1 {
		t.Fatalf("actividad: %+v", pts)
	}
}

func TestDevice_Patch(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	id := a.seedDevice(seed{User: "antes", Tags: []string{"x"}})
	op := a.loginAs(auth.RoleOperator)
	path := "/devices/" + id.String()

	resp := op.do("PATCH", path, map[string]any{"group_id": g.ID, "assigned_user": " ana ", "tags": []string{"vip"}})
	wantStatus(t, resp, 200)
	d := decode[deviceResp](t, resp)
	if d.GroupID == nil || *d.GroupID != g.ID.String() || d.GroupName != "Ventas" || d.AssignedUser != "ana" || len(d.Tags) != 1 || d.Tags[0] != "vip" {
		t.Fatalf("tras editar: %+v", d)
	}
	// Un campo ausente no se toca; group_id null quita el grupo.
	d = decode[deviceResp](t, op.do("PATCH", path, `{"group_id":null}`))
	if d.GroupID != nil || d.AssignedUser != "ana" || len(d.Tags) != 1 {
		t.Fatalf("tras quitar el grupo: %+v", d)
	}
	d = decode[deviceResp](t, op.do("PATCH", path, `{"assigned_user":""}`))
	if d.AssignedUser != "" || len(d.Tags) != 1 {
		t.Fatalf("tras vaciar el usuario: %+v", d)
	}
	wantError(t, a.loginAs(auth.RoleReadonly).do("PATCH", path, `{"assigned_user":"x"}`), 403, "forbidden")
	wantError(t, op.do("PATCH", path, `{"group_id":"`+uuid.NewString()+`"}`), 400, "invalid_request")
	wantError(t, op.do("PATCH", path, `{"hostname":"otro"}`), 400, "invalid_request")
	wantError(t, op.do("PATCH", path, `{"tags":"no-lista"}`), 400, "invalid_request")
	wantError(t, op.do("PATCH", "/devices/"+uuid.NewString(), `{"assigned_user":"x"}`), 404, "not_found")
}

func TestDevice_PatchTagsNormalized(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{})
	op := a.loginAs(auth.RoleOperator)
	d := decode[deviceResp](t, op.do("PATCH", "/devices/"+id.String(), map[string]any{"tags": []string{" Ventas ", "ventas", "", "B"}}))
	if strings.Join(d.Tags, ",") != "ventas,b" {
		t.Fatalf("etiquetas: %v", d.Tags)
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	wantError(t, op.do("PATCH", "/devices/"+id.String(), map[string]any{"tags": many}), 400, "invalid_request")
	wantError(t, op.do("PATCH", "/devices/"+id.String(), map[string]any{"tags": []string{strings.Repeat("a", 33)}}), 400, "invalid_request")
}

func TestDevice_RevokeAndDelete_AdminOnly(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{SeenAgo: time.Minute})
	op, admin := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleAdmin)
	wantError(t, op.do("POST", "/devices/"+id.String()+"/revoke", nil), 403, "forbidden")
	wantError(t, op.do("DELETE", "/devices/"+id.String(), nil), 403, "forbidden")

	wantStatus(t, admin.do("POST", "/devices/"+id.String()+"/revoke", nil), 204)
	if d, _ := a.store.DeviceByID(ctx, id); d.RevokedAt == nil {
		t.Fatal("no quedó revocado")
	}
	if d := decode[deviceResp](t, admin.do("GET", "/devices/"+id.String(), nil)); d.Status != "revoked" {
		t.Fatalf("estado tras revocar: %s", d.Status)
	}
	wantStatus(t, admin.do("DELETE", "/devices/"+id.String(), nil), 204)
	wantError(t, admin.do("GET", "/devices/"+id.String(), nil), 404, "not_found")
	wantError(t, admin.do("DELETE", "/devices/"+id.String(), nil), 404, "not_found")
	wantError(t, admin.do("POST", "/devices/"+id.String()+"/revoke", nil), 404, "not_found")
}

func TestDevice_DeleteCascades(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{SeenAgo: time.Minute, Inventory: invWith(50, "on")})
	admin := a.loginAs(auth.RoleAdmin)
	wantStatus(t, admin.do("DELETE", "/devices/"+id.String(), nil), 204)
	if _, _, err := a.store.DeviceInventory(ctx, id); err != store.ErrNotFound {
		t.Errorf("inventario: %v", err)
	}
	if sw, n, _ := a.store.ListSoftware(ctx, id, "", 1, 10); n != 0 || len(sw) != 0 {
		t.Errorf("software: %d", n)
	}
	if act, _ := a.store.DeviceActivity(ctx, id, time.Time{}); len(act) != 0 {
		t.Errorf("actividad: %d", len(act))
	}
	e := a.findAudit("device.deleted")
	if e == nil || e.TargetID != id.String() || !strings.Contains(string(e.Details), "hostname") {
		t.Errorf("auditoría tras borrar: %+v", e)
	}
}

func TestAudit_DeviceActions(t *testing.T) {
	a := newTestAPI(t)
	id := a.seedDevice(seed{})
	admin := a.loginAs(auth.RoleAdmin)
	admin.do("PATCH", "/devices/"+id.String(), `{"assigned_user":"ana","tags":["a"]}`)
	admin.do("POST", "/devices/"+id.String()+"/revoke", nil)
	upd := a.findAudit("device.updated")
	if upd == nil || upd.TargetID != id.String() || !strings.Contains(string(upd.Details), "assigned_user") || !strings.Contains(string(upd.Details), "tags") {
		t.Errorf("device.updated: %+v", upd)
	}
	if rev := a.findAudit("device.revoked"); rev == nil || rev.ActorEmail != admin.admin.Email {
		t.Errorf("device.revoked: %+v", rev)
	}
}

func TestCSV_ContentAndHeaders(t *testing.T) {
	a := newTestAPI(t)
	g, _ := a.store.CreateGroup(ctx, "Ventas", "")
	a.seedDevice(seed{Hostname: "PC-1", Platform: "windows", OSName: "Windows 11 Pro", OSVersion: "10.0.26100", Serial: "S1", User: "ana", Tags: []string{"a", "b"}, Group: &g.ID, SeenAgo: time.Minute})
	a.seedDevice(seed{Hostname: "srv", Platform: "linux"})
	ro := a.loginAs(auth.RoleReadonly)
	resp := ro.do("GET", "/devices.csv?platform=windows", nil)
	wantStatus(t, resp, 200)
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Errorf("content-type: %s", ct)
	}
	wantName := "dispositivos-" + a.clock.Now().Format("2006-01-02") + ".csv"
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, wantName) || !strings.HasPrefix(cd, "attachment") {
		t.Errorf("content-disposition: %s", cd)
	}
	body := bodyString(resp)
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Error("falta el BOM")
	}
	lines := strings.Split(strings.TrimRight(strings.TrimPrefix(body, "\xEF\xBB\xBF"), "\r\n"), "\r\n")
	if lines[0] != "Nombre,Plataforma,Sistema,Versión,Serie,Fabricante,Modelo,Estado,Grupo,Usuario,Etiquetas,Última conexión,Inscrito" {
		t.Errorf("cabecera: %s", lines[0])
	}
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "PC-1,Windows,Windows 11 Pro,10.0.26100,S1,,,En línea,Ventas,ana,\"a, b\",") {
		t.Errorf("filas: %q", lines[1:])
	}
	if e := a.findAudit("devices.exported"); e == nil || e.ActorEmail != ro.admin.Email || !strings.Contains(string(e.Details), `"count": 1`) {
		t.Errorf("auditoría de exportación: %+v", e)
	}
}

func TestCSV_NotPaginated(t *testing.T) {
	a := newTestAPI(t)
	for i := 0; i < 230; i++ {
		a.seedDevice(seed{})
	}
	body := bodyString(a.loginAs(auth.RoleReadonly).do("GET", "/devices.csv", nil))
	if n := strings.Count(body, "\r\n"); n != 231 {
		t.Fatalf("líneas: %d", n)
	}
}

func TestCSV_FormulaInjectionNeutralized(t *testing.T) {
	a := newTestAPI(t)
	a.seedDevice(seed{Hostname: `=HYPERLINK("http://x")`, User: "+34 600", Tags: []string{"@todos"}, Serial: "-1"})
	a.seedDevice(seed{Hostname: "normal-pc", User: "ana"})
	body := bodyString(a.loginAs(auth.RoleReadonly).do("GET", "/devices.csv", nil))
	for _, want := range []string{`"'=HYPERLINK(""http://x"")"`, `'+34 600`, `'@todos`, `,'-1,`, "normal-pc,"} {
		if !strings.Contains(body, want) {
			t.Errorf("falta %s en:\n%s", want, body)
		}
	}
	if strings.Contains(body, "'normal-pc") || strings.Contains(body, ",=") || strings.Contains(body, ",+") || strings.Contains(body, ",@") {
		t.Errorf("celdas mal neutralizadas:\n%s", body)
	}
}
