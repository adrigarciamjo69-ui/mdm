package adminapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

type deliveryItemJSON struct {
	ID           string  `json:"id,omitempty"`
	AssetID      *string `json:"asset_id"`
	Category     string  `json:"category"`
	Brand        string  `json:"brand"`
	Model        string  `json:"model"`
	Serial       string  `json:"serial"`
	Observations string  `json:"observations"`
}

type deliveryUserJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Department string `json:"department"`
	JobTitle   string `json:"job_title"`
	EmployeeID string `json:"employee_id"`
}

type deliveryJSON struct {
	ID           string             `json:"id"`
	Number       string             `json:"number"`
	Type         string             `json:"type"`
	Status       string             `json:"status"`
	User         deliveryUserJSON   `json:"user"`
	RecipientDNI string             `json:"recipient_dni"`
	Date         string             `json:"date"`
	Responsible  string             `json:"responsible"`
	Notes        string             `json:"notes"`
	CancelReason string             `json:"cancel_reason"`
	CreatedBy    string             `json:"created_by"`
	CreatedAt    time.Time          `json:"created_at"`
	ConfirmedAt  *time.Time         `json:"confirmed_at"`
	Items        []deliveryItemJSON `json:"items"`
}

func deliveryToJSON(d store.Handover) deliveryJSON {
	out := deliveryJSON{ID: d.ID.String(), Number: d.Number, Type: d.Type, Status: d.Status,
		User: deliveryUserJSON{ID: d.User.ID.String(), Name: d.User.DisplayName, Email: d.User.Contact(), Department: d.User.Department,
			JobTitle: d.User.JobTitle, EmployeeID: d.User.EmployeeID},
		RecipientDNI: d.RecipientDNI, Date: d.Date.Format(dateLayout), Responsible: d.Responsible, Notes: d.Notes,
		CancelReason: d.CancelReason, CreatedBy: d.CreatedByEmail, CreatedAt: d.CreatedAt, ConfirmedAt: d.ConfirmedAt,
		Items: []deliveryItemJSON{}}
	for _, it := range d.Items {
		j := deliveryItemJSON{ID: it.ID.String(), Category: it.Category, Brand: it.Brand, Model: it.Model, Serial: it.Serial, Observations: it.Observations}
		if it.AssetID != nil {
			s := it.AssetID.String()
			j.AssetID = &s
		}
		out.Items = append(out.Items, j)
	}
	return out
}

func deliveriesToJSON(ds []store.Handover) []deliveryJSON {
	out := make([]deliveryJSON, 0, len(ds))
	for _, d := range ds {
		out = append(out, deliveryToJSON(d))
	}
	return out
}

func (a *api) listDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.HandoverFilter{Q: strings.TrimSpace(q.Get("q")), Type: q.Get("type"), Status: q.Get("status")}
	if f.Type != "" && f.Type != store.DeliveryEntrega && f.Type != store.DeliveryDevolucion {
		badRequest(w, "El tipo debe ser entrega o devolucion.")
		return
	}
	if f.Status != "" && f.Status != store.DeliveryDraft && f.Status != store.DeliveryConfirmed && f.Status != store.DeliveryCancelled {
		badRequest(w, "Estado desconocido.")
		return
	}
	if v := q.Get("user_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			badRequest(w, "La persona no es válida.")
			return
		}
		f.UserID = &id
	}
	var ok bool
	if f.Page, f.PerPage, ok = pageParams(w, r); !ok {
		return
	}
	ds, total, err := a.d.Store.ListHandovers(r.Context(), f)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pageJSON[deliveryJSON]{Items: deliveriesToJSON(ds), Total: total, Page: f.Page, PerPage: f.PerPage})
}

type deliveryInputJSON struct {
	Type         string             `json:"type"`
	UserID       string             `json:"user_id"`
	RecipientDNI string             `json:"recipient_dni"`
	Date         string             `json:"date"`
	Responsible  string             `json:"responsible"`
	Notes        string             `json:"notes"`
	Items        []deliveryItemJSON `json:"items"`
}

func (a *api) deliveryInput(w http.ResponseWriter, r *http.Request) (store.HandoverInput, bool) {
	var in deliveryInputJSON
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return store.HandoverInput{}, false
	}
	fail := func(msg string) (store.HandoverInput, bool) {
		badRequest(w, msg)
		return store.HandoverInput{}, false
	}
	if in.Type != store.DeliveryEntrega && in.Type != store.DeliveryDevolucion {
		return fail("El tipo debe ser entrega o devolucion.")
	}
	uid, err := uuid.Parse(in.UserID)
	if err != nil {
		return fail("Elige la persona que recibe o devuelve el material.")
	}
	if _, err := a.d.Store.DirectoryUserByID(r.Context(), uid); err != nil {
		return fail("La persona indicada no existe.")
	}
	date, err := time.Parse(dateLayout, strings.TrimSpace(in.Date))
	if err != nil {
		return fail("La fecha debe tener el formato AAAA-MM-DD.")
	}
	admin := adminFrom(r.Context())
	out := store.HandoverInput{Type: in.Type, UserID: uid, RecipientDNI: strings.ToUpper(strings.TrimSpace(in.RecipientDNI)), Date: date,
		Responsible: strings.TrimSpace(in.Responsible), Notes: strings.TrimSpace(in.Notes), CreatedBy: &admin.ID}
	if out.Responsible == "" {
		out.Responsible = admin.Email
	}
	if len(in.Items) > 100 {
		return fail("Un acta puede tener como mucho 100 líneas.")
	}
	for _, it := range in.Items {
		item := store.HandoverItem{Category: strings.TrimSpace(it.Category), Brand: strings.TrimSpace(it.Brand), Model: strings.TrimSpace(it.Model),
			Serial: strings.TrimSpace(it.Serial), Observations: strings.TrimSpace(it.Observations)}
		if it.AssetID != nil && *it.AssetID != "" {
			id, err := uuid.Parse(*it.AssetID)
			if err != nil {
				return fail("Algún activo del acta no es válido.")
			}
			item.AssetID = &id
		} else if item.Category == "" && item.Model == "" && item.Brand == "" {
			return fail("Cada línea sin activo necesita al menos el tipo de dispositivo o el modelo.")
		}
		for _, s := range []string{item.Category, item.Brand, item.Model, item.Serial, item.Observations} {
			if len([]rune(s)) > maxAssetText {
				return fail("Los campos de las líneas no pueden tener más de 200 caracteres.")
			}
		}
		out.Items = append(out.Items, item)
	}
	if len([]rune(out.Notes)) > maxAssetNotes || len([]rune(out.Responsible)) > maxAssetText || len([]rune(out.RecipientDNI)) > 20 {
		return fail("Algún campo es demasiado largo.")
	}
	return out, true
}

func (a *api) deliveryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrAssetUnavailable):
		badRequest(w, "Algún activo del acta ya no existe o está en la papelera.")
	case errors.Is(err, store.ErrConflict):
		conflict(w, "El acta ya está confirmada o anulada: no se puede cambiar.")
	case errors.Is(err, store.ErrNotFound):
		notFound(w, "Esta acta ya no existe.")
	case errors.Is(err, store.ErrInvalid):
		badRequest(w, "El acta no tiene material o la persona no existe.")
	default:
		a.serverError(w, r, err)
	}
}

func (a *api) auditDelivery(r *http.Request, action string, d store.Handover, extra map[string]any) {
	admin := adminFrom(r.Context())
	details := map[string]any{"number": d.Number, "type": d.Type, "user": d.User.DisplayName}
	for k, v := range extra {
		details[k] = v
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: action, TargetType: "delivery", TargetID: d.ID.String(), Details: details, IP: a.ip(r)})
}

func (a *api) createDelivery(w http.ResponseWriter, r *http.Request) {
	in, ok := a.deliveryInput(w, r)
	if !ok {
		return
	}
	d, err := a.d.Store.CreateHandover(r.Context(), in)
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	a.auditDelivery(r, audit.DeliveryCreated, d, nil)
	httpx.WriteJSON(w, http.StatusCreated, deliveryToJSON(d))
}

func (a *api) getDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := a.d.Store.HandoverByID(r.Context(), id)
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, deliveryToJSON(d))
}

func (a *api) updateDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	in, ok := a.deliveryInput(w, r)
	if !ok {
		return
	}
	d, err := a.d.Store.UpdateHandover(r.Context(), id, in)
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	a.auditDelivery(r, audit.DeliveryUpdated, d, nil)
	httpx.WriteJSON(w, http.StatusOK, deliveryToJSON(d))
}

func (a *api) deleteDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := a.d.Store.HandoverByID(r.Context(), id)
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	if err := a.d.Store.DeleteHandover(r.Context(), id); err != nil {
		a.deliveryError(w, r, err)
		return
	}
	a.auditDelivery(r, audit.DeliveryDeleted, d, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) confirmDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := a.d.Store.ConfirmHandover(r.Context(), id, a.d.Now().UTC())
	if errors.Is(err, store.ErrInvalid) {
		badRequest(w, "Añade al menos una línea de material antes de confirmar el acta.")
		return
	}
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	a.auditDelivery(r, audit.DeliveryConfirmed, d, map[string]any{"items": len(d.Items)})
	httpx.WriteJSON(w, http.StatusOK, deliveryToJSON(d))
}

func (a *api) cancelDelivery(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &body) {
		return
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" || len([]rune(reason)) > maxAssetText {
		badRequest(w, "Indica el motivo de la anulación (hasta 200 caracteres).")
		return
	}
	d, err := a.d.Store.CancelHandover(r.Context(), id, reason)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Solo se pueden anular actas confirmadas. Un borrador se elimina.")
		return
	}
	if err != nil {
		a.deliveryError(w, r, err)
		return
	}
	a.auditDelivery(r, audit.DeliveryCancelled, d, map[string]any{"reason": reason})
	httpx.WriteJSON(w, http.StatusOK, deliveryToJSON(d))
}

func (a *api) assetDeliveries(w http.ResponseWriter, r *http.Request) {
	row, ok := a.loadAsset(w, r)
	if !ok {
		return
	}
	ds, _, err := a.d.Store.ListHandovers(r.Context(), store.HandoverFilter{AssetID: &row.ID, All: true})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, deliveriesToJSON(ds))
}

func init() {
	directoryUserExtras = append(directoryUserExtras, func(a *api, r *http.Request, u store.DirectoryUser, out map[string]any) error {
		ds, _, err := a.d.Store.ListHandovers(r.Context(), store.HandoverFilter{UserID: &u.ID, All: true})
		if err != nil {
			return err
		}
		out["deliveries"] = deliveriesToJSON(ds)
		return nil
	})
}

// --- Ajustes de las actas ---

type pdfColors struct {
	Primary string `json:"primary"`
	Footer  string `json:"footer"`
}

type deliverySettingsJSON struct {
	Company  string `json:"company"`
	NIF      string `json:"nif"`
	Address1 string `json:"address1"`
	Address2 string `json:"address2"`
	Phone    string `json:"phone"`
	Web      string `json:"web"`
	Email    string `json:"email"`
	City     string `json:"city"`
	Logo     string `json:"logo"`
	Colors   struct {
		Entrega    pdfColors `json:"entrega"`
		Devolucion pdfColors `json:"devolucion"`
	} `json:"colors"`
	Clauses struct {
		Entrega    []string `json:"entrega"`
		Devolucion []string `json:"devolucion"`
	} `json:"clauses"`
}

func defaultDeliverySettings() deliverySettingsJSON {
	var s deliverySettingsJSON
	s.Company = "EMPRESA, S.A."
	s.Colors.Entrega = pdfColors{Primary: "#1c3ca8", Footer: "#0f172a"}
	s.Colors.Devolucion = pdfColors{Primary: "#b45309", Footer: "#0f172a"}
	s.Clauses.Entrega = []string{
		"Se ha comprobado el buen funcionamiento del equipamiento relacionado anteriormente y se encuentra en perfecto estado para su uso.",
		"El material entregado es propiedad de {empresa} y debe ser utilizado exclusivamente para fines laborales.",
		"El empleado es responsable del buen uso, mantenimiento y seguridad del dispositivo mientras esté bajo su custodia.",
		"En caso de pérdida, robo o daño, el empleado lo debe notificar inmediatamente al Departamento TIC.",
		"El material deberá ser devuelto en buen estado al finalizar la relación laboral o cuando sea solicitado por la empresa.",
	}
	s.Clauses.Devolucion = []string{
		"El trabajador {trabajador} declara devolver el material listado anteriormente a {empresa}.",
		"Se ha verificado el estado del equipo en el momento de la devolución y se acepta conforme.",
		"El empleado queda eximido de responsabilidad sobre dicho material a partir de la fecha indicada en este justificante.",
		"El material devuelto pasa a disposición de {empresa} para los fines que estime oportunos.",
	}
	return s
}

func (a *api) loadDeliverySettings(r *http.Request) (deliverySettingsJSON, error) {
	raw, err := a.d.Store.DeliverySettingsRaw(r.Context())
	if err != nil || raw == nil {
		return defaultDeliverySettings(), err
	}
	s := defaultDeliverySettings()
	err = json.Unmarshal(raw, &s)
	return s, err
}

func (a *api) getDeliverySettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadDeliverySettings(r)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

var (
	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	logoRe   = regexp.MustCompile(`^data:image/(png|jpeg);base64,([A-Za-z0-9+/=]+)$`)
)

const maxLogo = 500 << 10

func (a *api) putDeliverySettings(w http.ResponseWriter, r *http.Request) {
	var s deliverySettingsJSON
	if !httpx.DecodeJSON(w, r, 1<<20, &s) {
		return
	}
	for _, c := range []pdfColors{s.Colors.Entrega, s.Colors.Devolucion} {
		if !hexColor.MatchString(c.Primary) || !hexColor.MatchString(c.Footer) {
			badRequest(w, "Los colores deben tener el formato #RRGGBB.")
			return
		}
	}
	if s.Logo != "" {
		m := logoRe.FindStringSubmatch(s.Logo)
		if m == nil {
			badRequest(w, "El logotipo debe ser una imagen PNG o JPG.")
			return
		}
		img, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil || len(img) > maxLogo {
			badRequest(w, "El logotipo debe ser una imagen PNG o JPG de 500 KB como mucho.")
			return
		}
		if (m[1] == "png" && !bytes.HasPrefix(img, []byte("\x89PNG"))) || (m[1] == "jpeg" && !bytes.HasPrefix(img, []byte{0xFF, 0xD8})) {
			badRequest(w, "El logotipo no es una imagen PNG o JPG válida.")
			return
		}
	}
	for _, f := range []string{s.Company, s.NIF, s.Address1, s.Address2, s.Phone, s.Web, s.Email, s.City} {
		if len([]rune(f)) > maxAssetText {
			badRequest(w, "Los datos de la empresa no pueden tener más de 200 caracteres por campo.")
			return
		}
	}
	for _, list := range [][]string{s.Clauses.Entrega, s.Clauses.Devolucion} {
		if len(list) > 15 {
			badRequest(w, "Cada tipo de acta admite como mucho 15 cláusulas.")
			return
		}
		for _, c := range list {
			if len([]rune(c)) > 1000 {
				badRequest(w, "Cada cláusula puede tener como mucho 1000 caracteres.")
				return
			}
		}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.d.Store.SaveDeliverySettings(r.Context(), raw); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.DeliverySettingsSaved, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusOK, s)
}
