//go:build !js

package cmd

import (
	"os/exec"
	"syscall"
)

// setProcessGroup detaches cmd into a new process group so it survives
// the parent process exiting. Critical for automate workflows spawned
// from agent tool calls — without this, the child receives SIGHUP when
// the tool call completes and the agent tears down its process tree.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
