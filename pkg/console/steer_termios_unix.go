//go:build (aix || linux || solaris || zos) && !js

package console

import (
	"golang.org/x/sys/unix"
)

// steerTermiosState is the opaque platform-specific termios snapshot
// returned by enterSteerMode. Restore via exitSteerMode.
type steerTermiosState struct {
	saved unix.Termios
}

// enterSteerMode configures the terminal for keystroke-level input
// without disabling output post-processing — unlike term.MakeRaw which
// turns off OPOST and causes streaming text to "staircase" (LF not
// translated to CR-LF).
//
// What we change:
//   - ICANON off: reads return per-keystroke, not per-line
//   - ECHO off:   typed chars don't echo (we draw them ourselves)
//   - ISIG off:   Ctrl+C arrives as 0x03 byte, not a SIGINT signal
//   - IEXTEN off: disables platform-specific input processing
//
// What we KEEP intact (the bug that motivated this file):
//   - OPOST: output post-processing — without this, `\n` is not
//     translated to `\r\n` and every line of streaming text from the
//     LLM scrolls but the cursor never returns to column 0, producing
//     a staircase. We need this on while the steer reader is active.
//
// VMIN/VTIME are set to (0,0) so a read returns immediately if no
// byte is ready (the steer reader uses a poll loop with its own tick).
func enterSteerMode(fd int) (*steerTermiosState, error) {
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	st := &steerTermiosState{saved: *termios}

	t := *termios
	t.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	// Disable input CR-to-NL conversion so `\r` arrives as Enter
	// (0x0D) which our reader normalizes to submit.
	t.Iflag &^= unix.ICRNL
	// Leave Oflag alone — preserves OPOST so streaming output keeps
	// rendering correctly.
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &t); err != nil {
		return nil, err
	}
	return st, nil
}

// exitSteerMode restores the termios state captured by enterSteerMode.
// Safe to call with a nil state (no-op).
func exitSteerMode(fd int, st *steerTermiosState) error {
	if st == nil {
		return nil
	}
	saved := st.saved
	return unix.IoctlSetTermios(fd, unix.TCSETS, &saved)
}
