package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mdm/internal/agent/config"
)

type cli struct {
	d           deps
	out, errOut bytes.Buffer
	uninstalls  int
	dir         string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	c := &cli{dir: filepath.Join(t.TempDir(), "mdm-agent")}
	c.d = deps{
		stdout: &c.out, stderr: &c.errOut,
		isAdmin:          func() bool { return true },
		configDir:        c.dir,
		uninstallService: func() error { c.uninstalls++; return nil },
		installService:   func(string) error { return nil },
		stopService:      func() {},
		runService:       func(func(context.Context) error) error { return nil },
		installPath:      filepath.Join(t.TempDir(), "bin", "mdm-agent"),
		now:              func() time.Time { return time.Date(2026, 11, 13, 10, 2, 51, 0, time.UTC) },
	}
	return c
}

func (c *cli) run(args ...string) int { return run(args, c.d) }

func TestCLI_UnknownCommand(t *testing.T) {
	c := newCLI(t)
	if code := c.run("bailar"); code != 1 || !strings.Contains(c.errOut.String(), "Uso:") || !strings.Contains(c.errOut.String(), "bailar") {
		t.Fatalf("código %d: %s", code, c.errOut.String())
	}
	c = newCLI(t)
	if code := c.run(); code != 1 || !strings.Contains(c.errOut.String(), "Uso:") {
		t.Fatalf("sin argumentos: %d %s", code, c.errOut.String())
	}
}

func TestCLI_EnrollRequiresFlags(t *testing.T) {
	c := newCLI(t)
	if code := c.run("enroll", "--token", "abc"); code != 1 || !strings.Contains(c.errOut.String(), "--server") {
		t.Errorf("sin --server: %d %s", code, c.errOut.String())
	}
	c = newCLI(t)
	if code := c.run("enroll", "--server", "https://mdm.ejemplo.es"); code != 1 || !strings.Contains(c.errOut.String(), "--token") {
		t.Errorf("sin --token: %d %s", code, c.errOut.String())
	}
}

func TestCLI_Version(t *testing.T) {
	c := newCLI(t)
	c.d.isAdmin = func() bool { return false } // version no exige privilegios
	if code := c.run("version"); code != 0 || strings.TrimSpace(c.out.String()) != version {
		t.Fatalf("código %d: %q", code, c.out.String())
	}
}

func TestCLI_RequiresAdmin(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"run"}, {"uninstall"}, {"enroll", "--server", "https://x", "--token", "t"}} {
		c := newCLI(t)
		c.d.isAdmin = func() bool { return false }
		if code := c.run(args...); code != 1 || !strings.Contains(c.errOut.String(), "Hace falta ejecutar como administrador (root).") {
			t.Errorf("%v: código %d: %s", args, code, c.errOut.String())
		}
		if c.uninstalls != 0 {
			t.Errorf("%v: actuó sin privilegios", args)
		}
	}
}

func TestCLI_StatusNotEnrolled(t *testing.T) {
	c := newCLI(t)
	if code := c.run("status"); code != 0 || !strings.Contains(c.out.String(), "Estado:           sin inscribir") {
		t.Fatalf("código %d:\n%s", code, c.out.String())
	}
}

func TestCLI_StatusEnrolled(t *testing.T) {
	c := newCLI(t)
	_ = config.Save(filepath.Join(c.dir, "agent.json"), config.Config{ServerURL: "https://mdm.ejemplo.es", DeviceID: "dev-1", Credential: "secreto-que-no-debe-verse"})
	if code := c.run("status"); code != 0 {
		t.Fatal(c.errOut.String())
	}
	out := c.out.String()
	if !strings.Contains(out, "Servidor:         https://mdm.ejemplo.es") || !strings.Contains(out, "Estado:           activo") {
		t.Errorf("salida:\n%s", out)
	}
	if strings.Contains(out, "secreto-que-no-debe-verse") {
		t.Error("status muestra la credencial")
	}
}

func TestCLI_RunNotEnrolled(t *testing.T) {
	c := newCLI(t)
	if code := c.run("run"); code != 1 || !strings.Contains(c.errOut.String(), "no está inscrito") {
		t.Fatalf("código %d: %s", code, c.errOut.String())
	}
}

func TestFormatStatus(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 2, 51, 0, time.UTC)
	seen := now.Add(-40 * time.Second)
	cfg := &config.Config{ServerURL: "https://mdm.ejemplo.es", DeviceID: "0b0e7a2e-1111-4222-8333-444455556666"}
	got := formatStatus(cfg, config.State{LastSuccess: &seen}, now)
	want := `Servidor:         https://mdm.ejemplo.es
Dispositivo:      0b0e7a2e-1111-4222-8333-444455556666
Estado:           activo
Última conexión:  2026-11-13 10:02:11 (hace 40 s)
Último error:     ninguno
`
	if got != want {
		t.Errorf("activo:\n%s\nse esperaba:\n%s", got, want)
	}

	errAt := now.Add(-3 * time.Minute)
	cfg.Revoked = true
	got = formatStatus(cfg, config.State{LastSuccess: &seen, LastError: "check-in: credencial rechazada", LastErrorAt: &errAt}, now)
	want = `Servidor:         https://mdm.ejemplo.es
Dispositivo:      0b0e7a2e-1111-4222-8333-444455556666
Estado:           revocado
Última conexión:  2026-11-13 10:02:11 (hace 40 s)
Último error:     check-in: credencial rechazada (hace 3 min)
`
	if got != want {
		t.Errorf("revocado:\n%s\nse esperaba:\n%s", got, want)
	}

	got = formatStatus(nil, config.State{}, now)
	want = `Servidor:         —
Dispositivo:      —
Estado:           sin inscribir
Última conexión:  nunca
Último error:     ninguno
`
	if got != want {
		t.Errorf("sin inscribir:\n%s", got)
	}
}

func TestAgo(t *testing.T) {
	cases := map[time.Duration]string{
		0: "hace 0 s", 59 * time.Second: "hace 59 s", 60 * time.Second: "hace 1 min", 59 * time.Minute: "hace 59 min",
		time.Hour: "hace 1 h", 47 * time.Hour: "hace 47 h", 48 * time.Hour: "hace 2 días", -5 * time.Second: "hace 0 s",
	}
	for d, want := range cases {
		if got := ago(d); got != want {
			t.Errorf("%v → %q, se esperaba %q", d, got, want)
		}
	}
}

func TestUninstall_RemovesServiceAndConfig(t *testing.T) {
	c := newCLI(t)
	_ = config.Save(filepath.Join(c.dir, "agent.json"), config.Config{ServerURL: "https://x"})
	_ = os.MkdirAll(filepath.Dir(c.d.installPath), 0o755)
	_ = os.WriteFile(c.d.installPath, []byte("binario"), 0o755)
	if code := c.run("uninstall"); code != 0 {
		t.Fatal(c.errOut.String())
	}
	if c.uninstalls != 1 {
		t.Errorf("Uninstall del servicio llamado %d veces", c.uninstalls)
	}
	if _, err := os.Stat(c.dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("el directorio de configuración sigue ahí: %v", err)
	}
	if _, err := os.Stat(c.d.installPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("el binario instalado sigue ahí: %v", err)
	}
	if code := c.run("uninstall"); code != 0 {
		t.Fatalf("repetir la desinstalación no debe fallar: %s", c.errOut.String())
	}
}

func TestUninstall_ServiceFailureKeepsConfig(t *testing.T) {
	c := newCLI(t)
	_ = config.Save(filepath.Join(c.dir, "agent.json"), config.Config{ServerURL: "https://x"})
	c.d.uninstallService = func() error { return errors.New("el servicio no se detiene") }
	if code := c.run("uninstall"); code != 1 || !strings.Contains(c.errOut.String(), "no se detiene") {
		t.Fatalf("código %d: %s", code, c.errOut.String())
	}
	if _, err := os.Stat(c.dir); err != nil {
		t.Error("si el servicio no se pudo quitar, la configuración debe conservarse para no dejarlo huérfano")
	}
}

func TestCopyBinary(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "descarga"), filepath.Join(dir, "destino", "mdm-agent")
	_ = os.WriteFile(src, []byte("v2"), 0o700)
	if err := copyBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(dst)
	fi, _ := os.Stat(dst)
	if string(b) != "v2" || fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("copia: %q %o", b, fi.Mode().Perm())
	}
	_ = os.WriteFile(src, []byte("v3"), 0o700)
	if err := copyBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "v3" {
		t.Fatalf("no sobrescribió: %q", b)
	}
	if err := copyBinary(dst, dst); err != nil {
		t.Fatalf("copiar sobre sí mismo no debe fallar ni vaciar el fichero: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "v3" {
		t.Fatalf("copiar sobre sí mismo dañó el fichero: %q", b)
	}
}

func TestCLI_SelftestExitCodes(t *testing.T) {
	orig := http.DefaultTransport
	defer func() { http.DefaultTransport = orig }()
	for _, tc := range []struct {
		status int
		want   int
	}{{200, 0}, {401, 1}} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/agent/v1/policy" || r.Method != "GET" {
				t.Errorf("petición inesperada: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"hash":"x","settings":[],"escrowed_key_ids":[]}`))
		}))
		c := newCLI(t)
		_ = config.Save(filepath.Join(c.dir, "agent.json"), config.Config{ServerURL: srv.URL, DeviceID: "d", Credential: "s"})
		http.DefaultTransport = srv.Client().Transport
		if code := c.run("selftest"); code != tc.want {
			t.Errorf("respuesta %d: código %d (%s)", tc.status, code, c.errOut.String())
		}
		srv.Close()
	}
	c := newCLI(t)
	if code := c.run("selftest"); code != 1 {
		t.Error("sin configuración debe fallar")
	}
}
