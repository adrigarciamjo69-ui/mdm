package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSession(ctx context.Context, se Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id, admin_id, csrf_token, verified, created_at, expires_at, ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		se.TokenHash, se.AdminID, se.CSRFToken, se.Verified, se.CreatedAt, se.ExpiresAt, se.IP)
	if err != nil {
		return fmt.Errorf("crear sesión: %w", err)
	}
	return nil
}

// SessionByTokenHash devuelve la sesión y su administrador. Una sesión
// caducada, o de un administrador desactivado, no existe a todos los efectos.
func (s *Store) SessionByTokenHash(ctx context.Context, hash string, now time.Time) (Session, Admin, error) {
	var se Session
	var a Admin
	err := s.pool.QueryRow(ctx,
		`SELECT s.id, s.admin_id, s.csrf_token, s.verified, s.pending_totp, s.created_at, s.expires_at, s.ip,
		        a.id, a.email, a.password_hash, a.totp_secret, a.role, a.disabled, a.created_at, a.last_login_at
		 FROM sessions s JOIN admins a ON a.id = s.admin_id
		 WHERE s.id = $1 AND s.expires_at > $2 AND NOT a.disabled`, hash, now).Scan(
		&se.TokenHash, &se.AdminID, &se.CSRFToken, &se.Verified, &se.PendingTOTP, &se.CreatedAt, &se.ExpiresAt, &se.IP,
		&a.ID, &a.Email, &a.PasswordHash, &a.TOTPSecret, &a.Role, &a.Disabled, &a.CreatedAt, &a.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, Admin{}, ErrNotFound
	}
	if err != nil {
		return Session{}, Admin{}, fmt.Errorf("leer sesión: %w", err)
	}
	return se, a, nil
}

// VerifySession marca la sesión como completa tras superar el segundo factor.
func (s *Store) VerifySession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET verified = true, pending_totp = NULL WHERE id = $1`, tokenHash)
	return err
}

// SetSessionPendingTOTP guarda el secreto propuesto durante el alta del segundo factor.
func (s *Store) SetSessionPendingTOTP(ctx context.Context, tokenHash, secret string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET pending_totp = $2 WHERE id = $1`, tokenHash, secret)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, tokenHash)
	return err
}

func (s *Store) DeleteSessionsForAdmin(ctx context.Context, adminID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE admin_id = $1`, adminID)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return tag.RowsAffected(), err
}
