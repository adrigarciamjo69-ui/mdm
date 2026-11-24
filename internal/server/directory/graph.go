// Package directory sincroniza las personas de la empresa desde Microsoft
// Entra ID (Microsoft Graph) con permisos de solo lectura.
package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mdm/internal/server/store"
)

const (
	defaultAuthBase  = "https://login.microsoftonline.com"
	defaultGraphBase = "https://graph.microsoft.com"
	maxPages         = 200
)

// Client habla con el inicio de sesión de Microsoft y con Graph.
type Client struct {
	AuthBase  string
	GraphBase string
	HTTP      *http.Client
}

func NewClient(authBase, graphBase string) *Client {
	if authBase == "" {
		authBase = defaultAuthBase
	}
	if graphBase == "" {
		graphBase = defaultGraphBase
	}
	return &Client{AuthBase: strings.TrimRight(authBase, "/"), GraphBase: strings.TrimRight(graphBase, "/"),
		HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Credentials identifican la aplicación registrada en Entra.
type Credentials struct {
	TenantID string
	ClientID string
	Secret   string
}

// apiError lee el mensaje de error de Microsoft si lo hay.
func apiError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var e struct {
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	msg := ""
	if json.Unmarshal(body, &e) == nil {
		if e.ErrorDescription != "" {
			msg = e.ErrorDescription
		} else {
			var ge struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(e.Error, &ge) == nil && ge.Message != "" {
				msg = ge.Code + ": " + ge.Message
			}
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
}

func (c *Client) token(ctx context.Context, cr Credentials) (string, error) {
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {cr.ClientID}, "client_secret": {cr.Secret},
		"scope": {"https://graph.microsoft.com/.default"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.AuthBase+"/"+url.PathEscape(cr.TenantID)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("no se puede conectar con Microsoft: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Microsoft ha rechazado las credenciales: %w", apiError(resp))
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil || t.AccessToken == "" {
		return "", errors.New("respuesta de inicio de sesión no válida")
	}
	return t.AccessToken, nil
}

type graphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	Department        string `json:"department"`
	JobTitle          string `json:"jobTitle"`
	EmployeeID        string `json:"employeeId"`
	MobilePhone       string `json:"mobilePhone"`
	OfficeLocation    string `json:"officeLocation"`
	AccountEnabled    *bool  `json:"accountEnabled"`
}

// Users devuelve todos los usuarios miembro (sin invitados) del inquilino.
// Si alguna página falla, devuelve el error y ningún usuario.
func (c *Client) Users(ctx context.Context, cr Credentials) ([]store.DirectoryUser, error) {
	tok, err := c.token(ctx, cr)
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"$select": {"id,displayName,mail,userPrincipalName,department,jobTitle,employeeId,mobilePhone,officeLocation,accountEnabled"},
		"$filter": {"userType eq 'Member'"},
		"$top":    {"999"},
	}
	next := c.GraphBase + "/v1.0/users?" + q.Encode()
	var out []store.DirectoryUser
	for page := 0; next != ""; page++ {
		if page >= maxPages {
			return nil, errors.New("demasiadas páginas de usuarios")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("ConsistencyLevel", "eventual")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("no se puede conectar con Microsoft Graph: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			err := apiError(resp)
			resp.Body.Close()
			return nil, fmt.Errorf("Microsoft Graph ha devuelto un error: %w", err)
		}
		var body struct {
			Value    []graphUser `json:"value"`
			NextLink string      `json:"@odata.nextLink"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("respuesta de Microsoft Graph no válida: %w", err)
		}
		for _, u := range body.Value {
			enabled := u.AccountEnabled == nil || *u.AccountEnabled
			out = append(out, store.DirectoryUser{EntraID: u.ID, DisplayName: u.DisplayName, Email: u.Mail,
				UPN: u.UserPrincipalName, Department: u.Department, JobTitle: u.JobTitle, EmployeeID: u.EmployeeID,
				Phone: u.MobilePhone, Office: u.OfficeLocation, Enabled: enabled})
		}
		next = body.NextLink
	}
	return out, nil
}
