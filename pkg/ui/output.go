package ui

import (
	"fmt"
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
