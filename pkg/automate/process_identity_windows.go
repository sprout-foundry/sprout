//go:build windows && !js

package automate

import (
	"time"

	"golang.org/x/sys/windows"
)

// processStartedBefore reads the creation time via GetProcessTimes. Windows
// recycles PIDs aggressively, so without this a stale session file could
// point StopProcess at an unrelated process. Fails open when the process
// cannot be queried.
func processStartedBefore(pid int, cutoff time.Time) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return true
	}
	return time.Unix(0, creation.Nanoseconds()).Before(cutoff)
}
