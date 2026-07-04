//go:build unix

package tools

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// isProcessGone reports whether err indicates the target process no
// longer exists. Used by interruptProcessGroup to avoid a redundant
// fallback when the process group is already gone. Lives in the Unix
// file because only the Unix interrupt path calls it.
func isProcessGone(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such process")
}

// setProcessGroup makes the command start in a new process group so that
// later signals can target the whole group (the process and any children
// it forks). Unix process-group semantics.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// detachFromSession starts the command in a new process group, detaching it
// from the parent's terminal session so SIGHUP doesn't propagate when the
// parent exits. Used for long-running background processes (automate runners
// and background shells) that must outlive the agent that spawned them.
//
// We intentionally use only Setpgid (not Setsid). Setpgid alone is sufficient:
// SIGHUP is sent to the foreground process group of the controlling terminal,
// and a child in its own process group is not the foreground group. Setsid
// would be extra isolation, but Go 1.24+ loads a seccomp filter that blocks
// the setsid(2) syscall from child processes, causing fork/exec to fail with
// EPERM. See SP-107 automation runner investigation (2026-07-02).
func detachFromSession(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// interruptProcessGroup sends SIGINT to the process group rooted at p,
// falling back to a per-process SIGINT if the group signal fails for a
// reason other than the process already being gone.
func interruptProcessGroup(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGINT); err == nil {
		return nil
	} else if isProcessGone(err) {
		return nil
	}
	return p.Signal(syscall.SIGINT)
}

// terminateProcessGroup sends SIGTERM to the process group rooted at p,
// falling back to a per-process SIGTERM if the group signal fails for a
// reason other than the process already being gone.
func terminateProcessGroup(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err == nil {
		return nil
	} else if isProcessGone(err) {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}

// killProcessGroup sends SIGKILL to the process group rooted at p so any
// children the process forked are killed alongside it.
func killProcessGroup(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
