//go:build unix

package tools

import (
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var (
	setsidShellOnce sync.Once
	setsidForShell  bool
)

// sighupIgnored is a sync.Once that ignores SIGHUP for the parent process
// when the Setpgid fallback path is used. The ignore disposition is inherited
// by child processes at fork time, so children also ignore SIGHUP (same
// effect as nohup). Once called, the disposition persists for the lifetime
// of the process.
var sighupIgnored sync.Once

// probeSetsidSupport checks whether setsid(2) is available for child
// processes started through the user's shell. Go 1.24+ on some Linux
// configurations applies a seccomp filter that blocks setsid(2) from
// child processes, causing fork/exec to fail with EPERM. The filter
// is per-binary in some profiles, so we probe with the actual shell
// binary that background processes use.
//
// IMPORTANT: Setsid and Setpgid must NOT be used together. setsid(2)
// creates a new session AND makes the calling process a process group
// leader (pgid == pid). A subsequent setpgid(0, 0) on a session leader
// fails with EPERM. When Setsid is available, use it alone.
//
// We probe once at first use and cache the result so every subsequent
// background spawn skips the probe.
func probeSetsidSupport() bool {
	setsidShellOnce.Do(func() {
		shell := resolveShell()
		cmd := exec.Command(shell, "-c", "exit 0")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		setsidForShell = (cmd.Run() == nil)
	})
	return setsidForShell
}

// resolveShell returns the user's configured shell, falling back to /bin/sh.
func resolveShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

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

// detachFromSession fully detaches the command from the parent's session
// so it survives the parent's terminal teardown without needing `nohup`.
//
// When the kernel allows it (most systems), we create a NEW session via
// Setsid. This gives full session isolation: the child is in its own
// session and process group, it has no controlling terminal, and SIGHUP
// from the parent's terminal teardown or session leader exit never
// reaches it. Setsid alone is sufficient — it already places the child
// in a new process group (pgid == pid), so Setpgid is redundant and
// would actually fail with EPERM when called after Setsid (you can't
// change the process group ID of a session leader).
//
// On Go 1.24+ Linux configurations where seccomp blocks setsid(2) from
// child processes (causing fork/exec to fail with EPERM), we fall back
// to Setpgid only — the same isolation as earlier versions. The
// fallback is safe because the caller also:
//   1. Closes stdin (cmd.Stdin = nil → /dev/null in Go 1.20+)
//   2. Redirects stdout/stderr to a file (not the parent's terminal)
//   3. Ignores SIGHUP in the parent before fork so the child inherits
//      the ignore disposition (same effect as nohup). The ignore is
//      applied once via sync.Once; once set, all future children also
//      inherit SIG_IGN.
func detachFromSession(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if probeSetsidSupport() {
		// Setsid alone creates a new session AND a new process group
		// (pgid == pid). Do NOT also set Setpgid — calling setpgid
		// on a session leader returns EPERM.
		cmd.SysProcAttr.Setsid = true
	} else {
		// Fall back to a new process group only. The child stays in
		// the parent's session, so it can still receive SIGHUP from
		// terminal teardown. Mitigate by ignoring SIGHUP in the parent
		// before fork — the child inherits the SIG_IGN disposition
		// (same mechanism as nohup). Use sync.Once so the ignore is
		// applied only once; the disposition persists for all future
		// children.
		cmd.SysProcAttr.Setpgid = true
		sighupIgnored.Do(func() {
			signal.Ignore(syscall.SIGHUP)
		})
	}
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
