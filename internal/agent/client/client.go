// Package client es el cliente HTTP del agente hacia el servidor.
package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"mdm/internal/protocol"
)

var (
	// ErrRevoked: el servidor no reconoce la credencial del equipo (401).
	ErrRevoked = errors.New("el servidor ha rechazado la credencial de este equipo")
	// ErrTokenRejected: el token de inscripción no es válido (403).
	ErrTokenRejected = errors.New("el servidor ha rechazado el token de inscripción")
	// ErrTooLarge: el servidor considera el envío demasiado grande (413).
	ErrTooLarge = errors.New("el servidor ha rechazado el envío por ser demasiado grande")
)

// RetryAfterError indica que el servidor pide esperar antes de reintentar (429).
type RetryAfterError struct {
	After time.Duration
}

func (e *RetryAfterError) Error() string {
	return fmt.Sprintf("el servidor pide esperar %s antes de reintentar", e.After)
}

// TemporaryError envuelve los fallos que merece la pena reintentar: red,
// errores 5xx del servidor o respuestas ilegibles.
type TemporaryError struct {
	Err error
}

func (e *TemporaryError) Error() string { return "fallo temporal: " + e.Err.Error() }
func (e *TemporaryError) Unwrap() error { return e.Err }

// maxResponse limita lo que el agente lee de una respuesta del servidor.
const maxResponse = 1 << 20

type Client struct {
	base       string
	credential string
	http       *http.Client
	// Version viaja en la cabecera User-Agent.
	Version string
}

// New crea el cliente. Solo admite direcciones https://: la credencial del
// equipo y su inventario nunca viajan sin cifrar.
func New(serverURL, credential string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("la dirección del servidor debe empezar por https:// (recibido: %q)", serverURL)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), credential: credential, http: hc, Version: "dev"}, nil
}

func (c *Client) Enroll(ctx context.Context, req protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	var out protocol.EnrollResponse
	err := c.post(ctx, "/agent/v1/enroll", req, false, false, &out)
	return out, err
}

func (c *Client) Checkin(ctx context.Context, req protocol.CheckinRequest) (protocol.CheckinResponse, error) {
	var out protocol.CheckinResponse
	err := c.post(ctx, "/agent/v1/checkin", req, true, false, &out)
	return out, err
}

// SendInventory envía el inventario comprimido con gzip.
func (c *Client) SendInventory(ctx context.Context, inv protocol.Inventory) error {
	return c.post(ctx, "/agent/v1/inventory", inv, true, true, nil)
}

// GetPolicy descarga la política efectiva del equipo.
func (c *Client) GetPolicy(ctx context.Context) (protocol.EffectivePolicy, error) {
	var p protocol.EffectivePolicy
	err := c.do(ctx, http.MethodGet, "/agent/v1/policy", nil, true, false, &p)
	return p, err
}

// SendCompliance envía el resultado de evaluar la política.
func (c *Client) SendCompliance(ctx context.Context, r protocol.ComplianceReport) error {
	return c.post(ctx, "/agent/v1/compliance", r, true, false, nil)
}

// SendRecoveryKey custodia en el servidor una clave de recuperación de BitLocker.
func (c *Client) SendRecoveryKey(ctx context.Context, k protocol.RecoveryKey) error {
	return c.post(ctx, "/agent/v1/recovery-keys", k, true, false, nil)
}

// maxDownload limita el tamaño de un binario descargado.
const maxDownload = 200 << 20

// Download descarga path (relativo al servidor) en dst.
func (c *Client) Download(ctx context.Context, path, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mdm-agent/"+c.Version)
	req.Header.Set("Authorization", "Bearer "+c.credential)
	resp, err := c.http.Do(req)
	if err != nil {
		return &TemporaryError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("el servidor respondió %d al descargar %s", resp.StatusCode, path)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxDownload {
		err = errors.New("el fichero descargado supera el tamaño máximo")
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}

// SendUpdates envía el resultado de una búsqueda de actualizaciones.
func (c *Client) SendUpdates(ctx context.Context, s protocol.UpdateScan) error {
	return c.post(ctx, "/agent/v1/updates", s, true, true, nil)
}

// SendUpdateResults envía el resultado de una instalación de actualizaciones.
func (c *Client) SendUpdateResults(ctx context.Context, r protocol.UpdateResults) error {
	return c.post(ctx, "/agent/v1/updates/results", r, true, false, nil)
}

// SendAppStatus envía el estado de los despliegues de software.
func (c *Client) SendAppStatus(ctx context.Context, r protocol.AppStatusReport) error {
	return c.post(ctx, "/agent/v1/apps/status", r, true, false, nil)
}

// Base devuelve la dirección del servidor; HTTP, el cliente HTTP configurado
// (con la CA del equipo); Bearer, la credencial.
func (c *Client) Base() string       { return c.base }
func (c *Client) HTTP() *http.Client { return c.http }
func (c *Client) Bearer() string     { return c.credential }

// StartCommand confirma al servidor que la orden ha empezado.
func (c *Client) StartCommand(ctx context.Context, id string) error {
	return c.post(ctx, "/agent/v1/commands/"+url.PathEscape(id)+"/start", struct{}{}, true, false, nil)
}

// FinishCommand envía el resultado de una orden.
func (c *Client) FinishCommand(ctx context.Context, id string, r protocol.CommandResult) error {
	return c.post(ctx, "/agent/v1/commands/"+url.PathEscape(id)+"/result", r, true, false, nil)
}

func (c *Client) post(ctx context.Context, path string, body any, withAuth, compress bool, out any) error {
	return c.do(ctx, http.MethodPost, path, body, withAuth, compress, out)
}

func (c *Client) do(ctx context.Context, method, path string, body any, withAuth, compress bool, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	if compress {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(raw); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		raw = buf.Bytes()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "mdm-agent/"+c.Version)
	if compress {
		req.Header.Set("Content-Encoding", "gzip")
	}
	if withAuth {
		req.Header.Set("Authorization", "Bearer "+c.credential)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TemporaryError{Err: err}
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponse))

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrRevoked
	case resp.StatusCode == http.StatusForbidden:
		return ErrTokenRejected
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case resp.StatusCode == http.StatusTooManyRequests:
		after := time.Minute
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			after = time.Duration(s) * time.Second
		}
		return &RetryAfterError{After: after}
	case resp.StatusCode >= 500:
		return &TemporaryError{Err: fmt.Errorf("el servidor respondió %d", resp.StatusCode)}
	case resp.StatusCode >= 400:
		return fmt.Errorf("el servidor rechazó la petición (%d): %s", resp.StatusCode, serverMessage(respBody))
	}
	if readErr != nil {
		return &TemporaryError{Err: readErr}
	}
	if out == nil {
		return nil
	}
	// Una respuesta 2xx que no es JSON suele venir de un portal cautivo o un
	// proxy intermedio, no del servidor: se reintenta más tarde.
	if err := json.Unmarshal(respBody, out); err != nil {
		return &TemporaryError{Err: fmt.Errorf("respuesta ilegible del servidor: %w", err)}
	}
	return nil
}

// serverMessage extrae el mensaje del formato de error común del servidor.
func serverMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return "sin detalle"
}
