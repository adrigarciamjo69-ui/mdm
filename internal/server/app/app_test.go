package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"mdm/internal/protocol"
	"mdm/internal/server/files"
	"mdm/internal/server/store"
	"mdm/internal/server/store/storetest"
)

var ctx = context.Background()

var staticFS = fstest.MapFS{
	"index.html":     {Data: []byte("<!doctype html><title>MDM</title>")},
	"assets/app.js":  {Data: []byte("console.log('mdm')")},
	"assets/app.css": {Data: []byte("body{}")},
}

func newHandler(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	s := storetest.New(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mdm-agent-linux-amd64"), []byte("binario"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secreto"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{PublicURL: "https://mdm.ejemplo.es", DownloadsDir: dir, SecretKey: testSecretKey}
	return NewHandler(cfg, s, staticFS, time.Now), s, dir
}

func get(h http.Handler, path string) (*http.Response, string) {
	r := httptest.NewRequest("GET", "http://mdm.ejemplo.es/", nil)
	r.URL.Path = path
	r.RequestURI = path
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	return w.Result(), string(b)
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := LoadConfig(env(map[string]string{"MDM_PUBLIC_URL": "https://x.es"})); err == nil || !strings.Contains(err.Error(), "MDM_DATABASE_URL") {
		t.Errorf("sin base de datos: %v", err)
	}
	if _, err := LoadConfig(env(map[string]string{"MDM_DATABASE_URL": "postgres://x"})); err == nil || !strings.Contains(err.Error(), "MDM_PUBLIC_URL") {
		t.Errorf("sin URL pública: %v", err)
	}
	if _, err := LoadConfig(env(map[string]string{"MDM_DATABASE_URL": "postgres://x", "MDM_PUBLIC_URL": "http://x.es"})); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Errorf("URL sin https: %v", err)
	}
	c, err := LoadConfig(env(map[string]string{"MDM_DATABASE_URL": "postgres://x", "MDM_PUBLIC_URL": "https://x.es/", "MDM_SECRET_KEY": testSecretKey}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenAddr != ":8080" || c.DownloadsDir != "/downloads" || c.TrustProxy || c.PublicURL != "https://x.es" {
		t.Errorf("valores por defecto: %+v", c)
	}
	c, _ = LoadConfig(env(map[string]string{"MDM_DATABASE_URL": "postgres://x", "MDM_PUBLIC_URL": "https://x.es",
		"MDM_TRUSTED_PROXY": "true", "MDM_LISTEN_ADDR": ":9000", "MDM_DOWNLOADS_DIR": "/srv/d", "MDM_SECRET_KEY": testSecretKey}))
	if !c.TrustProxy || c.ListenAddr != ":9000" || c.DownloadsDir != "/srv/d" {
		t.Errorf("valores explícitos: %+v", c)
	}
}

func TestHandler_Healthz(t *testing.T) {
	h, _, _ := newHandler(t)
	resp, body := get(h, "/healthz")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != `{"status":"ok"}` {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestHandler_SPAFallback(t *testing.T) {
	h, _, _ := newHandler(t)
	for _, path := range []string{"/", "/dispositivos/abc", "/auditoria"} {
		resp, body := get(h, path)
		if resp.StatusCode != 200 || !strings.Contains(body, "<title>MDM</title>") {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control %q", path, cc)
		}
	}
	resp, body := get(h, "/assets/app.js")
	if resp.StatusCode != 200 || body != "console.log('mdm')" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("recurso estático: %d %q %s", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
	}
	// Un recurso inexistente no devuelve la página: sería un JS que no es JS.
	if resp, _ := get(h, "/assets/no-existe.js"); resp.StatusCode != 404 {
		t.Errorf("recurso inexistente: %d", resp.StatusCode)
	}
	for _, path := range []string{"/api/v1/nope", "/agent/v1/nope", "/api/otra"} {
		resp, body := get(h, path)
		if resp.StatusCode != 404 || !strings.Contains(body, `"code":"not_found"`) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}

func TestHandler_RoutesMounted(t *testing.T) {
	h, _, _ := newHandler(t)
	if resp, body := get(h, "/api/v1/auth/me"); resp.StatusCode != 401 || !strings.Contains(body, "unauthorized") {
		t.Errorf("/api/v1/auth/me: %d %s", resp.StatusCode, body)
	}
	r := httptest.NewRequest("POST", "/agent/v1/checkin", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("/agent/v1/checkin: %d", w.Code)
	}
}

func TestHandler_DownloadsNoTraversal(t *testing.T) {
	h, _, _ := newHandler(t)
	if resp, body := get(h, "/downloads/mdm-agent-linux-amd64"); resp.StatusCode != 200 || body != "binario" {
		t.Errorf("fichero existente: %d %q", resp.StatusCode, body)
	}
	for _, path := range []string{"/downloads/../secreto", "/downloads/", "/downloads", "/downloads/sub", "/downloads/sub/",
		"/downloads/no-existe", "/downloads/..%2Fsecreto", "/downloads/.."} {
		resp, body := get(h, path)
		if resp.StatusCode != 404 || strings.Contains(body, "no") && body == "no" {
			t.Errorf("%s: %d %q", path, resp.StatusCode, body)
		}
	}
}

func TestHandler_SecurityHeaders(t *testing.T) {
	h, _, _ := newHandler(t)
	for _, path := range []string{"/", "/api/v1/auth/me", "/healthz"} {
		resp, _ := get(h, path)
		for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer"} {
			if got := resp.Header.Get(k); got != want {
				t.Errorf("%s: %s = %q", path, k, got)
			}
		}
	}
	resp, _ := get(h, "/")
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'" {
		t.Errorf("CSP: %q", csp)
	}
}

func TestHandler_DatabaseDownReturns503(t *testing.T) {
	h, s, _ := newHandler(t)
	s.Close()
	for _, path := range []string{"/api/v1/auth/me", "/healthz"} {
		resp, body := get(h, path)
		if resp.StatusCode != 503 || !strings.Contains(body, "unavailable") {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	r := httptest.NewRequest("POST", "/agent/v1/checkin", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"unavailable"`) {
		t.Errorf("agente: %d %s", w.Code, w.Body)
	}
	// La consola se sigue sirviendo para poder mostrar el aviso.
	if resp, _ := get(h, "/"); resp.StatusCode != 200 {
		t.Errorf("la consola debería servirse sin base de datos: %d", resp.StatusCode)
	}
}

func TestJanitor_PurgesOldRows(t *testing.T) {
	s := storetest.New(t)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	adm, _ := s.CreateAdmin(ctx, "a@ejemplo.es", "x", "admin")
	tok, _ := s.CreateToken(ctx, store.TokenParams{Name: "t", TokenHash: "h", CreatedBy: adm.ID})
	_ = tok
	res, err := s.EnrollDevice(ctx, store.EnrollParams{TokenHash: "h", CredentialHash: "c", At: now,
		Req: protocol.EnrollRequest{HardwareID: "hw", Hostname: "pc", Platform: "linux"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{91, 89} {
		_ = s.RecordCheckin(ctx, res.DeviceID, store.CheckinParams{At: now.Add(-time.Duration(days) * 24 * time.Hour)})
	}
	_ = s.CreateSession(ctx, store.Session{TokenHash: "vieja", AdminID: adm.ID, CSRFToken: "c", CreatedAt: now.Add(-13 * time.Hour), ExpiresAt: now.Add(-time.Hour)})
	_ = s.CreateSession(ctx, store.Session{TokenHash: "viva", AdminID: adm.ID, CSRFToken: "c", CreatedAt: now, ExpiresAt: now.Add(time.Hour)})

	_ = store.EnqueueJob(ctx, s.Pool(), store.NewJob{Kind: "vieja", RunAt: now.Add(-9 * 24 * time.Hour)})
	_ = store.EnqueueJob(ctx, s.Pool(), store.NewJob{Kind: "reciente", RunAt: now.Add(-time.Hour)})
	if j, _ := s.ClaimJob(ctx, now.Add(-8*24*time.Hour), time.Minute); j != nil {
		_ = s.FinishJob(ctx, j.ID, now.Add(-8*24*time.Hour))
	}

	PurgeOnce(ctx, s, now)

	if _, n, _ := s.ListJobs(ctx, "finished", 1, 10); n != 0 {
		t.Errorf("quedan tareas terminadas de hace 8 días: %d", n)
	}
	// Pendientes: «reciente» y el suceso device.enrolled de la inscripción.
	if _, n, _ := s.ListJobs(ctx, "pending", 1, 10); n != 2 {
		t.Errorf("se borró una tarea pendiente: %d", n)
	}

	act, _ := s.DeviceActivity(ctx, res.DeviceID, time.Time{})
	if len(act) != 1 || now.Sub(act[0].Hour) > 90*24*time.Hour {
		t.Errorf("actividad tras la limpieza: %+v", act)
	}
	if _, _, err := s.SessionByTokenHash(ctx, "viva", now); err != nil {
		t.Errorf("se borró una sesión vigente: %v", err)
	}
	if n, _ := s.PurgeExpiredSessions(ctx, now); n != 0 {
		t.Errorf("quedaban %d sesiones caducadas", n)
	}
}

func TestPurgeFiles_RemovesOldUnreferenced(t *testing.T) {
	s := storetest.New(t)
	fs := files.New(t.TempDir(), 0)
	now := time.Now().UTC()
	old, _, _ := fs.Put(ctx, strings.NewReader("viejo"), 100)
	recent, _, _ := fs.Put(ctx, strings.NewReader("reciente"), 100)
	_ = s.SaveFile(ctx, store.File{SHA256: old, Size: 5, UploadedAt: now.Add(-8 * 24 * time.Hour)})
	_ = s.SaveFile(ctx, store.File{SHA256: recent, Size: 8, UploadedAt: now.Add(-time.Hour)})
	PurgeFiles(ctx, s, fs, now)
	if _, _, err := fs.Open(old); err == nil {
		t.Error("el fichero sin uso de hace 8 días debe borrarse del disco")
	}
	if _, err := s.GetFile(ctx, old); err == nil {
		t.Error("y su fila")
	}
	if _, _, err := fs.Open(recent); err != nil {
		t.Error("el reciente se conserva")
	}
}
