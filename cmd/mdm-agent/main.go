// mdm-agent es el agente que se instala en cada equipo gestionado: lo
// inscribe en el servidor, mantiene el check-in y envía el inventario.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mdm/internal/agent/apps"
	"mdm/internal/agent/client"
	"mdm/internal/agent/commands"
	"mdm/internal/agent/config"
	"mdm/internal/agent/enroll"
	agentexec "mdm/internal/agent/exec"
	"mdm/internal/agent/fetch"
	"mdm/internal/agent/inventory"
	"mdm/internal/agent/loop"
	"mdm/internal/agent/patches"
	"mdm/internal/agent/policy"
	"mdm/internal/agent/service"
	"mdm/internal/agent/settings"
	"mdm/internal/agent/update"
	"mdm/internal/protocol"
)

// version se fija al compilar con -ldflags "-X main.version=…".
var version = "dev"

const usage = `Uso:
  mdm-agent enroll --server <https://servidor> --token <token>
                        Inscribe este equipo e instala el servicio
  mdm-agent status      Muestra el estado del agente
  mdm-agent uninstall   Detiene y elimina el servicio y borra la configuración
  mdm-agent run         Ejecuta el agente (lo usa el gestor de servicios)
  mdm-agent version     Muestra la versión
`

// deps reúne lo que la línea de órdenes necesita del sistema, para poder
// sustituirlo en las pruebas.
type deps struct {
	stdout, stderr   io.Writer
	isAdmin          func() bool
	configDir        string
	installPath      string
	installService   func(binPath string) error
	stopService      func()
	uninstallService func() error
	runService       func(func(context.Context) error) error
	now              func() time.Time
}

func main() {
	os.Exit(run(os.Args[1:], deps{
		stdout: os.Stdout, stderr: os.Stderr,
		isAdmin:          service.IsAdmin,
		configDir:        config.Dir(),
		installPath:      service.BinaryPath(),
		installService:   service.Install,
		stopService:      service.Stop,
		uninstallService: service.Uninstall,
		runService:       service.Run,
		now:              time.Now,
	}))
}

func (d deps) configPath() string { return filepath.Join(d.configDir, "agent.json") }
func (d deps) statePath() string  { return filepath.Join(d.configDir, "state.json") }

func run(args []string, d deps) int {
	if len(args) == 0 {
		fmt.Fprint(d.stderr, usage)
		return 1
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		fmt.Fprintln(d.stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(d.stdout, usage)
		return 0
	case "enroll", "status", "uninstall", "run", "selftest":
	default:
		fmt.Fprintf(d.stderr, "Orden desconocida: %s\n\n%s", cmd, usage)
		return 1
	}
	if !d.isAdmin() {
		fmt.Fprintln(d.stderr, "Hace falta ejecutar como administrador (root).")
		return 1
	}
	switch cmd {
	case "enroll":
		return cmdEnroll(rest, d)
	case "status":
		return cmdStatus(d)
	case "uninstall":
		return cmdUninstall(d)
	case "selftest":
		return cmdSelftest(d)
	default:
		return cmdRun(d)
	}
}

func cmdEnroll(args []string, d deps) int {
	fl := flag.NewFlagSet("enroll", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	server := fl.String("server", "", "")
	token := fl.String("token", "", "")
	if err := fl.Parse(args); err != nil {
		fmt.Fprintf(d.stderr, "Error: %v\n\n%s", err, usage)
		return 1
	}
	if strings.TrimSpace(*server) == "" {
		fmt.Fprintln(d.stderr, "Error: falta la dirección del servidor (--server https://…).")
		return 1
	}
	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(d.stderr, "Error: falta el token de inscripción (--token …).")
		return 1
	}
	err := enroll.Run(context.Background(), enroll.Options{
		ServerURL: *server, Token: *token, Version: version,
		ConfigPath: d.configPath(), Collector: inventory.New(),
		InstallService: func() error {
			// Se detiene el servicio anterior, si lo hay, antes de sustituir
			// su ejecutable: en Windows un ejecutable en uso no se puede reemplazar.
			d.stopService()
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if err := copyBinary(exe, d.installPath); err != nil {
				return fmt.Errorf("copiar el agente a %s: %w", d.installPath, err)
			}
			return d.installService(d.installPath)
		},
	})
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return enroll.ExitCode(err)
	}
	fmt.Fprintln(d.stdout, "Equipo inscrito. El agente está en marcha y aparecerá en la consola en uno o dos minutos.")
	return 0
}

// copyBinary copia el ejecutable a su ubicación definitiva. Escribe en un
// fichero temporal y lo renombra, para no dejar un binario a medias.
func copyBinary(src, dst string) error {
	if same, err := sameFile(src, dst); err == nil && same {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func sameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}

func cmdStatus(d deps) int {
	cfg, err := config.Load(d.configPath())
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprint(d.stdout, formatStatus(nil, config.State{}, d.now()))
		return 0
	}
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	state, _ := config.LoadState(d.statePath())
	fmt.Fprint(d.stdout, formatStatus(&cfg, state, d.now()))
	return 0
}

// ago expresa una duración pasada en la unidad más natural.
func ago(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("hace %d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("hace %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("hace %d h", int(d.Hours()))
	}
	return fmt.Sprintf("hace %d días", int(d.Hours()/24))
}

// formatStatus compone la salida de «status». cfg nulo significa que el
// equipo no está inscrito. Nunca muestra la credencial.
func formatStatus(cfg *config.Config, st config.State, now time.Time) string {
	server, device, state := "—", "—", "sin inscribir"
	if cfg != nil {
		server, device, state = cfg.ServerURL, cfg.DeviceID, "activo"
		if cfg.Revoked {
			state = "revocado"
		}
	}
	last := "nunca"
	if st.LastSuccess != nil {
		last = fmt.Sprintf("%s (%s)", st.LastSuccess.In(now.Location()).Format("2006-01-02 15:04:05"), ago(now.Sub(*st.LastSuccess)))
	}
	lastErr := "ninguno"
	if st.LastError != "" {
		lastErr = st.LastError
		if st.LastErrorAt != nil {
			lastErr += " (" + ago(now.Sub(*st.LastErrorAt)) + ")"
		}
	}
	return fmt.Sprintf("Servidor:         %s\nDispositivo:      %s\nEstado:           %s\nÚltima conexión:  %s\nÚltimo error:     %s\n",
		server, device, state, last, lastErr)
}

func cmdUninstall(d deps) int {
	if err := d.uninstallService(); err != nil {
		fmt.Fprintln(d.stderr, "Error: no se ha podido eliminar el servicio:", err)
		return 1
	}
	if err := config.Remove(d.configDir); err != nil {
		fmt.Fprintln(d.stderr, "Error: no se ha podido borrar la configuración:", err)
		return 1
	}
	// El ejecutable instalado se borra si se puede. En Windows, si es el que
	// está ejecutando esta orden, el sistema no lo permite: se avisa.
	if err := os.Remove(d.installPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(d.stdout, "Agente desinstalado. Queda por borrar a mano el ejecutable: %s\n", d.installPath)
		return 0
	}
	fmt.Fprintln(d.stdout, "Agente desinstalado. El equipo seguirá apareciendo en la consola hasta que un administrador lo elimine.")
	return 0
}

func cmdRun(d deps) int {
	cfg, err := config.Load(d.configPath())
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(d.stderr, "Error: este equipo no está inscrito. Ejecuta primero «mdm-agent enroll --server … --token …».")
		return 1
	}
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	api, err := client.New(cfg.ServerURL, cfg.Bearer(), nil)
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	api.Version = version
	state, _ := config.LoadState(d.statePath())
	logger := slog.New(slog.NewTextHandler(logOutput(d.configDir), nil))
	logger.Info("agente en marcha", "version", version, "servidor", cfg.ServerURL, "dispositivo", cfg.DeviceID)

	var forceInventory atomic.Bool
	updater := update.New(update.Deps{
		Version:  version,
		Download: api.Download,
		SelfTest: func(ctx context.Context, bin string) error {
			out, err := osexec.CommandContext(ctx, bin, "selftest").CombinedOutput()
			if err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		},
		Swap:    update.Swap,
		Exit:    os.Exit,
		BinPath: d.installPath,
		Dir:     d.configDir,
		Log:     logger,
	})
	var cleanupOnce sync.Once
	cacheDir := filepath.Join(d.configDir, "cache")
	_ = os.MkdirAll(cacheDir, 0o700)
	getter := fetch.Getter{Base: api.Base(), Bearer: api.Bearer(), HTTP: api.HTTP(), Free: fetch.DiskFree}
	var engine *policy.Engine
	appsManager := apps.New(apps.Deps{
		Platform: runtime.GOOS, DeviceID: cfg.DeviceID, API: api, Fetch: getter.Get, Exec: agentexec.Run,
		Run: inventory.ExecRunner, Has: hasCommand,
		Winget:    func() (string, error) { return apps.LocateWinget(os.Getenv("ProgramFiles")) },
		Inventory: func(ctx context.Context) []protocol.Software { return inventory.New().Collect(ctx).Software },
		Window: func() *protocol.Window {
			if p := engine.Policy().Patches; p != nil {
				return &p.Window
			}
			return nil
		},
		Power: commands.Power, StatePath: filepath.Join(d.configDir, "apps.json"), CacheDir: cacheDir, Log: logger,
		NoJitter: os.Getenv("MDM_AGENT_NO_JITTER") == "1",
	})
	patchManager := patches.New(patches.Deps{
		Platform: runtime.GOOS, DeviceID: cfg.DeviceID, API: api, Power: commands.Power, Log: logger,
		Scanner:   patches.NewScanner(runtime.GOOS, patches.SystemDeps{Run: inventory.ExecRunner, Exec: agentexec.Run, Has: hasCommand, ScriptDir: cacheDir}),
		StatePath: filepath.Join(d.configDir, "patches.json"),
	})
	engine = policy.New(policy.Deps{
		OnPolicy: func(p protocol.EffectivePolicy) {
			appsManager.Desired(p.Apps)
			patchManager.SetPolicy(p.Patches)
		},
		API: api,
		Build: func(s protocol.PolicySetting, escrowed []string) (settings.Setting, error) {
			return settings.Build(runtime.GOOS, s, settings.Deps{Run: inventory.ExecRunner, Exec: agentexec.Run,
				Has: hasCommand, Root: "/", ScriptDir: d.configDir, Escrowed: escrowed})
		},
		Log: logger,
	})
	runner, err := commands.New(commands.Deps{
		API:               api,
		Exec:              agentexec.Run,
		Power:             commands.Power,
		RefreshInventory:  func() { forceInventory.Store(true) },
		EvaluatePolicies:  engine.Trigger,
		ScanUpdates:       patchManager.ScanNow,
		InstallUpdatesNow: patchManager.InstallNow,
		// Retirar: primero se borra la configuración (la credencial) y luego se
		// quita el servicio, que detiene este mismo proceso.
		Retire: func() error {
			if err := config.Remove(d.configDir); err != nil {
				return err
			}
			return d.uninstallService()
		},
		StatePath: filepath.Join(d.configDir, "commands.json"),
		ScriptDir: d.configDir,
		Log:       logger,
	})
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	err = d.runService(func(ctx context.Context) error {
		go runner.Run(ctx)
		go engine.Run(ctx)
		go appsManager.Run(ctx)
		go patchManager.Run(ctx)
		return loop.Run(ctx, loop.Deps{
			API: api, Collector: inventory.New(), Config: cfg, State: state, Version: version, Log: logger,
			SaveConfig:     func(c config.Config) error { return config.Save(d.configPath(), c) },
			SaveState:      func(s config.State) error { return config.SaveState(d.statePath(), s) },
			Sleep:          sleep,
			Rand:           rand.Float64,
			Now:            time.Now,
			Capabilities:   capabilities(),
			Commands:       runner.Submit,
			ForceInventory: func() bool { return forceInventory.Swap(false) },
			PolicyHash:     engine.Hash,
			PolicyWanted:   engine.Wanted,
			Update:         updater.Offer,
			AfterCheckin:   func() { cleanupOnce.Do(updater.CleanupOld) },
		})
	})
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	return 0
}

// cmdSelftest comprueba que este binario puede hablar con el servidor con la
// configuración instalada. Lo usa la actualización antes de sustituir el
// binario en uso. No registra nada en el servidor.
func cmdSelftest(d deps) int {
	cfg, err := config.Load(d.configPath())
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	api, err := client.New(cfg.ServerURL, cfg.Bearer(), nil)
	if err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	api.Version = version
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := api.GetPolicy(ctx); err != nil {
		fmt.Fprintln(d.stderr, "Error:", err)
		return 1
	}
	fmt.Fprintln(d.stdout, "Correcto.")
	return 0
}

// capabilities son las funciones que declara esta versión del agente.
func capabilities() []string {
	caps := []string{protocol.CapPolicy, protocol.CapCommands, protocol.CapScripts, protocol.CapSelfUpdate, protocol.CapApps, protocol.CapPatches}
	if runtime.GOOS == "windows" {
		caps = append(caps, protocol.CapBitLocker)
	}
	return caps
}

func hasCommand(name string) bool {
	_, err := osexec.LookPath(name)
	return err == nil
}

// sleep espera d o hasta que se cancele el contexto.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
