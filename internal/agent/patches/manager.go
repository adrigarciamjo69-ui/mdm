// Package patches busca e instala las actualizaciones del sistema en el
// agente: una búsqueda al día a una hora estable por equipo (y bajo orden),
// instalación de lo aprobado dentro de la ventana de mantenimiento (o en el
// momento, bajo orden) y reinicio según la política.
package patches

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"mdm/internal/protocol"
)

const (
	// installCooldown evita reintentar una instalación fallida en la misma ventana más de una vez por hora.
	installCooldown = time.Hour
	stepEvery       = 5 * time.Minute
)

type Scanner interface {
	Scan(ctx context.Context) (protocol.UpdateScan, error)
	// Install instala ids (Windows) o, en Linux, lo que indique scope (security o all).
	Install(ctx context.Context, ids []string, scope string) (protocol.UpdateResults, error)
}

type API interface {
	SendUpdates(ctx context.Context, s protocol.UpdateScan) error
	SendUpdateResults(ctx context.Context, r protocol.UpdateResults) error
}

type Deps struct {
	Platform  string
	DeviceID  string
	API       API
	Scanner   Scanner
	Power     func(action string, delay time.Duration, message string) error
	Now       func() time.Time
	StatePath string
	Log       *slog.Logger
}

type persisted struct {
	LastScan    time.Time             `json:"last_scan"`
	LastInstall time.Time             `json:"last_install"`
	Pending     []protocol.UpdateInfo `json:"pending"`
}

type Manager struct {
	d       Deps
	trigger chan struct{}

	mu         sync.Mutex
	st         persisted
	policy     *protocol.PatchPolicy
	scanNow    bool
	installNow bool
}

func New(d Deps) *Manager {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	m := &Manager{d: d, trigger: make(chan struct{}, 1)}
	if b, err := os.ReadFile(d.StatePath); err == nil {
		_ = json.Unmarshal(b, &m.st)
	}
	return m
}

// scanTime es la hora del día (desde medianoche) a la que busca este equipo.
func scanTime(deviceID string) time.Duration {
	h := fnv.New64a()
	_, _ = h.Write([]byte(deviceID))
	return time.Duration(h.Sum64()%uint64(24*60)) * time.Minute
}

func (m *Manager) poke() {
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

// SetPolicy recibe la política de parches vigente (nil si no hay).
func (m *Manager) SetPolicy(p *protocol.PatchPolicy) {
	m.mu.Lock()
	m.policy = p
	m.mu.Unlock()
	m.poke()
}

// ScanNow pide una búsqueda inmediata (orden «buscar actualizaciones»).
func (m *Manager) ScanNow() {
	m.mu.Lock()
	m.scanNow = true
	m.mu.Unlock()
	m.poke()
}

// InstallNow pide instalar lo aprobado sin esperar a la ventana.
func (m *Manager) InstallNow() {
	m.mu.Lock()
	m.installNow, m.scanNow = true, true
	m.mu.Unlock()
	m.poke()
}

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

func (m *Manager) save() {
	b, _ := json.Marshal(m.st)
	tmp := m.d.StatePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, m.d.StatePath)
	}
}

// dueScan informa de si toca la búsqueda diaria.
func (m *Manager) dueScan(now time.Time) bool {
	if m.st.LastScan.IsZero() {
		return true
	}
	local := now.Local()
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).Add(scanTime(m.d.DeviceID))
	return now.After(today) && m.st.LastScan.Before(today)
}

func (m *Manager) scan(ctx context.Context) {
	s, err := m.d.Scanner.Scan(ctx)
	if err != nil {
		m.d.Log.Warn("no se pudieron buscar actualizaciones", "err", err)
		return
	}
	s.ScannedAt = m.d.Now().UTC()
	m.st.LastScan, m.st.Pending = m.d.Now(), s.Updates
	if err := m.d.API.SendUpdates(ctx, s); err != nil {
		m.d.Log.Warn("no se pudo enviar la búsqueda de actualizaciones", "err", err)
	}
}

// toInstall devuelve lo que se instalaría ahora: en Windows, las aprobadas que
// faltan; en Linux, si hay algo pendiente dentro del alcance.
func (m *Manager) toInstall(p *protocol.PatchPolicy) ([]string, bool) {
	if m.d.Platform == protocol.PlatformWindows {
		var ids []string
		for _, u := range m.st.Pending {
			if slices.Contains(p.ApprovedUpdateIDs, u.ID) {
				ids = append(ids, u.ID)
			}
		}
		return ids, len(ids) > 0
	}
	for _, u := range m.st.Pending {
		if p.LinuxScope == "all" || u.Security {
			return nil, true
		}
	}
	return nil, false
}

// Step hace lo que toque: búsqueda diaria o pedida, e instalación en la
// ventana o pedida.
func (m *Manager) Step(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.d.Now()
	if m.scanNow || m.dueScan(now) {
		m.scanNow = false
		m.scan(ctx)
	}
	p := m.policy
	forced := m.installNow
	m.installNow = false
	if p == nil {
		m.save()
		return
	}
	inWindow := p.Window.Contains(now.Local())
	ids, any := m.toInstall(p)
	if any && (forced || (inWindow && now.Sub(m.st.LastInstall) >= installCooldown)) {
		m.st.LastInstall = now
		res, err := m.d.Scanner.Install(ctx, ids, p.LinuxScope)
		if err != nil {
			m.d.Log.Warn("error al instalar actualizaciones", "err", err)
		}
		if err := m.d.API.SendUpdateResults(ctx, res); err != nil {
			m.d.Log.Warn("no se pudo enviar el resultado de las actualizaciones", "err", err)
		}
		m.scan(ctx)
		if res.RebootPending {
			m.reboot(p, inWindow)
		}
	}
	m.save()
}

func (m *Manager) reboot(p *protocol.PatchPolicy, inWindow bool) {
	if m.d.Power == nil {
		return
	}
	delay := time.Duration(p.NotifyMinutes) * time.Minute
	if delay <= 0 {
		delay = 15 * time.Minute
	}
	msg := "El equipo se reiniciará para terminar de instalar actualizaciones."
	switch {
	case p.Reboot == "notify", p.Reboot == "in_window" && inWindow:
		if err := m.d.Power(protocol.CmdReboot, delay, msg); err != nil {
			m.d.Log.Warn("no se pudo programar el reinicio", "err", err)
		}
	}
}
