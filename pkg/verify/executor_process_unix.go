//go:build !js && !windows

package verify

import (
	"os/exec"
	"syscall"
)

// bindProcessGroup runs cmd in its own process group and makes context
// cancellation kill the whole group. Killing only the shell is not enough:
// a shell that does not exec its last command (dash, the default sh on
// Debian and Ubuntu) leaves the child running with the output pipe open,
// and Wait blocks until that child exits on its own.
func bindProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
