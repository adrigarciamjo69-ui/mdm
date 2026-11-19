package adminapi

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"mdm/internal/protocol"
	"mdm/internal/server/audit"
	"mdm/internal/server/files"
	"mdm/internal/server/httpx"
	"mdm/internal/server/policy"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
)

var (
	wingetIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-_+]{0,127}$`)
	pkgNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.+\-_:]{0,127}$`)
	// appSources indica, por plataforma, los orígenes admitidos.
	appSources = map[string]map[string]bool{
		protocol.PlatformWindows: {"winget": true, "upload": true},
		protocol.PlatformLinux:   {"apt": true, "dnf": true, "upload": true},
		"android":                {"google_play": true},
	}
	installerTypes = map[string]map[string]bool{
		protocol.PlatformWindows: {"msi": true, "exe": true},
		protocol.PlatformLinux:   {"deb": true, "rpm": true},
	}
)

// Órdenes por defecto. El agente las ejecuta con el intérprete del sistema
// (sh -c o cmd /c) sustituyendo {file} por la ruta del instalador descargado.
var defaultCommands = map[string][2]string{
	"msi": {`msiexec /i "{file}" /qn /norestart`, `msiexec /x "{file}" /qn /norestart`},
	"deb": {`apt-get install -y "{file}"`, `apt-get remove -y "$(dpkg-deb -f "{file}" Package)"`},
	"rpm": {`dnf install -y "{file}"`, `dnf remove -y "$(rpm -qp --qf '%{NAME}' "{file}")"`},
}

var defaultSuccessCodes = []int{0, 3010, 1641}

type appVersionJSON struct {
	ID               string             `json:"id"`
	Version          string             `json:"version"`
	FileSHA256       string             `json:"file_sha256"`
	FileSize         int64              `json:"file_size"`
	FileName         string             `json:"file_name"`
	InstallerType    string             `json:"installer_type"`
	InstallCommand   string             `json:"install_command"`
	UninstallCommand string             `json:"uninstall_command"`
	SuccessCodes     []int              `json:"success_codes"`
	Detection        protocol.Detection `json:"detection"`
	IsCurrent        bool               `json:"is_current"`
	CreatedAt        time.Time          `json:"created_at"`
}

type appJSON struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Publisher      string           `json:"publisher"`
	Description    string           `json:"description"`
	Platform       string           `json:"platform"`
	Source         string           `json:"source"`
	PackageID      string           `json:"package_id"`
	Archived       bool             `json:"archived"`
	CurrentVersion *appVersionJSON  `json:"current_version"`
	Versions       []appVersionJSON `json:"versions"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

func toAppVersionJSON(v store.AppVersion) appVersionJSON {
	return appVersionJSON{ID: v.ID.String(), Version: v.Version, FileSHA256: v.FileSHA256, FileSize: v.FileSize, FileName: v.FileName,
		InstallerType: v.InstallerType, InstallCommand: v.InstallCommand, UninstallCommand: v.UninstallCommand,
		SuccessCodes: v.SuccessCodes, Detection: v.Detection, IsCurrent: v.IsCurrent, CreatedAt: v.CreatedAt}
}

func toAppJSON(a store.App) appJSON {
	out := appJSON{ID: a.ID.String(), Name: a.Name, Publisher: a.Publisher, Description: a.Description, Platform: a.Platform,
		Source: a.Source, PackageID: a.PackageID, Archived: a.ArchivedAt != nil, Versions: []appVersionJSON{}, UpdatedAt: a.UpdatedAt}
	for _, v := range a.Versions {
		vj := toAppVersionJSON(v)
		out.Versions = append(out.Versions, vj)
		if v.IsCurrent {
			c := vj
			out.CurrentVersion = &c
		}
	}
	return out
}

func validAppText(name string, max int) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && utf8.RuneCountInString(name) <= max
}

func (a *api) listApps(w http.ResponseWriter, r *http.Request) {
	apps, err := a.d.Store.ListApps(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	out := make([]appJSON, 0, len(apps))
	for _, x := range apps {
		out = append(out, toAppJSON(x))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *api) createApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name, Publisher, Description, Platform, Source string
		PackageID                                      string `json:"package_id"`
	}
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	name, ok := validAppText(in.Name, 100)
	if !ok {
		badRequest(w, "El nombre es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	if !appSources[in.Platform][in.Source] {
		badRequest(w, "Origen no válido para la plataforma: winget o instalador subido en Windows; apt, dnf o instalador subido en Linux; Google Play en Android.")
		return
	}
	pkg := strings.TrimSpace(in.PackageID)
	switch in.Source {
	case "winget":
		if !wingetIDRe.MatchString(pkg) {
			badRequest(w, "El identificador de winget no es válido (por ejemplo, Mozilla.Firefox).")
			return
		}
	case "google_play":
		if !policy.PackageNameValid(pkg) {
			badRequest(w, "El nombre del paquete de Android no es válido (por ejemplo, com.microsoft.teams).")
			return
		}
	case "apt", "dnf":
		if !pkgNameRe.MatchString(pkg) {
			badRequest(w, "El nombre del paquete no es válido (minúsculas, cifras y . + - _ :).")
			return
		}
	default:
		pkg = ""
	}
	admin := adminFrom(r.Context())
	app, err := a.d.Store.CreateApp(r.Context(), store.App{Name: name, Publisher: strings.TrimSpace(in.Publisher),
		Description: strings.TrimSpace(in.Description), Platform: in.Platform, Source: in.Source, PackageID: pkg}, admin.ID)
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una aplicación con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AppCreated, TargetType: "app", TargetID: app.ID.String(),
		Details: map[string]any{"name": app.Name, "source": app.Source}, IP: a.ip(r)})
	httpx.WriteJSON(w, http.StatusCreated, toAppJSON(app))
}

func (a *api) loadApp(w http.ResponseWriter, r *http.Request) (store.App, bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return store.App{}, false
	}
	app, err := a.d.Store.GetApp(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esta aplicación no existe.")
		return app, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return app, false
	}
	return app, true
}

func (a *api) getApp(w http.ResponseWriter, r *http.Request) {
	if app, ok := a.loadApp(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toAppJSON(app))
	}
}

func (a *api) updateApp(w http.ResponseWriter, r *http.Request) {
	app, ok := a.loadApp(w, r)
	if !ok {
		return
	}
	var in struct{ Name, Publisher, Description string }
	if !httpx.DecodeJSON(w, r, maxBody, &in) {
		return
	}
	name, ok := validAppText(in.Name, 100)
	if !ok {
		badRequest(w, "El nombre es obligatorio y no puede superar los 100 caracteres.")
		return
	}
	err := a.d.Store.UpdateApp(r.Context(), app.ID, name, strings.TrimSpace(in.Publisher), strings.TrimSpace(in.Description), a.d.Now().UTC())
	if errors.Is(err, store.ErrConflict) {
		conflict(w, "Ya existe una aplicación con ese nombre.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AppUpdated, TargetType: "app", TargetID: app.ID.String(),
		Details: map[string]any{"name": name}, IP: a.ip(r)})
	updated, _ := a.d.Store.GetApp(r.Context(), app.ID)
	httpx.WriteJSON(w, http.StatusOK, toAppJSON(updated))
}

func (a *api) archiveApp(w http.ResponseWriter, r *http.Request) {
	app, ok := a.loadApp(w, r)
	if !ok {
		return
	}
	if err := a.d.Store.ArchiveApp(r.Context(), app.ID, a.d.Now().UTC()); err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AppArchived, TargetType: "app", TargetID: app.ID.String(),
		Details: map[string]any{"name": app.Name}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}

// validateDetection comprueba la regla de detección de una versión.
func validateDetection(platform string, d protocol.Detection) error {
	switch d.Type {
	case "inventory":
		if strings.TrimSpace(d.NameContains) == "" {
			return errors.New("La detección por inventario necesita el texto del nombre del programa.")
		}
	case "file":
		if platform != protocol.PlatformWindows || strings.TrimSpace(d.Path) == "" {
			return errors.New("La detección por fichero solo existe en Windows y necesita la ruta.")
		}
	case "registry":
		if platform != protocol.PlatformWindows || strings.TrimSpace(d.RegistryKey) == "" {
			return errors.New("La detección por registro solo existe en Windows y necesita la clave.")
		}
	case "script":
		if !interpreters[platform][d.Interpreter] || strings.TrimSpace(d.Script) == "" {
			return errors.New("La detección por script necesita un intérprete de la plataforma y el contenido.")
		}
		if len(d.Script) > maxScriptBytes {
			return errors.New("El script de detección no puede superar los 256 KB.")
		}
	default:
		return errors.New("Tipo de detección no válido: inventory, file, registry o script.")
	}
	if d.MinVersion != "" && !settings.ValidVersion(d.MinVersion) {
		return errors.New("La versión mínima de la detección no tiene un formato válido.")
	}
	return nil
}

func (a *api) addAppVersion(w http.ResponseWriter, r *http.Request) {
	app, ok := a.loadApp(w, r)
	if !ok {
		return
	}
	if app.Source != "upload" {
		badRequest(w, "Solo las aplicaciones con instalador propio tienen versiones: las de winget, apt y dnf las da el repositorio.")
		return
	}
	var in struct {
		Version          string              `json:"version"`
		FileSHA256       string              `json:"file_sha256"`
		InstallerType    string              `json:"installer_type"`
		InstallCommand   string              `json:"install_command"`
		UninstallCommand string              `json:"uninstall_command"`
		SuccessCodes     []int               `json:"success_codes"`
		Detection        *protocol.Detection `json:"detection"`
		MakeCurrent      *bool               `json:"make_current"`
	}
	if !httpx.DecodeJSON(w, r, maxScriptBytes+maxBody, &in) {
		return
	}
	version, ok := validAppText(in.Version, 50)
	if !ok {
		badRequest(w, "La versión es obligatoria (como mucho 50 caracteres).")
		return
	}
	if !installerTypes[app.Platform][in.InstallerType] {
		badRequest(w, "Tipo de instalador no válido para la plataforma: msi o exe en Windows; deb o rpm en Linux.")
		return
	}
	if !files.ValidSHA(in.FileSHA256) {
		badRequest(w, "Indica el fichero subido (su SHA-256).")
		return
	}
	if _, err := a.d.Store.GetFile(r.Context(), in.FileSHA256); err != nil {
		badRequest(w, "El fichero indicado no se ha subido al servidor.")
		return
	}
	install, uninstall := strings.TrimSpace(in.InstallCommand), strings.TrimSpace(in.UninstallCommand)
	if def, ok := defaultCommands[in.InstallerType]; ok {
		if install == "" {
			install = def[0]
		}
		if uninstall == "" {
			uninstall = def[1]
		}
	}
	if install == "" || uninstall == "" {
		badRequest(w, "Un instalador EXE necesita las órdenes de instalación y desinstalación silenciosas.")
		return
	}
	if len(install) > 2000 || len(uninstall) > 2000 {
		badRequest(w, "Las órdenes no pueden superar los 2.000 caracteres.")
		return
	}
	codes := in.SuccessCodes
	if len(codes) == 0 {
		codes = defaultSuccessCodes
	}
	if len(codes) > 20 {
		badRequest(w, "Como mucho 20 códigos de éxito.")
		return
	}
	det := protocol.Detection{Type: "inventory", NameContains: app.Name, MinVersion: version}
	if in.Detection != nil {
		det = *in.Detection
	}
	if !settings.ValidVersion(det.MinVersion) {
		det.MinVersion = "" // una versión comercial («2026 R2») no sirve para comparar
	}
	if err := validateDetection(app.Platform, det); err != nil {
		badRequest(w, err.Error())
		return
	}
	current := in.MakeCurrent == nil || *in.MakeCurrent
	v, err := a.d.Store.AddAppVersion(r.Context(), store.AppVersion{AppID: app.ID, Version: version, FileSHA256: in.FileSHA256,
		InstallerType: in.InstallerType, InstallCommand: install, UninstallCommand: uninstall, SuccessCodes: codes, Detection: det}, current)
	switch {
	case errors.Is(err, store.ErrConflict):
		conflict(w, fmt.Sprintf("La versión %s ya existe.", version))
		return
	case errors.Is(err, store.ErrBadReference):
		badRequest(w, "El fichero indicado no se ha subido al servidor.")
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AppUpdated, TargetType: "app", TargetID: app.ID.String(),
		Details: map[string]any{"name": app.Name, "version": version}, IP: a.ip(r)})
	full, _ := a.d.Store.GetApp(r.Context(), app.ID)
	for _, x := range full.Versions {
		if x.ID == v.ID {
			httpx.WriteJSON(w, http.StatusCreated, toAppVersionJSON(x))
			return
		}
	}
	httpx.WriteJSON(w, http.StatusCreated, toAppVersionJSON(v))
}

func (a *api) listAppVersions(w http.ResponseWriter, r *http.Request) {
	if app, ok := a.loadApp(w, r); ok {
		httpx.WriteJSON(w, http.StatusOK, toAppJSON(app).Versions)
	}
}

func (a *api) setCurrentAppVersion(w http.ResponseWriter, r *http.Request) {
	app, ok := a.loadApp(w, r)
	if !ok {
		return
	}
	vid, ok := pathID(w, r, "vid")
	if !ok {
		return
	}
	err := a.d.Store.SetCurrentVersion(r.Context(), app.ID, vid)
	if errors.Is(err, store.ErrNotFound) {
		notFound(w, "Esa versión no existe.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	admin := adminFrom(r.Context())
	a.d.Audit.Log(r.Context(), audit.Entry{Actor: &admin, Action: audit.AppUpdated, TargetType: "app", TargetID: app.ID.String(),
		Details: map[string]any{"name": app.Name, "current_version": vid.String()}, IP: a.ip(r)})
	w.WriteHeader(http.StatusNoContent)
}
