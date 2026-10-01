//go:build windows && !js

package tools

// attachProcessToJobAndGetHandle is called after cmd.Start() on Windows
// to assign the process to a Job Object. Returns the Job handle (0 on
// error). Build-tagged to windows; non-Windows callers get the no-op
// stub in background_process_job_other.go.
//
// SP-112-1.
func attachProcessToJobAndGetHandle(pid int) uintptr {
	if pid <= 0 {
		return 0
	}
	h, err := AttachProcessToJob(pid)
	if err != nil {
		return 0
	}
	return uintptr(h)
}

// closeJobHandleOnProcessExit is called in the monitor goroutine to
// close the Job Object handle when the process exits. This ensures any
// remaining descendants are killed (defensive cleanup). On Windows,
// closing a Job Object handle whose processes have all exited is a
// no-op for the kernel; closing it while descendants are still alive
// (because KILL_ON_JOB_CLOSE is set) terminates them.
//
// The handle is closed only through the registry's LoadAndDelete claim:
// jobHandle is the same handle stored there, so closing it directly as
// well double-closed it — and if killProcessGroup already claimed it, the
// stale value could close an unrelated handle that reused the number.
//
// SP-112-1.
func closeJobHandleOnProcessExit(jobHandle uintptr, pid int) {
	if jobHandle == 0 {
		return
	}
	CloseJobForPID(pid)
}
