//go:build unix && !js

package daemon

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

// applyDetach configures cmd to run detached from the parent's session so
// it survives the parent process exiting. It follows the same Setsid-vs-Setpgid
// strategy as pkg/agent_tools/background_process_signal_unix.go: try Setsid
// first (full session isolation), and fall back to Setpgid + SIGHUP ignore
// when the kernel's seccomp filter blocks setsid(2) (Go 1.24+ Linux).
//
// Setsid and Setpgid are NEVER combined: setsid(2) already makes the child
// a process-group leader (pgid == pid); calling setpgid(0, 0) on a session
// leader fails with EPERM.
func applyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if probeSetsid() {
		cmd.SysProcAttr.Setsid = true
		return
	}
	// Fallback: new process group + inherit SIGHUP ignore (nohup effect).
	cmd.SysProcAttr.Setpgid = true
	ignoreSighupOnce.Do(func() {
		signal.Ignore(syscall.SIGHUP)
	})
}

var (
	setsidProbeOnce  sync.Once
	setsidSupported  bool
	ignoreSighupOnce sync.Once
)

// probeSetsid checks whether setsid(2) works for a child process. Go 1.24+
// on some Linux configurations applies a seccomp filter that blocks setsid,
// causing fork/exec to fail with EPERM. The probe runs once and caches the
// result so every subsequent background spawn skips it.
func probeSetsid() bool {
	setsidProbeOnce.Do(func() {
		// Probe with a minimal shell command — the seccomp filter is
		// per-binary in some profiles, but using the same shell the
		// actual daemon command uses is a reasonable approximation.
		probe := exec.Command("sh", "-c", "exit 0")
		probe.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		probe.Stdin = nil
		probe.Stdout = nil
		probe.Stderr = nil
		setsidSupported = (probe.Run() == nil)
	})
	return setsidSupported
}

// ensureStdioDevNull sets the command's stdin/stdout/stderr to /dev/null
// so the detached process has no controlling terminal or leaked I/O.
func ensureStdioDevNull(cmd *exec.Cmd) error {
	cmd.Stdin = nil // Go 1.20+ → /dev/null automatically

	if cmd.Stdout == nil {
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		cmd.Stdout = f
	}
	if cmd.Stderr == nil {
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		cmd.Stderr = f
	}
	return nil
}
