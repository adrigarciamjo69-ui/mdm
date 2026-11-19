//go:build !windows

package commands

import (
	"fmt"
	"os/exec"
	"time"

	"mdm/internal/protocol"
)

// Power reinicia o apaga el equipo con aviso a los usuarios conectados.
func Power(action string, delay time.Duration, message string) error {
	flag := "-r"
	if action == protocol.CmdShutdown {
		flag = "-h"
	}
	when := "now"
	if mins := int((delay + time.Minute - 1) / time.Minute); mins > 0 {
		when = fmt.Sprintf("+%d", mins)
	}
	args := []string{flag, when}
	if message != "" {
		args = append(args, message)
	}
	if out, err := exec.Command("shutdown", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("shutdown: %w: %s", err, out)
	}
	return nil
}
