package tempadmin

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

type fakeOS struct {
	current string
	admins  map[string]bool
	failAdd error
	failRm  error
	removed []string
}

func (f *fakeOS) CurrentUser(context.Context) (string, error) {
	if f.current == "" {
		return "", errors.New("nadie")
	}
	return f.current, nil
}
func (f *fakeOS) IsAdmin(_ context.Context, u string) (bool, error) { return f.admins[u], nil }
func (f *fakeOS) Add(_ context.Context, u string) error {
	if f.failAdd != nil {
		return f.failAdd
	}
	f.admins[u] = true
	return nil
}
func (f *fakeOS) Remove(_ context.Context, u string) error {
	if f.failRm != nil {
		return f.failRm
	}
	delete(f.admins, u)
	f.removed = append(f.removed, u)
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T) (*Manager, *fakeOS, *clock, string) {
	t.Helper()
	os := &fakeOS{current: `EMPRESA\ana`, admins: map[string]bool{}}
	c := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "tempadmin.json")
	m, err := New(Deps{OS: os, StatePath: path, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return m, os, c, path
}

func TestGrant_DefaultsToLoggedInUserAndExpires(t *testing.T) {
	m, os, c, path := setup(t)
	g, err := m.Grant(ctx, "", time.Hour, "Instalar driver")
	if err != nil || g.User != `EMPRESA\ana` || !g.ExpiresAt.Equal(c.t.Add(time.Hour)) || !os.admins[`EMPRESA\ana`] {
		t.Fatalf("concesión: %+v %v", g, err)
	}
	c.t = c.t.Add(59 * time.Minute)
	m.Sweep(ctx)
	if !os.admins[`EMPRESA\ana`] {
		t.Fatal("antes de caducar sigue siendo administrador")
	}
	// Tras un reinicio del agente, se recuerda y se quita al caducar.
	m2, err := New(Deps{OS: os, StatePath: path, Now: c.now})
	if err != nil || len(m2.Active()) != 1 {
		t.Fatalf("estado recargado: %v %v", m2.Active(), err)
	}
	c.t = c.t.Add(2 * time.Minute)
	m2.Sweep(ctx)
	if os.admins[`EMPRESA\ana`] || len(m2.Active()) != 0 {
		t.Fatal("al caducar se quita")
	}
	m3, _ := New(Deps{OS: os, StatePath: path, Now: c.now})
	if len(m3.Active()) != 0 {
		t.Fatal("la retirada queda guardada")
	}
}

func TestGrant_NoLoggedInUser(t *testing.T) {
	m, os, _, _ := setup(t)
	os.current = ""
	if _, err := m.Grant(ctx, "", time.Hour, "x"); err == nil || !strings.Contains(err.Error(), "sesión") {
		t.Fatalf("sin usuario: %v", err)
	}
}

func TestGrant_RefusesPermanentAdmin(t *testing.T) {
	m, os, c, _ := setup(t)
	os.admins["jefe"] = true
	if _, err := m.Grant(ctx, "jefe", time.Hour, "x"); err == nil || !strings.Contains(err.Error(), "ya es administrador") {
		t.Fatalf("permanente: %v", err)
	}
	c.t = c.t.Add(2 * time.Hour)
	m.Sweep(ctx)
	if !os.admins["jefe"] {
		t.Fatal("nunca se quita a un administrador permanente")
	}
}

func TestGrant_RepeatExtends(t *testing.T) {
	m, _, c, _ := setup(t)
	_, _ = m.Grant(ctx, "ana", time.Hour, "x")
	c.t = c.t.Add(30 * time.Minute)
	g, err := m.Grant(ctx, "ANA", 2*time.Hour, "y")
	if err != nil || !g.ExpiresAt.Equal(c.t.Add(2*time.Hour)) || len(m.Active()) != 1 {
		t.Fatalf("ampliar: %+v %v %v", g, err, m.Active())
	}
	// Una concesión más corta no recorta la vigente.
	g, _ = m.Grant(ctx, "ana", 15*time.Minute, "z")
	if !g.ExpiresAt.Equal(c.t.Add(2 * time.Hour)) {
		t.Fatalf("no recorta: %v", g.ExpiresAt)
	}
}

func TestGrant_AddFailsLeavesNothing(t *testing.T) {
	m, os, _, _ := setup(t)
	os.failAdd = errors.New("acceso denegado")
	if _, err := m.Grant(ctx, "ana", time.Hour, "x"); err == nil {
		t.Fatal("debe fallar")
	}
	if len(m.Active()) != 0 {
		t.Fatal("sin concesión guardada")
	}
}

func TestRevoke(t *testing.T) {
	m, os, _, _ := setup(t)
	_, _ = m.Grant(ctx, "ana", time.Hour, "x")
	_, _ = m.Grant(ctx, "luis", time.Hour, "x")
	if _, err := m.Revoke(ctx, "pepe"); err == nil {
		t.Fatal("sin concesión para ese usuario")
	}
	users, err := m.Revoke(ctx, "Ana")
	if err != nil || len(users) != 1 || os.admins["ana"] || !os.admins["luis"] {
		t.Fatalf("retirar uno: %v %v", users, err)
	}
	users, err = m.Revoke(ctx, "")
	if err != nil || len(users) != 1 || os.admins["luis"] || len(m.Active()) != 0 {
		t.Fatalf("retirar todos: %v %v", users, err)
	}
}

func TestSweep_RetriesWhenRemoveFails(t *testing.T) {
	m, os, c, _ := setup(t)
	_, _ = m.Grant(ctx, "ana", time.Hour, "x")
	c.t = c.t.Add(2 * time.Hour)
	os.failRm = errors.New("ocupado")
	m.Sweep(ctx)
	if len(m.Active()) != 1 {
		t.Fatal("se conserva para reintentar")
	}
	os.failRm = nil
	m.Sweep(ctx)
	if len(m.Active()) != 0 || os.admins["ana"] {
		t.Fatal("segundo intento")
	}
}
