//go:build !js && !windows

package pidalive

import (
	"errors"
	"syscall"
)

// IsAlive uses syscall.Kill(pid, 0), the canonical POSIX liveness check.
// Sends no signal but performs the permission/existence check — ESRCH means
// no such process, EPERM means it exists but we can't signal it (still alive).
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but we lack permission to signal it —
	// it's still alive.
	return errors.Is(err, syscall.EPERM)
}
