// Package reports define los informes predefinidos, los genera (vista
// paginada o CSV) y ejecuta los envíos programados.
package reports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"mdm/internal/server/store"
)

type Definition struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Columns     []string `json:"columns"`
	// Filters son los filtros propios (además de grupo y plataforma).
	Filters []string `json:"filters"`
	sql     string   // SELECT … con $1 grupo, $2 plataforma, $3 q, $4 días; columnas de texto
	order   string
}

// Filters son los filtros de un informe.
type Filters struct {
	GroupID  *uuid.UUID `json:"group_id,omitempty"`
	Platform string     `json:"platform,omitempty"`
	Q        string     `json:"q,omitempty"`
	Days     int        `json:"days,omitempty"`
}

// dev es la condición común: equipo no revocado, del grupo y la plataforma pedidos.
const dev = `d.revoked_at IS NULL AND ($1::uuid IS NULL OR d.group_id = $1 OR EXISTS (SELECT 1 FROM group_members m WHERE m.group_id = $1 AND m.device_id = d.id))
	AND ($2 = '' OR d.platform = $2) AND $3::text IS NOT NULL AND $4::int IS NOT NULL`

var defs = []Definition{
	{Name: "hardware", Title: "Hardware", Description: "Una fila por equipo con su hardware, sistema, usuario y grupo.",
		Columns: []string{"Equipo", "Fabricante", "Modelo", "Serie", "CPU", "Memoria (GB)", "Disco (GB)", "Sistema", "Usuario", "Grupo"},
		sql: `SELECT d.hostname, d.manufacturer, d.model, d.serial, COALESCE(i.data->'hardware'->>'cpu_model', ''),
			COALESCE(round((i.data->'hardware'->>'memory_bytes')::numeric / 1073741824)::text, ''),
			COALESCE((SELECT round(sum((x->>'size_bytes')::numeric) / 1073741824)::text FROM jsonb_array_elements(i.data->'disks') x), ''),
			trim(d.os_name || ' ' || d.os_version), d.assigned_user, COALESCE(g.name, '')
			FROM devices d LEFT JOIN device_inventory i ON i.device_id = d.id LEFT JOIN groups g ON g.id = d.group_id WHERE ` + dev,
		order: `lower(d.hostname)`},
	{Name: "software_by_app", Title: "Software por programa", Description: "Cada programa y versión con el número de equipos que lo tienen.",
		Columns: []string{"Programa", "Versión", "Equipos"}, Filters: []string{"q"},
		sql: `SELECT s.name, s.version, count(DISTINCT d.id)::text FROM device_software s JOIN devices d ON d.id = s.device_id
			WHERE ` + dev + ` AND ($3 = '' OR s.name ILIKE '%' || $3 || '%') GROUP BY s.name, s.version`,
		order: `count(DISTINCT d.id) DESC, lower(s.name), s.version`},
	{Name: "software_by_device", Title: "Software por equipo", Description: "Cada programa instalado en cada equipo.",
		Columns: []string{"Equipo", "Programa", "Versión", "Editor"}, Filters: []string{"q"},
		sql: `SELECT d.hostname, s.name, s.version, s.publisher FROM device_software s JOIN devices d ON d.id = s.device_id
			WHERE ` + dev + ` AND ($3 = '' OR s.name ILIKE '%' || $3 || '%')`,
		order: `lower(d.hostname), lower(s.name)`},
	{Name: "compliance", Title: "Cumplimiento", Description: "Cada ajuste que un equipo no cumple, con su política y desde cuándo.",
		Columns: []string{"Equipo", "Política", "Ajuste", "Estado", "Detalle", "No cumple desde"},
		sql: `SELECT d.hostname, COALESCE(p.name, ''), r.key, r.status, r.message, COALESCE(to_char(c.noncompliant_since AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI'), '')
			FROM device_setting_results r JOIN devices d ON d.id = r.device_id LEFT JOIN policies p ON p.id = r.policy_id
			LEFT JOIN device_compliance c ON c.device_id = d.id WHERE ` + dev + ` AND r.status IN ('noncompliant', 'error')`,
		order: `lower(d.hostname), r.key`},
	{Name: "patches_by_device", Title: "Parches pendientes por equipo", Description: "Equipos con actualizaciones pendientes: cuántas, de seguridad y la más antigua.",
		Columns: []string{"Equipo", "Pendientes", "De seguridad", "Pendiente desde"},
		sql: `SELECT d.hostname, count(*)::text, count(*) FILTER (WHERE x.security)::text, to_char(min(u.first_seen_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD')
			FROM device_updates u JOIN updates x ON x.platform = u.platform AND x.id = u.update_id JOIN devices d ON d.id = u.device_id
			WHERE ` + dev + ` AND u.state IN ('missing', 'failed') GROUP BY d.id, d.hostname`,
		order: `count(*) FILTER (WHERE x.security) DESC, count(*) DESC, lower(d.hostname)`},
	{Name: "patches_by_update", Title: "Parches pendientes por actualización", Description: "Cada actualización pendiente con el número de equipos que la necesitan.",
		Columns: []string{"Actualización", "KB", "Clasificación", "Plataforma", "Equipos"},
		sql: `SELECT x.title, x.kb, x.classification, x.platform, count(DISTINCT d.id)::text
			FROM device_updates u JOIN updates x ON x.platform = u.platform AND x.id = u.update_id JOIN devices d ON d.id = u.device_id
			WHERE ` + dev + ` AND u.state IN ('missing', 'failed') GROUP BY x.platform, x.id, x.title, x.kb, x.classification`,
		order: `count(DISTINCT d.id) DESC, x.title`},
	{Name: "deployments", Title: "Despliegues", Description: "Cada despliegue y equipo con su estado.",
		Columns: []string{"Aplicación", "Acción", "Equipo", "Estado", "Detalle", "Actualizado"},
		sql: `SELECT a.name, dp.intent, d.hostname, st.state, st.message, COALESCE(to_char(st.updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI'), '')
			FROM deployment_status st JOIN deployments dp ON dp.id = st.deployment_id JOIN apps a ON a.id = dp.app_id JOIN devices d ON d.id = st.device_id
			WHERE ` + dev,
		order: `lower(a.name), (st.state = 'failed') DESC, lower(d.hostname)`},
	{Name: "inactive", Title: "Equipos inactivos", Description: "Equipos sin conexión desde hace más de los días indicados.",
		Columns: []string{"Equipo", "Plataforma", "Usuario", "Grupo", "Última conexión", "Días"}, Filters: []string{"days"},
		sql: `SELECT d.hostname, d.platform, d.assigned_user, COALESCE(g.name, ''),
			COALESCE(to_char(d.last_seen_at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI'), 'nunca'),
			COALESCE(floor(extract(epoch FROM now() - d.last_seen_at) / 86400)::text, '')
			FROM devices d LEFT JOIN groups g ON g.id = d.group_id
			WHERE ` + dev + ` AND COALESCE(d.last_seen_at, d.enrolled_at) < now() - make_interval(days => $4)`,
		order: `d.last_seen_at NULLS FIRST`},
	{Name: "android_patch_level", Title: "Nivel de parche de Android", Description: "Los móviles con su nivel de parche de seguridad, del más antiguo al más reciente.",
		Columns: []string{"Móvil", "Modelo", "Android", "Parche de seguridad", "Usuario"},
		sql: `SELECT d.hostname, d.model, d.os_version, COALESCE(i.data->'mobile'->>'security_patch_level', ''), d.assigned_user
			FROM devices d LEFT JOIN device_inventory i ON i.device_id = d.id WHERE ` + dev + ` AND d.platform = 'android'`,
		order: `COALESCE(i.data->'mobile'->>'security_patch_level', '') NULLS FIRST, lower(d.hostname)`},
	{Name: "assets", Title: "Inventario de activos", Description: "Todos los activos del inventario, con y sin agente, con su persona, estado y datos de compra.",
		Columns: []string{"Nombre", "Categoría", "Marca", "Modelo", "Número de serie", "Etiqueta", "Estado", "Persona", "Departamento",
			"Ubicación", "Origen", "Precio", "Fecha de compra", "Garantía hasta", "Pedido", "Proveedor"},
		Filters: []string{"q"},
		sql: `SELECT a.name, c.name, a.brand, a.model, a.serial, a.asset_tag,
			CASE a.status WHEN 'en_uso' THEN 'En uso' WHEN 'en_almacen' THEN 'En almacén' WHEN 'en_reparacion' THEN 'En reparación' ELSE 'Baja' END,
			COALESCE(u.display_name, ''), a.department, a.location, CASE WHEN a.device_id IS NULL THEN 'Manual' ELSE 'Agente' END,
			COALESCE(to_char(a.price, 'FM9999999990.00'), ''), COALESCE(to_char(a.purchase_date, 'YYYY-MM-DD'), ''),
			COALESCE(to_char(a.warranty_until, 'YYYY-MM-DD'), ''), a.purchase_order, a.supplier
			FROM assets a JOIN asset_categories c ON c.id = a.category_id LEFT JOIN directory_users u ON u.id = a.owner_id
			WHERE a.deleted_at IS NULL AND $1::uuid IS NULL IS NOT NULL AND $2::text IS NOT NULL AND $4::int IS NOT NULL
			AND ($3 = '' OR a.name ILIKE '%' || $3 || '%' OR a.serial ILIKE '%' || $3 || '%' OR c.name ILIKE '%' || $3 || '%'
			     OR COALESCE(u.display_name, '') ILIKE '%' || $3 || '%')`,
		order: `lower(c.name), lower(a.name)`},
	{Name: "licenses", Title: "Licencias", Description: "Cada licencia con sus puestos, asignaciones, instalaciones detectadas y caducidad.",
		Columns: []string{"Licencia", "Fabricante", "Tipo", "Estado", "Puestos", "Asignadas", "Instaladas", "Caducidad", "Precio", "Departamento"},
		Filters: []string{"q"},
		sql: `SELECT l.name, l.vendor, l.license_type,
			CASE WHEN l.status = 'activa' AND l.expiry_date < current_date THEN 'expirada' ELSE l.status END,
			l.seats::text, (SELECT count(*) FROM license_assignments la WHERE la.license_id = l.id)::text,
			(SELECT count(DISTINCT s.device_id) FROM device_software s JOIN devices dv ON dv.id = s.device_id
			 WHERE l.match_pattern <> '' AND dv.revoked_at IS NULL AND s.name ILIKE '%' || l.match_pattern || '%')::text,
			COALESCE(to_char(l.expiry_date, 'YYYY-MM-DD'), ''), COALESCE(to_char(l.price, 'FM9999999990.00'), ''), l.department
			FROM licenses l WHERE $1::uuid IS NULL IS NOT NULL AND $2::text IS NOT NULL AND $4::int IS NOT NULL
			AND ($3 = '' OR l.name ILIKE '%' || $3 || '%' OR l.vendor ILIKE '%' || $3 || '%')`,
		order: `lower(l.name)`},
}

// All devuelve las definiciones (con Filters nunca nulo: la consola lo recorre).
func All() []Definition {
	out := make([]Definition, len(defs))
	for i, d := range defs {
		if d.Filters == nil {
			d.Filters = []string{}
		}
		out[i] = d
	}
	return out
}

func Find(name string) (Definition, bool) {
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return Definition{}, false
}

// ParseFilters lee y valida los filtros de un informe.
func ParseFilters(name string, raw json.RawMessage) (Filters, error) {
	var in struct {
		GroupID  string `json:"group_id"`
		Platform string `json:"platform"`
		Q        string `json:"q"`
		Days     *int   `json:"days"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return Filters{}, errors.New("Los filtros deben ser un objeto JSON.")
		}
	}
	f := Filters{Platform: in.Platform, Q: in.Q}
	if in.Days != nil {
		f.Days = *in.Days
	}
	if in.GroupID != "" {
		id, err := uuid.Parse(in.GroupID)
		if err != nil {
			return f, errors.New("El grupo del filtro no es válido.")
		}
		f.GroupID = &id
	}
	if f.Platform != "" && f.Platform != "windows" && f.Platform != "linux" && f.Platform != "android" {
		return f, errors.New("La plataforma del filtro debe ser windows, linux o android.")
	}
	if len(f.Q) > 100 {
		return f, errors.New("El texto de búsqueda es demasiado largo.")
	}
	if name == "inactive" {
		if in.Days == nil {
			f.Days = 30
		}
		if f.Days < 1 || f.Days > 3650 {
			return f, errors.New("Los días deben estar entre 1 y 3.650.")
		}
	}
	return f, nil
}

type Result struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Total   int        `json:"total"`
}

// MaxRows es el máximo de filas de una exportación.
const MaxRows = 200000

// Run genera una página del informe (perPage 0 = todas, hasta MaxRows).
func Run(ctx context.Context, s *store.Store, name string, f Filters, page, perPage int) (Result, error) {
	d, ok := Find(name)
	if !ok {
		return Result{}, errors.New("informe desconocido")
	}
	if name == "inactive" && f.Days == 0 {
		f.Days = 30
	}
	args := []any{f.GroupID, f.Platform, f.Q, f.Days}
	var total int
	if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM (`+d.sql+`) t`, args...).Scan(&total); err != nil {
		return Result{}, fmt.Errorf("contar %s: %w", name, err)
	}
	limit, offset := MaxRows, 0
	if perPage > 0 {
		limit, offset = perPage, (max(page, 1)-1)*perPage
	}
	rows, err := s.Pool().Query(ctx, d.sql+` ORDER BY `+d.order+fmt.Sprintf(` LIMIT %d OFFSET %d`, limit, offset), args...)
	if err != nil {
		return Result{}, fmt.Errorf("informe %s: %w", name, err)
	}
	defer rows.Close()
	out := Result{Columns: d.Columns, Rows: [][]string{}, Total: total}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return Result{}, err
		}
		rec := make([]string, len(vals))
		for i, v := range vals {
			if v != nil {
				rec[i] = fmt.Sprint(v)
			}
		}
		out.Rows = append(out.Rows, rec)
	}
	return out, rows.Err()
}

func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// WriteCSV escribe el informe en UTF-8 con BOM, con las celdas protegidas
// contra fórmulas (como la exportación de dispositivos).
func WriteCSV(w io.Writer, header []string, rows [][]string) {
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	_ = cw.Write(header)
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, v := range r {
			rec[i] = csvSafe(v)
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}

// CSV genera el informe completo como CSV.
func CSV(ctx context.Context, s *store.Store, name string, f Filters) ([]byte, int, error) {
	r, err := Run(ctx, s, name, f, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	var buf bytes.Buffer
	WriteCSV(&buf, r.Columns, r.Rows)
	return buf.Bytes(), r.Total, nil
}
