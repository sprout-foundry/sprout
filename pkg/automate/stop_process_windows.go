//go:build windows && !js

package automate

import (
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils/console"
)

// StopProcess stops a process gracefully when it can, then forcefully.
// CTRL_BREAK_EVENT is only sent when canCtrlBreak proves it cannot spill
// over to sprout's own console; otherwise the process is terminated.
func StopProcess(pid int) (bool, error) {
	if pid <= 0 {
		return true, nil
	}

	if canCtrlBreak(pid) && console.SendCtrlBreak(pid) == nil && waitForDeath(pid, 10*time.Second) {
		return true, nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		if !IsProcessAlive(pid) {
			return true, nil
		}
		return false, err
	}
	defer process.Release()
	if err := process.Kill(); err != nil {
		if !IsProcessAlive(pid) {
			return true, nil
		}
		return false, err
	}
	waitForDeath(pid, 5*time.Second)
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
