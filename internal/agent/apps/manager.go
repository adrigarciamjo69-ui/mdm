// Package apps instala, mantiene y desinstala en el agente el software que
// piden los despliegues: una instalación a la vez, con un retraso aleatorio
// estable antes de la primera descarga, ventanas de mantenimiento,
// reintentos y reinicios controlados.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

const (
	maxJitter      = 30 * time.Minute
	installTimeout = 60 * time.Minute
	maxAttempts    = 4
	upgradeEvery   = 24 * time.Hour
	resendEvery    = time.Hour
	stepEvery      = 5 * time.Minute
	notifyDelay    = 10 * time.Minute
	windowDelay    = 5 * time.Minute
	maxMessage     = 4096
)

// backoff es la espera tras el intento n (1, 2, 3).
var backoff = []time.Duration{15 * time.Minute, time.Hour, 4 * time.Hour}

type API interface {
	SendAppStatus(ctx context.Context, r protocol.AppStatusReport) error
}

type Deps struct {
	Platform  string
	DeviceID  string
	API       API
	Fetch     func(ctx context.Context, sha string, size int64, dst string) error
	Exec      func(ctx context.Context, s exec.Spec) exec.Result
	Run       func(ctx context.Context, name string, args ...string) ([]byte, error)
	Has       func(name string) bool
	Winget    func() (string, error)
	Inventory func(ctx context.Context) []protocol.Software
	// Window devuelve la ventana de mantenimiento vigente (nil si no hay política de parches).
	Window    func() *protocol.Window
	Power     func(action string, delay time.Duration, message string) error
	Now       func() time.Time
	StatePath string
	CacheDir  string
	Log       *slog.Logger
	// NoJitter desactiva el retraso aleatorio (solo para pruebas: MDM_AGENT_NO_JITTER=1).
	NoJitter bool
}

type entry struct {
	Epoch         int                `json:"epoch"`
	Attempts      int                `json:"attempts"`
	NextAttempt   time.Time          `json:"next_attempt"`
	FirstSeen     time.Time          `json:"first_seen"`
	LastUpgrade   time.Time          `json:"last_upgrade"`
	RebootPending bool               `json:"reboot_pending"`
	Status        protocol.AppStatus `json:"status"`
}

type Manager struct {
	d       Deps
	trigger chan struct{}

	mu       sync.Mutex
	desired  []protocol.AppAssignment
	state    map[string]*entry
	lastSent string
	sentAt   time.Time
}

func New(d Deps) *Manager {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Winget == nil {
		d.Winget = func() (string, error) { return "", errors.New("winget no está disponible") }
	}
	m := &Manager{d: d, trigger: make(chan struct{}, 1), state: map[string]*entry{}}
	if b, err := os.ReadFile(d.StatePath); err == nil {
		_ = json.Unmarshal(b, &m.state)
	}
	return m
}

// Desired recibe los despliegues de la política efectiva.
func (m *Manager) Desired(list []protocol.AppAssignment) {
	m.mu.Lock()
	m.desired = append([]protocol.AppAssignment{}, list...)
	// El retraso aleatorio cuenta desde que llega el despliegue.
	for _, a := range list {
		if m.state[a.DeploymentID] == nil {
			m.state[a.DeploymentID] = &entry{FirstSeen: m.d.Now(), Epoch: a.RetryEpoch}
		}
	}
	m.mu.Unlock()
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

// Run evalúa los despliegues al cambiar y cada 5 minutos.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(stepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.trigger:
		}
		m.Step(ctx)
	}
}

// jitter es el retraso estable de un equipo antes de descargar un despliegue.
func jitter(deviceID, deploymentID string) time.Duration {
	h := fnv.New64a()
	_, _ = h.Write([]byte(deviceID + "/" + deploymentID))
	return time.Duration(h.Sum64() % uint64(maxJitter))
}

func (m *Manager) save() {
	b, _ := json.Marshal(m.state)
	tmp := m.d.StatePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, m.d.StatePath)
	}
}

// Step evalúa todos los despliegues una vez (como mucho una instalación cada uno).
func (m *Manager) Step(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	desired := append([]protocol.AppAssignment{}, m.desired...)

	var inv []protocol.Software
	invLoaded := false
	inventory := func() []protocol.Software {
		if !invLoaded {
			inv, invLoaded = m.d.Inventory(ctx), true
		}
		return inv
	}
	refresh := func() { invLoaded = false }

	live := map[string]bool{}
	for _, a := range desired {
		live[a.DeploymentID] = true
		e := m.state[a.DeploymentID]
		if e == nil {
			e = &entry{FirstSeen: m.d.Now(), Epoch: a.RetryEpoch}
			m.state[a.DeploymentID] = e
		}
		if e.Epoch != a.RetryEpoch { // «Reintentar» en la consola
			e.Epoch, e.Attempts, e.NextAttempt = a.RetryEpoch, 0, time.Time{}
		}
		m.evaluate(ctx, a, e, inventory, refresh)
		e.Status.DeploymentID = a.DeploymentID
		e.Status.Attempts = e.Attempts
	}
	for id := range m.state {
		if !live[id] {
			delete(m.state, id) // despliegue retirado
		}
	}
	m.save()
	m.report(ctx, desired)
}

func (m *Manager) set(e *entry, state, msg string) {
	if len(msg) > maxMessage {
		msg = msg[len(msg)-maxMessage:]
	}
	e.Status.State, e.Status.Message = state, msg
}

func (m *Manager) evaluate(ctx context.Context, a protocol.AppAssignment, e *entry, inv func() []protocol.Software, refresh func()) {
	now := m.d.Now()
	if reason := m.notApplicable(a); reason != "" {
		m.set(e, protocol.AppNotApplicable, reason)
		return
	}
	m.handlePendingReboot(a, e)
	present, err := m.installed(ctx, a, inv)
	if err != nil {
		m.set(e, protocol.AppFailed, "No se puede comprobar si está instalada: "+err.Error())
		return
	}
	want := a.Intent != "uninstall"
	if present == want {
		if want && a.KeepUpdated && now.Sub(e.LastUpgrade) >= upgradeEvery && a.Source != "upload" {
			e.LastUpgrade = now
			m.runPackage(ctx, a, e, true)
		}
		e.Attempts, e.NextAttempt = 0, time.Time{}
		if want {
			m.set(e, protocol.AppInstalled, e.Status.Message)
		} else {
			m.set(e, protocol.AppAbsent, "")
		}
		return
	}
	switch {
	case a.StartAt != nil && now.Before(*a.StartAt):
		m.set(e, protocol.AppPending, "Programado para "+a.StartAt.Local().Format("02/01/2006 15:04")+".")
		return
	case e.Attempts >= maxAttempts:
		e.Status.State = protocol.AppFailed
		return
	case now.Before(e.NextAttempt):
		return // esperando el siguiente reintento: se conserva el estado y el mensaje
	case a.RespectWindow && !m.inWindow(now):
		m.set(e, protocol.AppWaitingWindow, "Esperando la ventana de mantenimiento.")
		return
	case a.Source == "upload" && e.Attempts == 0 && !m.d.NoJitter && now.Before(e.FirstSeen.Add(jitter(m.d.DeviceID, a.DeploymentID))):
		m.set(e, protocol.AppPending, "Esperando su turno de descarga para no saturar el servidor.")
		return
	}

	var runErr error
	var code int
	if a.Source == "upload" {
		code, runErr = m.runUpload(ctx, a, e)
	} else {
		code, runErr = m.runPackage(ctx, a, e, false)
	}
	refresh()
	if runErr == nil {
		if now2, _ := m.installed(ctx, a, inv); now2 != want {
			runErr = errors.New("el instalador terminó pero la aplicación no se detecta; revisa la regla de detección")
		}
	}
	if runErr != nil {
		e.Attempts++
		if e.Attempts >= maxAttempts {
			m.set(e, protocol.AppFailed, runErr.Error())
			return
		}
		e.NextAttempt = now.Add(backoff[e.Attempts-1])
		m.set(e, protocol.AppPending, fmt.Sprintf("Intento %d fallido; se reintentará a las %s. %s", e.Attempts, e.NextAttempt.Local().Format("15:04"), runErr.Error()))
		return
	}
	e.Attempts, e.NextAttempt = 0, time.Time{}
	reboot := code == 3010 || code == 1641
	e.Status.RebootRequired = reboot
	if want {
		m.set(e, protocol.AppInstalled, "")
	} else {
		m.set(e, protocol.AppAbsent, "")
	}
	if reboot {
		e.RebootPending = true
		m.handlePendingReboot(a, e)
	}
}

func (m *Manager) inWindow(now time.Time) bool {
	w := m.d.Window()
	return w != nil && w.Contains(now.Local())
}

// handlePendingReboot reinicia según la política del despliegue si hay un reinicio pendiente.
func (m *Manager) handlePendingReboot(a protocol.AppAssignment, e *entry) {
	if !e.RebootPending || m.d.Power == nil {
		return
	}
	msg := "El equipo se reiniciará para terminar de instalar " + a.AppName + "."
	switch a.Reboot {
	case "notify":
		_ = m.d.Power(protocol.CmdReboot, notifyDelay, msg)
		e.RebootPending = false
	case "in_window":
		if m.inWindow(m.d.Now()) {
			_ = m.d.Power(protocol.CmdReboot, windowDelay, msg)
			e.RebootPending = false
		}
	}
}

func (m *Manager) notApplicable(a protocol.AppAssignment) string {
	switch a.Source {
	case "apt":
		if !m.d.Has("apt-get") {
			return "Este equipo no usa apt."
		}
	case "dnf":
		if !m.d.Has("dnf") {
			return "Este equipo no usa dnf."
		}
	case "winget":
		if _, err := m.d.Winget(); err != nil {
			return "No aplicable: " + err.Error() + "."
		}
	}
	return ""
}

func (m *Manager) shell(ctx context.Context, command string) exec.Result {
	interp := "sh"
	env := map[string]string{"DEBIAN_FRONTEND": "noninteractive"}
	if m.d.Platform == protocol.PlatformWindows {
		interp, env = "cmd", nil
	}
	return m.d.Exec(ctx, exec.Spec{Interpreter: interp, Content: command, Env: env, Timeout: installTimeout, ScriptDir: m.d.CacheDir})
}

func resultError(r exec.Result, ok []int) (int, error) {
	if r.Err != nil {
		return -1, r.Err
	}
	if r.TimedOut {
		return -1, errors.New("la instalación superó los 60 minutos y se canceló")
	}
	if !slices.Contains(ok, r.ExitCode) {
		detail := strings.TrimSpace(r.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(r.Stdout)
		}
		if len(detail) > 1000 {
			detail = detail[len(detail)-1000:]
		}
		return r.ExitCode, fmt.Errorf("terminó con el código %d: %s", r.ExitCode, detail)
	}
	return r.ExitCode, nil
}

func (m *Manager) runPackage(ctx context.Context, a protocol.AppAssignment, e *entry, upgrade bool) (int, error) {
	w := ""
	if a.Source == "winget" {
		w, _ = m.d.Winget()
	}
	m.set(e, protocol.AppInstalling, "")
	r := m.shell(ctx, packageCommand(a.Source, a.Intent, a.PackageID, upgrade, w))
	code, err := resultError(r, []int{0})
	e.Status.ExitCode = &code
	return code, err
}

func (m *Manager) runUpload(ctx context.Context, a protocol.AppAssignment, e *entry) (int, error) {
	ext := "." + a.InstallerType
	if a.InstallerType == "" {
		ext = ".bin"
	}
	file := filepath.Join(m.d.CacheDir, a.FileSHA256+ext)
	m.set(e, protocol.AppDownloading, "")
	if err := m.d.Fetch(ctx, a.FileSHA256, a.Size, file); err != nil {
		return -1, err
	}
	defer os.Remove(file)
	command := a.InstallCommand
	if a.Intent == "uninstall" {
		command = a.UninstallCommand
	}
	m.set(e, protocol.AppInstalling, "")
	r := m.shell(ctx, strings.ReplaceAll(command, "{file}", file))
	codes := a.SuccessCodes
	if len(codes) == 0 {
		codes = []int{0}
	}
	code, err := resultError(r, codes)
	e.Status.ExitCode = &code
	return code, err
}

// report envía los estados si han cambiado o cada hora.
func (m *Manager) report(ctx context.Context, desired []protocol.AppAssignment) {
	rep := protocol.AppStatusReport{Items: []protocol.AppStatus{}}
	for _, a := range desired {
		if e := m.state[a.DeploymentID]; e != nil {
			rep.Items = append(rep.Items, e.Status)
		}
	}
	b, _ := json.Marshal(rep)
	if string(b) == m.lastSent && m.d.Now().Sub(m.sentAt) < resendEvery {
		return
	}
	if err := m.d.API.SendAppStatus(ctx, rep); err != nil {
		m.d.Log.Warn("no se pudo enviar el estado del software", "err", err)
		return
	}
	m.lastSent, m.sentAt = string(b), m.d.Now()
}
