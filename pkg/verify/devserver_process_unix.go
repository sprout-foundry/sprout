//go:build !js && !windows

package verify

import (
	"bytes"
	"fmt"
	"os/exec"
	"syscall"
)

// DevProcess owns one launched dev-server process (the shell that runs the
// manifest's dev command) and its process group, so a caller can stop the
// whole tree (the shell and every child it spawned) with one signal.
// Page checks and the preview manager both drive dev servers
// through this type, so it is exported: the platform process handling
// (process groups on Unix, Job Objects on Windows, the no-op WASM stub)
// lives here and nowhere else.
type DevProcess struct {
	cmd *exec.Cmd
}

// StartDevProcess starts command through the system shell ("sh -c") in root,
// capturing combined output to out, in its own process group (pgid == pid)
// so the whole tree can be signalled to stop.
func StartDevProcess(root, command string, out *bytes.Buffer) (*DevProcess, error) {
	cmd := exec.Command("sh", "-c", command) //nolint:gosec // G204: the dev command is trusted starter-manifest configuration, by design
	if root != "" {
		cmd.Dir = root
	}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start dev server %q: %w", command, err)
	}
	return &DevProcess{cmd: cmd}, nil
}

// Stop SIGKILLs the dev server's process group (the shell and every
// descendant) so the port is actually freed. A stale pid is harmless: the
// group is not reaped until Wait returns, so the pid stays allocated to a
// dead-but-unreaped member and the signal is a no-op.
func (p *DevProcess) Stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// Wait reaps the process, guaranteeing the captured output is complete.
func (p *DevProcess) Wait() error {
	if p == nil || p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}
