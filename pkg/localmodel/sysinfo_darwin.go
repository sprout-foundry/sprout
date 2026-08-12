//go:build darwin

package localmodel

import "golang.org/x/sys/unix"

func tensorTotalSystemRAM() uint64 {
	mem, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return mem
}
