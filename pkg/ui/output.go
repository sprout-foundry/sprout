package ui

import (
	"fmt"
)

// OutputSink abstracts where messages go
type OutputSink interface {
	Print(text string)
	Printf(format string, args ...any)
}

// StdoutSink writes directly to standard output.
type StdoutSink struct{}

func (StdoutSink) Print(text string)                 { fmt.Print(text) }
func (StdoutSink) Printf(format string, args ...any) { fmt.Printf(format, args...) }

// Default sink selection
var defaultSink OutputSink = StdoutSink{}

// SetDefaultSink sets the global default OutputSink.
func SetDefaultSink(s OutputSink) { defaultSink = s }

// Out returns the current default output sink.
func Out() OutputSink { return defaultSink }

// UseStdoutSink switches default output back to stdout.
func UseStdoutSink() { defaultSink = StdoutSink{} }

// Global UI enabled state - deprecated, always false now
var uiEnabled bool = false

// SetEnabled is deprecated - UI is removed
func SetEnabled(enabled bool) {
	// No-op, UI is removed
}

// Enabled returns false - UI is removed
func Enabled() bool {
	return false
}

// FromEnv checks if UI should be enabled based on environment variables
// Deprecated - always returns false
func FromEnv() bool {
	return false
}

// IsUIActive always returns false - UI is removed
func IsUIActive() bool {
	return false
}

// PrintContext always prints to stdout now
func PrintContext(text string, forceInUI bool) {
	fmt.Print(text)
}

// PrintfContext always prints to stdout now
func PrintfContext(forceInUI bool, format string, args ...any) {
	fmt.Printf(format, args...)
}
