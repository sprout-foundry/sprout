//go:build !js && windows

package txn

import (
	"os/exec"

	"golang.org/x/sys/windows"

	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"
)

// txnShellCommand runs command through Git for Windows' bash (cmd.exe when
// none is installed): there is no /bin/sh to pin on Windows.
func txnShellCommand(command string) *exec.Cmd {
	return shellexec.Command(command)
}

func setTxnProcessGroup(cmd *exec.Cmd) {}

// txnProcessGroup is a Job Object holding the shell and every descendant
// it spawns — the Windows analogue of a process group. Killing only the
// shell would leave children holding the output pipes open, and cmd.Wait
// would block until they exit on their own.
type txnProcessGroup struct {
	cmd *exec.Cmd
	job windows.Handle
}

// trackTxnProcessGroup assigns the started shell to a new Job Object.
// Descendants spawned before the assignment escape the job; the shell has
// to load before it can spawn anything, so the window is tiny.
func trackTxnProcessGroup(cmd *exec.Cmd) *txnProcessGroup {
	g := &txnProcessGroup{cmd: cmd}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return g
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return g
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return g
	}
	g.job = job
	return g
}

func (g *txnProcessGroup) kill() {
	if g.job != 0 {
		_ = windows.TerminateJobObject(g.job, 1)
	}
	_ = g.cmd.Process.Kill()
}

// release closes the job without killing its members, matching Unix where
// a background child may outlive a shell that exited normally.
func (g *txnProcessGroup) release() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}
