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

// DirectoryUser es una persona sincronizada desde Entra ID.
type DirectoryUser struct {
	ID          uuid.UUID
	EntraID     string
	DisplayName string
	Email       string
	UPN         string
	Department  string
	JobTitle    string
	EmployeeID  string
	Phone       string
	Office      string
	Enabled     bool
	DeletedAt   *time.Time
	SyncedAt    time.Time
	Notes       string
}

// Contact es el correo de la persona o, si no tiene, su UPN.
func (u DirectoryUser) Contact() string {
	if u.Email != "" {
		return u.Email
	}
	return u.UPN
}

const dirCols = `u.id, u.entra_id, u.display_name, u.email, u.upn, u.department, u.job_title, u.employee_id, u.phone,
	u.office, u.enabled, u.deleted_at, u.synced_at, u.notes`

func dirDest(u *DirectoryUser) []any {
	return []any{&u.ID, &u.EntraID, &u.DisplayName, &u.Email, &u.UPN, &u.Department, &u.JobTitle, &u.EmployeeID,
		&u.Phone, &u.Office, &u.Enabled, &u.DeletedAt, &u.SyncedAt, &u.Notes}
}

func (s *Store) dirUserWhere(ctx context.Context, cond string, arg any) (DirectoryUser, error) {
	var u DirectoryUser
	err := s.pool.QueryRow(ctx, `SELECT `+dirCols+` FROM directory_users u WHERE `+cond, arg).Scan(dirDest(&u)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return DirectoryUser{}, ErrNotFound
	}
	return u, err
}

func (s *Store) DirectoryUserByID(ctx context.Context, id uuid.UUID) (DirectoryUser, error) {
	return s.dirUserWhere(ctx, "u.id = $1", id)
}

func (s *Store) DirectoryUserByEntraID(ctx context.Context, id string) (DirectoryUser, error) {
	return s.dirUserWhere(ctx, "u.entra_id = $1", id)
}

// DirectoryUserByContact busca por correo o UPN, sin distinguir mayúsculas.
func (s *Store) DirectoryUserByContact(ctx context.Context, contact string) (DirectoryUser, error) {
	c := strings.ToLower(strings.TrimSpace(contact))
	if c == "" {
		return DirectoryUser{}, ErrNotFound
	}
	return s.dirUserWhere(ctx, "(lower(u.email) = $1 OR lower(u.upn) = $1) ORDER BY u.deleted_at NULLS FIRST LIMIT 1", c)
}

// ApplyDirectorySync guarda el resultado completo de una sincronización: crea
// y actualiza por entra_id, marca como eliminados a los que ya no vienen y
// recupera a los que vuelven. Todo o nada.
func (s *Store) ApplyDirectorySync(ctx context.Context, users []DirectoryUser, at time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.EntraID)
		if _, err := tx.Exec(ctx,
			`INSERT INTO directory_users (entra_id, display_name, email, upn, department, job_title, employee_id, phone, office, enabled, synced_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 ON CONFLICT (entra_id) DO UPDATE SET display_name = EXCLUDED.display_name, email = EXCLUDED.email,
			     upn = EXCLUDED.upn, department = EXCLUDED.department, job_title = EXCLUDED.job_title,
			     employee_id = EXCLUDED.employee_id, phone = EXCLUDED.phone, office = EXCLUDED.office,
			     enabled = EXCLUDED.enabled, synced_at = EXCLUDED.synced_at, deleted_at = NULL`,
			u.EntraID, u.DisplayName, u.Email, u.UPN, u.Department, u.JobTitle, u.EmployeeID, u.Phone, u.Office, u.Enabled, at); err != nil {
			return fmt.Errorf("guardar usuario %s: %w", u.EntraID, err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE directory_users SET deleted_at = $2 WHERE deleted_at IS NULL AND NOT (entra_id = ANY($1))`, ids, at); err != nil {
		return err
	}
	// El departamento de los activos de cada persona sigue al de Entra ID, y
	// sus equipos recalculan los grupos dinámicos.
	rows, err := tx.Query(ctx, `UPDATE assets a SET department = u.department, updated_at = now()
	    FROM directory_users u
	    WHERE u.id = a.owner_id AND u.department <> '' AND a.department <> u.department AND a.deleted_at IS NULL
	    RETURNING a.device_id`)
	if err != nil {
		return err
	}
	var changed []uuid.UUID
	for rows.Next() {
		var dev *uuid.UUID
		if err := rows.Scan(&dev); err != nil {
			rows.Close()
			return err
		}
		if dev != nil {
			changed = append(changed, *dev)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, d := range changed {
		if err := EnqueueRecomputeDevice(ctx, tx, d, at); err != nil {
			return err
		}
	}
	// El correo de los equipos de cada persona sigue al del directorio.
	if _, err := tx.Exec(ctx, `UPDATE devices d SET assigned_user = COALESCE(NULLIF(u.email, ''), u.upn)
	    FROM assets a JOIN directory_users u ON u.id = a.owner_id
	    WHERE a.device_id = d.id AND d.assigned_user <> COALESCE(NULLIF(u.email, ''), u.upn)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type DirectoryFilter struct {
	Q          string
	Department string
	// Status: active, disabled, deleted o leavers (deshabilitados o
	// eliminados en Entra que aún tienen activos).
	Status  string
	Page    int
	PerPage int
}

type DirectoryUserRow struct {
	DirectoryUser
	AssetCount int
}

// ListDirectoryUsers devuelve una página de personas con su número de activos.
func (s *Store) ListDirectoryUsers(ctx context.Context, f DirectoryFilter) ([]DirectoryUserRow, int, error) {
	var conds []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Q != "" {
		p := arg("%" + escapeLike(f.Q) + "%")
		conds = append(conds, fmt.Sprintf("(u.display_name ILIKE %[1]s OR u.email ILIKE %[1]s OR u.upn ILIKE %[1]s OR u.employee_id ILIKE %[1]s)", p))
	}
	if f.Department != "" {
		conds = append(conds, "u.department = "+arg(f.Department))
	}
	const count = `(SELECT count(*) FROM assets a WHERE a.owner_id = u.id AND a.deleted_at IS NULL)`
	switch f.Status {
	case "active":
		conds = append(conds, "u.deleted_at IS NULL AND u.enabled")
	case "disabled":
		conds = append(conds, "u.deleted_at IS NULL AND NOT u.enabled")
	case "deleted":
		conds = append(conds, "u.deleted_at IS NOT NULL")
	case "leavers":
		conds = append(conds, "(u.deleted_at IS NOT NULL OR NOT u.enabled) AND "+count+" > 0")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM directory_users u`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page, perPage := normalizePage(f.Page, f.PerPage)
	args = append(args, perPage, (page-1)*perPage)
	rows, err := s.pool.Query(ctx, `SELECT `+dirCols+`, `+count+` FROM directory_users u`+where+
		fmt.Sprintf(" ORDER BY lower(u.display_name), u.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DirectoryUserRow{}
	for rows.Next() {
		var r DirectoryUserRow
		if err := rows.Scan(append(dirDest(&r.DirectoryUser), &r.AssetCount)...); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DirectoryDepartments devuelve los departamentos distintos, para los filtros.
func (s *Store) DirectoryDepartments(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT department FROM directory_users WHERE department <> '' AND deleted_at IS NULL ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDirectoryUserNotes guarda las notas propias de una persona.
func (s *Store) SetDirectoryUserNotes(ctx context.Context, id uuid.UUID, notes string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE directory_users SET notes = $2 WHERE id = $1`, id, notes)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// EntraConfig es la configuración y el estado de la conexión con Entra ID.
type EntraConfig struct {
	TenantID      string
	ClientID      string
	SealedSecret  string
	SecretExpires *time.Time
	LastSyncAt    *time.Time
	LastSuccessAt *time.Time
	LastError     string
	LastCount     int
}

func (s *Store) EntraConfig(ctx context.Context) (EntraConfig, error) {
	var c EntraConfig
	err := s.pool.QueryRow(ctx, `SELECT tenant_id, client_id, sealed_secret, secret_expires, last_sync_at, last_success_at,
	        last_error, last_count FROM entra_config`).
		Scan(&c.TenantID, &c.ClientID, &c.SealedSecret, &c.SecretExpires, &c.LastSyncAt, &c.LastSuccessAt, &c.LastError, &c.LastCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return EntraConfig{}, nil
	}
	return c, err
}

// SaveEntraConfig guarda las credenciales (no toca el estado de la última
// sincronización).
func (s *Store) SaveEntraConfig(ctx context.Context, c EntraConfig) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO entra_config (id, tenant_id, client_id, sealed_secret, secret_expires)
	    VALUES (true, $1, $2, $3, $4)
	    ON CONFLICT (id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id, client_id = EXCLUDED.client_id,
	        sealed_secret = EXCLUDED.sealed_secret, secret_expires = EXCLUDED.secret_expires`,
		c.TenantID, c.ClientID, c.SealedSecret, c.SecretExpires)
	return err
}

// RecordDirectorySync guarda el resultado de un intento de sincronización.
func (s *Store) RecordDirectorySync(ctx context.Context, errMsg string, count int, at time.Time) error {
	if errMsg == "" {
		_, err := s.pool.Exec(ctx, `UPDATE entra_config SET last_sync_at = $1, last_success_at = $1, last_error = '', last_count = $2`, at, count)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE entra_config SET last_sync_at = $1, last_error = $2`, at, errMsg)
	return err
}
