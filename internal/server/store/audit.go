package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	details := e.Details
	if len(details) == 0 {
		details = []byte(`{}`)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (at, actor_admin_id, actor_email, action, target_type, target_id, details, ip)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.At, e.ActorAdminID, e.ActorEmail, e.Action, e.TargetType, e.TargetID, details, e.IP)
	if err != nil {
		return fmt.Errorf("escribir auditoría: %w", err)
	}
	return nil
}

type AuditFilter struct {
	ActorEmail string
	Action     string
	From, To   *time.Time
	Page       int
	PerPage    int
}

// ListAudit devuelve una página del registro, de más reciente a más antiguo,
// y el total de entradas que cumplen el filtro.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEntry, int, error) {
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.ActorEmail != "" {
		add("actor_email ILIKE $%d", "%"+escapeLike(f.ActorEmail)+"%")
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.From != nil {
		add("at >= $%d", *f.From)
	}
	if f.To != nil {
		add("at <= $%d", *f.To)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contar auditoría: %w", err)
	}
	page, perPage := normalizePage(f.Page, f.PerPage)
	args = append(args, perPage, (page-1)*perPage)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, at, actor_admin_id, actor_email, action, target_type, target_id, details, ip
		 FROM audit_log%s ORDER BY at DESC, id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listar auditoría: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.ActorAdminID, &e.ActorEmail, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.IP); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

const (
	DefaultPerPage = 25
	MaxPerPage     = 200
)

// normalizePage acota la paginación: página mínima 1, 25 por defecto, 200 como máximo.
func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	return page, perPage
}

// escapeLike neutraliza los comodines de LIKE en texto que viene del usuario.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
