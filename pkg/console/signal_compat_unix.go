//go:build !windows
// +build !windows

package console

import (
	"os"
	"os/signal"
	"syscall"
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
