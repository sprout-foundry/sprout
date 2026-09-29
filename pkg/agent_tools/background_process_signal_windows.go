//go:build windows && !js

package tools

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"github.com/sprout-foundry/sprout/pkg/utils/console"
	"golang.org/x/sys/windows"
)

// jobRegistry maps a PID to the Job Object handle that owns it. When we
// kill the Job (by closing the handle with the KILL_ON_JOB_CLOSE limit
// set), all processes assigned to the Job are terminated atomically by
// the kernel — that's how descendants get cleaned up.
//
// Entries are inserted by AttachProcessToJob after cmd.Start() returns,
// and removed by killProcessGroup or when the process exits. Use a
// sync.Map for safe concurrent access from the spawn path and the
// cleanup path.
var jobRegistry sync.Map // key: int (pid), value: windows.Handle

// setProcessGroup starts the child as the leader of a new console
// process group — the Windows analogue of Setpgid. Without it the child's
// PID is not a valid CTRL_BREAK_EVENT target and interruptProcessGroup
// would broadcast the event to sprout's own console. Descendant cleanup
// is handled separately by the Job Object in AttachProcessToJob.
//
// SP-112-1.
func setProcessGroup(cmd *exec.Cmd) {
	console.SetNewProcessGroup(cmd)
}

// detachFromSession has no session concept to escape on Windows, but the
// child still needs its own process group so it can be interrupted
// without signalling the parent's console.
func detachFromSession(cmd *exec.Cmd) {
	console.SetNewProcessGroup(cmd)
}

// interruptProcessGroup sends a graceful shutdown request via
// CTRL_BREAK_EVENT, then falls back to forceful kill. Errors from the
// graceful path are ignored (per pkg/automate.StopProcess convention).
//
// SP-112-2.
func interruptProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	_ = console.SendCtrlBreak(p.Pid)
	return p.Kill()
}

// terminateProcessGroup is the SIGTERM-to-the-group step. Windows has no
// SIGTERM, so it closes the Job: killing only the leader would leave its
// descendants running and holding the output pipe open, which keeps
// cmd.Wait (and so Stop) blocked until the final kill escalation.
func terminateProcessGroup(p *os.Process) error {
	return killProcessGroup(p)
}

// killProcessGroup terminates the Job Object that owns the process,
// which the kernel translates to "kill every process assigned to this
// Job". This is the cascade-to-descendants semantic that Unix has via
// setpgid + kill(-pgid, ...).
//
// Uses LoadAndDelete to atomically claim the handle, so a concurrent
// closeJobHandleOnProcessExit from the monitor goroutine can't race
// with us and produce a double-CloseHandle (Windows rejects
// ERROR_INVALID_HANDLE on the second close).
func killProcessGroup(p *os.Process) error {
	if p == nil {
		return nil
	}
	if v, ok := jobRegistry.LoadAndDelete(p.Pid); ok {
		if h, ok := v.(windows.Handle); ok && h != 0 {
			if err := windows.CloseHandle(h); err != nil {
				_ = p.Kill()
			}
		}
		return nil
	}
	return p.Kill()
}

// AttachProcessToJob creates a new Job Object configured to kill all
// assigned processes when the Job handle is closed, then assigns the
// process with the given PID to the Job. Returns the Job handle so the
// caller can keep it alive for the process's lifetime.
//
// Must be called AFTER cmd.Start() has returned (so cmd.Process.Pid is
// valid). The PID is also registered in jobRegistry so killProcessGroup
// can find the handle later.
//
// Known race: between cmd.Start() and AssignProcessToJobObject, the
// child process can fork descendants that are NOT in the Job. Those
// descendants will leak. To eliminate this race, we'd need to use
// CREATE_SUSPENDED + resume-thread, which requires modifying the
// spawn flow in background_process.go — out of scope for SP-112-1.
//
// SP-112-1.
func AttachProcessToJob(pid int) (windows.Handle, error) {
	if pid <= 0 {
		return 0, nil
	}

	// Create the Job Object.
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	// Configure KILL_ON_JOB_CLOSE so closing the Job handle kills all
	// assigned processes. This is the cascade semantic.
	limitInfo := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limitInfo)),
		uint32(unsafe.Sizeof(limitInfo)),
	); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}

	// Open a process handle with the rights needed to assign it to a Job.
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	defer windows.CloseHandle(procHandle)

	// Assign the process to the Job.
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}

	// Register for killProcessGroup's lookup.
	jobRegistry.Store(pid, job)
	return job, nil
}

// CloseJobForPID removes the Job Object registration for a PID and
// closes the handle. Called by the monitor goroutine when the process
// exits to clean up the Job Object (which has no remaining processes
// to kill at that point, but we clean up for correctness).
func CloseJobForPID(pid int) {
	if h, ok := jobRegistry.LoadAndDelete(pid); ok {
		if handle, ok := h.(windows.Handle); ok && handle != 0 {
			windows.CloseHandle(handle)
		}
	}
}
