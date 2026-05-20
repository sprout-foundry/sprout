//go:build js
// +build js

package console

import "os"

// On WASM/js there are no POSIX signals. Every entry-point in this file is
// a no-op or returns a benign zero value, which matches the Windows stub.
// Console code that branches on the returned values (e.g. resizeSignal()==nil)
// already handles the absence gracefully — the browser environment doesn't
// have a terminal to resize, suspend, or detach from anyway.

func signalsToCapture() []os.Signal      { return []os.Signal{os.Interrupt} }
func extraInterruptSignals() []os.Signal { return []os.Signal{} }
func resizeSignal() os.Signal            { return nil }
func reRaiseSignal(_ os.Signal)          { os.Exit(0) }

func suspendTerminal() {}

func prepareSuspension() (notifyResume func(), resume <-chan os.Signal) {
	return func() {}, nil
}

func ignoreTerminalSignals() {}
func resetTerminalSignals()  {}

func setNonblock(_ int, _ bool) error { return nil }
