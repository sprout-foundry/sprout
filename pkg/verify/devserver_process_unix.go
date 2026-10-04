//go:build !js && !windows

package verify

import (
	"bytes"
	"fmt"
	"os/exec"
	"syscall"
)

// devProcess owns one launched dev-server process (the shell that runs the
// manifest's dev command) and its process group, so a page check can stop the
// whole tree (the shell and every child it spawned) with one signal.
type devProcess struct {
	cmd *exec.Cmd
}

// spawnDevProcess starts command through the system shell ("sh -c") in root,
// capturing combined output to out, in its own process group (pgid == pid) so
// the whole tree can be signalled to stop.
func spawnDevProcess(root, command string, out *bytes.Buffer) (*devProcess, error) {
	cmd := exec.Command("sh", "-c", command) //nolint:gosec // G204: the dev command is trusted starter-manifest configuration (SP-149 149b), by design
	if root != "" {
		cmd.Dir = root
	}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start dev server %q: %w", command, err)
	}
	return &devProcess{cmd: cmd}, nil
}

// stop SIGKILLs the dev server's process group (the shell and every
// descendant) so the port is actually freed. A stale pid is harmless: the
// group is not reaped until wait returns, so the pid stays allocated to a
// dead-but-unreaped member and the signal is a no-op.
func (p *devProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// wait reaps the process, guaranteeing the captured output is complete.
func (p *devProcess) wait() error {
	if p == nil || p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}
