//go:build !windows

package fetch

import "syscall"

// DiskFree devuelve los bytes libres para usuarios sin privilegios del disco de dir.
func DiskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
