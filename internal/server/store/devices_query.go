package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Problemas por los que se puede filtrar el listado.
const (
	IssueLowDisk     = "low_disk"
	IssueUnencrypted = "unencrypted"
	IssueStale       = "stale"
)

type DeviceFilter struct {
	Q         string
	Platform  string
	OSVersion string
	Tag       string
	Status    Status
	// Compliance filtra por estado de cumplimiento: compliant, noncompliant o unknown.
	Compliance string
	Issue      string
	GroupID    *uuid.UUID
	Sort       string
	Order      string
	Page       int
	PerPage    int
	// All devuelve todas las filas, sin paginar (exportación).
	All              bool
	Now              time.Time
	CheckinIntervalS int
}

type DeviceRow struct {
	Device
	GroupName  string
	Compliance string
}

// deviceSorts asocia los valores admitidos de sort a su expresión SQL.
var deviceSorts = map[string]string{
	"hostname":     "lower(d.hostname)",
	"last_seen_at": "d.last_seen_at",
	"enrolled_at":  "d.enrolled_at",
	"os_version":   "d.os_version",
}

// ValidDeviceSort informa de si sort es un criterio de ordenación admitido.
func ValidDeviceSort(sort string) bool {
	_, ok := deviceSorts[sort]
	return ok
}

const (
	sqlLowDisk = `d.revoked_at IS NULL AND EXISTS (
		SELECT 1 FROM device_inventory i, jsonb_array_elements(i.data->'disks') x
		WHERE i.device_id = d.id AND (x->>'size_bytes')::numeric > 0
		  AND (x->>'free_bytes')::numeric / (x->>'size_bytes')::numeric < %g)`
	sqlUnencrypted = `d.revoked_at IS NULL AND EXISTS (
		SELECT 1 FROM device_inventory i
		WHERE i.device_id = d.id AND i.data->'security'->>'disk_encryption' = 'off')`
)

// where compone las condiciones del filtro y sus argumentos.
func (f DeviceFilter) where() (string, []any) {
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	c := cutoffs(f.Now, f.CheckinIntervalS)

	if f.Q != "" {
		p := arg("%" + escapeLike(f.Q) + "%")
		conds = append(conds, fmt.Sprintf("(d.hostname ILIKE %[1]s OR d.serial ILIKE %[1]s OR d.assigned_user ILIKE %[1]s)", p))
	}
	if f.Platform != "" {
		conds = append(conds, "d.platform = "+arg(f.Platform))
	}
	if f.OSVersion != "" {
		conds = append(conds, "d.os_version = "+arg(f.OSVersion))
	}
	if f.Tag != "" {
		conds = append(conds, arg(f.Tag)+" = ANY(d.tags)")
	}
	if f.GroupID != nil {
		g := arg(*f.GroupID)
		conds = append(conds, fmt.Sprintf("(d.group_id = %[1]s OR EXISTS (SELECT 1 FROM group_members m WHERE m.group_id = %[1]s AND m.device_id = d.id))", g))
	}
	switch f.Status {
	case StatusRevoked:
		conds = append(conds, "d.revoked_at IS NOT NULL")
	case StatusOnline:
		conds = append(conds, "d.revoked_at IS NULL AND d.last_seen_at > "+onlineCutoffSQL(arg(c.online), arg(c.mobileOnline)))
	case StatusInactive:
		conds = append(conds, "d.revoked_at IS NULL AND d.last_seen_at <= "+arg(c.inactive))
	case StatusOffline:
		conds = append(conds, fmt.Sprintf(
			"d.revoked_at IS NULL AND (d.last_seen_at IS NULL OR (d.last_seen_at <= %s AND d.last_seen_at > %s))",
			onlineCutoffSQL(arg(c.online), arg(c.mobileOnline)), arg(c.inactive)))
	}
	switch f.Compliance {
	case "compliant", "noncompliant":
		conds = append(conds, "EXISTS (SELECT 1 FROM device_compliance c2 WHERE c2.device_id = d.id AND c2.state = "+arg(f.Compliance)+")")
	case "unknown":
		conds = append(conds, "NOT EXISTS (SELECT 1 FROM device_compliance c2 WHERE c2.device_id = d.id AND c2.state <> 'unknown')")
	}
	switch f.Issue {
	case IssueLowDisk:
		conds = append(conds, fmt.Sprintf(sqlLowDisk, lowDiskRatio))
	case IssueUnencrypted:
		conds = append(conds, sqlUnencrypted)
	case IssueStale:
		conds = append(conds, "d.revoked_at IS NULL AND COALESCE(d.last_seen_at, d.enrolled_at) <= "+arg(c.stale))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListDevices devuelve una página de dispositivos y el total que cumple el filtro.
func (s *Store) ListDevices(ctx context.Context, f DeviceFilter) ([]DeviceRow, int, error) {
	where, args := f.where()
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM devices d`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar dispositivos: %w", err)
	}

	sortExpr, ok := deviceSorts[f.Sort]
	if !ok {
		sortExpr = deviceSorts["hostname"]
	}
	dir := "ASC"
	if strings.EqualFold(f.Order, "desc") {
		dir = "DESC"
	}
	q := `SELECT ` + deviceCols + `, COALESCE(g.name, ''), COALESCE(c.state, 'unknown')
	      FROM devices d LEFT JOIN groups g ON g.id = d.group_id
	      LEFT JOIN device_compliance c ON c.device_id = d.id` + where +
		fmt.Sprintf(" ORDER BY %s %s NULLS LAST, d.id", sortExpr, dir)
	if !f.All {
		page, perPage := normalizePage(f.Page, f.PerPage)
		args = append(args, perPage, (page-1)*perPage)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar dispositivos: %w", err)
	}
	defer rows.Close()
	out := []DeviceRow{}
	for rows.Next() {
		var r DeviceRow
		if err := rows.Scan(append(deviceDest(&r.Device), &r.GroupName, &r.Compliance)...); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DeviceRowByID devuelve el dispositivo con el nombre de su grupo.
func (s *Store) DeviceRowByID(ctx context.Context, id uuid.UUID) (DeviceRow, error) {
	d, err := s.DeviceByID(ctx, id)
	if err != nil {
		return DeviceRow{}, err
	}
	r := DeviceRow{Device: d, Compliance: "unknown"}
	if err := s.pool.QueryRow(ctx, `SELECT state FROM device_compliance WHERE device_id = $1`, id).Scan(&r.Compliance); err != nil && err != pgx.ErrNoRows {
		return DeviceRow{}, err
	}
	if d.GroupID != nil {
		g, err := s.GroupByID(ctx, *d.GroupID)
		if err == nil {
			r.GroupName = g.Name
		} else if err != ErrNotFound {
			return DeviceRow{}, err
		}
	}
	return r, nil
}

// NormalizePage expone la normalización de paginación para que la API
// devuelva los mismos valores que aplica el almacén.
func NormalizePage(page, perPage int) (int, int) { return normalizePage(page, perPage) }
