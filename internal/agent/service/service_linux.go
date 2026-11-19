//go:build linux

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// Name es el nombre del servicio en systemd.
const Name = "mdm-agent"

const unitPath = "/etc/systemd/system/" + Name + ".service"

// BinaryPath es la ubicación definitiva del ejecutable del agente.
func BinaryPath() string { return "/usr/local/bin/mdm-agent" }

// IsAdmin informa de si el proceso tiene privilegios de administrador.
func IsAdmin() bool { return os.Geteuid() == 0 }

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install escribe la unidad, la habilita en el arranque y (re)inicia el
// servicio, de modo que una reinscripción arranca con la configuración nueva.
func Install(binPath string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("este sistema no usa systemd: el agente necesita systemd para ejecutarse como servicio")
	}
	if err := os.WriteFile(unitPath, []byte(unitFile(binPath)), 0o644); err != nil {
		return fmt.Errorf("escribir %s: %w", unitPath, err)
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := systemctl("enable", Name); err != nil {
		return err
	}
	return systemctl("restart", Name)
}

// Stop detiene el servicio si existe. Los errores se ignoran: se usa antes de
// sustituir el ejecutable y el servicio puede no existir todavía.
func Stop() {
	if _, err := os.Stat(unitPath); err == nil {
		_ = systemctl("stop", Name)
	}
}

// Uninstall detiene y elimina el servicio. No falla si ya no existe.
func Uninstall() error {
	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	_ = systemctl("disable", "--now", Name) // puede no estar cargado; lo importante es quitar la unidad
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("borrar %s: %w", unitPath, err)
	}
	return systemctl("daemon-reload")
}

// Run ejecuta el agente hasta recibir SIGTERM o SIGINT.
func Run(run func(ctx context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return run(ctx)
}
