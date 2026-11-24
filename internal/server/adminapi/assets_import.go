package adminapi

import (
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"strings"

	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

// importColumns son las columnas de la plantilla de importación, en orden.
var importColumns = []string{"categoria", "nombre", "marca", "modelo", "serie", "etiqueta", "estado", "persona",
	"departamento", "ubicacion", "precio", "fecha_compra", "garantia", "pedido", "proveedor", "notas"}

const (
	maxImportBytes = 5 << 20
	maxImportRows  = 5000
)

type importRowJSON struct {
	Line   int      `json:"line"`
	Name   string   `json:"name"`
	Errors []string `json:"errors"`
}

func (a *api) importTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="plantilla-inventario.csv"`)
	_, _ = io.WriteString(w, "\ufeff"+strings.Join(importColumns, ";")+"\r\n"+
		"Monitor;Monitor sala 1;Dell;P2422H;CN-0ABC123;EL-0001;en_almacen;;TIC;Planta 1;150,50;2026-01-15;2029-01-15;PO-2026-001;Proveedor S.L.;\r\n")
}

// readImportCSV lee el CSV (UTF-8, con o sin BOM, separado por ; o ,) y
// devuelve las filas de datos indexadas por nombre de columna.
func readImportCSV(raw []byte) ([]map[string]string, string) {
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
	firstLine := raw
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		firstLine = raw[:i]
	}
	cr := csv.NewReader(bytes.NewReader(raw))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		cr.Comma = ';'
	}
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, "El fichero no es un CSV válido: " + err.Error()
	}
	if len(records) < 2 {
		return nil, "El fichero no tiene filas de datos."
	}
	if len(records)-1 > maxImportRows {
		return nil, "Se pueden importar como mucho 5000 filas a la vez."
	}
	header := map[int]string{}
	for i, h := range records[0] {
		header[i] = strings.ToLower(strings.TrimSpace(h))
	}
	hasName := false
	for _, h := range header {
		if h == "nombre" {
			hasName = true
		}
	}
	if !hasName {
		return nil, "Falta la columna «nombre». Usa la plantilla."
	}
	out := make([]map[string]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := map[string]string{}
		for i, v := range rec {
			if h, ok := header[i]; ok {
				row[h] = strings.TrimSpace(v)
			}
		}
		out = append(out, row)
	}
	return out, ""
}

func (a *api) importAssets(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxImportBytes+1))
	if err != nil {
		badRequest(w, "No se ha podido leer el fichero.")
		return
	}
	if len(raw) > maxImportBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, httpx.CodeTooLarge, "El fichero supera los 5 MB.")
		return
	}
	rows, msg := readImportCSV(raw)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	cats, err := a.d.Store.ListCategories(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	catByName := map[string]string{}
	for _, c := range cats {
		catByName[strings.ToLower(c.Name)] = c.ID.String()
	}
	result := make([]importRowJSON, 0, len(rows))
	inputs := make([]store.AssetInput, 0, len(rows))
	seenSerial, seenTag := map[string]int{}, map[string]int{}
	invalid := 0
	for i, row := range rows {
		line := i + 2
		var errs []string
		body := assetInputJSON{Name: row["nombre"], Brand: row["marca"], Model: row["modelo"], Serial: row["serie"],
			AssetTag: row["etiqueta"], Status: row["estado"], Department: row["departamento"], Location: row["ubicacion"],
			PurchaseDate: row["fecha_compra"], WarrantyUntil: row["garantia"], PurchaseOrder: row["pedido"],
			Supplier: row["proveedor"], Notes: row["notas"]}
		if body.Status == "" {
			body.Status = store.AssetInStock
		}
		if id, ok := catByName[strings.ToLower(row["categoria"])]; ok {
			body.CategoryID = id
		} else {
			errs = append(errs, "La categoría «"+row["categoria"]+"» no existe.")
			body.CategoryID = cats[0].ID.String()
		}
		if p := row["precio"]; p != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(strings.ReplaceAll(p, "€", ""), ",", "."), 64)
			if err != nil {
				errs = append(errs, "El precio «"+p+"» no es un número.")
			} else {
				body.Price = &v
			}
		}
		if c := row["persona"]; c != "" {
			u, err := a.d.Store.DirectoryUserByContact(r.Context(), c)
			if err != nil {
				errs = append(errs, "No hay ninguna persona con el correo «"+c+"» en Usuarios.")
			} else {
				body.OwnerID = u.ID.String()
			}
		}
		in, msg := a.toInput(r, body)
		if msg != "" {
			if strings.Contains(msg, "fecha") || strings.Contains(msg, "AAAA") {
				msg = strings.TrimSuffix(msg, ".") + " (por ejemplo, 2026-01-15)."
			}
			errs = append(errs, msg)
		}
		if key := store.SerialKey(in.Serial); key != "" {
			if prev, dup := seenSerial[key]; dup {
				errs = append(errs, "El número de serie se repite en la línea "+strconv.Itoa(prev)+".")
			} else if used, err := a.d.Store.SerialKeyInUse(r.Context(), key); err == nil && used {
				errs = append(errs, "El número de serie ya es de otro activo del inventario.")
			}
			seenSerial[key] = line
		}
		if tag := strings.ToLower(in.AssetTag); tag != "" {
			if prev, dup := seenTag[tag]; dup {
				errs = append(errs, "La etiqueta se repite en la línea "+strconv.Itoa(prev)+".")
			} else if used, err := a.d.Store.AssetTagInUse(r.Context(), tag); err == nil && used {
				errs = append(errs, "La etiqueta ya es de otro activo del inventario.")
			}
			seenTag[tag] = line
		}
		if errs == nil {
			errs = []string{}
			inputs = append(inputs, in)
		} else {
			invalid++
		}
		result = append(result, importRowJSON{Line: line, Name: in.Name, Errors: errs})
	}
	resp := map[string]any{"rows": result, "valid": len(inputs), "invalid": invalid, "created": 0}
	if r.URL.Query().Get("dry_run") == "1" {
		httpx.WriteJSON(w, http.StatusOK, resp)
		return
	}
	if invalid > 0 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": httpx.CodeInvalidRequest,
			"message": "Hay filas con errores: corrígelas y vuelve a importar. No se ha guardado nada."}, "rows": result})
		return
	}
	n, err := a.d.Store.CreateAssets(r.Context(), inputs)
	if err != nil {
		a.assetStoreError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AssetsImported, TargetType: "asset", Details: map[string]any{"created": n}, IP: a.ip(r)})
	resp["created"] = n
	httpx.WriteJSON(w, http.StatusOK, resp)
}
