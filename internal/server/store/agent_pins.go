package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AgentPin struct {
	GroupID   uuid.UUID
	GroupName string
	Version   string
}

func (s *Store) SetAgentPin(ctx context.Context, groupID uuid.UUID, version string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO agent_pins (group_id, version) VALUES ($1, $2)
		ON CONFLICT (group_id) DO UPDATE SET version = EXCLUDED.version`, groupID, version)
	if isForeignKeyViolation(err) {
		return ErrBadReference
	}
	return err
}

func (s *Store) DeleteAgentPin(ctx context.Context, groupID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM agent_pins WHERE group_id = $1`, groupID)
	return err
}

func (s *Store) ListAgentPins(ctx context.Context) ([]AgentPin, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.group_id, g.name, p.version FROM agent_pins p JOIN groups g ON g.id = p.group_id ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentPin{}
	for rows.Next() {
		var p AgentPin
		if err := rows.Scan(&p.GroupID, &p.GroupName, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DevicePinned informa de si el grupo principal del equipo tiene la versión congelada.
func (s *Store) DevicePinned(ctx context.Context, deviceID uuid.UUID) (bool, error) {
	var v string
	err := s.pool.QueryRow(ctx, `SELECT p.version FROM devices d JOIN agent_pins p ON p.group_id = d.group_id WHERE d.id = $1`, deviceID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
