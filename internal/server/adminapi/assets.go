package adminapi

import (
	"encoding/csv"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const dateLayout = "2006-01-02"

type ownerJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// Department es el de Entra ID: si no está vacío, manda sobre el del activo.
	Department string `json:"department"`
}

type assetJSON struct {
	ID            string      `json:"id"`
	DeviceID      *string     `json:"device_id"`
	Origin        string      `json:"origin"`
	CategoryID    string      `json:"category_id"`
	Category      string      `json:"category"`
	CategoryAuto  bool        `json:"category_auto"`
	Name          string      `json:"name"`
	Brand         string      `json:"brand"`
	Model         string      `json:"model"`
	Serial        string      `json:"serial"`
	AssetTag      string      `json:"asset_tag"`
	Status        string      `json:"status"`
	Owner         *ownerJSON  `json:"owner"`
	Department    string      `json:"department"`
	Location      string      `json:"location"`
	Price         *float64    `json:"price"`
	PurchaseDate  string      `json:"purchase_date"`
	WarrantyUntil string      `json:"warranty_until"`
	PurchaseOrder string      `json:"purchase_order"`
	Supplier      string      `json:"supplier"`
	Notes         string      `json:"notes"`
	DeletedAt     *time.Time  `json:"deleted_at"`
	DeletedReason string      `json:"deleted_reason"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Device        *deviceJSON `json:"device"`
	// OwnerLocked y DepartmentLocked: la persona y el departamento no siguen
	// al usuario con sesión en el equipo.
	OwnerLocked      bool `json:"owner_locked"`
	DepartmentLocked bool `json:"department_locked"`
}

func dateStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(dateLayout)
}

func (a *api) assetToJSON(r store.AssetRow) assetJSON {
	x := r.Asset
	out := assetJSON{ID: x.ID.String(), Origin: "manual", CategoryID: x.CategoryID.String(), Category: x.CategoryName,
		CategoryAuto: x.CategoryAuto, Name: x.Name, Brand: x.Brand, Model: x.Model, Serial: x.Serial, AssetTag: x.AssetTag,
		Status: x.Status, Department: x.Department, Location: x.Location, Price: x.Price,
		PurchaseDate: dateStr(x.PurchaseDate), WarrantyUntil: dateStr(x.WarrantyUntil), PurchaseOrder: x.PurchaseOrder,
		Supplier: x.Supplier, Notes: x.Notes, DeletedAt: x.DeletedAt, DeletedReason: x.DeletedReason,
		CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt, OwnerLocked: x.OwnerLocked, DepartmentLocked: x.DepartmentLocked}
	if x.DeviceID != nil {
		s := x.DeviceID.String()
		out.DeviceID, out.Origin = &s, "agent"
	}
	if x.OwnerID != nil {
		out.Owner = &ownerJSON{ID: x.OwnerID.String(), Name: r.OwnerName, Email: r.OwnerEmail, Department: r.OwnerDepartment}
	}
	if r.Device != nil {
		d := a.deviceToJSON(*r.Device)
		out.Device = &d
	}
	return out
}

// assetInputJSON es el cuerpo de alta y edición de un activo.
type assetInputJSON struct {
	CategoryID    string   `json:"category_id"`
	Name          string   `json:"name"`
	Brand         string   `json:"brand"`
	Model         string   `json:"model"`
	Serial        string   `json:"serial"`
	AssetTag      string   `json:"asset_tag"`
	Status        string   `json:"status"`
	OwnerID       string   `json:"owner_id"`
	Department    string   `json:"department"`
	Location      string   `json:"location"`
	Price         *float64 `json:"price"`
	PurchaseDate  string   `json:"purchase_date"`
	WarrantyUntil string   `json:"warranty_until"`
	PurchaseOrder string   `json:"purchase_order"`
	Supplier      string   `json:"supplier"`
	Notes         string   `json:"notes"`
	// Ausentes valen false: la persona y el departamento siguen la sesión.
	OwnerLocked      bool `json:"owner_locked"`
	DepartmentLocked bool `json:"department_locked"`
}

const (
	maxAssetText  = 200
	maxAssetNotes = 4000
)

func parseOptDate(v string) (*time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true
	}
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		return nil, false
	}
	return &t, true
}

// toInput valida el cuerpo y lo convierte. Devuelve el mensaje de error para
// el usuario si algo no vale.
func (a *api) toInput(r *http.Request, in assetInputJSON) (store.AssetInput, string) {
	out := store.AssetInput{Name: strings.TrimSpace(in.Name), Brand: strings.TrimSpace(in.Brand), Model: strings.TrimSpace(in.Model),
		Serial: strings.TrimSpace(in.Serial), AssetTag: strings.TrimSpace(in.AssetTag), Status: in.Status,
		Department: strings.TrimSpace(in.Department), Location: strings.TrimSpace(in.Location), Price: in.Price,
		PurchaseOrder: strings.TrimSpace(in.PurchaseOrder), Supplier: strings.TrimSpace(in.Supplier), Notes: strings.TrimSpace(in.Notes),
		OwnerLocked: in.OwnerLocked, DepartmentLocked: in.DepartmentLocked}
	cat, err := uuid.Parse(in.CategoryID)
	if err != nil {
		return out, "Elige una categoría."
	}
	out.CategoryID = cat
	if out.Name == "" {
		return out, "Escribe un nombre para el activo."
	}
	for _, f := range []string{out.Name, out.Brand, out.Model, out.Serial, out.AssetTag, out.Department, out.Location, out.PurchaseOrder, out.Supplier} {
		if len([]rune(f)) > maxAssetText {
			return out, "Los campos de texto no pueden tener más de 200 caracteres."
		}
	}
	if len([]rune(out.Notes)) > maxAssetNotes {
		return out, "Las notas no pueden tener más de 4000 caracteres."
	}
	if !store.ValidAssetStatus(out.Status) {
		return out, "El estado debe ser en_uso, en_almacen, en_reparacion o baja."
	}
	if out.Price != nil && *out.Price < 0 {
		return out, "El precio no puede ser negativo."
	}
	var ok bool
	if out.PurchaseDate, ok = parseOptDate(in.PurchaseDate); !ok {
		return out, "La fecha de compra debe tener el formato AAAA-MM-DD."
	}
	if out.WarrantyUntil, ok = parseOptDate(in.WarrantyUntil); !ok {
		return out, "La fecha de fin de garantía debe tener el formato AAAA-MM-DD."
	}
	if v := strings.TrimSpace(in.OwnerID); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return out, "La persona indicada no existe."
		}
		if _, err := a.d.Store.DirectoryUserByID(r.Context(), id); err != nil {
			return out, "La persona indicada no existe."
		}
		out.OwnerID = &id
	}
	return out, ""
}

// assetStoreError responde a los errores conocidos del almacén.
func (a *api) assetStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrSerialTaken):
		conflict(w, "Ese número de serie ya es de otro activo.")
	case errors.Is(err, store.ErrTagTaken):
		conflict(w, "Esa etiqueta ya es de otro activo.")
	case errors.Is(err, store.ErrInvalid):
		badRequest(w, "La categoría o algún valor no es válido.")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este activo ya no existe.")
	default:
		a.serverError(w, r, err)
	}
}

func (a *api) assetFilter(w http.ResponseWriter, r *http.Request) (store.AssetFilter, bool) {
	q := r.URL.Query()
	f := store.AssetFilter{Q: strings.TrimSpace(q.Get("q")), Status: q.Get("status"), Origin: q.Get("origin"),
		Department: strings.TrimSpace(q.Get("department")), Trash: q.Get("trash") == "1",
		Sort: q.Get("sort"), Order: strings.ToLower(q.Get("order"))}
	if f.Status != "" && !store.ValidAssetStatus(f.Status) {
		badRequest(w, "Estado desconocido.")
		return f, false
	}
	if f.Origin != "" && f.Origin != "agent" && f.Origin != "manual" {
		badRequest(w, "El origen debe ser agent o manual.")
		return f, false
	}
	if f.Sort != "" && !store.ValidAssetSort(f.Sort) {
		badRequest(w, "No se puede ordenar por ese campo.")
		return f, false
	}
	if f.Order != "" && f.Order != "asc" && f.Order != "desc" {
		badRequest(w, "El orden debe ser asc o desc.")
		return f, false
	}
	for name, dst := range map[string]**uuid.UUID{"category_id": &f.CategoryID, "owner_id": &f.OwnerID} {
		if v := q.Get(name); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				badRequest(w, "El parámetro "+name+" no es válido.")
				return f, false
			}
			*dst = &id
		}
	}
	// Los filtros de equipo usan los mismos nombres que el listado de
	// dispositivos, salvo el estado de conexión (device_status).
	dq := r.URL.Query()
	dq.Del("q")
	dq.Del("status")
	dq.Del("sort")
	dq.Del("order")
	if v := q.Get("device_status"); v != "" {
		dq.Set("status", v)
	}
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = dq.Encode()
	dev, ok := a.deviceFilter(w, r2)
	if !ok {
		return f, false
	}
	f.Device = dev
	return f, true
}

func (a *api) listAssets(w http.ResponseWriter, r *http.Request) {
	f, ok := a.assetFilter(w, r)
	if !ok {
		return
	}
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	rows, total, err := a.d.Store.ListAssets(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := make([]assetJSON, 0, len(rows))
	for _, row := range rows {
		items = append(items, a.assetToJSON(row))
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[assetJSON]{Items: items, Total: total, Page: f.Page, PerPage: f.PerPage})
}

var assetStatusLabels = map[string]string{
	store.AssetInUse: "En uso", store.AssetInStock: "En almacén", store.AssetInRepair: "En reparación", store.AssetRetired: "Baja",
}

func (a *api) exportAssets(w http.ResponseWriter, r *http.Request) {
	f, ok := a.assetFilter(w, r)
	if !ok {
		return
	}
	f.All = true
	rows, _, err := a.d.Store.ListAssets(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="inventario-`+a.d.Now().Format(dateLayout)+`.csv"`)
	_, _ = w.Write([]byte("\ufeff"))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Nombre", "Categoría", "Marca", "Modelo", "Número de serie", "Etiqueta", "Estado", "Persona", "Correo",
		"Departamento", "Ubicación", "Origen", "Equipo", "Precio", "Fecha de compra", "Garantía hasta", "Pedido", "Proveedor", "Notas"})
	for _, row := range rows {
		x := row.Asset
		origin, host := "Manual", ""
		if row.Device != nil {
			origin, host = "Agente", row.Device.Hostname
		}
		price := ""
		if x.Price != nil {
			price = strconv.FormatFloat(*x.Price, 'f', 2, 64)
		}
		_ = cw.Write([]string{csvSafe(x.Name), csvSafe(x.CategoryName), csvSafe(x.Brand), csvSafe(x.Model), csvSafe(x.Serial),
			csvSafe(x.AssetTag), assetStatusLabels[x.Status], csvSafe(row.OwnerName), csvSafe(row.OwnerEmail), csvSafe(x.Department),
			csvSafe(x.Location), origin, csvSafe(host), price, dateStr(x.PurchaseDate), dateStr(x.WarrantyUntil),
			csvSafe(x.PurchaseOrder), csvSafe(x.Supplier), csvSafe(x.Notes)})
	}
	cw.Flush()
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AssetsExported, Details: map[string]any{"rows": len(rows)}, IP: a.ip(r)})
}

// loadAsset lee el activo de la ruta (incluidos los de la papelera).
func (a *api) loadAsset(w http.ResponseWriter, r *http.Request) (store.AssetRow, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.AssetRow{}, false
	}
	row, err := a.assetRow(r, id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este activo ya no existe.")
		return store.AssetRow{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.AssetRow{}, false
	}
	return row, true
}

func (a *api) assetRow(r *http.Request, id uuid.UUID) (store.AssetRow, error) {
	x, err := a.d.Store.AssetByID(r.Context(), id)
	if err != nil {
		return store.AssetRow{}, err
	}
	row := store.AssetRow{Asset: x}
	if x.OwnerID != nil {
		if u, err := a.d.Store.DirectoryUserByID(r.Context(), *x.OwnerID); err == nil {
			row.OwnerName, row.OwnerEmail, row.OwnerDepartment = u.DisplayName, u.Contact(), u.Department
		}
	}
	if x.DeviceID != nil {
		d, err := a.d.Store.DeviceRowByID(r.Context(), *x.DeviceID)
		if err == nil {
			row.Device = &d
		} else if !errors.Is(err, store.ErrNotFound) {
			return store.AssetRow{}, err
		}
	}
	return row, nil
}

func (a *api) getAsset(w http.ResponseWriter, r *http.Request) {
	if row, ok := a.loadAsset(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, a.assetToJSON(row))
	}
}

// getDeviceAsset devuelve el activo de un equipo con agente.
func (a *api) getDeviceAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	x, err := a.d.Store.AssetByDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este equipo aún no tiene activo en el inventario.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	row, err := a.assetRow(r, x.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.assetToJSON(row))
}

func (a *api) writeAsset(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	row, err := a.assetRow(r, id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, status, a.assetToJSON(row))
}

func (a *api) auditAsset(r *http.Request, action string, id uuid.UUID, details map[string]any) {
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: action, TargetType: "asset", TargetID: id.String(), Details: details, IP: a.ip(r)})
}

func (a *api) createAsset(w http.ResponseWriter, r *http.Request) {
	var body assetInputJSON
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	in, msg := a.toInput(r, body)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	x, err := a.d.Store.CreateAsset(r.Context(), in)
	if err != nil {
		a.assetStoreError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetCreated, x.ID, map[string]any{"name": x.Name, "serial": x.Serial})
	a.writeAsset(w, r, http.StatusCreated, x.ID)
}

func (a *api) updateAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body assetInputJSON
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	in, msg := a.toInput(r, body)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	before, err := a.d.Store.AssetByID(r.Context(), id)
	if err != nil || before.DeletedAt != nil {
		notFound(w, "Este activo ya no existe.")
		return
	}
	x, err := a.d.Store.UpdateAsset(r.Context(), id, in)
	if err != nil {
		a.assetStoreError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetUpdated, id, assetChanges(before, x))
	a.writeAsset(w, r, http.StatusOK, id)
}

// assetChanges resume qué campos han cambiado, para la auditoría.
func assetChanges(b, x store.Asset) map[string]any {
	ch := map[string]any{"name": x.Name}
	add := func(k, old, new string) {
		if old != new {
			ch[k] = map[string]string{"from": old, "to": new}
		}
	}
	add("category", b.CategoryName, x.CategoryName)
	add("status", b.Status, x.Status)
	add("serial", b.Serial, x.Serial)
	add("location", b.Location, x.Location)
	owner := func(id *uuid.UUID) string {
		if id == nil {
			return ""
		}
		return id.String()
	}
	add("owner_id", owner(b.OwnerID), owner(x.OwnerID))
	add("department", b.Department, x.Department)
	lock := strconv.FormatBool
	add("owner_locked", lock(b.OwnerLocked), lock(x.OwnerLocked))
	add("department_locked", lock(b.DepartmentLocked), lock(x.DepartmentLocked))
	return ch
}

func (a *api) trashAssetHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 && !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	admin := adminFrom(r.Context())
	err := a.d.Store.TrashAsset(r.Context(), id, &admin.ID, strings.TrimSpace(body.Reason))
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Este activo tiene un equipo con agente enlazado: elimina antes el equipo o desenlázalo.")
		return
	case err != nil:
		a.assetStoreError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetTrashed, id, map[string]any{"reason": strings.TrimSpace(body.Reason)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) restoreAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	x, err := a.d.Store.RestoreAsset(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrSerialTaken):
		conflict(w, "No se puede restaurar: otro activo tiene ahora su número de serie.")
		return
	case errors.Is(err, store.ErrTagTaken):
		conflict(w, "No se puede restaurar: otro activo tiene ahora su etiqueta.")
		return
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este activo no está en la papelera.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetRestored, id, map[string]any{"name": x.Name})
	a.writeAsset(w, r, http.StatusOK, id)
}

func (a *api) purgeAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	x, _ := a.d.Store.AssetByID(r.Context(), id)
	err := a.d.Store.PurgeAsset(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Solo se pueden eliminar definitivamente los activos que están en la papelera.")
		return
	case err != nil:
		a.assetStoreError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetPurged, id, map[string]any{"name": x.Name, "serial": x.Serial})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) linkAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		DeviceID string `json:"device_id"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	dev, err := uuid.Parse(body.DeviceID)
	if err != nil {
		badRequest(w, "Indica el equipo con device_id.")
		return
	}
	admin := adminFrom(r.Context())
	err = a.d.Store.LinkAsset(r.Context(), id, dev, &admin.ID)
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Este activo ya tiene un equipo o está en la papelera.")
		return
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "El activo o el equipo ya no existen.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetLinked, id, map[string]any{"device_id": dev.String()})
	a.writeAsset(w, r, http.StatusOK, id)
}

func (a *api) unlinkAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	err := a.d.Store.UnlinkAsset(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Este activo no tiene ningún equipo enlazado.")
		return
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Este activo ya no existe.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetUnlinked, id, nil)
	a.writeAsset(w, r, http.StatusOK, id)
}

func (a *api) bulkAssets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs        []string `json:"ids"`
		Status     *string  `json:"status"`
		CategoryID *string  `json:"category_id"`
		OwnerID    *string  `json:"owner_id"`
		Location   *string  `json:"location"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > 1000 {
		badRequest(w, "Selecciona entre 1 y 1000 activos.")
		return
	}
	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, s := range body.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			badRequest(w, "Algún activo seleccionado no es válido.")
			return
		}
		ids = append(ids, id)
	}
	var b store.AssetBulk
	if body.Status != nil {
		if !store.ValidAssetStatus(*body.Status) {
			badRequest(w, "Estado desconocido.")
			return
		}
		b.Status = body.Status
	}
	if body.CategoryID != nil {
		id, err := uuid.Parse(*body.CategoryID)
		if err != nil {
			badRequest(w, "La categoría no es válida.")
			return
		}
		b.CategoryID = &id
	}
	if body.OwnerID != nil {
		var owner *uuid.UUID
		if *body.OwnerID != "" {
			id, err := uuid.Parse(*body.OwnerID)
			if err != nil {
				badRequest(w, "La persona indicada no existe.")
				return
			}
			if _, err := a.d.Store.DirectoryUserByID(r.Context(), id); err != nil {
				badRequest(w, "La persona indicada no existe.")
				return
			}
			owner = &id
		}
		b.OwnerID = &owner
	}
	if body.Location != nil {
		l := strings.TrimSpace(*body.Location)
		b.Location = &l
	}
	if b.Status == nil && b.CategoryID == nil && b.OwnerID == nil && b.Location == nil {
		badRequest(w, "No se ha indicado ningún cambio.")
		return
	}
	n, err := a.d.Store.BulkUpdateAssets(r.Context(), ids, b)
	if err != nil {
		a.assetStoreError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AssetsBulkUpdated, TargetType: "asset",
		Details: map[string]any{"ids": body.IDs, "status": body.Status, "category_id": body.CategoryID, "owner_id": body.OwnerID, "location": body.Location}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"updated": n})
}

func (a *api) assetHistory(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return
	}
	entries, _, err := a.d.Store.ListAudit(r.Context(), store.AuditFilter{TargetType: "asset", TargetID: row.ID.String(), PerPage: 200})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]auditJSON, 0, len(entries))
	for _, e := range entries {
		out = append(out, auditJSON{ID: e.ID, At: e.At, ActorEmail: e.ActorEmail, Action: e.Action,
			TargetType: e.TargetType, TargetID: e.TargetID, Details: e.Details, IP: e.IP})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// mergeAssets une dos fichas del mismo objeto eligiendo de cuál se toma cada
// campo. El otro activo va a la papelera.
func (a *api) mergeAssets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeepID    string   `json:"keep_id"`
		OtherID   string   `json:"other_id"`
		FromOther []string `json:"from_other"`
		NotesBoth bool     `json:"notes_both"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	keep, err1 := uuid.Parse(body.KeepID)
	other, err2 := uuid.Parse(body.OtherID)
	if err1 != nil || err2 != nil || keep == other {
		badRequest(w, "Elige dos activos distintos: el que se conserva (keep_id) y el otro (other_id).")
		return
	}
	from := map[string]bool{}
	for _, f := range body.FromOther {
		if !slices.Contains(store.MergeFields, f) {
			badRequest(w, "Campo desconocido: "+f+".")
			return
		}
		from[f] = true
	}
	admin := adminFrom(r.Context())
	merged, err := a.d.Store.MergeAssets(r.Context(), store.AssetMerge{KeepID: keep, OtherID: other, FromOther: from,
		NotesBoth: body.NotesBoth, By: &admin.ID})
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "Los dos activos tienen un equipo con agente: para el MDM son dos equipos distintos. Si es el mismo equipo inscrito dos veces, elimina el que sobra en su ficha y vuelve a intentarlo.")
		return
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Alguno de los activos ya no existe o está en la papelera.")
		return
	case errors.Is(err, store.ErrSerialTaken), errors.Is(err, store.ErrTagTaken), errors.Is(err, store.ErrInvalid):
		badRequest(w, "No se puede fusionar con esos valores: el nombre es obligatorio y el número de serie y la etiqueta no pueden estar en otro activo.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.auditAsset(r, audit.AssetMerged, merged.ID, map[string]any{"merged_from": other.String(), "from_other": body.FromOther, "notes_both": body.NotesBoth})
	a.writeAsset(w, r, http.StatusOK, merged.ID)
}
