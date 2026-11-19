package adminapi

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/httpx"
	"mdm/internal/server/store"
)

const (
	maxScriptBytes     = 256 * 1024
	maxScriptParams    = 20
	maxParamValueBytes = 4096
	defaultScriptTO    = 300
)

var (
	interpreters = map[string]map[string]bool{
		protocol.PlatformWindows: {"powershell": true},
		protocol.PlatformLinux:   {"sh": true, "bash": true, "python3": true},
	}
	paramNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,31}$`)
)

type scriptJSON struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Platform      string              `json:"platform"`
	Interpreter   string              `json:"interpreter"`
	TimeoutS      int                 `json:"timeout_s"`
	Parameters    []store.ScriptParam `json:"parameters"`
	LatestVersion int                 `json:"latest_version"`
	Archived      bool                `json:"archived"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

func toScriptJSON(s store.Script) scriptJSON {
	return scriptJSON{ID: s.ID.String(), Name: s.Name, Description: s.Description, Platform: s.Platform, Interpreter: s.Interpreter,
		TimeoutS: s.TimeoutS, Parameters: s.Parameters, LatestVersion: s.LatestVersion, Archived: s.ArchivedAt != nil,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

type versionJSON struct {
	Version        int       `json:"version"`
	Content        string    `json:"content,omitempty"`
	CreatedByEmail string    `json:"created_by_email"`
	CreatedAt      time.Time `json:"created_at"`
}

func validateContent(c string) error {
	if strings.TrimSpace(c) == "" {
		return errors.New("El script no puede estar vacío.")
	}
	if len(c) > maxScriptBytes {
		return errors.New("El script no puede superar los 256 KB.")
	}
	if !utf8.ValidString(c) || strings.IndexByte(c, 0) >= 0 {
		return errors.New("El script contiene caracteres no válidos.")
	}
	return nil
}

func validateParams(ps []store.ScriptParam) ([]store.ScriptParam, error) {
	if len(ps) > maxScriptParams {
		return nil, errors.New("Un script admite como mucho 20 parámetros.")
	}
	seen := map[string]bool{}
	out := make([]store.ScriptParam, 0, len(ps))
	for _, p := range ps {
		if !paramNameRe.MatchString(p.Name) {
			return nil, fmt.Errorf("El parámetro «%s» debe ir en mayúsculas, empezar por una letra y usar solo letras, cifras y _.", p.Name)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("El parámetro «%s» está repetido.", p.Name)
		}
		seen[p.Name] = true
		p.Description = store.CleanString(strings.TrimSpace(p.Description))
		p.Default = store.CleanText(p.Default, maxParamValueBytes)
		out = append(out, p)
	}
	return out, nil
}

func validTimeout(t *int) (int, error) {
	if t == nil {
		return defaultScriptTO, nil
	}
	if *t < 1 || *t > 3600 {
		return 0, errors.New("El tiempo máximo debe estar entre 1 y 3.600 segundos.")
	}
	return *t, nil
}

func (a *api) listScripts(w http.ResponseWriter, r *http.Request) {
	scripts, err := a.d.Store.ListScripts(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]scriptJSON, 0, len(scripts))
	for _, s := range scripts {
		out = append(out, toScriptJSON(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createScript(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string              `json:"name"`
		Description string              `json:"description"`
		Platform    string              `json:"platform"`
		Interpreter string              `json:"interpreter"`
		TimeoutS    *int                `json:"timeout_s"`
		Parameters  []store.ScriptParam `json:"parameters"`
		Content     string              `json:"content"`
	}
	if !httpx.DecodeJSON(w, r, maxScriptBytes+maxBody, &in) {
		return
	}
	name, ok := validPolicyName(in.Name)
	if !ok {
		badRequest(w, "El nombre del script es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	if !interpreters[in.Platform][in.Interpreter] {
		badRequest(w, "El intérprete no es válido para la plataforma: powershell en Windows; sh, bash o python3 en Linux.")
		return
	}
	timeout, err := validTimeout(in.TimeoutS)
	if err == nil {
		err = validateContent(in.Content)
	}
	params, perr := validateParams(in.Parameters)
	if err == nil {
		err = perr
	}
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	admin := adminFrom(r.Context())
	s, err := a.d.Store.CreateScript(r.Context(), store.NewScript{Name: name, Description: strings.TrimSpace(in.Description), Platform: in.Platform,
		Interpreter: in.Interpreter, Content: in.Content, TimeoutS: timeout, Parameters: params, CreatedBy: admin.ID, CreatedByEmail: admin.Email, At: a.d.Now().UTC()})
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe un script con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ScriptCreated, TargetType: "script", TargetID: s.ID.String(),
		Details: map[string]any{"name": s.Name}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toScriptJSON(s))
}

func (a *api) loadScript(w http.ResponseWriter, r *http.Request) (store.Script, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.Script{}, false
	}
	s, err := a.d.Store.GetScript(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Este script no existe.")
		return s, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return s, false
	}
	return s, true
}

func (a *api) getScript(w http.ResponseWriter, r *http.Request) {
	if s, ok := a.loadScript(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toScriptJSON(s))
	}
}

func (a *api) updateScript(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadScript(w, r)
	if !ok {
		return
	}
	var in struct {
		Name        string              `json:"name"`
		Description string              `json:"description"`
		TimeoutS    *int                `json:"timeout_s"`
		Parameters  []store.ScriptParam `json:"parameters"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	name, ok := validPolicyName(in.Name)
	if !ok {
		badRequest(w, "El nombre del script es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	timeout := s.TimeoutS
	if in.TimeoutS != nil {
		t, err := validTimeout(in.TimeoutS)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		timeout = t
	}
	params, err := validateParams(in.Parameters)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	err = a.d.Store.UpdateScript(r.Context(), s.ID, name, strings.TrimSpace(in.Description), timeout, params, a.d.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe un script con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ScriptUpdated, TargetType: "script", TargetID: s.ID.String(),
		Details: map[string]any{"name": name}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetScript(r.Context(), s.ID)
	httpx.WriteJSON(w, http.StatusOK, toScriptJSON(updated))
}

func (a *api) addScriptVersion(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadScript(w, r)
	if !ok {
		return
	}
	var in struct {
		Content string `json:"content"`
	}
	if !httpx.DecodeJSON(w, r, maxScriptBytes+maxBody, &in) {
		return
	}
	if err := validateContent(in.Content); err != nil {
		badRequest(w, err.Error())
		return
	}
	admin := adminFrom(r.Context())
	v, err := a.d.Store.AddScriptVersion(r.Context(), s.ID, in.Content, admin.ID, admin.Email, a.d.Now().UTC())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ScriptUpdated, TargetType: "script", TargetID: s.ID.String(),
		Details: map[string]any{"name": s.Name, "version": v}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, versionJSON{Version: v, CreatedByEmail: admin.Email, CreatedAt: a.d.Now().UTC()})
}

func (a *api) listScriptVersions(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadScript(w, r)
	if !ok {
		return
	}
	vs, err := a.d.Store.ListScriptVersions(r.Context(), s.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]versionJSON, 0, len(vs))
	for _, v := range vs {
		out = append(out, versionJSON{Version: v.Version, CreatedByEmail: v.CreatedByEmail, CreatedAt: v.CreatedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) getScriptVersion(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadScript(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || n < 1 {
		badRequest(w, "El número de versión no es válido.")
		return
	}
	v, err := a.d.Store.GetScriptVersion(r.Context(), s.ID, n)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esa versión no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, versionJSON{Version: v.Version, Content: v.Content, CreatedByEmail: v.CreatedByEmail, CreatedAt: v.CreatedAt})
}

func (a *api) archiveScript(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadScript(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.ArchiveScript(r.Context(), s.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.ScriptArchived, TargetType: "script", TargetID: s.ID.String(),
		Details: map[string]any{"name": s.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

// scriptPayload construye la carga de run_script y separa los equipos de
// otra plataforma.
