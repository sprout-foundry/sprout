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

import "golang.org/x/sys/windows"

// SendCtrlBreak sends a CTRL_BREAK_EVENT to the process with the given PID.
// This is the Windows equivalent of SIGINT — it signals console applications
// to shut down gracefully. Returns an error if the event could not be sent
// (e.g. the process doesn't share our console, or the PID is invalid).
//
// Caller should treat the error as non-fatal: it's a signal that the
// graceful path is unavailable and a forceful TerminateProcess fallback
// should be used. See pkg/automate.StopProcess for the canonical
// graceful-then-forceful escalation pattern.
func SendCtrlBreak(pid int) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid))
}
