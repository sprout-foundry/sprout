//go:build unix && !js

package automate

import (
	"syscall"
	"time"
)

// StopProcess escalates signals to gracefully (then forcefully) stop a process.
// It sends SIGINT, waits 10s, then SIGTERM, waits 5s, then SIGKILL, waits 2s.
// Returns true if the process is confirmed dead after escalation.
func StopProcess(pid int) (bool, error) {
	if pid <= 0 {
		return true, nil
	}

	// SIGINT — give it a chance to clean up
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		if !isErrNotFound(err) {
			return false, err
		}
		return true, nil // already gone
	}
	if !waitForDeath(pid, 10*time.Second) {
		// SIGTERM — harder stop
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			if !isErrNotFound(err) {
				return false, err
			}
			return true, nil
		}
		if !waitForDeath(pid, 5*time.Second) {
			// SIGKILL — no way out
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				if !isErrNotFound(err) {
					return false, err
				}
				return true, nil
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

func isErrNotFound(err error) bool {
	return err == syscall.ESRCH
}
