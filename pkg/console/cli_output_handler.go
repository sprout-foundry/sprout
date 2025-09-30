package console

import (
	"fmt"
	"io"
	"os"
)

// CLIOutputHandler handles simple CLI mode output (direct to stdout)
type CLIOutputHandler struct {
	writer io.Writer
}

// NewCLIOutputHandler creates a new CLI output handler
func NewCLIOutputHandler() *CLIOutputHandler {
	return &CLIOutputHandler{
		writer: os.Stdout,
	}
}

// Mode returns the output mode
func (h *CLIOutputHandler) Mode() OutputMode {
	return OutputModeCLI
}

// Print writes formatted output
func (h *CLIOutputHandler) Print(format string, args ...interface{}) {
	if len(args) > 0 {
		fmt.Fprintf(h.writer, format, args...)
	} else {
		fmt.Fprint(h.writer, format)
	}
}

// Println writes output with newline
func (h *CLIOutputHandler) Println(args ...interface{}) {
	fmt.Fprintln(h.writer, args...)
}

// Printf writes formatted output
func (h *CLIOutputHandler) Printf(format string, args ...interface{}) {
	fmt.Fprintf(h.writer, format, args...)
}

// Write writes raw bytes
func (h *CLIOutputHandler) Write(p []byte) (int, error) {
	return h.writer.Write(p)
}

// Flush ensures output is written (no-op for CLI)
func (h *CLIOutputHandler) Flush() error {
	return nil
}

// Close cleans up resources (no-op for CLI)
func (h *CLIOutputHandler) Close() error {
	return nil
}