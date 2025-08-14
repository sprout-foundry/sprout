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
