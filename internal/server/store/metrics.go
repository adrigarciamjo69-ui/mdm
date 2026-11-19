package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MetricPoint struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
}

// HasMetrics informa de si ya hay instantánea de ese día.
func (s *Store) HasMetrics(ctx context.Context, day string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM metrics_daily WHERE day = $1::date)`, day).Scan(&ok)
	return ok, err
}

func (s *Store) SaveMetrics(ctx context.Context, day string, values map[string]float64) error {
	return s.Tx(ctx, func(q Querier) error {
		for k, v := range values {
			if _, err := q.Exec(ctx, `INSERT INTO metrics_daily (day, key, value) VALUES ($1::date, $2, $3) ON CONFLICT (day, key) DO UPDATE SET value = EXCLUDED.value`,
				day, k, v); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, `DELETE FROM metrics_daily WHERE day < $1::date - 400`, day)
		return err
	})
}

// Metrics devuelve las series pedidas desde since (incluido).
func (s *Store) Metrics(ctx context.Context, keys []string, since time.Time) (map[string][]MetricPoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, to_char(day, 'YYYY-MM-DD'), value FROM metrics_daily WHERE key = ANY($1) AND day >= $2::date ORDER BY day`,
		keys, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]MetricPoint{}
	for _, k := range keys {
		out[k] = []MetricPoint{}
	}
	for rows.Next() {
		var k string
		var p MetricPoint
		if err := rows.Scan(&k, &p.Day, &p.Value); err != nil {
			return nil, err
		}
		out[k] = append(out[k], p)
	}
	return out, rows.Err()
}

// CompliancePercent devuelve el porcentaje de equipos evaluados que cumplen (-1 si ninguno).
func (s *Store) CompliancePercent(ctx context.Context) (float64, error) {
	var ok, total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE c.state = 'compliant'), count(*) FILTER (WHERE c.state IN ('compliant', 'noncompliant'))
		FROM device_compliance c JOIN devices d ON d.id = c.device_id WHERE d.revoked_at IS NULL`).Scan(&ok, &total)
	if err != nil || total == 0 {
		return -1, err
	}
	return float64(ok) * 100 / float64(total), nil
}

func (s *Store) Preference(ctx context.Context, admin uuid.UUID, key string) (json.RawMessage, error) {
	var v json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT value FROM admin_preferences WHERE admin_id = $1 AND key = $2`, admin, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Store) SetPreference(ctx context.Context, admin uuid.UUID, key string, value json.RawMessage) error {
	if value == nil {
		_, err := s.pool.Exec(ctx, `DELETE FROM admin_preferences WHERE admin_id = $1 AND key = $2`, admin, key)
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO admin_preferences (admin_id, key, value) VALUES ($1, $2, $3)
		ON CONFLICT (admin_id, key) DO UPDATE SET value = EXCLUDED.value`, admin, key, value)
	return err
}
