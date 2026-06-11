//go:build windows && !js

package automate

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// StopProcess escalates signals to gracefully (then forcefully) stop a process.
// On Windows it sends CTRL_BREAK_EVENT, waits, then terminates the process.
func StopProcess(pid int) (bool, error) {
	if pid <= 0 {
		return true, nil
	}

	// Try graceful shutdown via CTRL_BREAK_EVENT. This only works if the
	// target process shares our console. If it fails we fall through to
	// TerminateProcess.
	_ = sendCtrlBreak(pid)
	if !waitForDeath(pid, 10*time.Second) {
		// Forceful termination via TerminateProcess.
		process, err := os.FindProcess(pid)
		if err != nil {
			return false, err
		}
		if err := process.Kill(); err != nil {
			if !IsProcessAlive(pid) {
				return true, nil
			}
			return false, err
		}
		waitForDeath(pid, 5*time.Second)
	}

	return !IsProcessAlive(pid), nil
}

// sendCtrlBreak sends a CTRL_BREAK_EVENT to the process with the given PID.
// This is the Windows equivalent of SIGINT — it signals console applications
// to shut down gracefully. Returns an error if the event could not be sent
// (e.g. the process doesn't share our console).
func sendCtrlBreak(pid int) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid))
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
