//go:build windows

package fetch

import "golang.org/x/sys/windows"

// DiskFree devuelve los bytes libres del disco de dir.
func DiskFree(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
