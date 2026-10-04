//go:build windows

package verify

import (
	"bytes"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// devProcess owns one launched dev-server process and the Job Object that
// holds it plus every descendant (the Windows analogue of a process group).
// Killing only the shell would leave children holding the output pipes open,
// and cmd.Wait would block until they exit on their own.
type devProcess struct {
	cmd *exec.Cmd
	job windows.Handle
}

// spawnDevProcess starts command through the platform shell in root, capturing
// combined output to out, and assigns the started shell to a Job Object so
// the whole tree can be terminated.
func spawnDevProcess(root, command string, out *bytes.Buffer) (*devProcess, error) {
	cmd := shellexec.Command(command) //nolint:gosec // G204: the dev command is trusted starter-manifest configuration (SP-149 149b), by design
	if root != "" {
		cmd.Dir = root
	}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start dev server %q: %w", command, err)
	}
	p := &devProcess{cmd: cmd}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return p, nil
	}
	proc, perr := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)) //nolint:gosec // G115: Windows PIDs fit in uint32
	if perr != nil {
		_ = windows.CloseHandle(job)
		return p, nil
	}
	defer func() { _ = windows.CloseHandle(proc) }()
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return p, nil
	}
	p.job = job
	return p, nil
}

// stop terminates the Job Object (the whole process tree) and falls back to
// killing the shell.
func (p *devProcess) stop() {
	if p == nil {
		return
	}
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// wait reaps the process, guaranteeing the captured output is complete.
func (p *devProcess) wait() error {
	if p == nil || p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}
