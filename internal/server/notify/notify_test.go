package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

// smtpServer es un servidor SMTP mínimo en memoria (sin TLS) para pruebas.
type smtpServer struct {
	addr string
	mu   sync.Mutex
	msgs []smtpMsg
	auth []string
}

type smtpMsg struct {
	from string
	to   []string
	data string
}

func newSMTP(t *testing.T) *smtpServer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpServer{addr: l.Addr().String()}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *smtpServer) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := func(line string) { _, _ = io.WriteString(c, line+"\r\n") }
	w("220 prueba ESMTP")
	var m smtpMsg
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250-prueba")
			w("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			s.mu.Lock()
			s.auth = append(s.auth, strings.TrimSpace(line[len("AUTH PLAIN"):]))
			s.mu.Unlock()
			w("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			m = smtpMsg{from: strings.Trim(strings.TrimSpace(line)[10:], "<>")}
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			m.to = append(m.to, strings.Trim(strings.TrimSpace(line)[8:], "<>"))
			w("250 ok")
		case cmd == "DATA":
			w("354 adelante")
			var b strings.Builder
			for {
				l, _ := r.ReadString('\n')
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			m.data = b.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, m)
			s.mu.Unlock()
			w("250 entregado")
		case cmd == "QUIT":
			w("221 adiós")
			return
		default:
			w("250 ok")
		}
	}
}

func TestEmail_SendsWithAttachment(t *testing.T) {
	s := newSMTP(t)
	host, port, _ := net.SplitHostPort(s.addr)
	p, _ := strconv.Atoi(port)
	cfg := SMTP{Host: host, Port: p, Security: "none", Username: "mdm", Password: "clave", From: "mdm@ejemplo.es", FromName: "MDM Ejemplo"}
	msg := Message{Subject: "Alerta: srv-db-01 sin conexión", Text: "Hola", HTML: "<p>Hola</p>",
		Attachment: &Attachment{Name: "parches.csv", ContentType: "text/csv", Data: []byte("\ufeffEquipo;Pendientes\r\nPC-1;3\r\n")}}
	if err := SendEmail(ctx, cfg, []string{"it@ejemplo.es", "jefe@ejemplo.es"}, msg); err != nil {
		t.Fatal(err)
	}
	if len(s.msgs) != 1 || len(s.msgs[0].to) != 2 || s.msgs[0].from != "mdm@ejemplo.es" || len(s.auth) != 1 {
		t.Fatalf("%+v", s.msgs)
	}
	d := s.msgs[0].data
	for _, want := range []string{"Subject: =?utf-8?", "multipart/mixed", `filename="parches.csv"`, "text/html", "From: MDM Ejemplo <mdm@ejemplo.es>"} {
		if !strings.Contains(d, want) {
			t.Errorf("falta %q en:\n%s", want, d)
		}
	}
}

func TestEmail_RejectsHeaderInjection(t *testing.T) {
	cfg := SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "mdm@ejemplo.es"}
	if err := SendEmail(ctx, cfg, []string{"a@b.es\r\nBcc: x@y.es"}, Message{Subject: "x"}); err == nil {
		t.Fatal("un destinatario con saltos de línea debe rechazarse")
	}
}

func TestWebhook_SignedAndHeaders(t *testing.T) {
	var got http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	err := SendWebhook(ctx, srv.Client(), srv.URL, "secreto", Message{Event: "alert.opened", Payload: map[string]any{"event": "alert.opened", "alert": map[string]any{"id": 1}}}, "1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte("secreto"))
	mac.Write(body)
	if got.Get("X-MDM-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("firma: %s", got.Get("X-MDM-Signature"))
	}
	if got.Get("X-MDM-Event") != "alert.opened" || got.Get("User-Agent") != "mdm-server/1.4.0" || len(got.Get("X-MDM-Delivery")) != 36 ||
		got.Get("Content-Type") != "application/json" {
		t.Fatalf("cabeceras: %v", got)
	}
}

func TestWebhook_NonSuccessIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(410) }))
	defer srv.Close()
	if err := SendWebhook(ctx, srv.Client(), srv.URL, "s", Message{Event: "x", Payload: map[string]any{}}, "1"); err == nil || !strings.Contains(err.Error(), "410") {
		t.Fatalf("%v", err)
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}
	for i, w := range want {
		if d, ok := Backoff(i + 1); !ok || d != w {
			t.Errorf("intento %d: %v", i+1, d)
		}
	}
	if _, ok := Backoff(6); ok {
		t.Error("tras el 6.º intento no hay más")
	}
}

func TestValidateWebhookURL(t *testing.T) {
	for _, ok := range []string{"https://hooks.slack.com/services/x", "http://10.0.0.5:8080/mdm", "http://192.168.1.10/x"} {
		if err := ValidateWebhookURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"http://ejemplo.es/x", "ftp://x", "https://", "javascript:alert(1)"} {
		if err := ValidateWebhookURL(bad); err == nil {
			t.Errorf("%s debería rechazarse", bad)
		}
	}
}
