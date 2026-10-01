//go:build js

package txn

import "os/exec"

// The WASM shell module is the browser-side editing plane and never
// executes commands — that is exactly the split ETH-2 draws (browser
// edits, container executes). These exist so the package builds.

func txnShellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

func setTxnProcessGroup(cmd *exec.Cmd) {}

type txnProcessGroup struct{}

func trackTxnProcessGroup(cmd *exec.Cmd) *txnProcessGroup { return &txnProcessGroup{} }

func (g *txnProcessGroup) kill() {}

func (g *txnProcessGroup) release() {}
