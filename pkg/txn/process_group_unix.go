//go:build !js && !windows

package txn

import (
	"os/exec"
	"syscall"
)

// setTxnProcessGroup puts the command in its own process group (pgid ==
// pid) so a timeout can kill the WHOLE tree — the shell and every compiler
// child it spawned — with one signal.
//
// Setpgid (not Setsid): the group must stay killable from this process.
// Setsid detaches into a new session, which is what cmd/automate wants for
// a surviving background workflow — the opposite of what a bounded run
// wants. See cmd/automate_process_group.go for that variant.
func setTxnProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killTxnProcessGroup SIGKILLs the negative pgid, which the kernel applies
// to every member of the group. A stale pid cannot be reused in the window
// between the child's exit and this call: the group is not reaped until
// cmd.Wait returns, so the pid stays allocated to a dead-but-unreaped
// member (SIGKILL on it is a harmless no-op).
func killTxnProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
