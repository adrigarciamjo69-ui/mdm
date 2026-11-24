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
	// El correo de los equipos de cada persona sigue al del directorio.
	if _, err := tx.Exec(ctx, `UPDATE devices d SET assigned_user = COALESCE(NULLIF(u.email, ''), u.upn)
	    FROM assets a JOIN directory_users u ON u.id = a.owner_id
	    WHERE a.device_id = d.id AND d.assigned_user <> COALESCE(NULLIF(u.email, ''), u.upn)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
