package adminapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mdm/internal/server/auth"
	"mdm/internal/server/store"
)

type assetResp struct {
	ID            string  `json:"id"`
	DeviceID      *string `json:"device_id"`
	CategoryID    string  `json:"category_id"`
	Category      string  `json:"category"`
	Name          string  `json:"name"`
	Serial        string  `json:"serial"`
	AssetTag      string  `json:"asset_tag"`
	Location      string  `json:"location"`
	Status        string  `json:"status"`
	Origin        string  `json:"origin"`
	Price         *float64
	PurchaseDate  string `json:"purchase_date"`
	DeletedReason string `json:"deleted_reason"`
	Owner         *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"owner"`
	Device *deviceResp `json:"device"`
}

func (a *testAPI) categoryID(name string) string {
	a.t.Helper()
	c, err := a.store.CategoryByName(ctx, name)
	if err != nil {
		a.t.Fatal(err)
	}
	return c.ID.String()
}

func (a *testAPI) dirUser(entraID, name, email string) store.DirectoryUser {
	a.t.Helper()
	us := []store.DirectoryUser{{EntraID: entraID, DisplayName: name, Email: email, Enabled: true}}
	// La sincronización completa marcaría como borrados a los demás: se
	// insertan uno a uno conservando los anteriores.
	prev, _, _ := a.store.ListDirectoryUsers(ctx, store.DirectoryFilter{PerPage: 500})
	for _, p := range prev {
		us = append(us, p.DirectoryUser)
	}
	if err := a.store.ApplyDirectorySync(ctx, us, time.Now()); err != nil {
		a.t.Fatal(err)
	}
	u, _ := a.store.DirectoryUserByEntraID(ctx, entraID)
	return u
}

func (a *testAPI) newAsset(c *testClient, body map[string]any) assetResp {
	a.t.Helper()
	if _, ok := body["status"]; !ok {
		body["status"] = "en_almacen"
	}
	resp := c.do("POST", "/assets", body)
	wantStatus(a.t, resp, 201)
	return decode[assetResp](a.t, resp)
}

func TestAssetsAPI_Roles(t *testing.T) {
	a := newTestAPI(t)
	ro := a.loginAs(auth.RoleReadonly)
	body := map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor", "status": "en_almacen"}
	wantError(t, ro.do("POST", "/assets", body), 403, "forbidden")
	op := a.loginAs(auth.RoleOperator)
	m := a.newAsset(op, body)
	wantStatus(t, ro.do("GET", "/assets/"+m.ID, nil), 200)
	wantError(t, op.do("DELETE", "/assets/"+m.ID, map[string]string{}), 403, "forbidden")
	wantError(t, op.do("POST", "/assets/"+m.ID+"/unlink", nil), 403, "forbidden")
	wantError(t, op.do("POST", "/asset-categories", map[string]string{"name": "X"}), 403, "forbidden")
}

func TestAssetsAPI_CreateValidatesAndLists(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana Pérez", "ana@electrans.es")
	created := a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": " Portátil Ana ", "serial": "SER-1234",
		"status": "en_uso", "owner_id": ana.ID.String(), "price": 999.9, "purchase_date": "2026-01-15"})
	if created.Name != "Portátil Ana" || created.Origin != "manual" || created.Owner == nil || created.Owner.Email != "ana@electrans.es" ||
		created.PurchaseDate != "2026-01-15" || created.Category != "Portátil" {
		t.Fatalf("creado: %+v", created)
	}
	bad := []map[string]any{
		{"category_id": a.categoryID("Monitor"), "name": "", "status": "en_uso"},
		{"category_id": a.categoryID("Monitor"), "name": "x", "status": "perdido"},
		{"category_id": "no-uuid", "name": "x", "status": "en_uso"},
		{"category_id": a.categoryID("Monitor"), "name": "x", "status": "en_uso", "purchase_date": "15/01/2026"},
		{"category_id": a.categoryID("Monitor"), "name": "x", "status": "en_uso", "price": -1},
		{"category_id": a.categoryID("Monitor"), "name": "x", "status": "en_uso", "owner_id": "00000000-0000-4000-8000-000000000000"},
	}
	for _, b := range bad {
		wantError(t, op.do("POST", "/assets", b), 400, "invalid_request")
	}
	wantError(t, op.do("POST", "/assets", map[string]any{"category_id": a.categoryID("Monitor"), "name": "dup", "status": "en_uso", "serial": "ser-1234"}), 409, "conflict")
	a.seedDevice(seed{Hostname: "PC-AGENTE", Serial: "AGT-0001"})

	page := decode[pageResp[assetResp]](t, op.do("GET", "/assets?sort=name", nil))
	if page.Total != 2 || page.Items[0].Name != "PC-AGENTE" || page.Items[0].Origin != "agent" || page.Items[0].Device == nil {
		t.Fatalf("listado: %+v", page)
	}
	if p := decode[pageResp[assetResp]](t, op.do("GET", "/assets?origin=manual", nil)); p.Total != 1 {
		t.Fatalf("manuales: %d", p.Total)
	}
	if p := decode[pageResp[assetResp]](t, op.do("GET", "/assets?owner_id="+ana.ID.String(), nil)); p.Total != 1 {
		t.Fatalf("de Ana: %d", p.Total)
	}
	wantError(t, op.do("GET", "/assets?origin=otro", nil), 400, "invalid_request")
	csv := bodyString(op.do("GET", "/assets.csv", nil))
	if !strings.Contains(csv, "Portátil Ana") || !strings.Contains(csv, "PC-AGENTE") {
		t.Fatalf("CSV: %s", csv)
	}
}

func TestAssetsAPI_SerialOfLinkedDevice409(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	a.seedDevice(seed{Hostname: "PC1", Serial: "SER-AGENT-1"})
	resp := op.do("POST", "/assets", map[string]any{"category_id": a.categoryID("Portátil"), "name": "x", "status": "en_uso", "serial": "ser-agent-1"})
	wantError(t, resp, 409, "conflict")
}

func TestAssetsAPI_TrashRestorePurge(t *testing.T) {
	a := newTestAPI(t)
	admin, op := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	m := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor A", "serial": "MON-0001", "owner_id": ana.ID.String()})

	wantStatus(t, admin.do("DELETE", "/assets/"+m.ID, map[string]string{"reason": "Roto"}), 204)
	if p := decode[pageResp[assetResp]](t, op.do("GET", "/assets", nil)); p.Total != 0 {
		t.Fatalf("sigue en el inventario: %d", p.Total)
	}
	trash := decode[pageResp[assetResp]](t, op.do("GET", "/assets?trash=1", nil))
	if trash.Total != 1 || trash.Items[0].DeletedReason != "Roto" || trash.Items[0].Owner != nil {
		t.Fatalf("papelera: %+v", trash)
	}
	wantError(t, op.do("PATCH", "/assets/"+m.ID, map[string]any{"category_id": m.CategoryID, "name": "x", "status": "en_uso"}), 404, "not_found")

	// Mientras está en la papelera, otro activo toma su número de serie.
	other := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor B", "serial": "mon-0001"})
	wantError(t, op.do("POST", "/assets/"+m.ID+"/restore", nil), 409, "conflict")
	wantStatus(t, admin.do("DELETE", "/assets/"+other.ID, map[string]string{}), 204)
	wantStatus(t, admin.do("DELETE", "/assets/"+other.ID+"/purge", nil), 204)
	resp := op.do("POST", "/assets/"+m.ID+"/restore", nil)
	wantStatus(t, resp, 200)
	if r := decode[assetResp](t, resp); r.Owner != nil || r.Serial != "MON-0001" {
		t.Fatalf("restaurado: %+v", r)
	}
	wantError(t, admin.do("DELETE", "/assets/"+m.ID+"/purge", nil), 409, "conflict") // no está en la papelera
}

func TestAssetsAPI_TrashedAssetNotLinkedOnEnroll(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	m := a.newAsset(admin, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Viejo", "serial": "LAP-0001"})
	wantStatus(t, admin.do("DELETE", "/assets/"+m.ID, map[string]string{}), 204)
	dev := a.seedDevice(seed{Hostname: "PC1", Serial: "LAP-0001"})
	got, _ := a.store.AssetByDevice(ctx, dev)
	if got.ID.String() == m.ID {
		t.Fatal("se ha enlazado un activo de la papelera")
	}
}

func TestAssetsAPI_DeleteLinked409AndLinkUnlink(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	dev := a.seedDevice(seed{Hostname: "PC1", Serial: "SER-12345"})
	auto, _ := a.store.AssetByDevice(ctx, dev)
	wantError(t, admin.do("DELETE", "/assets/"+auto.ID.String(), map[string]string{}), 409, "conflict")
	m := a.newAsset(admin, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Comprado"})
	wantStatus(t, admin.do("POST", "/assets/"+m.ID+"/link", map[string]string{"device_id": dev.String()}), 200)
	if got, _ := a.store.AssetByDevice(ctx, dev); got.ID.String() != m.ID {
		t.Fatal("no se ha enlazado")
	}
	wantStatus(t, admin.do("POST", "/assets/"+m.ID+"/unlink", nil), 200)
	wantError(t, admin.do("POST", "/assets/"+m.ID+"/unlink", nil), 409, "conflict")
}

func TestAssetsAPI_Bulk(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	ana := a.dirUser("e1", "Ana", "ana@electrans.es")
	x := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "M1"})
	y := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "M2"})
	resp := op.do("POST", "/assets/bulk", map[string]any{"ids": []string{x.ID, y.ID}, "status": "en_uso", "owner_id": ana.ID.String(), "location": "Planta 2"})
	wantStatus(t, resp, 200)
	for _, id := range []string{x.ID, y.ID} {
		got := decode[assetResp](t, op.do("GET", "/assets/"+id, nil))
		if got.Status != "en_uso" || got.Owner == nil || got.Owner.ID != ana.ID.String() {
			t.Fatalf("tras el cambio masivo: %+v", got)
		}
	}
	wantError(t, op.do("POST", "/assets/bulk", map[string]any{"ids": []string{x.ID}, "status": "perdido"}), 400, "invalid_request")
	wantError(t, op.do("POST", "/assets/bulk", map[string]any{"ids": []string{}}), 400, "invalid_request")
	// owner_id vacío quita el dueño.
	wantStatus(t, op.do("POST", "/assets/bulk", map[string]any{"ids": []string{x.ID}, "owner_id": ""}), 200)
	if got := decode[assetResp](t, op.do("GET", "/assets/"+x.ID, nil)); got.Owner != nil {
		t.Fatal("no ha quitado el dueño")
	}
}

func (c *testClient) postCSV(path, body string) *http.Response {
	c.api.t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "text/csv")
	c.authorize(r)
	w := httptest.NewRecorder()
	c.api.handler.ServeHTTP(w, r)
	return w.Result()
}

type importResp struct {
	Rows []struct {
		Line   int      `json:"line"`
		Errors []string `json:"errors"`
	} `json:"rows"`
	Valid   int `json:"valid"`
	Created int `json:"created"`
}

func TestAssetsImport_DryRunErrorsByLine(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	a.dirUser("e1", "Ana", "ana@electrans.es")
	csv := "\ufeffcategoria;nombre;marca;modelo;serie;etiqueta;estado;persona;departamento;ubicacion;precio;fecha_compra;garantia;pedido;proveedor;notas\n" +
		"Monitor;Monitor 1;Dell;P2422H;MON-1001;;en_almacen;;;;150,50;2026-01-10;;;;\n" +
		"Nave espacial;X;;;;;en_almacen;;;;;;;;;\n" +
		"Monitor;Monitor 2;;;;;en_uso;nadie@electrans.es;;;;;;;;\n" +
		"Monitor;Monitor 3;;;;;en_almacen;;;;;10/01/2026;;;;\n" +
		"Monitor;Monitor 4;;;mon-1001;;en_almacen;;;;;;;;;\n"
	resp := op.postCSV("/assets/import?dry_run=1", csv)
	wantStatus(t, resp, 200)
	got := decode[importResp](t, resp)
	errs := map[int]string{}
	for _, r := range got.Rows {
		errs[r.Line] = strings.Join(r.Errors, " ")
	}
	if errs[2] != "" || !strings.Contains(errs[3], "categoría") || !strings.Contains(errs[4], "persona") ||
		!strings.Contains(errs[5], "fecha") || !strings.Contains(errs[6], "serie") || got.Valid != 1 {
		t.Fatalf("errores: %+v", got)
	}
	// Con errores no se guarda nada.
	wantStatus(t, op.postCSV("/assets/import", csv), 400)
	if p := decode[pageResp[assetResp]](t, op.do("GET", "/assets", nil)); p.Total != 0 {
		t.Fatalf("se ha guardado algo: %d", p.Total)
	}
}

func TestAssetsImport_SavesAll(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	a.dirUser("e1", "Ana", "ana@electrans.es")
	csv := "categoria,nombre,marca,modelo,serie,etiqueta,estado,persona,departamento,ubicacion,precio,fecha_compra,garantia,pedido,proveedor,notas\n" +
		"Monitor,Monitor 1,Dell,P2422H,MON-1001,EL-1,en_uso,ANA@electrans.es,TIC,Planta 1,150.5,2026-01-10,2029-01-10,PO-1,Proveedor SA,\n" +
		"portátil,Portátil 2,,,,,,,,,,,,,,\n"
	resp := op.postCSV("/assets/import", csv)
	wantStatus(t, resp, 200)
	if got := decode[importResp](t, resp); got.Created != 2 {
		t.Fatalf("creados: %+v", got)
	}
	page := decode[pageResp[assetResp]](t, op.do("GET", "/assets?sort=name", nil))
	if page.Total != 2 || page.Items[0].Owner == nil || page.Items[1].Status != "en_almacen" || page.Items[1].Category != "Portátil" {
		t.Fatalf("importados: %+v", page.Items)
	}
	tpl := op.do("GET", "/assets/import/template.csv", nil)
	wantStatus(t, tpl, 200)
	if !strings.HasPrefix(bodyString(tpl), "\ufeffcategoria;nombre") {
		t.Fatal("plantilla")
	}
}

func (c *testClient) uploadTo(path, name string, content []byte) *http.Response {
	c.api.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", name)
	_, _ = part.Write(content)
	mw.Close()
	r := httptest.NewRequest("POST", path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	c.authorize(r)
	w := httptest.NewRecorder()
	c.api.handler.ServeHTTP(w, r)
	return w.Result()
}

func TestDocuments_TypeChecksAndDownload(t *testing.T) {
	a := newTestAPI(t)
	op, ro := a.loginAs(auth.RoleOperator), a.loginAs(auth.RoleReadonly)
	m := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor"})
	path := "/assets/" + m.ID + "/documents"
	pdf := []byte("%PDF-1.7\nfactura")
	wantError(t, ro.uploadTo(path, "factura.pdf", pdf), 403, "forbidden")
	wantError(t, op.uploadTo(path, "pagina.html", []byte("<script>alert(1)</script>")), 400, "invalid_request")
	wantError(t, op.uploadTo(path, "falso.pdf", []byte("no soy un pdf")), 400, "invalid_request")
	resp := op.uploadTo(path, `C:\fakepath\Factura 2026.pdf`, pdf)
	wantStatus(t, resp, 201)
	doc := decode[struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}](t, resp)
	if doc.Filename != "Factura 2026.pdf" || doc.Size != int64(len(pdf)) {
		t.Fatalf("documento: %+v", doc)
	}
	wantStatus(t, op.uploadTo(path, "foto.png", append([]byte("\x89PNG\r\n\x1a\n"), 0, 0)), 201)
	wantStatus(t, op.uploadTo(path, "notas.txt", []byte("texto plano")), 201)
	list := decode[[]struct {
		ID string `json:"id"`
	}](t, ro.do("GET", path, nil))
	if len(list) != 3 {
		t.Fatalf("documentos: %d", len(list))
	}
	dl := ro.do("GET", path+"/"+doc.ID, nil)
	wantStatus(t, dl, 200)
	if cd := dl.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "Factura") {
		t.Fatalf("Content-Disposition: %q", cd)
	}
	if dl.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("tipo: %q", dl.Header.Get("Content-Type"))
	}
	if b, _ := io.ReadAll(dl.Body); !bytes.Equal(b, pdf) {
		t.Fatal("contenido")
	}
	wantStatus(t, op.do("DELETE", path+"/"+doc.ID, nil), 204)
	wantError(t, ro.do("GET", path+"/"+doc.ID, nil), 404, "not_found")

	// Los ficheros de documentos no los borra la limpieza de instaladores sin uso.
	shas, err := a.store.UnreferencedFiles(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(shas) != 1 { // solo el del documento borrado
		t.Fatalf("ficheros sin uso: %v", shas)
	}
}

func TestCategoriesAPI(t *testing.T) {
	a := newTestAPI(t)
	admin := a.loginAs(auth.RoleAdmin)
	list := decode[[]struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Assets int    `json:"assets"`
	}](t, admin.do("GET", "/asset-categories", nil))
	if len(list) != 11 {
		t.Fatalf("categorías: %d", len(list))
	}
	resp := admin.do("POST", "/asset-categories", map[string]string{"name": "Proyector"})
	wantStatus(t, resp, 201)
	c := decode[struct {
		ID string `json:"id"`
	}](t, resp)
	wantError(t, admin.do("POST", "/asset-categories", map[string]string{"name": "proyector"}), 409, "conflict")
	wantStatus(t, admin.do("PATCH", "/asset-categories/"+c.ID, map[string]string{"name": "Proyectores"}), 200)
	a.newAsset(admin, map[string]any{"category_id": c.ID, "name": "Epson"})
	wantError(t, admin.do("DELETE", "/asset-categories/"+c.ID, nil), 409, "conflict")
}

func TestAssetHistory(t *testing.T) {
	a := newTestAPI(t)
	op := a.loginAs(auth.RoleOperator)
	m := a.newAsset(op, map[string]any{"category_id": a.categoryID("Monitor"), "name": "Monitor"})
	op.do("PATCH", "/assets/"+m.ID, map[string]any{"category_id": m.CategoryID, "name": "Monitor 2", "status": "en_uso"})
	h := decode[[]struct {
		Action string `json:"action"`
	}](t, op.do("GET", "/assets/"+m.ID+"/history", nil))
	if len(h) != 2 || h[0].Action != "asset.updated" || h[1].Action != "asset.created" {
		t.Fatalf("historial: %+v", h)
	}
}

func TestAssetsAPI_AndroidPlatformFilter(t *testing.T) {
	a := newTestAPI(t)
	ro := a.loginAs(auth.RoleReadonly)
	a.seedDevice(seed{Hostname: "PC1"})
	resp := ro.do("GET", "/assets?origin=agent&platform=android", nil)
	wantStatus(t, resp, 200)
	if p := decode[pageResp[assetResp]](t, resp); p.Total != 0 {
		t.Fatalf("móviles: %d", p.Total)
	}
}

func TestAssetsAPI_Merge(t *testing.T) {
	a := newTestAPI(t)
	admin, op := a.loginAs(auth.RoleAdmin), a.loginAs(auth.RoleOperator)
	dev := a.seedDevice(seed{Hostname: "PC-MERGE", Serial: "AGENTE-1"})
	agentAsset, _ := a.store.AssetByDevice(ctx, dev)
	m := a.newAsset(op, map[string]any{"category_id": a.categoryID("Portátil"), "name": "Portátil de Ana", "asset_tag": "EL-7", "location": "Bilbao"})
	body := map[string]any{"keep_id": agentAsset.ID.String(), "other_id": m.ID, "from_other": []string{"name", "asset_tag", "location"}}
	wantError(t, op.do("POST", "/assets/merge", body), 403, "forbidden")
	wantError(t, admin.do("POST", "/assets/merge", map[string]any{"keep_id": m.ID, "other_id": m.ID}), 400, "invalid_request")
	wantError(t, admin.do("POST", "/assets/merge", map[string]any{"keep_id": m.ID, "other_id": agentAsset.ID.String(), "from_other": []string{"precio"}}), 400, "invalid_request")
	got := decode[assetResp](t, admin.do("POST", "/assets/merge", body))
	if got.ID != agentAsset.ID.String() || got.Name != "Portátil de Ana" || got.AssetTag != "EL-7" || got.Location != "Bilbao" {
		t.Fatalf("fusionado: %+v", got)
	}
	if e := a.findAudit("asset.merged"); e == nil || !strings.Contains(string(e.Details), m.ID) {
		t.Fatalf("auditoría: %+v", e)
	}
	wantError(t, admin.do("POST", "/assets/merge", body), 404, "not_found")

	other := a.seedDevice(seed{Hostname: "PC-OTRO", Serial: "AGENTE-2"})
	oa, _ := a.store.AssetByDevice(ctx, other)
	wantError(t, admin.do("POST", "/assets/merge", map[string]any{"keep_id": agentAsset.ID.String(), "other_id": oa.ID.String()}), 409, "conflict")
}
