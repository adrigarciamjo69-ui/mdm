//go:build windows

package exec

import (
	osexec "os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree mete el proceso en un objeto de trabajo: cerrarlo con
// KILL_ON_JOB_CLOSE mata el proceso y todos sus descendientes.
//
// Limitación conocida: los procesos que el script cree antes de asignarlo al
// objeto (los primeros milisegundos) quedan fuera.
type tree struct {
	job windows.Handle
}

func prepareTree(cmd *osexec.Cmd) *tree {
	t := &tree{}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return t
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return t
	}
	t.job = job
	cmd.Cancel = func() error {
		if t.job != 0 {
			windows.CloseHandle(t.job) // mata todo el árbol
			t.job = 0
		}
		return cmd.Process.Kill()
	}
	return t
}

func (t *tree) attach(cmd *osexec.Cmd) {
	if t.job == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(t.job, h)
}

// release cierra el objeto de trabajo. Si el script terminó por sí solo, antes
// se quita KILL_ON_JOB_CLOSE para no matar los procesos que dejó en segundo
// plano a propósito.
func (t *tree) release(killed bool) {
	if t.job == 0 {
		return
	}
	if !killed {
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		_, _ = windows.SetInformationJobObject(t.job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	}
	windows.CloseHandle(t.job)
	t.job = 0
}
