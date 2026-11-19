package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RecoveryKeyMeta struct {
	KeyID      string
	Volume     string
	ReportedAt time.Time
}

// SaveRecoveryKey guarda (o sustituye) la clave ya cifrada de un protector.
func (s *Store) SaveRecoveryKey(ctx context.Context, deviceID uuid.UUID, volume, keyID, sealed string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO recovery_keys (device_id, key_id, volume, secret, reported_at) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_id, key_id) DO UPDATE SET volume = EXCLUDED.volume, secret = EXCLUDED.secret, reported_at = EXCLUDED.reported_at`,
		deviceID, keyID, volume, sealed, at)
	return err
}

func (s *Store) ListRecoveryKeys(ctx context.Context, deviceID uuid.UUID) ([]RecoveryKeyMeta, error) {
	rows, err := s.pool.Query(ctx, `SELECT key_id, volume, reported_at FROM recovery_keys WHERE device_id = $1 ORDER BY volume, reported_at DESC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecoveryKeyMeta{}
	for rows.Next() {
		var m RecoveryKeyMeta
		if err := rows.Scan(&m.KeyID, &m.Volume, &m.ReportedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RecoveryKeySecret devuelve la clave cifrada.
func (s *Store) RecoveryKeySecret(ctx context.Context, deviceID uuid.UUID, keyID string) (RecoveryKeyMeta, string, error) {
	var m RecoveryKeyMeta
	var sealed string
	err := s.pool.QueryRow(ctx, `SELECT key_id, volume, reported_at, secret FROM recovery_keys WHERE device_id = $1 AND key_id = $2`, deviceID, keyID).
		Scan(&m.KeyID, &m.Volume, &m.ReportedAt, &sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, "", ErrNotFound
	}
	return m, sealed, err
}

func init() {
	escrowedKeyIDs = func(ctx context.Context, s *Store, deviceID uuid.UUID) ([]string, error) {
		keys, err := s.ListRecoveryKeys(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, len(keys))
		for i, k := range keys {
			ids[i] = k.KeyID
		}
		return ids, nil
	}
}
