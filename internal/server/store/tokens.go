package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrBadReference indica que la petición apunta a una fila que no existe
// (por ejemplo, un grupo inexistente).
var ErrBadReference = errors.New("referencia inexistente")

type TokenParams struct {
	Name      string
	TokenHash string
	GroupID   *uuid.UUID
	ExpiresAt *time.Time
	MaxUses   *int
	CreatedBy uuid.UUID
}

type TokenRow struct {
	EnrollmentToken
	GroupName string
}

const tokenCols = `t.id, t.name, t.token_hash, t.group_id, t.expires_at, t.max_uses, t.uses, t.revoked_at, t.created_by, t.created_at`

func (s *Store) CreateToken(ctx context.Context, p TokenParams) (EnrollmentToken, error) {
	var t EnrollmentToken
	err := s.pool.QueryRow(ctx,
		`INSERT INTO enrollment_tokens AS t (name, token_hash, group_id, expires_at, max_uses, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+tokenCols,
		p.Name, p.TokenHash, p.GroupID, p.ExpiresAt, p.MaxUses, p.CreatedBy).Scan(
		&t.ID, &t.Name, &t.TokenHash, &t.GroupID, &t.ExpiresAt, &t.MaxUses, &t.Uses, &t.RevokedAt, &t.CreatedBy, &t.CreatedAt)
	if isForeignKeyViolation(err) {
		return EnrollmentToken{}, ErrBadReference
	}
	if err != nil {
		return EnrollmentToken{}, fmt.Errorf("crear token: %w", err)
	}
	return t, nil
}

func (s *Store) ListTokens(ctx context.Context) ([]TokenRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+tokenCols+`, COALESCE(g.name, '')
		 FROM enrollment_tokens t LEFT JOIN groups g ON g.id = t.group_id
		 ORDER BY t.created_at DESC, t.id`)
	if err != nil {
		return nil, fmt.Errorf("listar tokens: %w", err)
	}
	defer rows.Close()
	out := []TokenRow{}
	for rows.Next() {
		var t TokenRow
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &t.GroupID, &t.ExpiresAt, &t.MaxUses, &t.Uses,
			&t.RevokedAt, &t.CreatedBy, &t.CreatedAt, &t.GroupName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken revoca el token. Revocar uno ya revocado no cambia nada.
func (s *Store) RevokeToken(ctx context.Context, id uuid.UUID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE enrollment_tokens SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("revocar token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Estados de un token de inscripción.
const (
	TokenActive    = "active"
	TokenExpired   = "expired"
	TokenExhausted = "exhausted"
	TokenRevoked   = "revoked"
)

// TokenState calcula el estado del token. Las condiciones coinciden con las
// que aplica EnrollDevice al consumirlo.
func TokenState(t EnrollmentToken, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return TokenRevoked
	case t.ExpiresAt != nil && !t.ExpiresAt.After(now):
		return TokenExpired
	case t.MaxUses != nil && t.Uses >= *t.MaxUses:
		return TokenExhausted
	}
	return TokenActive
}
