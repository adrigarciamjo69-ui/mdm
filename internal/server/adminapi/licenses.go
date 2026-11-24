package adminapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type licenseAssignmentJSON struct {
	ID    string `json:"id"`
	Asset *struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Serial string `json:"serial"`
	} `json:"asset"`
	User       *ownerJSON `json:"user"`
	Notes      string     `json:"notes"`
	AssignedAt time.Time  `json:"assigned_at"`
}

type licenseJSON struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Vendor          string                  `json:"vendor"`
	Version         string                  `json:"version"`
	LicenseType     string                  `json:"license_type"`
	Seats           int                     `json:"seats"`
	HasKey          bool                    `json:"has_key"`
	PurchaseDate    string                  `json:"purchase_date"`
	ExpiryDate      string                  `json:"expiry_date"`
	PurchaseOrder   string                  `json:"purchase_order"`
	Price           *float64                `json:"price"`
	Department      string                  `json:"department"`
	Status          string                  `json:"status"`
	EffectiveStatus string                  `json:"effective_status"`
	MatchPattern    string                  `json:"match_pattern"`
	Notes           string                  `json:"notes"`
	Assigned        int                     `json:"assigned"`
	Installed       int                     `json:"installed"`
	Over            bool                    `json:"over"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
	Assignments     []licenseAssignmentJSON `json:"assignments,omitempty"`
}

func (a *api) licenseToJSON(l store.License) licenseJSON {
	return licenseJSON{ID: l.ID.String(), Name: l.Name, Vendor: l.Vendor, Version: l.Version, LicenseType: l.LicenseType,
		Seats: l.Seats, HasKey: l.HasKey, PurchaseDate: dateStr(l.PurchaseDate), ExpiryDate: dateStr(l.ExpiryDate),
		PurchaseOrder: l.PurchaseOrder, Price: l.Price, Department: l.Department, Status: l.Status,
		EffectiveStatus: l.EffectiveStatus(a.d.Now()), MatchPattern: l.MatchPattern, Notes: l.Notes,
		Assigned: l.Assigned, Installed: l.Installed, Over: l.Over(), CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt}
}

func (a *api) licensesToJSON(ls []store.License) []licenseJSON {
	out := make([]licenseJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, a.licenseToJSON(l))
	}
	return out
}

func (a *api) listLicenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.LicenseFilter{Q: strings.TrimSpace(q.Get("q")), LicenseType: q.Get("license_type"), Status: q.Get("status"), Today: a.d.Now()}
	if f.LicenseType != "" && !inList(f.LicenseType, store.LicenseTypes) {
		badRequest(w, "Tipo de licencia desconocido.")
		return
	}
	if f.Status != "" && !inList(f.Status, store.LicenseStatuses) {
		badRequest(w, "Estado desconocido.")
		return
	}
	if v := q.Get("expiring"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			badRequest(w, "«expiring» debe ser un número de días entre 1 y 365.")
			return
		}
		f.ExpiringDays = n
	}
	var ok bool
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	ls, total, err := a.d.Store.ListLicenses(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[licenseJSON]{Items: a.licensesToJSON(ls), Total: total, Page: f.Page, PerPage: f.PerPage})
}

func inList(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type licenseInputJSON struct {
	Name          string   `json:"name"`
	Vendor        string   `json:"vendor"`
	Version       string   `json:"version"`
	LicenseType   string   `json:"license_type"`
	Seats         int      `json:"seats"`
	LicenseKey    *string  `json:"license_key"`
	PurchaseDate  string   `json:"purchase_date"`
	ExpiryDate    string   `json:"expiry_date"`
	PurchaseOrder string   `json:"purchase_order"`
	Price         *float64 `json:"price"`
	Department    string   `json:"department"`
	Status        string   `json:"status"`
	MatchPattern  string   `json:"match_pattern"`
	Notes         string   `json:"notes"`
}

func (a *api) licenseInput(w http.ResponseWriter, r *http.Request) (store.LicenseInput, bool) {
	var in licenseInputJSON
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.LicenseInput{}, false
	}
	out := store.LicenseInput{Name: strings.TrimSpace(in.Name), Vendor: strings.TrimSpace(in.Vendor), Version: strings.TrimSpace(in.Version),
		LicenseType: in.LicenseType, Seats: in.Seats, PurchaseOrder: strings.TrimSpace(in.PurchaseOrder), Price: in.Price,
		Department: strings.TrimSpace(in.Department), Status: in.Status, MatchPattern: strings.TrimSpace(in.MatchPattern),
		Notes: strings.TrimSpace(in.Notes)}
	fail := func(msg string) (store.LicenseInput, bool) {
		badRequest(w, msg)
		return store.LicenseInput{}, false
	}
	switch {
	case out.Name == "":
		return fail("Escribe el nombre de la licencia.")
	case !inList(out.LicenseType, store.LicenseTypes):
		return fail("El tipo de licencia no es válido.")
	case !inList(out.Status, store.LicenseStatuses):
		return fail("El estado debe ser activa, expirada o baja.")
	case out.Seats < 0 || out.Seats > 1000000:
		return fail("Los puestos deben ser 0 (ilimitados) o un número positivo.")
	case out.Price != nil && *out.Price < 0:
		return fail("El precio no puede ser negativo.")
	case len([]rune(out.MatchPattern)) > 0 && len([]rune(out.MatchPattern)) < 3:
		return fail("El texto para detectar instalaciones debe tener al menos 3 caracteres.")
	case len([]rune(out.Notes)) > maxAssetNotes:
		return fail("Las notas no pueden tener más de 4000 caracteres.")
	}
	for _, s := range []string{out.Name, out.Vendor, out.Version, out.PurchaseOrder, out.Department, out.MatchPattern} {
		if len([]rune(s)) > maxAssetText {
			return fail("Los campos de texto no pueden tener más de 200 caracteres.")
		}
	}
	var ok bool
	if out.PurchaseDate, ok = parseOptDate(in.PurchaseDate); !ok {
		return fail("La fecha de compra debe tener el formato AAAA-MM-DD.")
	}
	if out.ExpiryDate, ok = parseOptDate(in.ExpiryDate); !ok {
		return fail("La fecha de caducidad debe tener el formato AAAA-MM-DD.")
	}
	if in.LicenseKey != nil {
		key := strings.TrimSpace(*in.LicenseKey)
		sealed := ""
		if key != "" {
			if len(key) > 4000 {
				return fail("La clave es demasiado larga.")
			}
			if a.d.Secrets == nil {
				httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "Falta MDM_SECRET_KEY en el servidor: no se pueden guardar claves.")
				return store.LicenseInput{}, false
			}
			var err error
			if sealed, err = a.d.Secrets.Seal([]byte(key)); err != nil {
				a.serverError(w, r, err)
				return store.LicenseInput{}, false
			}
		}
		out.KeySealed = &sealed
	}
	return out, true
}

func (a *api) auditLicense(r *http.Request, action string, id uuid.UUID, details map[string]any) {
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: action, TargetType: "license", TargetID: id.String(), Details: details, IP: a.ip(r)})
}

func (a *api) createLicense(w http.ResponseWriter, r *http.Request) {
	in, ok := a.licenseInput(w, r)
	if !ok {
		return
	}
	l, err := a.d.Store.CreateLicense(r.Context(), in)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.auditLicense(r, audit.LicenseCreated, l.ID, map[string]any{"name": l.Name})
	httpx.WriteJSON(w, http.StatusCreated, a.licenseToJSON(l))
}

func (a *api) updateLicense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	in, ok := a.licenseInput(w, r)
	if !ok {
		return
	}
	l, err := a.d.Store.UpdateLicense(r.Context(), id, in)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta licencia ya no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.auditLicense(r, audit.LicenseUpdated, id, map[string]any{"name": l.Name, "key_changed": in.KeySealed != nil})
	httpx.WriteJSON(w, http.StatusOK, a.licenseToJSON(l))
}

func (a *api) loadLicense(w http.ResponseWriter, r *http.Request) (store.License, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.License{}, false
	}
	l, err := a.d.Store.LicenseByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta licencia ya no existe.")
		return store.License{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return store.License{}, false
	}
	return l, true
}

func (a *api) getLicense(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	as, err := a.d.Store.LicenseAssignments(r.Context(), l.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := a.licenseToJSON(l)
	out.Assignments = []licenseAssignmentJSON{}
	for _, x := range as {
		j := licenseAssignmentJSON{ID: x.ID.String(), Notes: x.Notes, AssignedAt: x.AssignedAt}
		if x.AssetID != nil {
			j.Asset = &struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Serial string `json:"serial"`
			}{x.AssetID.String(), x.AssetName, x.AssetSerial}
		}
		if x.UserID != nil {
			j.User = &ownerJSON{ID: x.UserID.String(), Name: x.UserName, Email: x.UserEmail}
		}
		out.Assignments = append(out.Assignments, j)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) deleteLicense(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.DeleteLicense(r.Context(), l.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.auditLicense(r, audit.LicenseDeleted, l.ID, map[string]any{"name": l.Name})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) revealLicenseKey(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	sealed, err := a.d.Store.LicenseKeySealed(r.Context(), l.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if sealed == "" {
		notFound(w, "Esta licencia no tiene clave guardada.")
		return
	}
	if a.d.Secrets == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "Falta MDM_SECRET_KEY en el servidor: no se puede descifrar la clave.")
		return
	}
	plain, err := a.d.Secrets.Open(sealed)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "No se puede descifrar la clave: MDM_SECRET_KEY ha cambiado.")
		return
	}
	a.auditLicense(r, audit.LicenseKeyViewed, l.ID, map[string]any{"name": l.Name})
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"key": string(plain)})
}

func (a *api) licenseInstallations(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	devs, err := a.d.Store.LicenseInstallations(r.Context(), l.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type devJSON struct {
		DeviceID string `json:"device_id"`
		Hostname string `json:"hostname"`
		Software string `json:"software"`
		Version  string `json:"version"`
	}
	out := make([]devJSON, 0, len(devs))
	for _, d := range devs {
		out = append(out, devJSON{d.ID.String(), d.Hostname, d.Software, d.Version})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) addLicenseAssignment(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	var body struct {
		AssetID string `json:"asset_id"`
		UserID  string `json:"user_id"`
		Notes   string `json:"notes"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	if (body.AssetID == "") == (body.UserID == "") {
		badRequest(w, "Asigna la licencia a un activo o a una persona (uno de los dos).")
		return
	}
	var assetID, userID *uuid.UUID
	for v, dst := range map[string]**uuid.UUID{body.AssetID: &assetID, body.UserID: &userID} {
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			badRequest(w, "El activo o la persona no son válidos.")
			return
		}
		*dst = &id
	}
	if assetID != nil {
		if x, err := a.d.Store.AssetByID(r.Context(), *assetID); err != nil || x.DeletedAt != nil {
			badRequest(w, "El activo indicado no existe.")
			return
		}
	}
	id, err := a.d.Store.AddLicenseAssignment(r.Context(), l.ID, assetID, userID, strings.TrimSpace(body.Notes))
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, "La licencia ya está asignada a ese activo o persona.")
		return
	case errors.Is(err, store.ErrNotFound):
		badRequest(w, "El activo o la persona indicados no existen.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.auditLicense(r, audit.LicenseAssigned, l.ID, map[string]any{"name": l.Name, "asset_id": body.AssetID, "user_id": body.UserID})
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String()})
}

func (a *api) removeLicenseAssignment(w http.ResponseWriter, r *http.Request) {
	l, ok := a.loadLicense(w, r)
	if !ok {
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "aid"))
	if err != nil {
		notFound(w, "Esta asignación ya no existe.")
		return
	}
	if err := a.d.Store.RemoveLicenseAssignment(r.Context(), l.ID, aid); errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta asignación ya no existe.")
		return
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.auditLicense(r, audit.LicenseUnassigned, l.ID, map[string]any{"name": l.Name, "assignment_id": aid.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) assetLicenses(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return
	}
	ls, err := a.d.Store.LicensesFor(r.Context(), &row.ID, nil)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.licensesToJSON(ls))
}

func init() {
	directoryUserExtras = append(directoryUserExtras, func(a *api, r *http.Request, u store.DirectoryUser, out map[string]any) error {
		ls, err := a.d.Store.LicensesFor(r.Context(), nil, &u.ID)
		if err != nil {
			return err
		}
		out["licenses"] = a.licensesToJSON(ls)
		return nil
	})
}
