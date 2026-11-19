//go:build linux

package inventory

import "syscall"

// New devuelve el recolector de inventario de Linux.
func New() Collector {
	c := newLinux("/", execRunner)
	c.statfs = func(path string) (uint64, uint64, error) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			return 0, 0, err
		}
		bs := uint64(st.Bsize)
		return st.Blocks * bs, st.Bavail * bs, nil
	}
	return c
}
