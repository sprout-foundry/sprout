package ui

import (
	"fmt"
	"os"
)

// OutputSink abstracts where messages go (stdout vs TUI collector).
type OutputSink interface {
	Print(text string)
	Printf(format string, args ...any)
}

// StdoutSink writes directly to standard output.
type StdoutSink struct{}

func (StdoutSink) Print(text string)                 { fmt.Print(text) }
func (StdoutSink) Printf(format string, args ...any) { fmt.Printf(format, args...) }

// TuiSink publishes content to the UI event stream.
type TuiSink struct{}

func (TuiSink) Print(text string)                 { Log(text) }
func (TuiSink) Printf(format string, args ...any) { Logf(format, args...) }

// Default sink selection
var defaultSink OutputSink = StdoutSink{}

// SetDefaultSink sets the global default OutputSink.
func SetDefaultSink(s OutputSink) { defaultSink = s }

// Out returns the current default output sink.
func Out() OutputSink { return defaultSink }

// UseStdoutSink switches default output back to stdout.
func UseStdoutSink() { defaultSink = StdoutSink{} }

// IsUIActive returns true if we're currently using TUI sink (UI is active)
func IsUIActive() bool {
	_, isTui := defaultSink.(TuiSink)
	return isTui
}

// Global UI enabled state
var uiEnabled bool = false

// SetEnabled sets whether the UI is enabled
func SetEnabled(enabled bool) {
	uiEnabled = enabled
	if enabled {
		SetDefaultSink(TuiSink{})
	} else {
		UseStdoutSink()
	}
}

// Enabled returns true if UI is enabled
func Enabled() bool {
	return uiEnabled
}

// FromEnv checks if UI should be enabled based on environment variables
func FromEnv() bool {
	return os.Getenv("LEDIT_UI") == "1" || os.Getenv("LEDIT_UI") == "true"
}

// PrintContext prints text only when appropriate for the current context
// - In UI mode: only prints to logs if forceInUI is true
// - In console mode: always prints to stdout
func PrintContext(text string, forceInUI bool) {
	if IsUIActive() {
		if forceInUI {
			Log(text)
		}
		// Otherwise suppress output in UI mode
	} else {
		fmt.Print(text)
	}
}

// PrintfContext formats and prints text only when appropriate for the current context
func PrintfContext(forceInUI bool, format string, args ...any) {
	if IsUIActive() {
		if forceInUI {
			Logf(format, args...)
		}
		// Otherwise suppress output in UI mode
	} else {
		fmt.Printf(format, args...)
	}
}
