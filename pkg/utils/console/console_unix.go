//go:build !js && !windows

// Package console provides Windows console utilities shared between
// pkg/automate (StopProcess escalation) and pkg/agent_tools
// (interruptProcessGroup). On Unix, this package is a stub — Unix builds
// use syscall.Kill directly for SIGINT and don't need GenerateConsoleCtrlEvent.
package console

// SendCtrlBreak is a stub on Unix. The Windows implementation uses
// GenerateConsoleCtrlEvent which has no Unix equivalent. Unix code paths
// should use syscall.Kill(pid, syscall.SIGINT) directly.
func SendCtrlBreak(_ int) error {
	return nil
}
