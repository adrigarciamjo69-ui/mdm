package agentapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/auth"
	"mdm/internal/server/files"
	"mdm/internal/server/ratelimit"
	"mdm/internal/server/secrets"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type agentTest struct {
	t       *testing.T
	store   *store.Store
	handler http.Handler
	clock   *fakeClock
	admin   store.Admin
	n       int
	files   *files.Store
}

type agentOpt func(*Deps)

func withReleases(r Releases) agentOpt { return func(d *Deps) { d.Releases = r } }

func newAgentAPI(t *testing.T, opts ...agentOpt) *agentTest {
	t.Helper()
	s := storetest.New(t)
	clock := &fakeClock{t: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)}
	admin, err := s.CreateAdmin(ctx, "admin@ejemplo.es", "x", "admin")
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{
		Store:         s,
		Audit:         audit.New(s).WithClock(clock.Now),
		EnrollLimiter: ratelimit.New(10, time.Minute, clock.Now),
		Now:           clock.Now,
		Secrets:       testBox,
		Files:         files.New(t.TempDir(), 0),
	}
	for _, o := range opts {
		o(&deps)
	}
	h := New(deps)
	fileStore := deps.Files
	return &agentTest{t: t, store: s, handler: h, clock: clock, admin: admin, files: fileStore}
}

// token crea un token de inscripción y devuelve su valor en claro.
func (a *agentTest) token(maxUses *int, expires *time.Time) string {
	plain, _ := a.tokenWith(store.TokenParams{MaxUses: maxUses, ExpiresAt: expires})
	return plain
}

func (a *agentTest) tokenWith(p store.TokenParams) (string, store.EnrollmentToken) {
	a.t.Helper()
	plain, hash, _ := auth.NewSecret()
	p.TokenHash, p.CreatedBy = hash, a.admin.ID
	if p.Name == "" {
		p.Name = "Oficina"
	}
	tok, err := a.store.CreateToken(ctx, p)
	if err != nil {
		a.t.Fatal(err)
	}
	return plain, tok
}

func sampleEnroll(token, hardwareID string) protocol.EnrollRequest {
	return protocol.EnrollRequest{
		Token: token, HardwareID: hardwareID, Hostname: "PC-A", Platform: "windows",
		OSName: "Windows 11 Pro", OSVersion: "10.0.26100", Arch: "amd64",
		Serial: "SN123", Manufacturer: "Dell", Model: "Latitude", AgentVersion: "1.0.0",
	}
}

func (a *agentTest) post(path, ip, authz string, body any, gz bool) *http.Response {
	a.t.Helper()
	var raw []byte
	switch v := body.(type) {
	case nil:
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		var err error
		if raw, err = json.Marshal(v); err != nil {
			a.t.Fatal(err)
		}
	}
	if gz {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		raw = buf.Bytes()
	}
	r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	if ip == "" {
		ip = "203.0.113.10"
	}
	r.RemoteAddr = ip + ":40000"
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	if gz {
		r.Header.Set("Content-Encoding", "gzip")
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w.Result()
}

func (a *agentTest) enroll(req protocol.EnrollRequest) (*http.Response, protocol.EnrollResponse) {
	return a.enrollFrom("", req)
}

func (a *agentTest) enrollFrom(ip string, req protocol.EnrollRequest) (*http.Response, protocol.EnrollResponse) {
	a.t.Helper()
	resp := a.post("/enroll", ip, "", req, false)
	var out protocol.EnrollResponse
	if resp.StatusCode == http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, &out); err != nil {
			a.t.Fatalf("respuesta de inscripción: %s", b)
		}
	}
	return resp, out
}

func (a *agentTest) device(id string) store.Device {
	a.t.Helper()
	d, err := a.store.DeviceByID(ctx, uuid.MustParse(id))
	if err != nil {
		a.t.Fatalf("leer dispositivo %s: %v", id, err)
	}
	return d
}

func (a *agentTest) audit(action string) []store.AuditEntry {
	a.t.Helper()
	es, _, err := a.store.ListAudit(ctx, store.AuditFilter{Action: action, Page: 1, PerPage: 200})
	if err != nil {
		a.t.Fatal(err)
	}
	return es
}

func bodyString(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	body := bodyString(resp)
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &e)
	if resp.StatusCode != status || e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("got %d %s; se esperaba %d con código %s", resp.StatusCode, body, status, code)
	}
}

func (a *agentTest) get(path, authz string) *http.Response {
	a.t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = "203.0.113.50:40000"
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w.Result()
}

// testBox usa una clave fija: las pruebas de la consola descifran con la misma.
var testBox, _ = secrets.New("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
