//go:build windows && !js

package automate

import (
	"os"
	"syscall"
	"time"
)

// StopProcess escalates signals to gracefully (then forcefully) stop a process.
// On Windows it sends SIGINT, waits, then tries to kill the process handle.
func StopProcess(pid int) (bool, error) {
	if pid <= 0 {
		return true, nil
	}

	// SIGINT
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		// Process may already be gone
		if !IsProcessAlive(pid) {
			return true, nil
		}
		return false, err
	}
	if !waitForDeath(pid, 10*time.Second) {
		// SIGTERM
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			if !IsProcessAlive(pid) {
				return true, nil
			}
			return false, err
		}
		if !waitForDeath(pid, 5*time.Second) {
			// Last resort: open and terminate
			process, err := os.FindProcess(pid)
			if err == nil {
				_ = process.Kill()
			}
			waitForDeath(pid, 2*time.Second)
		}
	}

	return !IsProcessAlive(pid), nil
}

func waitForDeath(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !IsProcessAlive(pid) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return !IsProcessAlive(pid)
}
