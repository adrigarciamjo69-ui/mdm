package apps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"mdm/internal/protocol"
)

// LocateWinget busca winget.exe del paquete App Installer instalado para todo
// el sistema (el agente se ejecuta como SYSTEM y no tiene el alias del
// usuario). Si hay varias versiones, elige la más alta de x64.
func LocateWinget(programFiles string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(programFiles, "WindowsApps", "Microsoft.DesktopAppInstaller_*_x64__8wekyb3d8bbwe", "winget.exe"))
	best, bestVer := "", ""
	for _, m := range matches {
		dir := filepath.Base(filepath.Dir(m))
		ver := strings.TrimSuffix(strings.TrimPrefix(dir, "Microsoft.DesktopAppInstaller_"), "_x64__8wekyb3d8bbwe")
		if _, err := os.Stat(m); err == nil && (best == "" || protocol.CompareVersions(ver, bestVer) > 0) {
			best, bestVer = m, ver
		}
	}
	if best == "" {
		return "", errors.New("winget no está instalado para todo el equipo (habitual en Windows Server)")
	}
	return best, nil
}

// packageCommand construye la orden de un gestor de paquetes.
func packageCommand(source, intent, pkg string, keepUpdated bool, winget string) string {
	switch source {
	case "apt":
		switch {
		case intent == "uninstall":
			return "apt-get remove -y " + pkg
		case keepUpdated:
			return "apt-get install -y --only-upgrade " + pkg
		}
		return "apt-get install -y " + pkg
	case "dnf":
		switch {
		case intent == "uninstall":
			return "dnf remove -y " + pkg
		case keepUpdated:
			return "dnf upgrade -y " + pkg
		}
		return "dnf install -y " + pkg
	case "winget":
		w := `"` + winget + `"`
		switch {
		case intent == "uninstall":
			return w + " uninstall --id " + pkg + " --exact --silent --disable-interactivity"
		case keepUpdated:
			return w + " upgrade --id " + pkg + " --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity"
		}
		return w + " install --id " + pkg + " --exact --silent --scope machine --accept-package-agreements --accept-source-agreements --disable-interactivity"
	}
	return ""
}
