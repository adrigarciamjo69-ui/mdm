package patches

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"strings"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

//go:embed scripts/scan.ps1
var scanPS string

//go:embed scripts/install.ps1
var installPS string

const (
	scanTimeout    = 30 * time.Minute
	installTimeout = 3 * time.Hour
)

// SystemDeps son las funciones del sistema que usan los buscadores.
type SystemDeps struct {
	Run       func(ctx context.Context, name string, args ...string) ([]byte, error)
	Exec      func(ctx context.Context, s exec.Spec) exec.Result
	Has       func(name string) bool
	ScriptDir string
}

// NewScanner devuelve el buscador de la plataforma.
func NewScanner(platform string, d SystemDeps) Scanner {
	if platform == protocol.PlatformWindows {
		return &windowsScanner{d}
	}
	return &linuxScanner{d}
}

// ---------- Linux ----------

type linuxScanner struct{ d SystemDeps }

func (l *linuxScanner) Scan(ctx context.Context) (protocol.UpdateScan, error) {
	var s protocol.UpdateScan
	switch {
	case l.d.Has("apt-get"):
		_, _ = l.d.Run(ctx, "apt-get", "update", "-q")
		out, err := l.d.Run(ctx, "apt-get", "-s", "upgrade")
		if err != nil && len(out) == 0 {
			return s, err
		}
		s.Updates = parseAptSimulate(string(out))
		_, statErr := os.Stat("/var/run/reboot-required")
		s.RebootPending = statErr == nil
	case l.d.Has("dnf"):
		secOut, _ := l.d.Run(ctx, "dnf", "-q", "updateinfo", "list", "--security")
		out, _ := l.d.Run(ctx, "dnf", "-q", "check-update") // termina con 100 si hay actualizaciones
		s.Updates = parseDnfCheckUpdate(string(out), parseDnfSecurity(string(secOut)))
		if l.d.Has("needs-restarting") || l.d.Has("dnf") {
			_, err := l.d.Run(ctx, "dnf", "needs-restarting", "-r")
			s.RebootPending = err != nil // termina con 1 si hace falta reiniciar
		}
	default:
		return s, errors.New("el equipo no usa apt ni dnf")
	}
	if s.Updates == nil {
		s.Updates = []protocol.UpdateInfo{}
	}
	return s, nil
}

func (l *linuxScanner) Install(ctx context.Context, _ []string, scope string) (protocol.UpdateResults, error) {
	before, err := l.Scan(ctx)
	if err != nil {
		return protocol.UpdateResults{}, err
	}
	var targets []string
	for _, u := range before.Updates {
		if scope == "all" || u.Security {
			targets = append(targets, u.ID)
		}
	}
	var cmd string
	switch {
	case l.d.Has("apt-get"):
		opts := " -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold "
		if scope == "all" {
			cmd = "apt-get upgrade -y" + opts
		} else {
			cmd = "apt-get install -y --only-upgrade" + opts + strings.Join(targets, " ")
		}
	default:
		cmd = "dnf upgrade -y"
		if scope != "all" {
			cmd += " --security"
		}
	}
	r := l.d.Exec(ctx, exec.Spec{Interpreter: "sh", Content: cmd, Env: map[string]string{"DEBIAN_FRONTEND": "noninteractive"},
		Timeout: installTimeout, ScriptDir: l.d.ScriptDir})
	after, _ := l.Scan(ctx)
	still := map[string]bool{}
	for _, u := range after.Updates {
		still[u.ID] = true
	}
	detail := strings.TrimSpace(r.Stderr)
	if len(detail) > 500 {
		detail = detail[len(detail)-500:]
	}
	res := protocol.UpdateResults{RebootPending: after.RebootPending, Results: []protocol.UpdateResult{}}
	for _, id := range targets {
		if still[id] {
			res.Results = append(res.Results, protocol.UpdateResult{ID: id, Status: "failed", Error: detail})
		} else {
			res.Results = append(res.Results, protocol.UpdateResult{ID: id, Status: "installed"})
		}
	}
	return res, nil
}

// ---------- Windows ----------

type windowsScanner struct{ d SystemDeps }

func (w *windowsScanner) Scan(ctx context.Context) (protocol.UpdateScan, error) {
	r := w.d.Exec(ctx, exec.Spec{Interpreter: "powershell", Content: scanPS, Timeout: scanTimeout, ScriptDir: w.d.ScriptDir})
	if r.Err != nil {
		return protocol.UpdateScan{}, r.Err
	}
	return parseWindowsScan(r.Stdout)
}

func (w *windowsScanner) Install(ctx context.Context, ids []string, _ string) (protocol.UpdateResults, error) {
	if len(ids) == 0 {
		return protocol.UpdateResults{Results: []protocol.UpdateResult{}}, nil
	}
	r := w.d.Exec(ctx, exec.Spec{Interpreter: "powershell", Content: installPS, Timeout: installTimeout, ScriptDir: w.d.ScriptDir,
		Env: map[string]string{"MDM_IDS": strings.Join(ids, ",")}})
	if r.Err != nil {
		return protocol.UpdateResults{}, r.Err
	}
	return parseWindowsInstall(r.Stdout)
}
