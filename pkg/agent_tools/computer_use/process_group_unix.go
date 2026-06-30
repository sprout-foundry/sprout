//go:build unix

package computer_use

import (
	"os"
	"os/exec"
	"syscall"
)

// SetProcessGroup makes cmd start in a new process group so that later
// signals can target the whole group (the process and any children it forks).
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroup sends SIGKILL to the process group rooted at p so any
// children the process forked are killed alongside it.
func KillProcessGroup(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
