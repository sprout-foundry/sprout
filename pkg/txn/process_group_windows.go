//go:build !js && windows

package txn

import "os/exec"

// setTxnProcessGroup is a no-op on Windows: there is no process-group
// concept to opt into, so the timeout falls back to killing the shell
// process itself via killTxnProcessGroup below. The daemon runs in Linux
// containers; this file exists so the package still builds everywhere.
func setTxnProcessGroup(cmd *exec.Cmd) {}

// killTxnProcessGroup kills just the shell. Child processes may outlive it
// on Windows — documented as a platform limitation rather than papered
// over with a job-object implementation the contract has no way to test
// in this repo's CI.
func killTxnProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
