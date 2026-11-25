// Package tempadmin da permisos de administrador local durante un tiempo y
// los quita él mismo al caducar, aunque el equipo no tenga red o se haya
// reiniciado: cada concesión se guarda en disco antes de añadir al usuario.
package tempadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OS es lo que hace falta del sistema operativo.
type OS interface {
	// CurrentUser devuelve el usuario con la sesión iniciada en la consola.
	CurrentUser(ctx context.Context) (string, error)
	IsAdmin(ctx context.Context, user string) (bool, error)
	Add(ctx context.Context, user string) error
	// Remove no falla si el usuario ya no está en el grupo.
	Remove(ctx context.Context, user string) error
}

// Grant es un permiso temporal vigente.
type Grant struct {
	User      string    `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"`
}

type Deps struct {
	OS        OS
	StatePath string
	Now       func() time.Time
	Log       *slog.Logger
	// Every es cada cuánto se revisan las caducidades (un minuto por defecto).
	Every time.Duration
}

type Manager struct {
	d      Deps
	mu     sync.Mutex
	grants []Grant
}

// New carga las concesiones guardadas.
func New(d Deps) (*Manager, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Every <= 0 {
		d.Every = time.Minute
	}
	m := &Manager{d: d}
	b, err := os.ReadFile(d.StatePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &m.grants); err != nil {
			// Un estado ilegible no puede olvidarse: quedaría alguien como
			// administrador para siempre. Se falla para que se vea.
			return nil, fmt.Errorf("estado del administrador temporal ilegible: %w", err)
		}
	}
	return m, nil
}

// save escribe el estado de forma atómica. Llamar con mu tomado.
func (m *Manager) save() error {
	b, _ := json.Marshal(m.grants)
	if m.grants == nil {
		b = []byte("[]")
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.d.StatePath), "tempadmin.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.d.StatePath)
}

func (m *Manager) find(user string) int {
	for i, g := range m.grants {
		if strings.EqualFold(g.User, user) {
			return i
		}
	}
	return -1
}

// Grant hace administrador al usuario (vacío: el de la sesión iniciada)
// durante d. Si ya tenía un permiso temporal, lo amplía; si ya era
// administrador por otra vía, no hace nada y falla, para no quitárselo luego.
func (m *Manager) Grant(ctx context.Context, user string, d time.Duration, reason string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user = strings.TrimSpace(user)
	if user == "" {
		u, err := m.d.OS.CurrentUser(ctx)
		if err != nil || strings.TrimSpace(u) == "" {
			return Grant{}, errors.New("No hay ningún usuario con la sesión iniciada en el equipo: indica el usuario.")
		}
		user = strings.TrimSpace(u)
	}
	expires := m.d.Now().Add(d)
	if i := m.find(user); i >= 0 {
		if expires.After(m.grants[i].ExpiresAt) {
			m.grants[i].ExpiresAt = expires
		}
		m.grants[i].Reason = reason
		if err := m.save(); err != nil {
			return Grant{}, err
		}
		if err := m.d.OS.Add(ctx, m.grants[i].User); err != nil {
			if ok, _ := m.d.OS.IsAdmin(ctx, m.grants[i].User); !ok {
				return Grant{}, err
			}
		}
		return m.grants[i], nil
	}
	admin, err := m.d.OS.IsAdmin(ctx, user)
	if err != nil {
		return Grant{}, err
	}
	if admin {
		return Grant{}, fmt.Errorf("%s ya es administrador de este equipo de forma permanente: no se cambia nada.", user)
	}
	g := Grant{User: user, ExpiresAt: expires, Reason: reason}
	m.grants = append(m.grants, g)
	if err := m.save(); err != nil {
		m.grants = m.grants[:len(m.grants)-1]
		return Grant{}, fmt.Errorf("no se pudo guardar el permiso; no se concede: %w", err)
	}
	if err := m.d.OS.Add(ctx, user); err != nil {
		m.grants = m.grants[:len(m.grants)-1]
		_ = m.save()
		return Grant{}, err
	}
	m.d.Log.Info("administrador temporal concedido", "usuario", user, "hasta", expires)
	return g, nil
}

// Revoke quita antes de tiempo el permiso del usuario (vacío: todos).
func (m *Manager) Revoke(ctx context.Context, user string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user = strings.TrimSpace(user)
	var done []string
	var errs []error
	keep := m.grants[:0:0]
	matched := false
	for _, g := range m.grants {
		if user != "" && !strings.EqualFold(g.User, user) {
			keep = append(keep, g)
			continue
		}
		matched = true
		if err := m.d.OS.Remove(ctx, g.User); err != nil {
			// Se deja caducada: la revisión periódica lo reintenta.
			g.ExpiresAt = m.d.Now()
			keep = append(keep, g)
			errs = append(errs, fmt.Errorf("%s: %w", g.User, err))
			continue
		}
		done = append(done, g.User)
	}
	if !matched {
		if user == "" {
			return nil, errors.New("No hay ningún permiso de administrador temporal en este equipo.")
		}
		return nil, fmt.Errorf("No hay ningún permiso de administrador temporal de %s en este equipo.", user)
	}
	m.grants = keep
	if err := m.save(); err != nil {
		errs = append(errs, err)
	}
	for _, u := range done {
		m.d.Log.Info("administrador temporal retirado", "usuario", u)
	}
	return done, errors.Join(errs...)
}

// Sweep quita los permisos caducados. Los que no se pueden quitar se
// conservan para reintentarlo en la siguiente revisión.
func (m *Manager) Sweep(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.d.Now()
	keep := m.grants[:0:0]
	changed := false
	for _, g := range m.grants {
		if now.Before(g.ExpiresAt) {
			keep = append(keep, g)
			continue
		}
		if err := m.d.OS.Remove(ctx, g.User); err != nil {
			m.d.Log.Error("no se pudo retirar el administrador temporal; se reintentará", "usuario", g.User, "err", err)
			keep = append(keep, g)
			continue
		}
		m.d.Log.Info("administrador temporal caducado y retirado", "usuario", g.User)
		changed = true
	}
	if !changed {
		return
	}
	m.grants = keep
	if err := m.save(); err != nil {
		m.d.Log.Error("no se pudo guardar el estado del administrador temporal", "err", err)
	}
}

// Active devuelve los permisos vigentes.
func (m *Manager) Active() []Grant {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Grant(nil), m.grants...)
}

// Run revisa las caducidades al arrancar y cada minuto.
func (m *Manager) Run(ctx context.Context) {
	m.Sweep(ctx)
	t := time.NewTicker(m.d.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Sweep(ctx)
		}
	}
}
