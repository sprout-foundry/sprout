package console

import (
	"os"
	"syscall"

	"golang.org/x/term"
)

// openDevTtyForSize opens /dev/tty for direct size probing. Returns
// an *os.File on success. Used as a fallback when no global status
// footer is registered and we still need cols/rows for the CLI-D
// tooltip positioning. Errors are returned for the caller to handle —
// we don't synthesize a fake fd.
func openDevTtyForSize() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDONLY, 0)
}

// readTermSize returns (cols, rows) for fd, or (0, 0) on error.
func readTermSize(f *os.File) (int, int) {
	if f == nil {
		return 0, 0
	}
	c, r, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0, 0
	}
	return c, r
}

// closeDevTty closes a fd returned by openDevTtyForSize. Errors are
// ignored — /dev/tty close failures are not actionable for callers.
func closeDevTty(f *os.File) {
	if f == nil {
		return
	}
	_ = f.Close()
	_ = syscall.Fsync(int(f.Fd())) // best-effort
}