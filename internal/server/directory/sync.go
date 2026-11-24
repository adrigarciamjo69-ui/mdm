package directory

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
)

type Deps struct {
	Store     *store.Store
	Secrets   *secrets.Box
	AuthBase  string
	GraphBase string
	Now       func() time.Time
}

// Syncer sincroniza el directorio (tarea directory.sync).
type Syncer struct {
	d      Deps
	client *Client
}

func NewSyncer(d Deps) *Syncer {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Syncer{d: d, client: NewClient(d.AuthBase, d.GraphBase)}
}

// ErrNotConfigured indica que falta la configuración de Entra ID.
var ErrNotConfigured = errors.New("Entra ID no está configurado")

// Credentials lee y descifra la configuración guardada.
func (s *Syncer) Credentials(ctx context.Context) (Credentials, error) {
	cfg, err := s.d.Store.EntraConfig(ctx)
	if err != nil {
		return Credentials{}, err
	}
	if cfg.TenantID == "" || cfg.ClientID == "" || cfg.SealedSecret == "" {
		return Credentials{}, ErrNotConfigured
	}
	if s.d.Secrets == nil {
		return Credentials{}, errors.New("falta MDM_SECRET_KEY para descifrar el secreto de Entra ID")
	}
	secret, err := s.d.Secrets.Open(cfg.SealedSecret)
	if err != nil {
		return Credentials{}, errors.New("no se puede descifrar el secreto de Entra ID: guárdalo de nuevo en Ajustes")
	}
	return Credentials{TenantID: cfg.TenantID, ClientID: cfg.ClientID, Secret: string(secret)}, nil
}

// Test comprueba unas credenciales pidiendo los usuarios, sin guardar nada.
func (s *Syncer) Test(ctx context.Context, cr Credentials) (int, error) {
	users, err := s.client.Users(ctx, cr)
	return len(users), err
}

// Run sincroniza. Sin configuración no hace nada. Un fallo de Microsoft se
// guarda en el estado (y abre una alerta) pero no es un error de la tarea:
// se volverá a intentar en la siguiente hora.
func (s *Syncer) Run(ctx context.Context) error {
	cr, err := s.Credentials(ctx)
	if errors.Is(err, ErrNotConfigured) {
		return nil
	}
	now := s.d.Now().UTC()
	if err != nil {
		return s.d.Store.RecordDirectorySync(ctx, err.Error(), 0, now)
	}
	users, err := s.client.Users(ctx, cr)
	if err == nil && len(users) == 0 {
		// Una respuesta vacía (permisos mal concedidos, filtro…) marcaría a
		// todo el mundo como eliminado: se trata como un error.
		err = errors.New("Entra ID no ha devuelto ningún usuario: revisa el permiso User.Read.All y el consentimiento del administrador")
	}
	if err == nil {
		err = s.d.Store.ApplyDirectorySync(ctx, users, now)
	}
	if err != nil {
		slog.Error("directorio: sincronización fallida", "err", err)
		return s.d.Store.RecordDirectorySync(ctx, err.Error(), 0, now)
	}
	return s.d.Store.RecordDirectorySync(ctx, "", len(users), now)
}
