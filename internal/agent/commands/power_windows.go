//go:build windows

package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"mdm/internal/protocol"
)

// Power reinicia o apaga el equipo con el aviso de Windows.
func Power(action string, delay time.Duration, message string) error {
	flag := "/r"
	if action == protocol.CmdShutdown {
		flag = "/s"
	}
	args := []string{flag, "/t", strconv.Itoa(int(delay.Seconds()))}
	if message != "" {
		args = append(args, "/c", message)
	}
	if out, err := exec.Command("shutdown", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("shutdown: %w: %s", err, out)
	}
	return nil
}
