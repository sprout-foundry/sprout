//go:build !windows
// +build !windows

package console

import (
	"os"
	"os/signal"
	"syscall"
)

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
