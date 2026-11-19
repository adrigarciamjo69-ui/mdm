package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mdm/internal/server/schedule"
)

type ReportSchedule struct {
	ID          uuid.UUID
	Report      string
	Filters     json.RawMessage
	Spec        schedule.Spec
	ChannelID   uuid.UUID
	ChannelName string
	Enabled     bool
	LastRunAt   *time.Time
	LastStatus  string
	CreatedAt   time.Time
}

const reportScheduleCols = `r.id, r.report, r.filters, r.frequency, r.time, r.day, r.channel_id, c.name, r.enabled, r.last_run_at, r.last_status, r.created_at`

func scanReportSchedule(row pgx.Row) (ReportSchedule, error) {
	var r ReportSchedule
	err := row.Scan(&r.ID, &r.Report, &r.Filters, &r.Spec.Frequency, &r.Spec.Time, &r.Spec.Day, &r.ChannelID, &r.ChannelName, &r.Enabled,
		&r.LastRunAt, &r.LastStatus, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// CreateReportSchedule crea la programación. La creación cuenta como última
// ejecución: no se envía en el momento de crearla.
func (s *Store) CreateReportSchedule(ctx context.Context, r ReportSchedule, at time.Time) (ReportSchedule, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO report_schedules (report, filters, frequency, time, day, channel_id, enabled, last_run_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		r.Report, r.Filters, r.Spec.Frequency, r.Spec.Time, r.Spec.Day, r.ChannelID, r.Enabled, at).Scan(&id)
	if isForeignKeyViolation(err) {
		return r, ErrBadReference
	}
	if err != nil {
		return r, err
	}
	return s.GetReportSchedule(ctx, id)
}

func (s *Store) GetReportSchedule(ctx context.Context, id uuid.UUID) (ReportSchedule, error) {
	return scanReportSchedule(s.pool.QueryRow(ctx, `SELECT `+reportScheduleCols+` FROM report_schedules r JOIN notification_channels c ON c.id = r.channel_id WHERE r.id = $1`, id))
}

func (s *Store) ListReportSchedules(ctx context.Context) ([]ReportSchedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+reportScheduleCols+` FROM report_schedules r JOIN notification_channels c ON c.id = r.channel_id ORDER BY r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportSchedule{}
	for rows.Next() {
		r, err := scanReportSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateReportSchedule(ctx context.Context, r ReportSchedule) error {
	tag, err := s.pool.Exec(ctx, `UPDATE report_schedules SET filters = $2, frequency = $3, time = $4, day = $5, channel_id = $6, enabled = $7 WHERE id = $1`,
		r.ID, r.Filters, r.Spec.Frequency, r.Spec.Time, r.Spec.Day, r.ChannelID, r.Enabled)
	if isForeignKeyViolation(err) {
		return ErrBadReference
	}
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteReportSchedule(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM report_schedules WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) SetReportScheduleRun(ctx context.Context, id uuid.UUID, at time.Time, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE report_schedules SET last_run_at = $2, last_status = $3 WHERE id = $1`, id, at, CleanText(status, 500))
	return err
}
