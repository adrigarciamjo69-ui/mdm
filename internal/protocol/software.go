package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"
)

// Estados de un despliegue en un equipo.
const (
	AppPending       = "pending"
	AppWaitingWindow = "waiting_window"
	AppDownloading   = "downloading"
	AppInstalling    = "installing"
	AppInstalled     = "installed"
	AppAbsent        = "absent"
	AppFailed        = "failed"
	AppNotApplicable = "not_applicable"
)

// Detection decide si una aplicación subida está instalada.
type Detection struct {
	Type          string `json:"type"` // inventory, file, registry, script
	NameContains  string `json:"name_contains,omitempty"`
	MinVersion    string `json:"min_version,omitempty"`
	Path          string `json:"path,omitempty"`
	RegistryKey   string `json:"registry_key,omitempty"`
	RegistryValue string `json:"registry_value,omitempty"`
	Interpreter   string `json:"interpreter,omitempty"`
	Script        string `json:"script,omitempty"`
}

// AppAssignment es un despliegue tal como lo recibe el agente.
type AppAssignment struct {
	DeploymentID     string     `json:"deployment_id"`
	AppName          string     `json:"app_name"`
	Version          string     `json:"version"`
	Intent           string     `json:"intent"` // required, uninstall
	Source           string     `json:"source"` // winget, apt, dnf, upload
	PackageID        string     `json:"package_id"`
	FileSHA256       string     `json:"file_sha256"`
	InstallerType    string     `json:"installer_type"` // msi, exe, deb, rpm (solo instaladores subidos)
	Size             int64      `json:"size"`
	InstallCommand   string     `json:"install_command"`
	UninstallCommand string     `json:"uninstall_command"`
	SuccessCodes     []int      `json:"success_codes"`
	Detection        *Detection `json:"detection"`
	KeepUpdated      bool       `json:"keep_updated"`
	StartAt          *time.Time `json:"start_at"`
	RespectWindow    bool       `json:"respect_window"`
	Reboot           string     `json:"reboot"` // never, notify, in_window
	// RetryEpoch cambia cuando un operador pulsa «Reintentar»: el agente
	// olvida sus intentos fallidos.
	RetryEpoch int `json:"retry_epoch"`
}

// Window es una ventana de mantenimiento en hora local del equipo. Days usa
// 0 = domingo. Si End es anterior a Start, la ventana cruza la medianoche y
// pertenece al día en que empieza. Start igual a End es el día entero.
type Window struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type PatchPolicy struct {
	Window            Window   `json:"window"`
	Reboot            string   `json:"reboot"` // never, notify, in_window
	NotifyMinutes     int      `json:"notify_minutes"`
	LinuxScope        string   `json:"linux_scope"` // security, all
	ApprovedUpdateIDs []string `json:"approved_update_ids"`
}

type AppStatus struct {
	DeploymentID   string    `json:"deployment_id"`
	State          string    `json:"state"`
	Message        string    `json:"message"`
	Attempts       int       `json:"attempts"`
	ExitCode       *int      `json:"exit_code"`
	RebootRequired bool      `json:"reboot_required"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AppStatusReport struct {
	Items []AppStatus `json:"items"`
}

type UpdateInfo struct {
	ID             string     `json:"id"`
	KB             string     `json:"kb"`
	Title          string     `json:"title"`
	Classification string     `json:"classification"`
	Severity       string     `json:"severity"`
	SizeBytes      int64      `json:"size_bytes"`
	PublishedAt    *time.Time `json:"published_at"`
	Security       bool       `json:"security"`
}

type UpdateScan struct {
	ScannedAt     time.Time    `json:"scanned_at"`
	RebootPending bool         `json:"reboot_pending"`
	Updates       []UpdateInfo `json:"updates"`
}

type UpdateResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // installed, failed
	Error  string `json:"error"`
}

type UpdateResults struct {
	Results       []UpdateResult `json:"results"`
	RebootPending bool           `json:"reboot_pending"`
}

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func minutes(s string) int {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h*60 + m
}

// Validate comprueba el formato de la ventana.
func (w Window) Validate() error {
	if len(w.Days) == 0 {
		return errors.New("La ventana debe tener al menos un día.")
	}
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return errors.New("Los días de la ventana van de 0 (domingo) a 6 (sábado).")
		}
	}
	if !hhmm.MatchString(w.Start) || !hhmm.MatchString(w.End) {
		return errors.New("Las horas de la ventana deben tener el formato HH:MM.")
	}
	return nil
}

// Contains informa de si t (en su propia zona horaria) cae dentro de la ventana.
func (w Window) Contains(t time.Time) bool {
	if len(w.Days) == 0 {
		return false
	}
	now := t.Hour()*60 + t.Minute()
	start, end := minutes(w.Start), minutes(w.End)
	today := slices.Contains(w.Days, int(t.Weekday()))
	yesterday := slices.Contains(w.Days, int(t.AddDate(0, 0, -1).Weekday()))
	switch {
	case start == end:
		return today
	case start < end:
		return today && now >= start && now < end
	default: // cruza la medianoche
		return (today && now >= start) || (yesterday && now < end)
	}
}

// PolicyHashFull es la huella de la política efectiva completa. Sin apps ni
// parches coincide con PolicyHash, de modo que actualizar el servidor no
// cambia la política de los equipos que no tienen nada de la fase 3.
func PolicyHashFull(p EffectivePolicy) string {
	if len(p.Apps) == 0 && p.Patches == nil {
		return PolicyHash(p.Settings, p.EscrowedKeyIDs)
	}
	apps := append([]AppAssignment{}, p.Apps...)
	sort.Slice(apps, func(i, j int) bool { return apps[i].DeploymentID < apps[j].DeploymentID })
	var patches *PatchPolicy
	if p.Patches != nil {
		c := *p.Patches
		c.ApprovedUpdateIDs = append([]string{}, c.ApprovedUpdateIDs...)
		sort.Strings(c.ApprovedUpdateIDs)
		c.Window.Days = append([]int{}, c.Window.Days...)
		sort.Ints(c.Window.Days)
		patches = &c
	}
	b, _ := json.Marshal(struct {
		Base    string          `json:"base"`
		Apps    []AppAssignment `json:"apps"`
		Patches *PatchPolicy    `json:"patches"`
	}{PolicyHash(p.Settings, p.EscrowedKeyIDs), apps, patches})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
