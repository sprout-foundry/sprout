//go:build windows && !js

package pidalive

import (
	"errors"

	"golang.org/x/sys/windows"
)

// IsAlive uses OpenProcess to obtain a real handle to the PID and then
// GetExitCodeProcess: STILL_ACTIVE (259) means the process is running.
// Unlike os.FindProcess this distinguishes running PIDs from exited ones.
//
// ERROR_ACCESS_DENIED means the PID names a live process we may not query
// (another user's, or an elevated one); OpenProcess fails with
// ERROR_INVALID_PARAMETER when no such process exists.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}
