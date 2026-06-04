//go:build windows

package tools

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows. Process-group semantics differ
// from Unix (Windows uses Job Objects); for the current desktop scope we
// accept that killing a background process won't cascade to its
// descendants. A future Windows hardening pass could wire job objects via
// golang.org/x/sys/windows so a kill propagates to the whole spawn tree.
func setProcessGroup(_ *exec.Cmd) {}

// interruptProcessGroup terminates the process. Windows has no faithful
// SIGINT analogue for arbitrary children, so this is a best-effort kill —
// the same fallback path Unix takes when its group signal fails.
func interruptProcessGroup(p *os.Process) error {
	return p.Kill()
}

// terminateProcessGroup terminates the process. Windows has no SIGTERM,
// so this falls back to a direct kill like the interrupt path.
func terminateProcessGroup(p *os.Process) error {
	return p.Kill()
}

// killProcessGroup terminates the process. See setProcessGroup re: the
// scope limit (the Process tree may have orphaned descendants).
func killProcessGroup(p *os.Process) error {
	return p.Kill()
}
