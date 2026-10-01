//go:build windows && !js

// Package console provides Windows console utilities shared between
// pkg/automate (StopProcess escalation) and pkg/agent_tools
// (interruptProcessGroup). Both paths need to send CTRL_BREAK_EVENT
// for graceful shutdown before falling back to TerminateProcess.
//
// SP-112-2: extracted from pkg/automate/stop_process_windows.go to
// eliminate duplication. The Windows API call (GenerateConsoleCtrlEvent)
// lives here as the single canonical implementation.
package console

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// SendCtrlBreak sends a CTRL_BREAK_EVENT to the process group led by pid.
// This is the Windows equivalent of SIGINT — it signals console applications
// to shut down gracefully.
//
// GenerateConsoleCtrlEvent addresses process groups, not processes: pid 0,
// or the PID of a process that is not a group leader, broadcasts the event
// to every process attached to our console — sprout itself included. Only
// call this for children started via SetNewProcessGroup.
//
// Caller should treat the error as non-fatal: it's a signal that the
// graceful path is unavailable and a forceful TerminateProcess fallback
// should be used. See pkg/automate.StopProcess for the canonical
// graceful-then-forceful escalation pattern.
func SendCtrlBreak(pid int) error {
	if pid <= 0 {
		return errors.New("console: refusing to broadcast CTRL_BREAK_EVENT to the whole console")
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid))
}

// SetNewProcessGroup starts cmd as the leader of a new console process
// group so SendCtrlBreak(cmd.Process.Pid) reaches only that child's tree.
func SetNewProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}
