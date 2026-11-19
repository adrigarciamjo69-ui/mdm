package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

var ctx = context.Background()

// sim simula el equipo: lo instalado, las órdenes ejecutadas y el reloj.
type sim struct {
	mu        sync.Mutex
	t         *testing.T
	now       time.Time
	installed map[string]string // nombre → versión (inventario)
	commands  []string
	exitCode  int
	noEffect  bool // la orden termina bien pero no instala nada
	reports   []protocol.AppStatusReport
	powers    []string
	fetches   int
	fetchErr  error
	window    *protocol.Window
	dir       string
}

func newSim(t *testing.T) *sim {
	return &sim{t: t, now: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), installed: map[string]string{}, dir: t.TempDir()}
}

func (s *sim) deps(platform string) Deps {
	return Deps{
		Platform: platform,
		DeviceID: "dev-1",
		API: apiFunc(func(_ context.Context, r protocol.AppStatusReport) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.reports = append(s.reports, r)
			return nil
		}),
		Fetch: func(_ context.Context, sha string, size int64, dst string) error {
			s.fetches++
			if s.fetchErr != nil {
				return s.fetchErr
			}
			return os.WriteFile(dst, []byte("instalador"), 0o600)
		},
		Exec: func(_ context.Context, sp exec.Spec) exec.Result {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.commands = append(s.commands, sp.Content)
			if !s.noEffect && s.exitCode == 0 || s.exitCode == 3010 {
				switch {
				case (strings.Contains(sp.Content, "install") || strings.Contains(sp.Content, "/i ")) && !strings.Contains(sp.Content, "remove") && !strings.Contains(sp.Content, "/x"):
					s.installed["Contabilidad Pro"] = "3.2.1"
					s.installed["nginx"] = "1.24"
				case strings.Contains(sp.Content, "remove") || strings.Contains(sp.Content, "/x"):
					delete(s.installed, "nginx")
					delete(s.installed, "Contabilidad Pro")
				}
			}
			return exec.Result{ExitCode: s.exitCode, Stdout: "salida", Stderr: "detalle del error"}
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if name == "dpkg-query" {
				if _, ok := s.installed[args[len(args)-1]]; ok {
					return []byte("install ok installed"), nil
				}
				return nil, errors.New("no instalado")
			}
			return nil, nil
		},
		Has:       func(name string) bool { return name == "apt-get" || name == "dpkg-query" },
		Inventory: func(context.Context) []protocol.Software { return s.software() },
		Window:    func() *protocol.Window { return s.window },
		Power: func(action string, delay time.Duration, msg string) error {
			s.powers = append(s.powers, fmt.Sprintf("%s %v", action, delay))
			return nil
		},
		Now:       func() time.Time { return s.now },
		StatePath: filepath.Join(s.dir, "apps.json"),
		CacheDir:  s.dir,
	}
}

func (s *sim) software() []protocol.Software {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []protocol.Software{}
	for n, v := range s.installed {
		out = append(out, protocol.Software{Name: n, Version: v})
	}
	return out
}

func (s *sim) last(id string) protocol.AppStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.reports) - 1; i >= 0; i-- {
		for _, it := range s.reports[i].Items {
			if it.DeploymentID == id {
				return it
			}
		}
	}
	return protocol.AppStatus{}
}

type apiFunc func(context.Context, protocol.AppStatusReport) error

func (f apiFunc) SendAppStatus(c context.Context, r protocol.AppStatusReport) error { return f(c, r) }

func msiApp(id string) protocol.AppAssignment {
	return protocol.AppAssignment{DeploymentID: id, AppName: "Contabilidad Pro", Version: "3.2.1", Intent: "required", Source: "upload",
		FileSHA256: strings.Repeat("a", 64), Size: 10, InstallerType: "msi", InstallCommand: `msiexec /i "{file}" /qn`, UninstallCommand: `msiexec /x "{file}" /qn`,
		SuccessCodes: []int{0, 3010, 1641}, Detection: &protocol.Detection{Type: "inventory", NameContains: "contabilidad", MinVersion: "3.2"}, Reboot: "never"}
}

func aptApp(id, intent string) protocol.AppAssignment {
	return protocol.AppAssignment{DeploymentID: id, AppName: "nginx", Intent: intent, Source: "apt", PackageID: "nginx", Reboot: "never"}
}

// pastJitter adelanta el reloj más allá del retraso aleatorio máximo.
func (s *sim) pastJitter() { s.now = s.now.Add(31 * time.Minute) }

func TestManager_InstallsWhenMissing(t *testing.T) {
	s := newSim(t)
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppPending {
		t.Fatalf("antes del retraso aleatorio: %+v", st)
	}
	s.pastJitter()
	m.Step(ctx)
	st := s.last("d1")
	if st.State != protocol.AppInstalled || s.fetches != 1 || len(s.commands) != 1 || !strings.Contains(s.commands[0], filepath.Join(s.dir, "")) {
		t.Fatalf("instalación: %+v órdenes %v", st, s.commands)
	}
	if strings.Contains(s.commands[0], "{file}") {
		t.Fatal("{file} debe sustituirse por la ruta")
	}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), strings.Repeat("a", 8)) {
			t.Fatal("el instalador descargado se borra tras instalar")
		}
	}
}

func TestManager_AlreadyInstalledReportsInstalled(t *testing.T) {
	s := newSim(t)
	s.installed["Contabilidad Pro"] = "3.5"
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppInstalled || s.fetches != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestManager_SuccessButNotDetected(t *testing.T) {
	s := newSim(t)
	s.noEffect = true
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	s.pastJitter()
	m.Step(ctx)
	st := s.last("d1")
	if st.State != protocol.AppPending || st.Attempts != 1 || !strings.Contains(st.Message, "el instalador terminó pero la aplicación no se detecta") {
		t.Fatalf("debe contar como intento fallido: %+v", st)
	}
	m.Step(ctx)
	if len(s.commands) != 1 {
		t.Fatal("no debe reintentar antes de la espera")
	}
}

func TestManager_RetriesThenFails(t *testing.T) {
	s := newSim(t)
	s.exitCode = 1603
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	s.pastJitter()
	m.Step(ctx)
	for i, wait := range []time.Duration{15 * time.Minute, time.Hour, 4 * time.Hour} {
		s.now = s.now.Add(wait - time.Second)
		m.Step(ctx)
		if len(s.commands) != i+1 {
			t.Fatalf("reintento %d antes de tiempo", i+1)
		}
		s.now = s.now.Add(time.Second)
		m.Step(ctx)
		if len(s.commands) != i+2 {
			t.Fatalf("reintento %d no se hizo a su hora", i+1)
		}
	}
	st := s.last("d1")
	if st.State != protocol.AppFailed || st.Attempts != 4 || st.ExitCode == nil || *st.ExitCode != 1603 || len(s.commands) != 4 {
		t.Fatalf("tras 4 intentos: %+v (%d órdenes)", st, len(s.commands))
	}
	s.now = s.now.Add(48 * time.Hour)
	m.Step(ctx)
	if len(s.commands) != 4 {
		t.Fatal("fallido definitivo: no se reintenta solo")
	}
	retry := msiApp("d1")
	retry.RetryEpoch = 1
	s.exitCode = 0
	m.Desired([]protocol.AppAssignment{retry})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppInstalled {
		t.Fatalf("«Reintentar» en la consola cambia la época y vuelve a intentarlo: %+v", st)
	}
}

func TestManager_StableJitter(t *testing.T) {
	a := jitter("dev-1", "d1")
	if a != jitter("dev-1", "d1") || a < 0 || a >= 30*time.Minute {
		t.Fatalf("retraso: %v", a)
	}
	if jitter("dev-1", "d1") == jitter("dev-2", "d1") && jitter("dev-1", "d1") == jitter("dev-3", "d1") {
		t.Fatal("equipos distintos deberían repartirse")
	}
	// Reiniciar el agente no reinicia la espera.
	s := newSim(t)
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	m.Step(ctx)
	s.pastJitter()
	m2 := New(s.deps("windows"))
	m2.Desired([]protocol.AppAssignment{msiApp("d1")})
	m2.Step(ctx)
	if s.fetches != 1 {
		t.Fatal("tras reiniciar, el retraso ya cumplido no vuelve a empezar")
	}
}

func TestManager_RespectsWindowAndStart(t *testing.T) {
	s := newSim(t)
	s.window = &protocol.Window{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "22:00", End: "05:00"}
	app := aptApp("d1", "required")
	app.RespectWindow = true
	m := New(s.deps("linux"))
	m.Desired([]protocol.AppAssignment{app})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppWaitingWindow {
		t.Fatalf("fuera de la ventana: %+v", st)
	}
	s.now = time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC)
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppInstalled {
		t.Fatalf("dentro de la ventana: %+v", st)
	}
	future := s.now.Add(48 * time.Hour)
	later := aptApp("d2", "required")
	later.PackageID, later.AppName = "htop", "htop"
	later.StartAt = &future
	m.Desired([]protocol.AppAssignment{later})
	m.Step(ctx)
	if st := s.last("d2"); st.State != protocol.AppPending {
		t.Fatalf("antes de la fecha de inicio: %+v", st)
	}
}

func TestManager_Uninstall(t *testing.T) {
	s := newSim(t)
	s.installed["nginx"] = "1.24"
	m := New(s.deps("linux"))
	m.Desired([]protocol.AppAssignment{aptApp("d1", "uninstall")})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppAbsent || !strings.Contains(strings.Join(s.commands, ";"), "apt-get remove -y nginx") {
		t.Fatalf("%+v %v", st, s.commands)
	}
}

func TestManager_NotApplicable(t *testing.T) {
	s := newSim(t)
	d := s.deps("linux")
	d.Has = func(string) bool { return false }
	m := New(d)
	m.Desired([]protocol.AppAssignment{aptApp("d1", "required")})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppNotApplicable || !strings.Contains(st.Message, "apt") {
		t.Fatalf("%+v", st)
	}
}

func TestManager_RebootRequiredCodes(t *testing.T) {
	s := newSim(t)
	s.exitCode = 3010
	s.window = &protocol.Window{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "22:00", End: "05:00"}
	app := msiApp("d1")
	app.Reboot = "in_window"
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{app})
	s.pastJitter()
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppInstalled || !st.RebootRequired || len(s.powers) != 0 {
		t.Fatalf("3010 = instalada con reinicio pendiente, sin reiniciar fuera de la ventana: %+v %v", st, s.powers)
	}
	s.now = time.Date(2026, 11, 17, 23, 0, 0, 0, time.UTC)
	m.Step(ctx)
	if len(s.powers) != 1 || !strings.HasPrefix(s.powers[0], "reboot") {
		t.Fatalf("dentro de la ventana reinicia: %v", s.powers)
	}
	m.Step(ctx)
	if len(s.powers) != 1 {
		t.Fatal("el reinicio se pide una vez")
	}
}

func TestManager_FetchErrorRetries(t *testing.T) {
	s := newSim(t)
	s.fetchErr = errors.New("no hay espacio suficiente en el disco: hacen falta 900 MB y hay 100 MB libres")
	m := New(s.deps("windows"))
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	s.pastJitter()
	m.Step(ctx)
	if st := s.last("d1"); st.Attempts != 1 || !strings.Contains(st.Message, "100 MB libres") {
		t.Fatalf("%+v", st)
	}
}

func TestManager_NoJitterForTests(t *testing.T) {
	s := newSim(t)
	d := s.deps("windows")
	d.NoJitter = true
	m := New(d)
	m.Desired([]protocol.AppAssignment{msiApp("d1")})
	m.Step(ctx)
	if st := s.last("d1"); st.State != protocol.AppInstalled {
		t.Fatalf("sin retraso debe instalar en el primer paso: %+v", st)
	}
}
