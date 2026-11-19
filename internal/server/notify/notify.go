// Package notify envía avisos por correo (SMTP) y por webhook firmado.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Message es un aviso. El correo usa Subject, Text, HTML y Attachment; el
// webhook, Event y Payload.
type Message struct {
	Event      string
	Subject    string
	Text       string
	HTML       string
	Attachment *Attachment
	Payload    map[string]any
}

type SMTP struct {
	Host     string
	Port     int
	Security string // starttls, tls, none
	Username string
	Password string
	From     string
	FromName string
}

const smtpTimeout = 30 * time.Second

func clean(s string) error {
	if strings.ContainsAny(s, "\r\n") {
		return errors.New("una dirección de correo no puede contener saltos de línea")
	}
	return nil
}

// build compone el mensaje MIME: texto y HTML alternativos y, si hay, un adjunto.
func build(cfg SMTP, to []string, m Message) ([]byte, error) {
	var buf bytes.Buffer
	from := cfg.From
	if cfg.FromName != "" {
		from = mime.QEncoding.Encode("utf-8", cfg.FromName) + " <" + cfg.From + ">"
	}
	hdr := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	hdr("From", from)
	hdr("To", strings.Join(to, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+uuid.NewString()+"@mdm>")
	hdr("MIME-Version", "1.0")
	mixed := multipart.NewWriter(&buf)
	hdr("Content-Type", `multipart/mixed; boundary="`+mixed.Boundary()+`"`)
	buf.WriteString("\r\n")

	var alt bytes.Buffer
	altW := multipart.NewWriter(&alt)
	for _, part := range []struct{ ct, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		if part.body == "" {
			continue
		}
		w, _ := altW.CreatePart(textproto.MIMEHeader{"Content-Type": {part.ct + "; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		qp := quotedprintable.NewWriter(w)
		_, _ = qp.Write([]byte(part.body))
		_ = qp.Close()
	}
	_ = altW.Close()
	w, _ := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {`multipart/alternative; boundary="` + altW.Boundary() + `"`}})
	_, _ = w.Write(alt.Bytes())
	if a := m.Attachment; a != nil {
		w, _ := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {a.ContentType}, "Content-Transfer-Encoding": {"base64"},
			"Content-Disposition": {`attachment; filename="` + strings.ReplaceAll(a.Name, `"`, "") + `"`}})
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			_, _ = io.WriteString(w, enc[:76]+"\r\n")
			enc = enc[76:]
		}
		_, _ = io.WriteString(w, enc+"\r\n")
	}
	_ = mixed.Close()
	return buf.Bytes(), nil
}

// SendEmail envía el mensaje con el servidor SMTP configurado.
func SendEmail(ctx context.Context, cfg SMTP, to []string, m Message) error {
	if len(to) == 0 {
		return errors.New("no hay destinatarios")
	}
	for _, a := range append([]string{cfg.From}, to...) {
		if err := clean(a); err != nil {
			return err
		}
	}
	body, err := build(cfg, to, m)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: smtpTimeout}
	var conn net.Conn
	if cfg.Security == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("no se puede conectar con el servidor de correo %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(smtpTimeout))
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("el servidor de correo no responde como SMTP: %w", err)
	}
	defer c.Close()
	if cfg.Security == "starttls" {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		// smtp.PlainAuth solo envía la contraseña con TLS o a localhost: sin
		// cifrado (security=none) se permite expresamente porque lo eligió el administrador.
		auth := plainAuth{user: cfg.Username, pass: cfg.Password}
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("el servidor de correo rechazó el usuario o la contraseña: %w", err)
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return fmt.Errorf("remitente rechazado: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("destinatario %s rechazado: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("el servidor de correo no aceptó el mensaje: %w", err)
	}
	return c.Quit()
}

type plainAuth struct{ user, pass string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a plainAuth) Next([]byte, bool) ([]byte, error) { return nil, nil }

const webhookTimeout = 10 * time.Second

// SendWebhook envía el aviso como JSON firmado con HMAC-SHA256.
func SendWebhook(ctx context.Context, hc *http.Client, target, secret string, m Message, version string) error {
	body, err := json.Marshal(m.Payload)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mdm-server/"+version)
	req.Header.Set("X-MDM-Event", m.Event)
	req.Header.Set("X-MDM-Delivery", uuid.NewString())
	req.Header.Set("X-MDM-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if hc == nil {
		hc = &http.Client{Timeout: webhookTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo entregar el webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("el receptor del webhook respondió %d", resp.StatusCode)
	}
	return nil
}

var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}

// Backoff devuelve la espera tras el intento n (1, 2…) y si quedan reintentos.
func Backoff(n int) (time.Duration, bool) {
	if n < 1 || n > len(backoff) {
		return 0, false
	}
	return backoff[n-1], true
}

// ValidateWebhookURL exige https; http solo hacia direcciones privadas.
func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return errors.New("La dirección del webhook debe ser https://…")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
			return errors.New("Un webhook sin cifrar (http) solo se admite hacia una dirección IP privada de la red interna.")
		}
	}
	return nil
}

// NewSecret genera el secreto de firma de un webhook.
func NewSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
