package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mdm/internal/agent/exec"
	"mdm/internal/protocol"
)

const detectTimeout = 5 * time.Minute

// installed informa de si la aplicación está instalada en el equipo.
func (m *Manager) installed(ctx context.Context, a protocol.AppAssignment, inv func() []protocol.Software) (bool, error) {
	switch a.Source {
	case "apt":
		out, err := m.d.Run(ctx, "dpkg-query", "-W", "-f=${Status}", a.PackageID)
		return err == nil && strings.Contains(string(out), "install ok installed"), nil
	case "dnf":
		_, err := m.d.Run(ctx, "rpm", "-q", a.PackageID)
		return err == nil, nil
	case "winget":
		w, err := m.d.Winget()
		if err != nil {
			return false, err
		}
		out, err := m.d.Run(ctx, w, "list", "--id", a.PackageID, "--exact", "--accept-source-agreements", "--disable-interactivity")
		return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(a.PackageID)), nil
	case "upload":
		if a.Detection == nil {
			return false, errors.New("el despliegue no tiene regla de detección")
		}
		return m.detectRule(ctx, *a.Detection, inv)
	}
	return false, fmt.Errorf("origen desconocido: %s", a.Source)
}

func (m *Manager) detectRule(ctx context.Context, d protocol.Detection, inv func() []protocol.Software) (bool, error) {
	switch d.Type {
	case "inventory":
		needle := strings.ToLower(d.NameContains)
		for _, s := range inv() {
			if strings.Contains(strings.ToLower(s.Name), needle) &&
				(d.MinVersion == "" || protocol.CompareVersions(s.Version, d.MinVersion) >= 0) {
				return true, nil
			}
		}
		return false, nil
	case "script":
		r := m.d.Exec(ctx, exec.Spec{Interpreter: d.Interpreter, Content: d.Script, Timeout: detectTimeout, ScriptDir: m.d.CacheDir})
		if r.Err != nil {
			return false, r.Err
		}
		return r.ExitCode == 0 && !r.TimedOut, nil
	case "file", "registry":
		if m.d.Platform != protocol.PlatformWindows {
			return false, errors.New("la detección por fichero o registro solo existe en Windows")
		}
		r := m.d.Exec(ctx, exec.Spec{Interpreter: "powershell", Content: windowsDetectScript, Timeout: detectTimeout, ScriptDir: m.d.CacheDir,
			Env: map[string]string{"MDM_TYPE": d.Type, "MDM_PATH": d.Path, "MDM_KEY": d.RegistryKey, "MDM_VALUE": d.RegistryValue, "MDM_MIN": d.MinVersion}})
		if r.Err != nil {
			return false, r.Err
		}
		if r.ExitCode == 0 {
			return true, nil
		}
		if r.ExitCode == 1 {
			return false, nil
		}
		return false, fmt.Errorf("la detección falló: %s", strings.TrimSpace(r.Stderr))
	}
	return false, fmt.Errorf("tipo de detección desconocido: %s", d.Type)
}

// windowsDetectScript termina con 0 si se detecta, 1 si no y 2 si hay un error.
const windowsDetectScript = `$ErrorActionPreference = 'Stop'
try {
    $path = [Environment]::ExpandEnvironmentVariables($env:MDM_PATH)
    if ($env:MDM_TYPE -eq 'file') {
        if (-not (Test-Path -LiteralPath $path)) { exit 1 }
        if ($env:MDM_MIN) {
            $v = (Get-Item -LiteralPath $path).VersionInfo.FileVersion
            if (-not $v -or ([version]($v -replace '[^0-9.].*$','')) -lt [version]$env:MDM_MIN) { exit 1 }
        }
        exit 0
    }
    $key = $env:MDM_KEY -replace '^HKEY_LOCAL_MACHINE', 'HKLM:' -replace '^HKLM\\', 'HKLM:\'
    if (-not (Test-Path -LiteralPath $key)) { exit 1 }
    if ($env:MDM_VALUE) {
        $item = Get-ItemProperty -LiteralPath $key -Name $env:MDM_VALUE -ErrorAction SilentlyContinue
        if ($null -eq $item) { exit 1 }
        if ($env:MDM_MIN -and ([version]$item.($env:MDM_VALUE)) -lt [version]$env:MDM_MIN) { exit 1 }
    }
    exit 0
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 2
}
`
