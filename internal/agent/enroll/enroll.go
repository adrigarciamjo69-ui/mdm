// Package enroll inscribe el equipo en el servidor.
package enroll

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"mdm/internal/agent/client"
	"mdm/internal/agent/config"
	"mdm/internal/agent/inventory"
)

type Options struct {
	ServerURL  string
	Token      string
	Version    string
	ConfigPath string
	Collector  inventory.Collector
	// HTTPClient permite sustituir el transporte en las pruebas; nil usa el
	// cliente por defecto, que valida el certificado con el almacén del sistema.
	HTTPClient *http.Client
	// InstallService instala y arranca el servicio una vez guardada la configuración.
	InstallService func() error
}

// Códigos de salida de la orden «enroll».
const (
	ExitOK            = 0
	ExitError         = 1
	ExitTokenRejected = 2
	ExitNetwork       = 3
)

// Run inscribe el equipo: pide una credencial al servidor con el token, la
// guarda e instala el servicio. Si algo falla no deja nada a medias: ni
// configuración ni servicio.
func Run(ctx context.Context, o Options) error {
	if strings.TrimSpace(o.Token) == "" {
		return errors.New("falta el token de inscripción (--token)")
	}
	c, err := client.New(o.ServerURL, "", o.HTTPClient)
	if err != nil {
		return err
	}
	c.Version = o.Version

	req := o.Collector.Identify(ctx)
	req.Token = strings.TrimSpace(o.Token)
	req.AgentVersion = o.Version
	if req.HardwareID == "" {
		return errors.New("no se ha podido obtener un identificador de hardware de este equipo")
	}

	resp, err := c.Enroll(ctx, req)
	var tmp *client.TemporaryError
	switch {
	case errors.Is(err, client.ErrTokenRejected):
		return explain(err, "el servidor ha rechazado el token: no es válido, ha caducado o ya no admite más equipos. Pide uno nuevo en la consola.")
	case errors.As(err, &tmp):
		return explain(err, fmt.Sprintf("no se ha podido contactar con %s (%v). Comprueba la dirección, la red y el certificado, y vuelve a intentarlo.", o.ServerURL, tmp.Err))
	case err != nil:
		return fmt.Errorf("la inscripción ha fallado: %w", err)
	}

	cfg := config.Config{
		ServerURL:          strings.TrimRight(strings.TrimSpace(o.ServerURL), "/"),
		DeviceID:           resp.DeviceID,
		Credential:         resp.Credential,
		CheckinIntervalS:   resp.CheckinIntervalS,
		InventoryIntervalS: resp.InventoryIntervalS,
	}
	if err := config.Save(o.ConfigPath, cfg); err != nil {
		return fmt.Errorf("el equipo se ha inscrito pero no se ha podido guardar la configuración: %w", err)
	}
	if err := o.InstallService(); err != nil {
		_ = os.Remove(o.ConfigPath)
		return fmt.Errorf("no se ha podido instalar el servicio: %w", err)
	}
	return nil
}

// ExitCode traduce el error de Run al código de salida de la orden.
func ExitCode(err error) int {
	var tmp *client.TemporaryError
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, client.ErrTokenRejected):
		return ExitTokenRejected
	case errors.As(err, &tmp):
		return ExitNetwork
	}
	return ExitError
}

// explained sustituye el texto de un error por una explicación para la
// persona que instala, conservando el error original para errors.Is y errors.As.
type explained struct {
	msg string
	err error
}

func (e *explained) Error() string { return e.msg }
func (e *explained) Unwrap() error { return e.err }

func explain(err error, msg string) error { return &explained{msg: msg, err: err} }
