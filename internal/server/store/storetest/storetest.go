// Package storetest da a cada prueba una base de datos aislada: un esquema de
// PostgreSQL propio, migrado y eliminado al terminar.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/google/uuid"
	"mdm/internal/protocol"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mdm/internal/server/store"
)

const envVar = "MDM_TEST_DATABASE_URL"

// New devuelve un almacén sobre un esquema recién migrado.
func New(t *testing.T) *store.Store {
	s, _ := NewWithURL(t)
	return s
}

// NewWithURL devuelve además la cadena de conexión que apunta al esquema.
func NewWithURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("prueba con PostgreSQL omitida con -short")
	}
	base := os.Getenv(envVar)
	if base == "" {
		t.Fatalf("falta %s: estas pruebas necesitan un PostgreSQL real (o ejecuta con -short para omitirlas)", envVar)
	}
	ctx := context.Background()

	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	schema := "t_" + hex.EncodeToString(raw)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("conectar con %s: %v", envVar, err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("crear esquema: %v", err)
	}
	admin.Close(ctx)

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("%s no es una URL válida: %v", envVar, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped := u.String()

	if err := store.Migrate(ctx, scoped); err != nil {
		t.Fatalf("migrar: %v", err)
	}
	s, err := store.Open(ctx, scoped)
	if err != nil {
		t.Fatalf("abrir almacén: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	return s, scoped
}

// SeedDevice inscribe un equipo (con su token) y devuelve su id.
func SeedDevice(t *testing.T, s *store.Store, hostname, platform string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	admin, err := s.CreateAdmin(ctx, "semilla-"+uuid.NewString()[:8]+"@ejemplo.es", "x", "admin")
	if err != nil {
		t.Fatal(err)
	}
	hash := "tok-" + uuid.NewString()
	if _, err := s.CreateToken(ctx, store.TokenParams{Name: "semilla", TokenHash: hash, CreatedBy: admin.ID}); err != nil {
		t.Fatal(err)
	}
	res, err := s.EnrollDevice(ctx, store.EnrollParams{TokenHash: hash, CredentialHash: "h", IP: "10.0.0.1", At: time.Now(),
		Req: protocol.EnrollRequest{HardwareID: "hw-" + uuid.NewString(), Hostname: hostname, Platform: platform, Arch: "amd64", AgentVersion: "1.0.0"}})
	if err != nil {
		t.Fatal(err)
	}
	return res.DeviceID
}
