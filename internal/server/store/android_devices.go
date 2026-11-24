package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AndroidDevice son los datos con que la sincronización crea o actualiza un móvil.
type AndroidDevice struct {
	AmapiName    string
	Hostname     string
	Serial       string
	Manufacturer string
	Model        string
	OSVersion    string
	LastSeenAt   *time.Time
	EnrolledAt   time.Time
	GroupID      *uuid.UUID // solo al crearlo (del token de inscripción)
}

// androidCredential no es un hash válido: un móvil nunca usa la API de agentes.
const androidCredential = "android"

// UpsertAndroidDevice crea el móvil si no existe y, si existe, actualiza lo
// que informa Google (sin tocar grupo, usuario ni etiquetas).
func (s *Store) UpsertAndroidDevice(ctx context.Context, d AndroidDevice) (uuid.UUID, bool, error) {
	var id uuid.UUID
	var created bool
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return id, false, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`INSERT INTO devices (hardware_id, amapi_name, hostname, platform, os_name, os_version, serial, manufacturer, model,
		                      credential_hash, group_id, enrolled_at, last_seen_at)
		 VALUES ($1, $1, $2, 'android', 'Android', $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (hardware_id) DO UPDATE SET os_version = EXCLUDED.os_version, serial = EXCLUDED.serial,
		     manufacturer = EXCLUDED.manufacturer, model = EXCLUDED.model,
		     last_seen_at = GREATEST(devices.last_seen_at, EXCLUDED.last_seen_at)
		 RETURNING id, (xmax = 0)`,
		d.AmapiName, d.Hostname, d.OSVersion, d.Serial, d.Manufacturer, d.Model, androidCredential, d.GroupID, d.EnrolledAt, d.LastSeenAt).
		Scan(&id, &created)
	if err != nil {
		return id, false, err
	}
	if _, err := ensureDeviceAsset(ctx, tx, id); err != nil {
		return id, false, err
	}
	return id, created, tx.Commit(ctx)
}

func (s *Store) DeviceIDByAmapiName(ctx context.Context, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM devices WHERE amapi_name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, ErrNotFound
	}
	return id, err
}

// RevokeAndroidMissing revoca los móviles activos que Google ya no lista.
func (s *Store) RevokeAndroidMissing(ctx context.Context, present []string, at time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE devices SET revoked_at = $2 WHERE platform = 'android' AND revoked_at IS NULL AND NOT (amapi_name = ANY($1))`, present, at)
	return tag.RowsAffected(), err
}

// ---------- Tokens de inscripción ----------

type AndroidToken struct {
	ID        uuid.UUID
	Name      string
	AmapiName string
	QRCode    string
	GroupID   *uuid.UUID
	GroupName string
	OneTime   bool
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s *Store) CreateAndroidToken(ctx context.Context, t AndroidToken, by uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`INSERT INTO android_enrollment_tokens (name, amapi_name, qr_code, group_id, one_time, expires_at, created_by)
		 VALUES ($1, '', '', $2, $3, $4, $5) RETURNING id`, t.Name, t.GroupID, t.OneTime, t.ExpiresAt, by).Scan(&id)
	return id, err
}

func (s *Store) SetAndroidTokenGoogle(ctx context.Context, id uuid.UUID, amapiName, qr string, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE android_enrollment_tokens SET amapi_name = $2, qr_code = $3, expires_at = $4 WHERE id = $1`, id, amapiName, qr, expires)
	return err
}

func (s *Store) DeleteAndroidToken(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM android_enrollment_tokens WHERE id = $1`, id)
	return err
}

func (s *Store) RevokeAndroidToken(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE android_enrollment_tokens SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at)
	return err
}

func (s *Store) AndroidTokens(ctx context.Context) ([]AndroidToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.name, t.amapi_name, t.qr_code, t.group_id, COALESCE(g.name, ''), t.one_time, t.expires_at, t.revoked_at, t.created_at
		 FROM android_enrollment_tokens t LEFT JOIN groups g ON g.id = t.group_id ORDER BY t.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AndroidToken{}
	for rows.Next() {
		var t AndroidToken
		if err := rows.Scan(&t.ID, &t.Name, &t.AmapiName, &t.QRCode, &t.GroupID, &t.GroupName, &t.OneTime, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AndroidToken(ctx context.Context, id uuid.UUID) (AndroidToken, error) {
	list, err := s.AndroidTokens(ctx)
	if err != nil {
		return AndroidToken{}, err
	}
	for _, t := range list {
		if t.ID == id {
			return t, nil
		}
	}
	return AndroidToken{}, ErrNotFound
}

// ---------- Políticas publicadas ----------

// PublishedAndroidPolicy devuelve el nombre con que se publicó una huella.
func (s *Store) PublishedAndroidPolicy(ctx context.Context, hash string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `UPDATE android_policies SET last_used_at = now() WHERE hash = $1 RETURNING amapi_name`, hash).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

func (s *Store) SaveAndroidPolicy(ctx context.Context, hash, name string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO android_policies (hash, amapi_name) VALUES ($1, $2) ON CONFLICT (hash) DO UPDATE SET last_used_at = now()`, hash, name)
	return err
}

// ---------- Órdenes a móviles ----------

type AndroidCommand struct {
	Command
	AmapiName   string
	ExternalRef string
}

// AndroidCommands devuelve las órdenes de móviles en ese estado.
func (s *Store) AndroidCommands(ctx context.Context, status string, now time.Time) ([]AndroidCommand, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.device_id, c.type, c.payload, c.external_ref, d.amapi_name
		 FROM commands c JOIN devices d ON d.id = c.device_id
		 WHERE d.platform = 'android' AND d.amapi_name IS NOT NULL AND c.status = $1 AND c.expires_at > $2 ORDER BY c.created_at`, status, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AndroidCommand
	for rows.Next() {
		var c AndroidCommand
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Type, &c.Payload, &c.ExternalRef, &c.AmapiName); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkAndroidCommandSent pasa la orden a «enviada» con la operación de Google.
func (s *Store) MarkAndroidCommandSent(ctx context.Context, id uuid.UUID, ref string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE commands SET status = 'sent', sent_at = $3, started_at = $3, external_ref = $2 WHERE id = $1 AND status = 'pending'`, id, ref, at)
	return err
}

// FinishAndroidCommand cierra una orden de móvil (correcta o con error).
func (s *Store) FinishAndroidCommand(ctx context.Context, id uuid.UUID, ok bool, msg string, at time.Time) error {
	status := "failed"
	if ok {
		status = "succeeded"
	}
	return s.Tx(ctx, func(q Querier) error {
		var deviceID uuid.UUID
		var typ string
		err := q.QueryRow(ctx,
			`UPDATE commands SET status = $2, error = $3, finished_at = $4, started_at = COALESCE(started_at, $4)
			 WHERE id = $1 AND status IN ('pending', 'sent') RETURNING device_id, type`, id, status, CleanText(msg, 1000), at).Scan(&deviceID, &typ)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil || ok {
			return err
		}
		return emitCommandFailed(ctx, q, id, deviceID, typ, msg, at)
	})
}
