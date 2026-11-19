package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type NewJob struct {
	Kind    string
	Payload any
	RunAt   time.Time
	// UniqueKey evita encolar una tarea igual mientras haya otra viva.
	UniqueKey   string
	MaxAttempts int
}

type Job struct {
	ID          int64
	Kind        string
	Payload     json.RawMessage
	UniqueKey   string
	RunAt       time.Time
	Attempts    int
	MaxAttempts int
	LastError   string
	CreatedAt   time.Time
	FinishedAt  *time.Time
	FailedAt    *time.Time
}

// EnqueueJob encola una tarea. Con q puede formar parte de otra transacción.
// Si ya hay una tarea viva con la misma UniqueKey, no hace nada.
func EnqueueJob(ctx context.Context, q Querier, j NewJob) error {
	payload, err := json.Marshal(j.Payload)
	if err != nil {
		return err
	}
	if j.Payload == nil {
		payload = []byte(`{}`)
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 5
	}
	var key *string
	if j.UniqueKey != "" {
		key = &j.UniqueKey
	}
	_, err = q.Exec(ctx,
		`INSERT INTO jobs (kind, payload, unique_key, run_at, max_attempts) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (unique_key) WHERE finished_at IS NULL AND failed_at IS NULL DO NOTHING`,
		j.Kind, payload, key, j.RunAt, j.MaxAttempts)
	if err != nil {
		return fmt.Errorf("encolar tarea %s: %w", j.Kind, err)
	}
	return nil
}

const jobCols = `id, kind, payload, COALESCE(unique_key, ''), run_at, attempts, max_attempts, last_error, created_at, finished_at, failed_at`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Kind, &j.Payload, &j.UniqueKey, &j.RunAt, &j.Attempts, &j.MaxAttempts, &j.LastError, &j.CreatedAt, &j.FinishedAt, &j.FailedAt)
	return j, err
}

// ClaimJob toma la siguiente tarea lista, la bloquea durante lockFor y suma un
// intento. Devuelve nil si no hay ninguna. Dos procesos nunca toman la misma.
func (s *Store) ClaimJob(ctx context.Context, now time.Time, lockFor time.Duration) (*Job, error) {
	j, err := scanJob(s.pool.QueryRow(ctx,
		`UPDATE jobs SET locked_until = $2, attempts = attempts + 1
		 WHERE id = (
		     SELECT id FROM jobs
		     WHERE finished_at IS NULL AND failed_at IS NULL AND run_at <= $1
		       AND (locked_until IS NULL OR locked_until < $1)
		     ORDER BY run_at, id
		     FOR UPDATE SKIP LOCKED LIMIT 1)
		 RETURNING `+jobCols, now, now.Add(lockFor)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tomar tarea: %w", err)
	}
	return &j, nil
}

func (s *Store) ExtendJobLock(ctx context.Context, id int64, until time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET locked_until = $2 WHERE id = $1`, id, until)
	return err
}

func (s *Store) FinishJob(ctx context.Context, id int64, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET finished_at = $2, locked_until = NULL WHERE id = $1`, id, now)
	return err
}

// RetryJob anota el error y reprograma la tarea.
func (s *Store) RetryJob(ctx context.Context, id int64, msg string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET last_error = $2, run_at = $3, locked_until = NULL WHERE id = $1`, id, msg, at)
	return err
}

// FailJob da la tarea por fallida definitivamente.
func (s *Store) FailJob(ctx context.Context, id int64, msg string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET last_error = $2, failed_at = $3, locked_until = NULL WHERE id = $1`, id, msg, now)
	return err
}

// ListJobs lista tareas por estado: "pending" (vivas), "failed" o "finished".
func (s *Store) ListJobs(ctx context.Context, state string, page, perPage int) ([]Job, int, error) {
	var where string
	switch state {
	case "pending":
		where = "finished_at IS NULL AND failed_at IS NULL"
	case "failed":
		where = "failed_at IS NOT NULL"
	case "finished":
		where = "finished_at IS NOT NULL"
	default:
		return nil, 0, fmt.Errorf("estado de tarea desconocido: %q", state)
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE `+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	page, perPage = normalizePage(page, perPage)
	rows, err := s.pool.Query(ctx, `SELECT `+jobCols+` FROM jobs WHERE `+where+` ORDER BY COALESCE(failed_at, run_at) DESC, id DESC LIMIT $1 OFFSET $2`,
		perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, j)
	}
	return out, total, rows.Err()
}

// PurgeJobs borra las tareas terminadas o fallidas antes de before.
func (s *Store) PurgeJobs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM jobs WHERE COALESCE(finished_at, failed_at) < $1`, before)
	return tag.RowsAffected(), err
}
