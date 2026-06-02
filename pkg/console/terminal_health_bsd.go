//go:build (darwin || freebsd || netbsd || openbsd || dragonfly) && !js

package console

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// GroundTruthTermios holds a pristine snapshot of the terminal's termios
// state taken once at REPL startup — before any raw-mode / steer-mode
// manipulation. All subsequent mode transitions should restore toward
// this snapshot (not toward whatever state the previous mode happened
// to save), preventing termios-state descent across PauseSteer /
// ResumeSteer cycles.
type GroundTruthTermios struct {
	fd      int
	termios unix.Termios
}

// CaptureGroundTruth snapshots the current termios of stdin. Call once
// at REPL startup when the terminal is in its default cooked state.
// Returns nil on non-TTY or error (callers degrade gracefully).
//
// If the captured state is not actually cooked (some bootstrap path left
// the terminal in raw mode before we got here), the snapshot is
// normalized so the cooked flags we depend on are forced on. Without
// this, every later Restore() would set ICANON-off and the recovery
// mechanism would be self-defeating.
func CaptureGroundTruth() *GroundTruthTermios {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil
	}
	normalizeCookedTermios(t)
	return &GroundTruthTermios{fd: fd, termios: *t}
}

// normalizeCookedTermios forces the flag bits that define cooked mode.
// Idempotent. Preserves baud rate, character size, control characters
// and other user-tuned settings — only flips the bits that have to be
// in a specific state for the REPL prompt's MakeRaw/Restore cycle to
// behave correctly on the way back to cooked.
func normalizeCookedTermios(t *unix.Termios) {
	t.Lflag |= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	t.Iflag |= unix.ICRNL | unix.IXON
	t.Oflag |= unix.OPOST
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
}

// Restore resets the terminal to the ground-truth state. Returns an
// error if the ioctl fails (caller can log and continue — a failed
// restore is not fatal, just means the terminal might be in a weird
// mode).
func (g *GroundTruthTermios) Restore() error {
	if g == nil {
		return nil
	}
	saved := g.termios
	return unix.IoctlSetTermios(g.fd, unix.TIOCSETA, &saved)
}

// IsTerminalSane checks whether the terminal is currently in an
// ICANON-on state (i.e. cooked mode). Returns false when the terminal
// appears stuck in raw / steer mode. Returns true on non-TTY (nothing
// to check) or when the terminal is healthy.
func (g *GroundTruthTermios) IsTerminalSane() bool {
	if g == nil {
		return true
	}
	t, err := unix.IoctlGetTermios(g.fd, unix.TIOCGETA)
	if err != nil {
		return true // can't check, assume fine
	}
	return t.Lflag&unix.ICANON != 0
}

// EnsureSane restores the ground-truth state if the terminal appears
// stuck in raw mode (ICANON off when we expect cooked). Returns true
// if a restore was performed.
func (g *GroundTruthTermios) EnsureSane() bool {
	if g == nil || g.IsTerminalSane() {
		return false
	}
	fmt.Fprintln(os.Stderr, "\r\x1b[K[terminal recovery: restoring cooked mode]")
	_ = g.Restore()
	return true
}

// EnsureCooked unconditionally writes a known-cooked termios derived
// from the ground-truth snapshot. Unlike EnsureSane, it does not first
// check whether the terminal "looks" sane — ICANON alone doesn't catch
// every way input can be broken (VMIN=0 leftover from steer mode,
// IXOFF stop, missing OPOST). Call at the top of every ReadLine to
// guarantee a clean baseline before MakeRaw saves the to-be-restored
// state.
func (g *GroundTruthTermios) EnsureCooked() {
	if g == nil {
		return
	}
	saved := g.termios
	normalizeCookedTermios(&saved)
	_ = unix.IoctlSetTermios(g.fd, unix.TIOCSETA, &saved)
}

// Fd returns the file descriptor the ground truth was captured from.
func (g *GroundTruthTermios) Fd() int {
	if g == nil {
		return -1
	}
	return g.fd
}
