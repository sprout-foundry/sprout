//go:build linux

package localmodel

import "syscall"

func tensorTotalSystemRAM() uint64 {
	var info syscall.Statfs_t
	if err := syscall.Statfs("/", &info); err != nil {
		return 0
	}
	// Statfs doesn't give RAM, use sysinfo instead
	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err != nil {
		return 0
	}
	return uint64(si.Totalram) * uint64(si.Unit)
}
