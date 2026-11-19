package client

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mdm/internal/protocol"
)

var ctx = context.Background()

type recorded struct {
	path, auth, encoding, userAgent, contentType string
	body                                         []byte
}

// server levanta un servidor TLS que responde con status/body y anota la petición.
func server(t *testing.T, status int, body string, headers map[string]string) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path, rec.auth, rec.encoding = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Encoding")
		rec.userAgent, rec.contentType = r.Header.Get("User-Agent"), r.Header.Get("Content-Type")
		rec.body, _ = io.ReadAll(r.Body)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/", "id.secreto", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.Version = "1.2.3"
	return c, rec
}

func TestClient_RejectsNonHTTPS(t *testing.T) {
	for _, u := range []string{"http://mdm.example", "mdm.example", "", "ftp://x", "https://"} {
		if _, err := New(u, "c", nil); err == nil {
			t.Errorf("%q debería rechazarse", u)
		}
	}
	if _, err := New("https://mdm.example", "c", nil); err != nil {
		t.Errorf("https válido rechazado: %v", err)
	}
}

func TestClient_SendsBearer(t *testing.T) {
	c, rec := server(t, 200, `{"config":{"checkin_interval_s":60,"inventory_interval_s":21600},"send_inventory":true,"commands":[]}`, nil)
	out, err := c.Checkin(ctx, protocol.CheckinRequest{AgentVersion: "1.2.3", InventoryHash: "sha256:x"})
	if err != nil || !out.SendInventory || out.Config.CheckinIntervalS != 60 {
		t.Fatalf("checkin: %+v %v", out, err)
	}
	if rec.auth != "Bearer id.secreto" || rec.path != "/agent/v1/checkin" || rec.userAgent != "mdm-agent/1.2.3" || rec.contentType != "application/json" {
		t.Fatalf("petición: %+v", rec)
	}
	var sent protocol.CheckinRequest
	if err := json.Unmarshal(rec.body, &sent); err != nil || sent.InventoryHash != "sha256:x" {
		t.Fatalf("cuerpo: %s", rec.body)
	}
}

func TestClient_EnrollSendsNoCredential(t *testing.T) {
	c, rec := server(t, 201, `{"device_id":"d1","credential":"s1","checkin_interval_s":60,"inventory_interval_s":21600}`, nil)
	out, err := c.Enroll(ctx, protocol.EnrollRequest{Token: "tok", HardwareID: "hw"})
	if err != nil || out.DeviceID != "d1" || out.Credential != "s1" {
		t.Fatalf("enroll: %+v %v", out, err)
	}
	if rec.auth != "" || rec.path != "/agent/v1/enroll" {
		t.Fatalf("petición: %+v", rec)
	}
}

func TestClient_InventoryIsGzipped(t *testing.T) {
	c, rec := server(t, 204, "", nil)
	inv := protocol.Inventory{Hash: "sha256:x", Software: []protocol.Software{{Name: "bash", Version: "5.2"}}}
	if err := c.SendInventory(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if rec.encoding != "gzip" || rec.path != "/agent/v1/inventory" || rec.auth != "Bearer id.secreto" {
		t.Fatalf("petición: %+v", rec)
	}
	zr, err := gzip.NewReader(bytesReader(rec.body))
	if err != nil {
		t.Fatalf("el cuerpo no es gzip: %v", err)
	}
	raw, _ := io.ReadAll(zr)
	var got protocol.Inventory
	if err := json.Unmarshal(raw, &got); err != nil || got.Hash != "sha256:x" || got.Software[0].Name != "bash" {
		t.Fatalf("contenido: %s", raw)
	}
}

func TestClient_ErrorMapping(t *testing.T) {
	check := func(status int, headers map[string]string) error {
		c, _ := server(t, status, `{"error":{"code":"x","message":"Mensaje del servidor."}}`, headers)
		_, err := c.Checkin(ctx, protocol.CheckinRequest{})
		return err
	}
	if err := check(401, nil); !errors.Is(err, ErrRevoked) {
		t.Errorf("401: %v", err)
	}
	if err := check(403, nil); !errors.Is(err, ErrTokenRejected) {
		t.Errorf("403: %v", err)
	}
	if err := check(413, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("413: %v", err)
	}
	var ra *RetryAfterError
	if err := check(429, map[string]string{"Retry-After": "120"}); !errors.As(err, &ra) || ra.After != 2*time.Minute {
		t.Errorf("429: %v", err)
	}
	if err := check(429, nil); !errors.As(err, &ra) || ra.After != time.Minute {
		t.Errorf("429 sin cabecera debe esperar 1 min: %v", err)
	}
	var tmp *TemporaryError
	for _, s := range []int{500, 502, 503, 504} {
		if err := check(s, nil); !errors.As(err, &tmp) {
			t.Errorf("%d: %v", s, err)
		}
	}
	// Otros 4xx no son reintentables ni significan revocación.
	err := check(400, nil)
	if err == nil || errors.As(err, &tmp) || errors.Is(err, ErrRevoked) {
		t.Errorf("400: %v", err)
	}
	if err != nil && !contains(err.Error(), "Mensaje del servidor.") {
		t.Errorf("el error debe incluir el mensaje del servidor: %v", err)
	}
}

func TestClient_NetworkErrorIsTemporary(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	c, _ := New(srv.URL, "id.s", srv.Client())
	srv.Close()
	_, err := c.Checkin(ctx, protocol.CheckinRequest{})
	var tmp *TemporaryError
	if !errors.As(err, &tmp) {
		t.Fatalf("conexión rechazada: %v", err)
	}
}

func TestClient_GarbageResponseIsTemporary(t *testing.T) {
	c, _ := server(t, 200, "<html>portal cautivo</html>", nil)
	_, err := c.Checkin(ctx, protocol.CheckinRequest{})
	var tmp *TemporaryError
	if !errors.As(err, &tmp) {
		t.Fatalf("una respuesta que no es JSON (portal cautivo, proxy) debe reintentarse: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
