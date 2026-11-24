// Package fake simula el inicio de sesión de Microsoft y Microsoft Graph
// (solo lo que usa la sincronización de usuarios) para las pruebas.
package fake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
)

// User es un usuario tal como lo devuelve Graph.
type User struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	Department        string `json:"department"`
	JobTitle          string `json:"jobTitle"`
	EmployeeID        string `json:"employeeId"`
	MobilePhone       string `json:"mobilePhone"`
	OfficeLocation    string `json:"officeLocation"`
	AccountEnabled    bool   `json:"accountEnabled"`
}

type Server struct {
	*httptest.Server
	mu       sync.Mutex
	Users    []User
	PageSize int
	// FailPage hace que esa página (empezando en 1) devuelva un 500.
	FailPage int
	// Secret es el secreto de cliente que se acepta.
	Secret string
	// Queries guarda la consulta de la primera página de usuarios.
	Queries []string
}

func New() *Server {
	s := &Server{PageSize: 2, Secret: "secreto"}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/{tenant}/oauth2/v2.0/token", s.token)
	mux.HandleFunc("GET /graph/v1.0/users", s.users)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) AuthBase() string  { return s.URL + "/auth" }
func (s *Server) GraphBase() string { return s.URL + "/graph" }

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("client_secret") != s.Secret ||
		r.PostForm.Get("scope") != "https://graph.microsoft.com/.default" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"AADSTS7000215: Invalid client secret provided."}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-" + r.PathValue("tenant"), "expires_in": 3600})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page == 0 {
		page = 1
		s.Queries = append(s.Queries, r.URL.RawQuery)
	}
	if page == s.FailPage {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"serviceNotAvailable","message":"Fallo simulado"}}`))
		return
	}
	from := (page - 1) * s.PageSize
	to := min(from+s.PageSize, len(s.Users))
	body := map[string]any{"value": s.Users[min(from, len(s.Users)):to]}
	if to < len(s.Users) {
		body["@odata.nextLink"] = fmt.Sprintf("%s/graph/v1.0/users?page=%d", s.URL, page+1)
	}
	_ = json.NewEncoder(w).Encode(body)
}
