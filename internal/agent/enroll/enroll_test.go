package enroll

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mdm/internal/agent/config"
	"mdm/internal/protocol"
)

var ctx = context.Background()

type fakeCollector struct{}

func (fakeCollector) Identify(context.Context) protocol.EnrollRequest {
	return protocol.EnrollRequest{HardwareID: "hw-1", Hostname: "pc-1", Platform: "linux", OSName: "Ubuntu", OSVersion: "24.04", Arch: "amd64", Serial: "S1"}
}
func (fakeCollector) Collect(context.Context) protocol.Inventory { return protocol.Inventory{} }

type env struct {
	opts      Options
	path      string
	installed int
	received  protocol.EnrollRequest
}

func setup(t *testing.T, status int, body string) *env {
	t.Helper()
	e := &env{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &e.received)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	e.path = filepath.Join(t.TempDir(), "mdm-agent", "agent.json")
	e.opts = Options{ServerURL: srv.URL, Token: "tok-123", Version: "1.0.0", ConfigPath: e.path,
		Collector: fakeCollector{}, HTTPClient: srv.Client(),
		InstallService: func() error { e.installed++; return nil }}
	return e
}

const okBody = `{"device_id":"dev-1","credential":"sec-1","checkin_interval_s":90,"inventory_interval_s":7200}`

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestEnroll_SavesConfigAndInstallsService(t *testing.T) {
	e := setup(t, 201, okBody)
	if err := Run(ctx, e.opts); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(e.path)
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{ServerURL: e.opts.ServerURL, DeviceID: "dev-1", Credential: "sec-1", CheckinIntervalS: 90, InventoryIntervalS: 7200}
	if cfg != want {
		t.Errorf("configuración: %+v", cfg)
	}
	if e.installed != 1 {
		t.Errorf("InstallService llamado %d veces", e.installed)
	}
	if ExitCode(nil) != 0 {
		t.Error("ExitCode(nil) debe ser 0")
	}
}

func TestEnroll_SendsIdentity(t *testing.T) {
	e := setup(t, 201, okBody)
	_ = Run(ctx, e.opts)
	r := e.received
	if r.Token != "tok-123" || r.AgentVersion != "1.0.0" || r.HardwareID != "hw-1" || r.Hostname != "pc-1" || r.Platform != "linux" || r.Serial != "S1" {
		t.Errorf("petición: %+v", r)
	}
}

func TestEnroll_TokenRejected(t *testing.T) {
	e := setup(t, 403, `{"error":{"code":"forbidden","message":"no"}}`)
	err := Run(ctx, e.opts)
	if ExitCode(err) != 2 {
		t.Fatalf("código %d: %v", ExitCode(err), err)
	}
	if exists(e.path) || e.installed != 0 {
		t.Error("un token rechazado no debe dejar configuración ni instalar el servicio")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("el mensaje debe mencionar el token: %v", err)
	}
}

func TestEnroll_NetworkFailure(t *testing.T) {
	e := setup(t, 201, okBody)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	e.opts.ServerURL, e.opts.HTTPClient = srv.URL, srv.Client()
	srv.Close()
	err := Run(ctx, e.opts)
	if ExitCode(err) != 3 || exists(e.path) || e.installed != 0 {
		t.Fatalf("código %d, fichero=%v, instalado=%d: %v", ExitCode(err), exists(e.path), e.installed, err)
	}
}

func TestEnroll_ServerErrorIsRetryable(t *testing.T) {
	e := setup(t, 503, `{"error":{"code":"unavailable","message":"no"}}`)
	if err := Run(ctx, e.opts); ExitCode(err) != 3 {
		t.Fatalf("código %d", ExitCode(err))
	}
}

func TestEnroll_RejectsHTTP(t *testing.T) {
	e := setup(t, 201, okBody)
	e.opts.ServerURL = "http://mdm.ejemplo.es"
	err := Run(ctx, e.opts)
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "https") || exists(e.path) {
		t.Fatalf("código %d: %v", ExitCode(err), err)
	}
}

func TestEnroll_MissingTokenOrIdentity(t *testing.T) {
	e := setup(t, 201, okBody)
	e.opts.Token = " "
	if err := Run(ctx, e.opts); ExitCode(err) != 1 || e.received.HardwareID != "" {
		t.Fatalf("sin token no debe llamar al servidor: %v", err)
	}
}

func TestEnroll_ServiceInstallFailureRemovesConfig(t *testing.T) {
	e := setup(t, 201, okBody)
	boom := errors.New("sin permisos")
	e.opts.InstallService = func() error { return boom }
	err := Run(ctx, e.opts)
	if !errors.Is(err, boom) || ExitCode(err) != 1 {
		t.Fatalf("error: %v", err)
	}
	if exists(e.path) {
		t.Error("la configuración debe borrarse si el servicio no se pudo instalar")
	}
}

func TestEnroll_ReenrollOverwritesRevokedConfig(t *testing.T) {
	e := setup(t, 201, okBody)
	_ = config.Save(e.path, config.Config{ServerURL: "https://viejo", DeviceID: "x", Credential: "y", Revoked: true})
	if err := Run(ctx, e.opts); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := config.Load(e.path); cfg.Revoked || cfg.DeviceID != "dev-1" {
		t.Fatalf("configuración tras reinscribir: %+v", cfg)
	}
}

func TestEnroll_ErrorMessagesAreNotDuplicated(t *testing.T) {
	e := setup(t, 403, `{"error":{"code":"forbidden","message":"no"}}`)
	err := Run(ctx, e.opts)
	if n := strings.Count(err.Error(), "rechazado"); n != 1 {
		t.Fatalf("el mensaje repite la causa %d veces: %v", n, err)
	}
	if !strings.HasSuffix(err.Error(), ".") {
		t.Fatalf("el mensaje debe ser una frase completa: %v", err)
	}
}
