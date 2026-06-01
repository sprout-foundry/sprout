//go:build (aix || linux || solaris || zos) && !js

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
func CaptureGroundTruth() *GroundTruthTermios {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil
	}
	return &GroundTruthTermios{fd: fd, termios: *t}
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
	return unix.IoctlSetTermios(g.fd, unix.TCGETS, &saved)
}

// IsTerminalSane checks whether the terminal is currently in an
// ICANON-on state (i.e. cooked mode). Returns false when the terminal
// appears stuck in raw / steer mode. Returns true on non-TTY (nothing
// to check) or when the terminal is healthy.
func (g *GroundTruthTermios) IsTerminalSane() bool {
	if g == nil {
		return true
	}
	t, err := unix.IoctlGetTermios(g.fd, unix.TCGETS)
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

// Fd returns the file descriptor the ground truth was captured from.
func (g *GroundTruthTermios) Fd() int {
	if g == nil {
		return -1
	}
	return g.fd
}
