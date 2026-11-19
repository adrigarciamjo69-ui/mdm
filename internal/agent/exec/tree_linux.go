//go:build !windows

package exec

import (
	osexec "os/exec"
	"syscall"
)

// tree agrupa el proceso y sus descendientes en un grupo de procesos propio
// para poder matarlos todos juntos.
type tree struct{}

func prepareTree(cmd *osexec.Cmd) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return &tree{}
}

func (t *tree) attach(*osexec.Cmd) {}

// release no hace nada en Unix: al agotarse el tiempo, Cancel ya mató el grupo.
func (t *tree) release(bool) {}
