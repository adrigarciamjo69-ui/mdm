package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SMTPConfig struct {
	Host, Security, Username, SealedPassword, FromAddress, FromName string
	Port                                                            int
}

func (s *Store) SMTPConfig(ctx context.Context) (SMTPConfig, error) {
	var c SMTPConfig
	err := s.pool.QueryRow(ctx, `SELECT host, port, security, username, password, from_address, from_name FROM smtp_config`).
		Scan(&c.Host, &c.Port, &c.Security, &c.Username, &c.SealedPassword, &c.FromAddress, &c.FromName)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) SaveSMTPConfig(ctx context.Context, c SMTPConfig) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO smtp_config (host, port, security, username, password, from_address, from_name, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (id) DO UPDATE SET host = EXCLUDED.host, port = EXCLUDED.port, security = EXCLUDED.security, username = EXCLUDED.username,
		     password = EXCLUDED.password, from_address = EXCLUDED.from_address, from_name = EXCLUDED.from_name, updated_at = now()`,
		c.Host, c.Port, c.Security, c.Username, c.SealedPassword, c.FromAddress, c.FromName)
	return err
}

type Channel struct {
	ID           uuid.UUID
	Name         string
	Type         string
	Recipients   []string
	URL          string
	SealedSecret string
	LastError    string
	LastErrorAt  *time.Time
	CreatedAt    time.Time
}

const channelCols = `id, name, type, recipients, url, secret, last_error, last_error_at, created_at`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Recipients, &c.URL, &c.SealedSecret, &c.LastError, &c.LastErrorAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateChannel(ctx context.Context, c Channel) (Channel, error) {
	out, err := scanChannel(s.pool.QueryRow(ctx,
		`INSERT INTO notification_channels (name, type, recipients, url, secret) VALUES ($1, $2, $3, $4, $5) RETURNING `+channelCols,
		c.Name, c.Type, nonNil(c.Recipients), c.URL, c.SealedSecret))
	if isUniqueViolation(err) {
		return out, ErrConflict
	}
	return out, err
}

func (s *Store) UpdateChannel(ctx context.Context, c Channel) error {
	tag, err := s.pool.Exec(ctx, `UPDATE notification_channels SET name = $2, recipients = $3, url = $4 WHERE id = $1`, c.ID, c.Name, nonNil(c.Recipients), c.URL)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) GetChannel(ctx context.Context, id uuid.UUID) (Channel, error) {
	return scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelCols+` FROM notification_channels WHERE id = $1`, id))
}

func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelCols+` FROM notification_channels ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SetChannelError(ctx context.Context, id uuid.UUID, msg string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE notification_channels SET last_error = $2, last_error_at = CASE WHEN $2 = '' THEN last_error_at ELSE $3 END WHERE id = $1`,
		id, CleanText(msg, 1000), at)
	return err
}

type Delivery struct {
	ID        int64
	ChannelID uuid.UUID
	Event     string
	Message   json.RawMessage
	ExtraTo   []string
	AlertIDs  []int64
	Status    string
	Attempts  int
	Error     string
}

// QueueDelivery crea un envío y su tarea, con q (puede ser una transacción).
func QueueDelivery(ctx context.Context, q Querier, d Delivery, at time.Time) (int64, error) {
	var id int64
	if d.AlertIDs == nil {
		d.AlertIDs = []int64{}
	}
	err := q.QueryRow(ctx, `INSERT INTO deliveries (channel_id, event, message, extra_to, alert_ids) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		d.ChannelID, d.Event, d.Message, nonNil(d.ExtraTo), d.AlertIDs).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, EnqueueJob(ctx, q, NewJob{Kind: "notify.deliver", Payload: map[string]int64{"delivery_id": id}, RunAt: at, MaxAttempts: 1})
}

func (s *Store) GetDelivery(ctx context.Context, id int64) (Delivery, error) {
	var d Delivery
	err := s.pool.QueryRow(ctx, `SELECT id, channel_id, event, message, extra_to, alert_ids, status, attempts, error FROM deliveries WHERE id = $1`, id).
		Scan(&d.ID, &d.ChannelID, &d.Event, &d.Message, &d.ExtraTo, &d.AlertIDs, &d.Status, &d.Attempts, &d.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// FinishDelivery anota el resultado de un intento y, si hay reintento, encola el siguiente.
func (s *Store) FinishDelivery(ctx context.Context, id int64, errMsg string, retryAt *time.Time, now time.Time) error {
	return s.Tx(ctx, func(q Querier) error {
		status := "sent"
		switch {
		case errMsg != "" && retryAt != nil:
			status = "pending"
		case errMsg != "":
			status = "failed"
		}
		if _, err := q.Exec(ctx, `UPDATE deliveries SET status = $2, attempts = attempts + 1, error = $3, sent_at = CASE WHEN $2 = 'sent' THEN $4 ELSE sent_at END WHERE id = $1`,
			id, status, CleanText(errMsg, 1000), now); err != nil {
			return err
		}
		if retryAt != nil {
			return EnqueueJob(ctx, q, NewJob{Kind: "notify.deliver", Payload: map[string]int64{"delivery_id": id}, RunAt: *retryAt, MaxAttempts: 1})
		}
		return nil
	})
}
