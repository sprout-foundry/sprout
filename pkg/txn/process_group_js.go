//go:build js

package txn

import "os/exec"

// setTxnProcessGroup is a no-op under GOOS=js: the WASM shell module is the
// browser-side editing plane and never executes commands — that is exactly
// the split ETH-2 draws (browser edits, container executes).
func setTxnProcessGroup(cmd *exec.Cmd) {}

// killTxnProcessGroup is unreachable under GOOS=js.
func killTxnProcessGroup(cmd *exec.Cmd) {}
