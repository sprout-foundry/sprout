//go:build !js

package cmd

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in a new session so it survives the parent
// process exiting and is fully detached from the parent's controlling
// terminal. Critical for automate workflows spawned from CLI mode —
// without Setsid, the child remains in the parent's session and receives
// SIGHUP when the session group is torn down.
//
// Setsid alone is sufficient — it creates a new session AND a new process
// group (pgid == pid). Do NOT also set Setpgid: Go applies SysProcAttr
// operations in the order Setsid → Setpgid (see exec_linux.go), and
// calling setpgid(2) on a process that is already a session leader
// returns EPERM.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
