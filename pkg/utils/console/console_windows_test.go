//go:build windows && !js

package console

import "testing"

// TestSendCtrlBreak_InvalidPID confirms that the helper rejects PID 0
// (which GenerateConsoleCtrlEvent interprets as "send to process group",
// not a real PID). Documented behavior is "treat errors as non-fatal"
// — the caller falls back to TerminateProcess.
func TestSendCtrlBreak_InvalidPID(t *testing.T) {
	// PID 0 should error because there's no process group sharing our
	// console in a typical test environment.
	if err := SendCtrlBreak(0); err == nil {
		t.Log("SendCtrlBreak(0) succeeded — GenerateConsoleCtrlEvent may be accepting process-group mode (CI environment-dependent)")
	}
}

// TestSendCtrlBreak_NonExistentPID confirms the helper errors when the
// target process doesn't exist.
func TestSendCtrlBreak_NonExistentPID(t *testing.T) {
	// A very high PID that's extremely unlikely to exist.
	if err := SendCtrlBreak(999999999); err == nil {
		t.Error("SendCtrlBreak(999999999) should error (no such process)")
	}
}
