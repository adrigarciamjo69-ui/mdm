//go:build !linux && !windows

package service

import (
	"context"
	"errors"
	"os"
	"os/signal"
)

// Name es el nombre del servicio. En esta plataforma no hay integración: el
// agente solo se admite como servicio en Linux y Windows.
const Name = "mdm-agent"

var errUnsupported = errors.New("esta plataforma no está admitida: el agente solo funciona como servicio en Linux y Windows")

func BinaryPath() string   { return "/usr/local/bin/mdm-agent" }
func IsAdmin() bool        { return os.Geteuid() == 0 }
func Install(string) error { return errUnsupported }
func Stop()                {}
func Uninstall() error     { return nil }

func Run(run func(ctx context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return run(ctx)
}
