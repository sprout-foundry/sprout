//go:build !unix

package computer_use

import (
	"os"
	"os/exec"
)

// SetProcessGroup is a no-op on non-Unix platforms (Windows, JS/WASM).
// The computer-use toolchain (xdotool/cliclick) requires Unix-like process
// semantics, so this no-op is effectively dead code in production. It exists
// only so that the build compiles for non-Unix GOOSes (e.g. js/wasm used by
// the WebUI's WASM shell module).
func SetProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroup is a no-op on non-Unix platforms. Returns nil so callers
// that don't check the error continue to work. Real process-group kill
// semantics require Unix process group syscalls.
func KillProcessGroup(p *os.Process) error {
	return nil
}
