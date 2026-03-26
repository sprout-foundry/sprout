//go:build windows

package webui

import "os"

func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Windows, FindProcess succeeds for arbitrary PIDs. Signal(0) is not
	// portable here, so treat successful process resolution as "potentially
	// alive" for host-selection purposes rather than failing the build.
	return process != nil
}
