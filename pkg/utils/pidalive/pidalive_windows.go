//go:build windows && !js

package pidalive

import (
	"golang.org/x/sys/windows"
)

// IsAlive uses OpenProcess to obtain a real handle to the PID. If the
// process is not accessible (already exited, recycled, access denied),
// OpenProcess returns an error and we report not-alive. For accessible
// PIDs, we query GetExitCodeProcess: an exit code of 259 (STILL_ACTIVE)
// means the process is still running; any other code means it exited.
//
// This is the Windows-correct alternative to os.FindProcess, which
// returns a non-nil handle even for dead/recycled PIDs.
//
// Uses golang.org/x/sys/windows (modern pattern) — not the legacy
// syscall.NewLazyDLL pattern in pkg/utils/terminal_windows.go. SP-112
// platform parity spec, C2 convention note.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE
}
