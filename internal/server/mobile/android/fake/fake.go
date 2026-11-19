// Package fake simula la Android Management API de Google (y su servidor de
// tokens) en memoria, para probar la integración sin credenciales reales.
// Solo implementa las rutas que usa el MDM, con su formato JSON.
package fake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// AccessToken es el token que entrega el servidor de tokens simulado.
const AccessToken = "token-de-prueba"

// GoodEnterpriseToken es el enterpriseToken que acepta enterprises.create.
const GoodEnterpriseToken = "enterprise-token-ok"

type Server struct {
	*httptest.Server

	mu          sync.Mutex
	Enterprises map[string]map[string]any
	Tokens      map[string]map[string]any
	Policies    map[string]json.RawMessage
	Devices     map[string]map[string]any
	Operations  map[string]map[string]any
	Deleted     []string // dispositivos borrados (con sus wipeDataFlags)
	TokenCalls  int
	Requests    []string
	lastBody    string
	PageSize    int
	failures    []failure
	n           int
}

type failure struct {
	prefix string
	status int
	left   int
}

func New() *Server {
	s := &Server{Enterprises: map[string]map[string]any{}, Tokens: map[string]map[string]any{}, Policies: map[string]json.RawMessage{},
		Devices: map[string]map[string]any{}, Operations: map[string]map[string]any{}, PageSize: 100}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// TokenURL es la dirección del servidor de tokens simulado.
func (s *Server) TokenURL() string { return s.URL + "/token" }

// APIBase es la dirección base de la API simulada.
func (s *Server) APIBase() string { return s.URL + "/v1" }

// FailNext hace que las próximas count peticiones cuya «MÉTODO ruta» empiece
// por prefix respondan con status.
func (s *Server) FailNext(prefix string, status, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, failure{prefix, status, count})
}

// AddDevice añade un dispositivo a la empresa (como si se hubiera inscrito).
func (s *Server) AddDevice(enterprise, id string, d map[string]any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := enterprise + "/devices/" + id
	d["name"] = name
	s.Devices[name] = d
	return name
}

// SetDevice cambia campos de un dispositivo.
func (s *Server) SetDevice(name string, fields map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range fields {
		s.Devices[name][k] = v
	}
}

// CompleteOperation termina una operación, con error si msg no está vacío.
func (s *Server) CompleteOperation(name, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op := s.Operations[name]
	op["done"] = true
	if msg != "" {
		op["error"] = map[string]any{"code": 9, "message": msg}
	}
}

// Policy devuelve la política publicada con ese nombre.
func (s *Server) Policy(name string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var m map[string]any
	_ = json.Unmarshal(s.Policies[name], &m)
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": status, "message": msg, "status": http.StatusText(status)}})
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	s.Requests = append(s.Requests, key)
	for i := range s.failures {
		f := &s.failures[i]
		if f.left > 0 && strings.HasPrefix(key, f.prefix) {
			f.left--
			if f.status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "1")
			}
			apiError(w, f.status, "fallo simulado")
			return
		}
	}
	if r.URL.Path == "/token" {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
			writeJSON(w, 400, map[string]any{"error": "invalid_grant"})
			return
		}
		s.TokenCalls++
		writeJSON(w, 200, map[string]any{"access_token": AccessToken, "expires_in": 3600, "token_type": "Bearer"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+AccessToken {
		apiError(w, 401, "Request had invalid authentication credentials.")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	q := r.URL.Query()
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		s.lastBody = string(body)
	}
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	s.n++

	switch {
	case r.Method == "POST" && path == "signupUrls":
		writeJSON(w, 200, map[string]any{"name": fmt.Sprintf("signupUrls/C%d", s.n),
			"url": "https://play.google.com/work/adminsignup?token=X&callback=" + url.QueryEscape(q.Get("callbackUrl"))})
	case r.Method == "POST" && path == "enterprises":
		if q.Get("enterpriseToken") != GoodEnterpriseToken || q.Get("signupUrlName") == "" {
			apiError(w, 400, "Invalid enterprise token.")
			return
		}
		name := fmt.Sprintf("enterprises/LC%04d", s.n)
		e := map[string]any{"name": name, "enterpriseDisplayName": "Empresa de prueba"}
		s.Enterprises[name] = e
		writeJSON(w, 200, e)
	case r.Method == "DELETE" && strings.Count(path, "/") == 1 && strings.HasPrefix(path, "enterprises/"):
		delete(s.Enterprises, path)
		writeJSON(w, 200, map[string]any{})
	case r.Method == "POST" && strings.HasSuffix(path, "/enrollmentTokens"):
		ent := strings.TrimSuffix(path, "/enrollmentTokens")
		name := fmt.Sprintf("%s/enrollmentTokens/ET%d", ent, s.n)
		qr, _ := json.Marshal(map[string]any{"android.app.extra.PROVISIONING_DEVICE_ADMIN_COMPONENT_NAME": "com.google.android.apps.work.clouddpc/.receivers.CloudDeviceAdminReceiver",
			"android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE": map[string]any{"com.google.android.apps.work.clouddpc.EXTRA_ENROLLMENT_TOKEN": "VAL" + fmt.Sprint(s.n)}})
		tok := map[string]any{"name": name, "value": "VAL" + fmt.Sprint(s.n), "qrCode": string(qr), "policyName": in["policyName"],
			"duration": in["duration"], "oneTimeOnly": in["oneTimeOnly"], "additionalData": in["additionalData"],
			"expirationTimestamp": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
		s.Tokens[name] = tok
		writeJSON(w, 200, tok)
	case r.Method == "DELETE" && strings.Contains(path, "/enrollmentTokens/"):
		delete(s.Tokens, path)
		writeJSON(w, 200, map[string]any{})
	case r.Method == "GET" && strings.HasSuffix(path, "/devices"):
		ent := strings.TrimSuffix(path, "/devices")
		var names []string
		for n := range s.Devices {
			if strings.HasPrefix(n, ent+"/") {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		start := 0
		if t := q.Get("pageToken"); t != "" {
			fmt.Sscanf(t, "%d", &start)
		}
		end := min(start+s.PageSize, len(names))
		out := map[string]any{"devices": []any{}}
		devs := []any{}
		for _, n := range names[start:end] {
			devs = append(devs, s.Devices[n])
		}
		out["devices"] = devs
		if end < len(names) {
			out["nextPageToken"] = fmt.Sprint(end)
		}
		writeJSON(w, 200, out)
	case r.Method == "PATCH" && strings.Contains(path, "/policies/"):
		s.Policies[path] = body
		var p map[string]any
		_ = json.Unmarshal(body, &p)
		p["name"] = path
		writeJSON(w, 200, p)
	case r.Method == "DELETE" && strings.Contains(path, "/policies/"):
		delete(s.Policies, path)
		writeJSON(w, 200, map[string]any{})
	case r.Method == "PATCH" && strings.Contains(path, "/devices/"):
		d, ok := s.Devices[path]
		if !ok {
			apiError(w, 404, "Device not found.")
			return
		}
		if q.Get("updateMask") == "policyName" {
			d["policyName"] = in["policyName"]
		}
		writeJSON(w, 200, d)
	case r.Method == "POST" && strings.HasSuffix(path, ":issueCommand"):
		dev := strings.TrimSuffix(path, ":issueCommand")
		if _, ok := s.Devices[dev]; !ok {
			apiError(w, 404, "Device not found.")
			return
		}
		name := fmt.Sprintf("%s/operations/%d", dev, s.n)
		op := map[string]any{"name": name, "done": false, "metadata": map[string]any{"type": in["type"]}}
		s.Operations[name] = op
		writeJSON(w, 200, op)
	case r.Method == "GET" && strings.Contains(path, "/operations/"):
		op, ok := s.Operations[path]
		if !ok {
			apiError(w, 404, "Operation not found.")
			return
		}
		writeJSON(w, 200, op)
	case r.Method == "DELETE" && strings.Contains(path, "/devices/"):
		if _, ok := s.Devices[path]; !ok {
			apiError(w, 404, "Device not found.")
			return
		}
		delete(s.Devices, path)
		s.Deleted = append(s.Deleted, path+"?"+q.Get("wipeDataFlags"))
		writeJSON(w, 200, map[string]any{})
	default:
		apiError(w, 404, "ruta no simulada: "+key)
	}
}

// LastBody devuelve el último cuerpo no vacío enviado a la API.
func (s *Server) LastBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBody
}
