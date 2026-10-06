package console

import (
	"io"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

// fdWriter is satisfied by *os.File and by wrappers that expose the
// descriptor they ultimately write to.
type fdWriter interface {
	Fd() uintptr
}

var (
	ttyCache      sync.Map
	vtUnsupported atomic.Bool
)

// isTerminalFd reports whether fd is a terminal. The answer is cached
// because the capability checks run on every colored write and a file
// descriptor does not change from terminal to non-terminal mid-process.
func isTerminalFd(fd uintptr) bool {
	if v, ok := ttyCache.Load(fd); ok {
		return v.(bool)
	}
	tty := term.IsTerminal(int(fd))
	ttyCache.Store(fd, tty)
	return tty
}

// IsTerminalWriter reports whether w writes directly to a terminal.
// Writers that do not expose a file descriptor are never terminals.
func IsTerminalWriter(w io.Writer) bool {
	f, ok := w.(fdWriter)
	if !ok || f == nil {
		return false
	}
	if file, isFile := w.(*os.File); isFile && file == nil {
		return false
	}
	return isTerminalFd(f.Fd())
}

func termIsDumb() bool {
	return os.Getenv("TERM") == "dumb"
}

// ColorEnabled reports whether SGR color/style sequences should be written
// to w. NO_COLOR disables color unconditionally and FORCE_COLOR enables it
// for any writer (including buffers and pipes); otherwise color requires a
// terminal that is not TERM=dumb and, on Windows, has VT processing on.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if vtUnsupported.Load() || termIsDumb() {
		return false
	}
	return IsTerminalWriter(w)
}

// ColorOn is the color decision for output whose final destination is not
// known at format time (strings built before being printed, e.g. via
// Esc/Colorize). FORCE_COLOR wins outright; otherwise a common
// configuration is one stream redirected (stderr to a log) while the
// other stays a terminal, so color is enabled when either standard stream
// accepts it rather than requiring both.
func ColorOn() bool {
	return ColorEnabled(os.Stdout) || ColorEnabled(os.Stderr)
}

// SupportsCursorControl reports whether w is a terminal that interprets
// cursor movement, line erasure and scroll regions. Spinners, the status
// footer and in-place redraws must stay inert when this is false.
// NO_COLOR does not affect it; FORCE_COLOR does not fake it.
func SupportsCursorControl(w io.Writer) bool {
	if vtUnsupported.Load() || termIsDumb() {
		return false
	}
	return IsTerminalWriter(w)
}

// Interactive reports whether both stdin and w are terminals capable of
// cursor control, i.e. chrome that reads keys and redraws in place is safe.
func Interactive(w io.Writer) bool {
	return SupportsCursorControl(w) && isTerminalFd(os.Stdin.Fd())
}

// markVTUnsupported records that the console rejected virtual-terminal
// processing, which turns off color and cursor control process-wide. Called
// only from the Windows console initialiser (stdin_read_windows.go), so on
// other platforms the symbol looks unused to static analysis.
//
//nolint:unused // referenced by the windows-only build
func markVTUnsupported() {
	vtUnsupported.Store(true)
}

// SGR returns code when color is enabled for w, otherwise "".
func SGR(w io.Writer, code string) string {
	if !ColorEnabled(w) {
		return ""
	}
	return code
}
