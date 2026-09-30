package commands

import (
	"fmt"
	"io"
	"os"
)

// outputSink gives a command the registry-provided writer. The WebUI runs
// slash commands with output captured through the registry
// (CommandRegistry.SetOutput); a command that prints to os.Stdout instead
// produces nothing in the browser. Embed it and write through out().
type outputSink struct {
	stdout io.Writer
}

func (s *outputSink) SetOutput(w io.Writer) { s.stdout = w }

func (s *outputSink) out() io.Writer {
	if s.stdout != nil {
		return s.stdout
	}
	return os.Stdout
}

// printf, println, and print write to out(). A failed write to the command's
// output has nowhere better to be reported, so the error is dropped here once
// rather than at every call site.
func (s *outputSink) printf(format string, args ...any) { _, _ = fmt.Fprintf(s.out(), format, args...) }
func (s *outputSink) println(args ...any)               { _, _ = fmt.Fprintln(s.out(), args...) }
func (s *outputSink) print(args ...any)                 { _, _ = fmt.Fprint(s.out(), args...) }
