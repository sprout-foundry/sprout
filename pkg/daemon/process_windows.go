//go:build windows && !js

package daemon

import (
	"os"
	"os/exec"
)

// applyDetach is a no-op on Windows. Windows processes are already
// independent of their parent after CreateProcess; no session
// detachment is needed.
func applyDetach(cmd *exec.Cmd) {
}

// ensureStdioDevNull sets the command's stdin/stdout/stderr to /dev/null
// (or the Windows equivalent NUL) so the detached process has no
// leaked I/O.
func ensureStdioDevNull(cmd *exec.Cmd) error {
	cmd.Stdin = nil

	if cmd.Stdout == nil {
		f, err := os.Open(os.DevNull)
		if err != nil {
			return err
		}
		cmd.Stdout = f
	}
	if cmd.Stderr == nil {
		f, err := os.Open(os.DevNull)
		if err != nil {
			return err
		}
		cmd.Stderr = f
	}
	return nil
}
