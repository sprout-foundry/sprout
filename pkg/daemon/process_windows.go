//go:build windows && !js

package daemon

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// applyDetach gives the daemon its own hidden console and process group.
// A child that shares the caller's console dies with it: closing the
// terminal, or a Ctrl+C/Ctrl+Break there, reaches every process attached
// to that console. CREATE_NO_WINDOW (rather than DETACHED_PROCESS) keeps a
// console so the shell commands the daemon runs don't each pop a window.
func applyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
}

// ensureStdioDevNull leaves stdio nil: os/exec connects nil streams to NUL
// itself and closes its copies after Start.
func ensureStdioDevNull(cmd *exec.Cmd) error {
	cmd.Stdin = nil
	return nil
}

// electionLockPath is a sidecar of the PID file on Windows. LockFileEx
// locks bytes of the file itself, so the winner could not write its PID
// into a file it holds the election lock on (and nobody could read it).
func electionLockPath(pidFilePath string) string {
	return pidFilePath + ".lock"
}
