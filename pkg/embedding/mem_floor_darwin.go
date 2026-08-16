//go:build darwin

package embedding

import (
	"os"

	"golang.org/x/sys/unix"
)

func init() {
	memAvailableFn = darwinMemAvailable
}

// darwinMemAvailable estimates free memory as the free page count times page size.
func darwinMemAvailable() (uint64, bool) {
	free, err := unix.SysctlUint32("vm.stats.vm.v_free_count")
	if err != nil {
		return 0, false
	}
	return uint64(free) * uint64(os.Getpagesize()), true
}
