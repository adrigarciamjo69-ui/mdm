package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AndroidConfig struct {
	SealedKey             string
	ProjectID             string
	ClientEmail           string
	SignupURLName         string
	SignupState           string
	SignupBy              *uuid.UUID
	EnterpriseName        string
	EnterpriseDisplayName string
	LinkedAt              *time.Time
	LinkedBy              *uuid.UUID
	LastSyncAt            *time.Time
	LastSyncError         string
}

// AndroidConfig devuelve la configuración; ErrNotFound si no se ha configurado.
func (s *Store) AndroidConfig(ctx context.Context) (AndroidConfig, error) {
	var c AndroidConfig
	err := s.pool.QueryRow(ctx,
		`SELECT service_account, project_id, client_email, signup_url_name, signup_state, signup_by, enterprise_name,
		        enterprise_display_name, linked_at, linked_by, last_sync_at, last_sync_error FROM android_config`).
		Scan(&c.SealedKey, &c.ProjectID, &c.ClientEmail, &c.SignupURLName, &c.SignupState, &c.SignupBy, &c.EnterpriseName,
			&c.EnterpriseDisplayName, &c.LinkedAt, &c.LinkedBy, &c.LastSyncAt, &c.LastSyncError)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SaveAndroidKey guarda la clave (ya cifrada) de la cuenta de servicio.
func (s *Store) SaveAndroidKey(ctx context.Context, sealed, projectID, email string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO android_config (service_account, project_id, client_email, updated_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET service_account = EXCLUDED.service_account, project_id = EXCLUDED.project_id,
		     client_email = EXCLUDED.client_email, updated_at = EXCLUDED.updated_at`, sealed, projectID, email, at)
	return err
}

func (s *Store) SetAndroidSignup(ctx context.Context, name, state string, by uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE android_config SET signup_url_name = $1, signup_state = $2, signup_by = $3`, name, state, by)
	return err
}

// LinkAndroidEnterprise guarda la empresa y consume el state del registro.
func (s *Store) LinkAndroidEnterprise(ctx context.Context, name, display string, by *uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE android_config SET enterprise_name = $1, enterprise_display_name = $2, linked_at = $3, linked_by = $4,
		     signup_state = '', signup_url_name = ''`, name, display, at, by)
	return err
}

func (s *Store) UnlinkAndroidEnterprise(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE android_config SET enterprise_name = '', enterprise_display_name = '', linked_at = NULL, linked_by = NULL`)
	return err
}

func (s *Store) SetAndroidSync(ctx context.Context, at time.Time, errMsg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE android_config SET last_sync_at = $1, last_sync_error = $2`, at, errMsg)
	return err
}
