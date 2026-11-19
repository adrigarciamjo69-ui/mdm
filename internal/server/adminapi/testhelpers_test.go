package adminapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

type testAPI struct {
	t       *testing.T
	store   *store.Store
	handler http.Handler
	clock   *fakeClock
	n       int
	box     *secrets.Box
	files   *files.Store
	deps    Deps
}

const testPublicURL = "https://mdm.ejemplo.es"

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	s := storetest.New(t)
	clock := &fakeClock{t: time.Now().UTC().Truncate(time.Second)}
	box := newBox(t)
	fileStore := files.New(t.TempDir(), 0)
	deps := Deps{
		Store:            s,
		Audit:            audit.New(s),
		LoginLimiter:     ratelimit.New(5, 15*time.Minute, clock.Now),
		Now:              clock.Now,
		CheckinIntervalS: 60,
		PublicURL:        testPublicURL,
		Secrets:          box,
		Files:            fileStore,
	}
	return &testAPI{t: t, store: s, handler: New(deps), clock: clock, box: box, files: fileStore, deps: deps}
}

// rebuild vuelve a montar la API con dependencias modificadas.
func (a *testAPI) rebuild(mod func(*Deps)) {
	mod(&a.deps)
	a.handler = New(a.deps)
}

// createAdmin crea un administrador con una contraseña real (Argon2), para
// las pruebas que recorren el inicio de sesión.
func (a *testAPI) createAdmin(email, password string, role auth.Role) store.Admin {
	a.t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		a.t.Fatal(err)
	}
	adm, err := a.store.CreateAdmin(ctx, email, h, string(role))
	if err != nil {
		a.t.Fatal(err)
	}
	return adm
}

// loginAs devuelve un cliente con una sesión ya verificada del rol pedido,
// creada directamente en el almacén para no pagar Argon2 en cada prueba.
func (a *testAPI) loginAs(role auth.Role) *testClient {
	a.t.Helper()
	a.n++
	email := string(role) + string(rune('0'+a.n)) + "@ejemplo.es"
	adm, err := a.store.CreateAdmin(ctx, email, "sin-contraseña", string(role))
	if err != nil {
		a.t.Fatal(err)
	}
	secret := "JBSWY3DPEHPK3PXP"
	if err := a.store.SetAdminTOTP(ctx, adm.ID, &secret); err != nil {
		a.t.Fatal(err)
	}
	plain, hash, _ := auth.NewSecret()
	csrf, _, _ := auth.NewSecret()
	now := a.clock.Now()
	if err := a.store.CreateSession(ctx, store.Session{TokenHash: hash, AdminID: adm.ID, CSRFToken: csrf, Verified: true,
		CreatedAt: now, ExpiresAt: now.Add(12 * time.Hour), IP: "192.0.2.1"}); err != nil {
		a.t.Fatal(err)
	}
	return &testClient{api: a, cookie: &http.Cookie{Name: "mdm_session", Value: plain}, csrf: csrf, admin: adm}
}

func (a *testAPI) anon() *testClient { return &testClient{api: a} }

type testClient struct {
	api    *testAPI
	cookie *http.Cookie
	csrf   string
	admin  store.Admin
	ip     string
}

func (c *testClient) do(method, path string, body any) *http.Response {
	return c.request(method, path, body, true)
}

func (c *testClient) doNoCSRF(method, path string, body any) *http.Response {
	return c.request(method, path, body, false)
}

func (c *testClient) request(method, path string, body any, withCSRF bool) *http.Response {
	c.api.t.Helper()
	var rd io.Reader
	if body != nil {
		if raw, ok := body.(string); ok {
			rd = bytes.NewBufferString(raw)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				c.api.t.Fatal(err)
			}
			rd = bytes.NewReader(b)
		}
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = "192.0.2.1:4000"
	if c.ip != "" {
		r.RemoteAddr = c.ip + ":4000"
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if withCSRF && c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	w := httptest.NewRecorder()
	c.api.handler.ServeHTTP(w, r)
	resp := w.Result()
	for _, ck := range resp.Cookies() {
		if ck.Name == "mdm_session" {
			if ck.MaxAge < 0 || ck.Value == "" {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("respuesta no es JSON (%d): %s", resp.StatusCode, b)
	}
	return v
}

func bodyString(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func wantStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("estado %d, se esperaba %d: %s", resp.StatusCode, want, bodyString(resp))
	}
}

type errResp struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Fatalf("estado %d, se esperaba %d: %s", resp.StatusCode, status, bodyString(resp))
	}
	if e := decode[errResp](t, resp); e.Error.Code != code || e.Error.Message == "" {
		t.Fatalf("error %+v, se esperaba código %s con mensaje", e.Error, code)
	}
}

// auditActions devuelve las acciones registradas, de más antigua a más reciente.
func (a *testAPI) auditEntries() []store.AuditEntry {
	a.t.Helper()
	es, _, err := a.store.ListAudit(ctx, store.AuditFilter{Page: 1, PerPage: 200})
	if err != nil {
		a.t.Fatal(err)
	}
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func (a *testAPI) findAudit(action string) *store.AuditEntry {
	for _, e := range a.auditEntries() {
		if e.Action == action {
			return &e
		}
	}
	return nil
}

func newBox(t *testing.T) *secrets.Box {
	t.Helper()
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	b, err := secrets.New(base64.StdEncoding.EncodeToString(k))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// authorize añade a r la cookie de sesión, el token CSRF y la IP del cliente.
func (c *testClient) authorize(r *http.Request) {
	r.RemoteAddr = "192.0.2.1:4000"
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
}
