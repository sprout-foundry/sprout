//go:build windows
// +build windows

package console

import (
	"os"
)

// signalsToCapture returns the list of signals to capture for cleanup on Windows.
// Windows supports os.Interrupt for Ctrl+C; other POSIX signals are not applicable.
func signalsToCapture() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// extraInterruptSignals returns additional interrupt signals for controller.Init on Windows.
func extraInterruptSignals() []os.Signal {
	return []os.Signal{}
}

// resizeSignal returns nil on Windows since SIGWINCH is not available.
func resizeSignal() os.Signal { return nil }

// reRaiseSignal cannot re-raise POSIX signals on Windows; exit after cleanup.
func reRaiseSignal(sig os.Signal) { os.Exit(0) }

// suspendTerminal is a no-op on Windows since SIGTSTP is not available.
func suspendTerminal() {
	// Terminal suspension is not supported on Windows
	// This function intentionally does nothing
}

// prepareSuspension is a no-op on Windows since SIGCONT is not available.
func prepareSuspension() (notifyResume func(), resume <-chan os.Signal) {
	return func() {}, nil
}
