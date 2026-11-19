package store

import (
	"context"
	"errors"
	"fmt"

	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type GroupWithCount struct {
	Group
	DeviceCount int
}

const groupCols = `id, name, description, kind, rule, created_at`

func groupDest(g *Group) []any {
	return []any{&g.ID, &g.Name, &g.Description, &g.Kind, &g.Rule, &g.CreatedAt}
}

// CreateGroup crea un grupo estático. Devuelve ErrConflict si el nombre ya
// existe (sin distinguir mayúsculas).
func (s *Store) CreateGroup(ctx context.Context, name, description string) (Group, error) {
	return s.CreateGroupWith(ctx, name, description, GroupStatic, nil)
}

// CreateGroupWith crea un grupo del tipo indicado; rule solo en los dinámicos.
func (s *Store) CreateGroupWith(ctx context.Context, name, description, kind string, rule json.RawMessage) (Group, error) {
	var g Group
	err := s.pool.QueryRow(ctx,
		`INSERT INTO groups (name, description, kind, rule) VALUES ($1, $2, $3, $4) RETURNING `+groupCols,
		name, description, kind, nullJSON(rule)).Scan(groupDest(&g)...)
	if isUniqueViolation(err) {
		return Group{}, ErrConflict
	}
	if err != nil {
		return Group{}, fmt.Errorf("crear grupo: %w", err)
	}
	return g, nil
}

func (s *Store) GroupByID(ctx context.Context, id uuid.UUID) (Group, error) {
	var g Group
	err := s.pool.QueryRow(ctx, `SELECT `+groupCols+` FROM groups WHERE id = $1`, id).Scan(groupDest(&g)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	return g, err
}

func (s *Store) ListGroups(ctx context.Context) ([]GroupWithCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT g.id, g.name, g.description, g.kind, g.rule, g.created_at,
		        CASE WHEN g.kind = 'dynamic'
		             THEN (SELECT count(*) FROM group_members m WHERE m.group_id = g.id)
		             ELSE (SELECT count(*) FROM devices d WHERE d.group_id = g.id) END
		 FROM groups g ORDER BY lower(g.name)`)
	if err != nil {
		return nil, fmt.Errorf("listar grupos: %w", err)
	}
	defer rows.Close()
	out := []GroupWithCount{}
	for rows.Next() {
		var g GroupWithCount
		if err := rows.Scan(append(groupDest(&g.Group), &g.DeviceCount)...); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// nullJSON convierte un JSON vacío en NULL.
func nullJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return []byte(b)
}

// UpdateGroupRule cambia la regla de un grupo dinámico.
func (s *Store) UpdateGroupRule(ctx context.Context, id uuid.UUID, rule json.RawMessage) error {
	tag, err := s.pool.Exec(ctx, `UPDATE groups SET rule = $2 WHERE id = $1 AND kind = 'dynamic'`, id, []byte(rule))
	if err != nil {
		return fmt.Errorf("actualizar regla: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateGroup(ctx context.Context, id uuid.UUID, name, description string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE groups SET name = $2, description = $3 WHERE id = $1`, id, name, description)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("actualizar grupo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGroup borra el grupo. Sus dispositivos y los tokens que apuntaban a él
// quedan sin grupo; no se borran.
func (s *Store) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("borrar grupo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
