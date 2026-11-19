//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Name es el nombre interno del servicio de Windows.
const Name = "MDMAgent"

const (
	displayName = "Agente MDM"
	description = "Mantiene este equipo inscrito en la plataforma de gestión de dispositivos y envía su inventario."
	stopTimeout = 30 * time.Second
)

// BinaryPath es la ubicación definitiva del ejecutable del agente.
func BinaryPath() string {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "MDMAgent", "mdm-agent.exe")
}

// IsAdmin informa de si el proceso se ejecuta elevado.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// stopAndWait pide al servicio que se detenga y espera a que lo haga.
func stopAndWait(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == svc.Stopped {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("el servicio %s no se ha detenido en %s", Name, stopTimeout)
}

// Install crea el servicio (o actualiza el existente) con inicio automático,
// cuenta LocalSystem y reinicio a los 10 s si falla, y lo arranca.
func Install(binPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar con el Administrador de control de servicios: %w", err)
	}
	defer m.Disconnect()

	cfg := mgr.Config{DisplayName: displayName, Description: description, StartType: mgr.StartAutomatic}
	s, err := m.OpenService(Name)
	if err == nil {
		// Ya existe (reinscripción o actualización): se detiene y se reconfigura.
		if err := stopAndWait(s); err != nil {
			s.Close()
			return err
		}
		current, err := s.Config()
		if err != nil {
			s.Close()
			return err
		}
		current.BinaryPathName = `"` + binPath + `" run`
		current.DisplayName, current.Description, current.StartType = cfg.DisplayName, cfg.Description, cfg.StartType
		if err := s.UpdateConfig(current); err != nil {
			s.Close()
			return fmt.Errorf("actualizar el servicio: %w", err)
		}
	} else {
		s, err = m.CreateService(Name, binPath, cfg, "run")
		if err != nil {
			return fmt.Errorf("crear el servicio: %w", err)
		}
	}
	defer s.Close()

	restart := mgr.RecoveryAction{Type: mgr.ServiceRestart, Delay: 10 * time.Second}
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{restart, restart, restart}, 86400); err != nil {
		return fmt.Errorf("configurar la recuperación del servicio: %w", err)
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("arrancar el servicio: %w", err)
	}
	return nil
}

// Stop detiene el servicio si existe; los errores se ignoran.
func Stop() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return
	}
	defer s.Close()
	_ = stopAndWait(s)
}

// Uninstall detiene y elimina el servicio. No falla si ya no existe.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("conectar con el Administrador de control de servicios: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		return err
	}
	defer s.Close()
	if err := stopAndWait(s); err != nil {
		return err
	}
	if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("eliminar el servicio: %w", err)
	}
	return nil
}

type handler struct {
	run func(ctx context.Context) error
}

// Execute atiende al Administrador de control de servicios: arranca el agente
// y lo detiene con orden cuando llega Stop o el apagado del equipo.
func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return false, 1
			}
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		}
	}
}

// Run ejecuta el agente bajo el gestor de servicios o, si se lanza a mano
// desde una consola, hasta Ctrl+C.
func Run(run func(ctx context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isService {
		return svc.Run(Name, &handler{run: run})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx)
}
