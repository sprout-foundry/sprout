//go:build windows && !js

package tools

import (
	"golang.org/x/sys/windows"
)

// stillActive is the Win32 STILL_ACTIVE sentinel (0x00000103).
// golang.org/x/sys/windows does not export it, so it's defined here.
const stillActive = 259

// ownerProcessAlive reports whether the sprout process that owns a
// background session is still running. Orphan cleanup uses this to
// distinguish true orphans (owner dead) from live sessions another
// concurrently-running sprout process still owns. See the Unix variant
// in background_owner_unix.go for the rationale.
func ownerProcessAlive(ownerPID int) bool {
	if ownerPID <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(ownerPID))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	// A still-running process reports the STILL_ACTIVE sentinel rather
	// than a real exit code.
	return exitCode == stillActive
}
