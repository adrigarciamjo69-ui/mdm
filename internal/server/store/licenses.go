package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tipos y estados de licencia.
var (
	LicenseTypes    = []string{"perpetua", "suscripcion", "volumen", "oem", "freeware", "open_source", "prueba"}
	LicenseStatuses = []string{"activa", "expirada", "baja"}
)

func oneOf(v string, list []string) bool {
	for _, x := range list {
		if v == x {
			return true
		}
	}
	return false
}

type License struct {
	ID            uuid.UUID
	Name          string
	Vendor        string
	Version       string
	LicenseType   string
	Seats         int
	HasKey        bool
	PurchaseDate  *time.Time
	ExpiryDate    *time.Time
	PurchaseOrder string
	Price         *float64
	Department    string
	Status        string
	MatchPattern  string
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Assigned      int
	Installed     int
}

// EffectiveStatus es «expirada» si la fecha de caducidad ya ha pasado,
// aunque el estado guardado diga otra cosa (salvo «baja»).
func (l License) EffectiveStatus(today time.Time) string {
	if l.Status == "activa" && l.ExpiryDate != nil && l.ExpiryDate.Before(dateOnly(today)) {
		return "expirada"
	}
	return l.Status
}

// Over informa de si las asignaciones o las instalaciones superan los puestos.
func (l License) Over() bool { return l.Seats > 0 && (l.Assigned > l.Seats || l.Installed > l.Seats) }

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// LicenseInput son los campos editables. Key: nil = no tocar la clave,
// "" = quitarla, otro valor = la clave cifrada nueva.
type LicenseInput struct {
	Name          string
	Vendor        string
	Version       string
	LicenseType   string
	Seats         int
	KeySealed     *string
	PurchaseDate  *time.Time
	ExpiryDate    *time.Time
	PurchaseOrder string
	Price         *float64
	Department    string
	Status        string
	MatchPattern  string
	Notes         string
}

func (in LicenseInput) valid() bool {
	return strings.TrimSpace(in.Name) != "" && oneOf(in.LicenseType, LicenseTypes) && oneOf(in.Status, LicenseStatuses) &&
		in.Seats >= 0 && (in.Price == nil || *in.Price >= 0)
}

// installedSQL cuenta los equipos (no revocados) con el patrón instalado.
const installedSQL = `(SELECT count(DISTINCT s.device_id) FROM device_software s JOIN devices dv ON dv.id = s.device_id
	WHERE l.match_pattern <> '' AND dv.revoked_at IS NULL AND s.name ILIKE '%' || l.match_pattern || '%')`

const licenseCols = `l.id, l.name, l.vendor, l.version, l.license_type, l.seats, l.key_sealed <> '', l.purchase_date, l.expiry_date,
	l.purchase_order, l.price::float8, l.department, l.status, l.match_pattern, l.notes, l.created_at, l.updated_at,
	(SELECT count(*) FROM license_assignments la WHERE la.license_id = l.id), ` + installedSQL

func licenseDest(l *License) []any {
	return []any{&l.ID, &l.Name, &l.Vendor, &l.Version, &l.LicenseType, &l.Seats, &l.HasKey, &l.PurchaseDate, &l.ExpiryDate,
		&l.PurchaseOrder, &l.Price, &l.Department, &l.Status, &l.MatchPattern, &l.Notes, &l.CreatedAt, &l.UpdatedAt,
		&l.Assigned, &l.Installed}
}

func (s *Store) CreateLicense(ctx context.Context, in LicenseInput) (License, error) {
	if !in.valid() {
		return License{}, ErrInvalid
	}
	key := ""
	if in.KeySealed != nil {
		key = *in.KeySealed
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `INSERT INTO licenses (name, vendor, version, license_type, seats, key_sealed, purchase_date,
	        expiry_date, purchase_order, price, department, status, match_pattern, notes)
	    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		strings.TrimSpace(in.Name), in.Vendor, in.Version, in.LicenseType, in.Seats, key, in.PurchaseDate, in.ExpiryDate,
		in.PurchaseOrder, in.Price, in.Department, in.Status, in.MatchPattern, in.Notes).Scan(&id)
	if err != nil {
		return License{}, err
	}
	return s.LicenseByID(ctx, id)
}

func (s *Store) UpdateLicense(ctx context.Context, id uuid.UUID, in LicenseInput) (License, error) {
	if !in.valid() {
		return License{}, ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE licenses SET name = $2, vendor = $3, version = $4, license_type = $5, seats = $6,
	        key_sealed = COALESCE($7, key_sealed), purchase_date = $8, expiry_date = $9, purchase_order = $10, price = $11,
	        department = $12, status = $13, match_pattern = $14, notes = $15, updated_at = now()
	    WHERE id = $1`, id, strings.TrimSpace(in.Name), in.Vendor, in.Version, in.LicenseType, in.Seats, in.KeySealed,
		in.PurchaseDate, in.ExpiryDate, in.PurchaseOrder, in.Price, in.Department, in.Status, in.MatchPattern, in.Notes)
	if err != nil {
		return License{}, err
	}
	if tag.RowsAffected() == 0 {
		return License{}, ErrNotFound
	}
	return s.LicenseByID(ctx, id)
}

func (s *Store) LicenseByID(ctx context.Context, id uuid.UUID) (License, error) {
	var l License
	err := s.pool.QueryRow(ctx, `SELECT `+licenseCols+` FROM licenses l WHERE l.id = $1`, id).Scan(licenseDest(&l)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return License{}, ErrNotFound
	}
	return l, err
}

func (s *Store) DeleteLicense(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM licenses WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LicenseKeySealed devuelve la clave cifrada ("" si no tiene).
func (s *Store) LicenseKeySealed(ctx context.Context, id uuid.UUID) (string, error) {
	var k string
	err := s.pool.QueryRow(ctx, `SELECT key_sealed FROM licenses WHERE id = $1`, id).Scan(&k)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return k, err
}

type LicenseFilter struct {
	Q           string
	LicenseType string
	// Status filtra por el estado efectivo (una caducada cuenta como expirada).
	Status string
	// ExpiringDays > 0: activas que caducan en los próximos días.
	ExpiringDays int
	Today        time.Time
	Page         int
	PerPage      int
	All          bool
}

func (s *Store) ListLicenses(ctx context.Context, f LicenseFilter) ([]License, int, error) {
	today := dateOnly(f.Today)
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Q != "" {
		p := arg("%" + escapeLike(f.Q) + "%")
		conds = append(conds, fmt.Sprintf("(l.name ILIKE %[1]s OR l.vendor ILIKE %[1]s OR l.department ILIKE %[1]s OR l.purchase_order ILIKE %[1]s)", p))
	}
	if f.LicenseType != "" {
		conds = append(conds, "l.license_type = "+arg(f.LicenseType))
	}
	switch f.Status {
	case "expirada":
		conds = append(conds, "(l.status = 'expirada' OR (l.status = 'activa' AND l.expiry_date < "+arg(today)+"))")
	case "activa":
		conds = append(conds, "l.status = 'activa' AND (l.expiry_date IS NULL OR l.expiry_date >= "+arg(today)+")")
	case "baja":
		conds = append(conds, "l.status = 'baja'")
	}
	if f.ExpiringDays > 0 {
		conds = append(conds, fmt.Sprintf("l.status = 'activa' AND l.expiry_date >= %s AND l.expiry_date <= %s",
			arg(today), arg(today.AddDate(0, 0, f.ExpiringDays))))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM licenses l`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + licenseCols + ` FROM licenses l` + where + ` ORDER BY lower(l.name), l.id`
	if !f.All {
		page, perPage := normalizePage(f.Page, f.PerPage)
		args = append(args, perPage, (page-1)*perPage)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []License{}
	for rows.Next() {
		var l License
		if err := rows.Scan(licenseDest(&l)...); err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

// LicensesFor devuelve las licencias asignadas a un activo o a una persona.
func (s *Store) LicensesFor(ctx context.Context, assetID, userID *uuid.UUID) ([]License, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+licenseCols+` FROM licenses l WHERE EXISTS (SELECT 1 FROM license_assignments la
	    WHERE la.license_id = l.id AND (la.asset_id = $1 OR la.user_id = $2)) ORDER BY lower(l.name)`, assetID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []License{}
	for rows.Next() {
		var l License
		if err := rows.Scan(licenseDest(&l)...); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

type LicenseAssignment struct {
	ID          uuid.UUID
	LicenseID   uuid.UUID
	AssetID     *uuid.UUID
	AssetName   string
	AssetSerial string
	UserID      *uuid.UUID
	UserName    string
	UserEmail   string
	Notes       string
	AssignedAt  time.Time
}

func (s *Store) LicenseAssignments(ctx context.Context, licenseID uuid.UUID) ([]LicenseAssignment, error) {
	rows, err := s.pool.Query(ctx, `SELECT la.id, la.license_id, la.asset_id, COALESCE(a.name, ''), COALESCE(a.serial, ''),
	        la.user_id, COALESCE(u.display_name, ''), COALESCE(NULLIF(u.email, ''), u.upn, ''), la.notes, la.assigned_at
	    FROM license_assignments la LEFT JOIN assets a ON a.id = la.asset_id LEFT JOIN directory_users u ON u.id = la.user_id
	    WHERE la.license_id = $1 ORDER BY la.assigned_at, la.id`, licenseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LicenseAssignment{}
	for rows.Next() {
		var x LicenseAssignment
		if err := rows.Scan(&x.ID, &x.LicenseID, &x.AssetID, &x.AssetName, &x.AssetSerial, &x.UserID, &x.UserName, &x.UserEmail,
			&x.Notes, &x.AssignedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// AddLicenseAssignment asigna la licencia a un activo o a una persona (uno de
// los dos). ErrConflict si ya estaba asignada.
func (s *Store) AddLicenseAssignment(ctx context.Context, licenseID uuid.UUID, assetID, userID *uuid.UUID, notes string) (uuid.UUID, error) {
	if (assetID == nil) == (userID == nil) {
		return uuid.Nil, ErrInvalid
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `INSERT INTO license_assignments (license_id, asset_id, user_id, notes) VALUES ($1, $2, $3, $4) RETURNING id`,
		licenseID, assetID, userID, notes).Scan(&id)
	switch {
	case isUniqueViolation(err):
		return uuid.Nil, ErrConflict
	case isForeignKeyViolation(err):
		return uuid.Nil, ErrNotFound
	}
	return id, err
}

func (s *Store) RemoveLicenseAssignment(ctx context.Context, licenseID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM license_assignments WHERE license_id = $1 AND id = $2`, licenseID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

type DeviceRef struct {
	ID       uuid.UUID
	Hostname string
	Software string
	Version  string
}

// LicenseInstallations lista los equipos con el software de la licencia.
func (s *Store) LicenseInstallations(ctx context.Context, licenseID uuid.UUID) ([]DeviceRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (dv.id) dv.id, dv.hostname, s.name, s.version
	    FROM licenses l JOIN device_software s ON s.name ILIKE '%' || l.match_pattern || '%'
	    JOIN devices dv ON dv.id = s.device_id
	    WHERE l.id = $1 AND l.match_pattern <> '' AND dv.revoked_at IS NULL ORDER BY dv.id, s.name`, licenseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceRef{}
	for rows.Next() {
		var d DeviceRef
		if err := rows.Scan(&d.ID, &d.Hostname, &d.Software, &d.Version); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
