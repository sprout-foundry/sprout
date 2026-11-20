//go:build unix && !js
// +build unix,!js

package console

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// resumeChan is used to signal when the process resumes after suspension
var resumeChan chan os.Signal

// signalsToCapture returns the list of signals to capture for cleanup on Unix-like systems.
func signalsToCapture() []os.Signal {
	return []os.Signal{
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGHUP,
	}
}

// extraInterruptSignals returns additional interrupt signals for controller.Init.
func extraInterruptSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM}
}

// resizeSignal returns the terminal resize signal (SIGWINCH) on Unix.
func resizeSignal() os.Signal {
	return syscall.SIGWINCH
}

// reRaiseSignal re-raises a signal so the default handler can run (Unix).
func reRaiseSignal(sig os.Signal) {
	signal.Reset(sig)
	syscall.Kill(syscall.Getpid(), sig.(syscall.Signal))
}

// prepareSuspension sets up SIGCONT notification for proper terminal restoration on resume.
// Returns a function that must be called before suspension and a channel that will receive
// the SIGCONT signal when the process is resumed.
func prepareSuspension() (notifyResume func(), resume <-chan os.Signal) {
	resumeChan = make(chan os.Signal, 1)
	return func() {
		signal.Notify(resumeChan, syscall.SIGCONT)
	}, resumeChan
}

// suspendTerminal suspends the current process using SIGTSTP (Ctrl+Z) on Unix systems.
func suspendTerminal() {
	syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
}

// ignoreTerminalSignals ignores SIGTTIN and SIGTTOU to prevent background job warnings.
//
// The ignore is meant to be sticky for the process lifetime: an interactive
// REPL that moves to the background (Ctrl+Z from the user, a `!` child that
// took the terminal's foreground group and left it orphaned) must never be
// kernel-stopped by its own termios writes or stdin reads. A previous
// implementation reset the dispositions back to default after re-entering
// raw mode, so any later orphaned-background state killed the process with
// "suspended (tty output)" at the next turn-end termios write.
func ignoreTerminalSignals() {
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
}

// resetTerminalSignals re-asserts the SIGTTIN/SIGTTOU ignore. It kept the
// name for call-site continuity but no longer restores the default
// dispositions: sprout is a terminal program for its whole lifetime and
// never wants the kernel's stop-on-background-IO job control. See
// ignoreTerminalSignals.
func resetTerminalSignals() {
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
}

// claimTerminalForeground re-asserts sprout's process group as the tty's
// foreground group when some child has left a different (or dead) group in
// charge. Interactive full-screen children (an `!vim`, an interactive zsh a
// tool command spawned) tcsetpgrp the terminal to their own group for their
// lifetime; well-behaved ones hand it back, but a killed or buggy child can
// leave ITS group as the foreground group forever. Every later termios write
// from sprout then raises SIGTTOU (before the ignore above, that froze the
// REPL with "suspended (tty output)").
//
// Best-effort and cheap: one TIOCGPGRP, and a TIOCSPGRP only when the
// foreground group is not ours. Call at return-to-prompt seams (the same
// places ResyncTerminal runs). Non-tty / non-Unix builds are no-ops.
func claimTerminalForeground() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}
	fg, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil || fg == syscall.Getpgrp() {
		return
	}
	// Set the foreground group under an ignored SIGTTOU (the kernel lets
	// backgrounded callers through only with that disposition in place).
	// No Reset afterwards: the sticky process-lifetime ignore is exactly
	// the state we want; a Reset here would open a window where a
	// concurrently raised SIGTTOU hits the default stop disposition.
	signal.Ignore(syscall.SIGTTOU)
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, int(syscall.Getpgrp()))
}

// setNonblock sets a file descriptor to non-blocking mode on Unix.
func setNonblock(fd int, nonblock bool) error {
	return syscall.SetNonblock(fd, nonblock)
}
