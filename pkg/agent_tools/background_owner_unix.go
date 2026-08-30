//go:build unix && !js

package tools

import (
	"errors"
	"os"
	"syscall"
)

// ownerProcessAlive reports whether the sprout process that owns a
// background session is still running. Orphan cleanup uses this to
// distinguish true orphans (owner dead) from live sessions that another
// concurrently-running sprout process (test binaries, subagents, CLI
// invocations sharing the config dir) still owns — killing those was the
// "exit code -1 + output file vanished" bug.
func ownerProcessAlive(ownerPID int) bool {
	if ownerPID <= 0 {
		return false
	}
	proc, err := os.FindProcess(ownerPID)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return true
	case errors.Is(err, os.ErrProcessDone):
		return false
	case errors.Is(err, syscall.ESRCH):
		return false
	default:
		// EPERM and friends: the process exists but belongs to another
		// user. Assume alive — the conservative direction for cleanup.
		return true
	}
}
