// Package android habla con la Android Management API de Google: vincula la
// empresa, crea tokens de inscripción, publica políticas, lee los
// dispositivos y les envía órdenes. La autenticación es la de una cuenta de
// servicio (JWT firmado con RS256 intercambiado por un token de acceso).
package android

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultAPIBase = "https://androidmanagement.googleapis.com/v1"
	scope          = "https://www.googleapis.com/auth/androidmanagement"
)

type ServiceAccount struct {
	ProjectID   string
	ClientEmail string
	TokenURI    string
	Key         *rsa.PrivateKey
}

// ParseServiceAccount lee la clave JSON de una cuenta de servicio de Google.
func ParseServiceAccount(b []byte) (ServiceAccount, error) {
	var in struct {
		Type        string `json:"type"`
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return ServiceAccount{}, errors.New("El fichero no es una clave JSON de Google.")
	}
	if in.Type != "service_account" {
		return ServiceAccount{}, errors.New("La clave no es de una cuenta de servicio (type debe ser service_account).")
	}
	if in.ProjectID == "" || in.ClientEmail == "" || in.PrivateKey == "" {
		return ServiceAccount{}, errors.New("A la clave le falta project_id, client_email o private_key.")
	}
	block, _ := pem.Decode([]byte(in.PrivateKey))
	if block == nil {
		return ServiceAccount{}, errors.New("La clave privada no tiene un formato válido.")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return ServiceAccount{}, errors.New("La clave privada no tiene un formato válido.")
	}
	rsaKey, ok := k.(*rsa.PrivateKey)
	if !ok {
		return ServiceAccount{}, errors.New("La clave privada no es RSA.")
	}
	if in.TokenURI == "" {
		in.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return ServiceAccount{ProjectID: in.ProjectID, ClientEmail: in.ClientEmail, TokenURI: in.TokenURI, Key: rsaKey}, nil
}

// APIError es un error devuelto por Google.
type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Google respondió %d: %s", e.Status, e.Message)
}

// Retryable informa de si conviene reintentar (cuota o error temporal).
func (e *APIError) Retryable() bool { return e.Status == 429 || e.Status >= 500 }

type Client struct {
	sa   ServiceAccount
	base string
	http *http.Client
	now  func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewClient(sa ServiceAccount, base string, hc *http.Client) *Client {
	if base == "" {
		base = DefaultAPIBase
	}
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{sa: sa, base: strings.TrimRight(base, "/"), http: hc, now: time.Now}
}

// CheckToken comprueba que Google acepta la clave pidiendo un token.
func (c *Client) CheckToken(ctx context.Context) error {
	_, err := c.accessToken(ctx)
	return err
}

// ProjectID devuelve el proyecto de Google de la cuenta de servicio.
func (c *Client) ProjectID() string { return c.sa.ProjectID }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// accessToken devuelve un token válido, pidiéndolo si hace falta.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.token != "" && now.Before(c.expires.Add(-time.Minute)) {
		return c.token, nil
	}
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iss": c.sa.ClientEmail, "scope": scope, "aud": c.sa.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	signing := header + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.sa.Key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {signing + "." + b64(sig)}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("no se pudo contactar con Google: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("credenciales de Google no válidas (respuesta %d al pedir el token): revisa la clave de la cuenta de servicio", resp.StatusCode)
	}
	c.token, c.expires = out.AccessToken, now.Add(time.Duration(out.ExpiresIn)*time.Second)
	return c.token, nil
}

// do hace una petición a la API y decodifica la respuesta en out (si no es nil).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	u := c.base + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &APIError{Status: 503, Message: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		apiErr := &APIError{Status: resp.StatusCode, Message: msg}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(s) * time.Second
		}
		if resp.StatusCode == 401 {
			c.mu.Lock()
			c.token = "" // que la siguiente pida otro
			c.mu.Unlock()
		}
		return apiErr
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type SignupURL struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// CreateSignupURL inicia el registro de la empresa en Google.
func (c *Client) CreateSignupURL(ctx context.Context, callbackURL string) (SignupURL, error) {
	var out SignupURL
	err := c.do(ctx, http.MethodPost, "signupUrls", url.Values{"projectId": {c.sa.ProjectID}, "callbackUrl": {callbackURL}}, nil, &out)
	return out, err
}

type Enterprise struct {
	Name        string `json:"name"`
	DisplayName string `json:"enterpriseDisplayName"`
}

// CreateEnterprise completa el registro con el token que devuelve Google.
func (c *Client) CreateEnterprise(ctx context.Context, signupURLName, enterpriseToken string) (Enterprise, error) {
	var out Enterprise
	err := c.do(ctx, http.MethodPost, "enterprises", url.Values{"projectId": {c.sa.ProjectID}, "signupUrlName": {signupURLName},
		"enterpriseToken": {enterpriseToken}}, map[string]any{}, &out)
	return out, err
}

func (c *Client) DeleteEnterprise(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, name, nil, nil, nil)
}

type EnrollmentTokenRequest struct {
	PolicyName     string
	Duration       time.Duration
	OneTimeOnly    bool
	AdditionalData string
}

type EnrollmentToken struct {
	Name      string
	Value     string
	QRCode    string
	ExpiresAt time.Time
}

func (c *Client) CreateEnrollmentToken(ctx context.Context, enterprise string, r EnrollmentTokenRequest) (EnrollmentToken, error) {
	var out struct {
		Name                string    `json:"name"`
		Value               string    `json:"value"`
		QRCode              string    `json:"qrCode"`
		ExpirationTimestamp time.Time `json:"expirationTimestamp"`
	}
	body := map[string]any{"policyName": r.PolicyName, "duration": fmt.Sprintf("%ds", int(r.Duration.Seconds())),
		"oneTimeOnly": r.OneTimeOnly, "additionalData": r.AdditionalData}
	err := c.do(ctx, http.MethodPost, enterprise+"/enrollmentTokens", nil, body, &out)
	return EnrollmentToken{Name: out.Name, Value: out.Value, QRCode: out.QRCode, ExpiresAt: out.ExpirationTimestamp}, err
}

func (c *Client) DeleteEnrollmentToken(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, name, nil, nil, nil)
}

// ListDevices devuelve todos los dispositivos de la empresa (todas las páginas).
func (c *Client) ListDevices(ctx context.Context, enterprise string) ([]Device, error) {
	var all []Device
	token := ""
	for {
		q := url.Values{"pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page struct {
			Devices       []Device `json:"devices"`
			NextPageToken string   `json:"nextPageToken"`
		}
		if err := c.do(ctx, http.MethodGet, enterprise+"/devices", q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Devices...)
		if page.NextPageToken == "" {
			return all, nil
		}
		token = page.NextPageToken
	}
}

// PatchPolicy crea o sustituye una política.
func (c *Client) PatchPolicy(ctx context.Context, name string, policy map[string]any) error {
	return c.do(ctx, http.MethodPatch, name, nil, policy, nil)
}

func (c *Client) DeletePolicy(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, name, nil, nil, nil)
}

// SetDevicePolicy cambia la política que aplica un dispositivo.
func (c *Client) SetDevicePolicy(ctx context.Context, device, policyName string) error {
	return c.do(ctx, http.MethodPatch, device, url.Values{"updateMask": {"policyName"}}, map[string]any{"policyName": policyName}, nil)
}

type Command struct {
	Type                string    `json:"type"`
	NewPassword         string    `json:"newPassword,omitempty"`
	ResetPasswordFlags  []string  `json:"resetPasswordFlags,omitempty"`
	StartLostModeParams *LostMode `json:"startLostModeParams,omitempty"`
}

type LostMode struct {
	LostMessage     *UserFacing `json:"lostMessage,omitempty"`
	LostPhoneNumber *UserFacing `json:"lostPhoneNumber,omitempty"`
}

type UserFacing struct {
	DefaultMessage string `json:"defaultMessage"`
}

type Operation struct {
	Name  string
	Done  bool
	Error string
}

func decodeOperation(raw rawOperation) Operation {
	op := Operation{Name: raw.Name, Done: raw.Done}
	if raw.Error != nil {
		op.Error = raw.Error.Message
		if op.Error == "" {
			op.Error = "La orden falló en el dispositivo."
		}
	}
	return op
}

type rawOperation = struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// IssueCommand envía una orden al dispositivo y devuelve la operación.
func (c *Client) IssueCommand(ctx context.Context, device string, cmd Command) (Operation, error) {
	var raw rawOperation
	err := c.do(ctx, http.MethodPost, device+":issueCommand", nil, cmd, &raw)
	return decodeOperation(raw), err
}

func (c *Client) GetOperation(ctx context.Context, name string) (Operation, error) {
	var raw rawOperation
	err := c.do(ctx, http.MethodGet, name, nil, nil, &raw)
	return decodeOperation(raw), err
}

// DeleteDevice borra el dispositivo de la empresa; en un móvil de empresa lo
// restablece de fábrica.
func (c *Client) DeleteDevice(ctx context.Context, device string, wipeFlags []string) error {
	var q url.Values
	if len(wipeFlags) > 0 {
		q = url.Values{"wipeDataFlags": wipeFlags}
	}
	return c.do(ctx, http.MethodDelete, device, q, nil, nil)
}
