//go:build !windows || js

package tools

// attachProcessToJobAndGetHandle is a no-op on non-Windows platforms.
// SP-112-1.
func attachProcessToJobAndGetHandle(pid int) uintptr {
	return 0
}

// closeJobHandleOnProcessExit is a no-op on non-Windows platforms.
// SP-112-1.
func closeJobHandleOnProcessExit(jobHandle uintptr, pid int) {
}
