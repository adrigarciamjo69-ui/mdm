package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/protocol"
)

type ComplianceRecord struct {
	State             string
	Reasons           []string
	NoncompliantSince *time.Time
	ReportHash        string
	ReportedAt        *time.Time
	EvaluatedAt       *time.Time
}

type SettingResultRow struct {
	protocol.SettingResult
	EvaluatedAt time.Time
}

// SaveComplianceReport sustituye los resultados del equipo por los del
// informe. reported_at es la hora del servidor, no la del equipo.
func (s *Store) SaveComplianceReport(ctx context.Context, deviceID uuid.UUID, rep protocol.ComplianceReport, at time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		if _, err := q.Exec(ctx, `DELETE FROM device_setting_results WHERE device_id = $1`, deviceID); err != nil {
			return err
		}
		for _, r := range rep.Results {
			if _, err := q.Exec(ctx,
				`INSERT INTO device_setting_results (device_id, policy_id, key, status, message, applied, evaluated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (device_id, policy_id, key) DO UPDATE
				 SET status = EXCLUDED.status, message = EXCLUDED.message, applied = EXCLUDED.applied`,
				deviceID, r.PolicyID, r.Key, r.Status, r.Message, r.Applied, at); err != nil {
				return fmt.Errorf("guardar resultado: %w", err)
			}
		}
		_, err := q.Exec(ctx,
			`INSERT INTO device_compliance (device_id, report_hash, reported_at) VALUES ($1, $2, $3)
			 ON CONFLICT (device_id) DO UPDATE SET report_hash = EXCLUDED.report_hash, reported_at = EXCLUDED.reported_at`,
			deviceID, rep.PolicyHash, at)
		return err
	})
}

// Compliance devuelve el estado guardado y los resultados del último informe.
func (s *Store) Compliance(ctx context.Context, deviceID uuid.UUID) (ComplianceRecord, []SettingResultRow, error) {
	rec := ComplianceRecord{State: "unknown", Reasons: []string{}}
	var reasons []byte
	err := s.pool.QueryRow(ctx,
		`SELECT state, reasons, noncompliant_since, report_hash, reported_at, evaluated_at FROM device_compliance WHERE device_id = $1`,
		deviceID).Scan(&rec.State, &reasons, &rec.NoncompliantSince, &rec.ReportHash, &rec.ReportedAt, &rec.EvaluatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return rec, nil, err
	}
	if len(reasons) > 0 {
		_ = json.Unmarshal(reasons, &rec.Reasons)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT policy_id::text, key, status, message, applied, evaluated_at FROM device_setting_results
		 WHERE device_id = $1 ORDER BY key, policy_id`, deviceID)
	if err != nil {
		return rec, nil, err
	}
	defer rows.Close()
	out := []SettingResultRow{}
	for rows.Next() {
		var r SettingResultRow
		if err := rows.Scan(&r.PolicyID, &r.Key, &r.Status, &r.Message, &r.Applied, &r.EvaluatedAt); err != nil {
			return rec, nil, err
		}
		out = append(out, r)
	}
	return rec, out, rows.Err()
}

// SaveComplianceState guarda el estado calculado. Devuelve si cambió.
func (s *Store) SaveComplianceState(ctx context.Context, deviceID uuid.UUID, state string, reasons []string, since *time.Time, at time.Time) (bool, error) {
	if reasons == nil {
		reasons = []string{}
	}
	b, _ := json.Marshal(reasons)
	var prev string
	err := s.Tx(ctx, func(q Querier) error {
		// Leer y bloquear el estado anterior antes de cambiarlo (un FOR UPDATE
		// dentro de la CTE del INSERT devolvería la fila ya modificada).
		err := q.QueryRow(ctx, `SELECT state FROM device_compliance WHERE device_id = $1 FOR UPDATE`, deviceID).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := q.Exec(ctx,
			`INSERT INTO device_compliance (device_id, state, reasons, noncompliant_since, evaluated_at) VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (device_id) DO UPDATE SET state = EXCLUDED.state, reasons = EXCLUDED.reasons,
			     noncompliant_since = EXCLUDED.noncompliant_since, evaluated_at = EXCLUDED.evaluated_at`, deviceID, state, b, since, at); err != nil {
			return err
		}
		if prev == state {
			return nil
		}
		return emit(ctx, q, Event{Type: EventComplianceChanged, DeviceID: devicePtr(deviceID), At: at,
			Data: map[string]any{"from": prev, "to": state, "reasons": reasons}})
	})
	if err != nil {
		return false, fmt.Errorf("guardar cumplimiento: %w", err)
	}
	return prev != state, nil
}

// SetCapabilities guarda las capacidades que declara el agente.
func (s *Store) SetCapabilities(ctx context.Context, deviceID uuid.UUID, caps []string) error {
	if caps == nil {
		caps = []string{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE devices SET capabilities = $2 WHERE id = $1 AND capabilities IS DISTINCT FROM $2`, deviceID, caps)
	return err
}

// DeviceIDs devuelve los identificadores de todos los equipos.
func (s *Store) DeviceIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
