package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const adminCols = `id, email, password_hash, totp_secret, role, disabled, created_at, last_login_at`

func scanAdmin(row pgx.Row) (Admin, error) {
	var a Admin
	err := row.Scan(&a.ID, &a.Email, &a.PasswordHash, &a.TOTPSecret, &a.Role, &a.Disabled, &a.CreatedAt, &a.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	return a, err
}

// CreateAdmin crea un administrador. Devuelve ErrConflict si el correo ya
// existe (sin distinguir mayúsculas).
func (s *Store) CreateAdmin(ctx context.Context, email, passwordHash, role string) (Admin, error) {
	a, err := scanAdmin(s.pool.QueryRow(ctx,
		`INSERT INTO admins (email, password_hash, role) VALUES ($1, $2, $3) RETURNING `+adminCols,
		email, passwordHash, role))
	if isUniqueViolation(err) {
		return Admin{}, ErrConflict
	}
	if err != nil {
		return Admin{}, fmt.Errorf("crear administrador: %w", err)
	}
	return a, nil
}

func (s *Store) AdminByEmail(ctx context.Context, email string) (Admin, error) {
	return scanAdmin(s.pool.QueryRow(ctx, `SELECT `+adminCols+` FROM admins WHERE lower(email) = lower($1)`, email))
}

func (s *Store) AdminByID(ctx context.Context, id uuid.UUID) (Admin, error) {
	return scanAdmin(s.pool.QueryRow(ctx, `SELECT `+adminCols+` FROM admins WHERE id = $1`, id))
}

func (s *Store) ListAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+adminCols+` FROM admins ORDER BY lower(email)`)
	if err != nil {
		return nil, fmt.Errorf("listar administradores: %w", err)
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AdminPatch lleva los cambios opcionales sobre un administrador.
type AdminPatch struct {
	Role     *string
	Disabled *bool
}

// UpdateAdmin aplica el cambio salvo que deje la instalación sin ningún
// administrador activo con rol admin, en cuyo caso devuelve ErrConflict.
// Desactivar a un administrador cierra además todas sus sesiones.
func (s *Store) UpdateAdmin(ctx context.Context, id uuid.UUID, p AdminPatch) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Bloquea a todos los administradores para que dos cambios simultáneos no
	// puedan, entre los dos, dejar la instalación sin ninguno.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM admins ORDER BY id FOR UPDATE`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE admins SET role = COALESCE($2, role), disabled = COALESCE($3, disabled) WHERE id = $1`,
		id, p.Role, p.Disabled)
	if err != nil {
		return fmt.Errorf("actualizar administrador: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM admins WHERE role = 'admin' AND NOT disabled`).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return ErrConflict
	}
	if p.Disabled != nil && *p.Disabled {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE admin_id = $1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetAdminTOTP fija o borra (con nil) el secreto del segundo factor.
func (s *Store) SetAdminTOTP(ctx context.Context, id uuid.UUID, secret *string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE admins SET totp_secret = $2 WHERE id = $1`, id, secret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchAdminLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE admins SET last_login_at = $2 WHERE id = $1`, id, at)
	return err
}
