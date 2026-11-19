package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SettingsRaw devuelve todos los ajustes guardados, por clave.
func (s *Store) SettingsRaw(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("leer ajustes: %w", err)
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SaveSettingsRaw guarda los ajustes dados en una transacción.
func (s *Store) SaveSettingsRaw(ctx context.Context, values map[string]json.RawMessage, by uuid.UUID, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		for k, v := range values {
			if _, err := q.Exec(ctx,
				`INSERT INTO settings (key, value, updated_at, updated_by) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
				k, v, at, by); err != nil {
				return fmt.Errorf("guardar ajuste %s: %w", k, err)
			}
		}
		return nil
	})
}
