package console

import (
	"fmt"
	"io"
)

// OutputMode represents the different output modes
//
//go:generate stringer -type=OutputMode
type OutputMode int

const (
	// OutputModeCLI is simple CLI output (direct to stdout, cooked terminal)
	OutputModeCLI OutputMode = iota
	// OutputModeInteractive is interactive buffer mode (Vim-like, raw terminal)
	OutputModeInteractive
)

// OutputHandler defines the interface for dual-mode output handling
type OutputHandler interface {
	// Mode returns the current output mode
	Mode() OutputMode

	// Print writes formatted output
	Print(format string, args ...interface{})

	// Println writes output with newline
	Println(args ...interface{})

	// Printf writes formatted output
	Printf(format string, args ...interface{})

	// Write writes raw bytes
	Write(p []byte) (n int, err error)

	// Buffer returns the console buffer (nil in CLI mode)
	Buffer() *ConsoleBuffer

	// Redraw forces a redraw of the content (only in interactive mode)
	Redraw()

	// SetMode changes the output mode
	SetMode(mode OutputMode)
}

// DualOutputHandler implements OutputHandler with dual-mode support
type DualOutputHandler struct {
	mode          OutputMode
	cliOutput     io.Writer
	consoleBuffer *ConsoleBuffer
	autoRedraw    bool // Whether to auto-redraw in interactive mode
}

// NewDualOutputHandler creates a new dual-mode output handler
func NewDualOutputHandler(cliOutput io.Writer, buffer *ConsoleBuffer) *DualOutputHandler {
	return &DualOutputHandler{
		mode:          OutputModeCLI,
		cliOutput:     cliOutput,
		consoleBuffer: buffer,
		autoRedraw:    true,
	}
}

// Mode returns the current output mode
func (doh *DualOutputHandler) Mode() OutputMode {
	return doh.mode
}

// Print writes formatted output
func (doh *DualOutputHandler) Print(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	doh.handleOutput(content)
}

// Println writes output with newline
func (doh *DualOutputHandler) Println(args ...interface{}) {
	content := fmt.Sprint(args...) + "\n"
	doh.handleOutput(content)
}

// Printf writes formatted output
func (doh *DualOutputHandler) Printf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	doh.handleOutput(content)
}

// Write writes raw bytes
func (doh *DualOutputHandler) Write(p []byte) (n int, err error) {
	content := string(p)
	doh.handleOutput(content)
	return len(p), nil
}

// Buffer returns the console buffer
func (doh *DualOutputHandler) Buffer() *ConsoleBuffer {
	return doh.consoleBuffer
}

// Redraw forces a redraw of the content
func (doh *DualOutputHandler) Redraw() {
	if doh.mode == OutputModeInteractive && doh.consoleBuffer != nil {
		// Redraw logic would be implemented by the component using this handler
		// This just provides the interface
	}
}

// SetMode changes the output mode
func (doh *DualOutputHandler) SetMode(mode OutputMode) {
	doh.mode = mode
}

// handleOutput handles output based on the current mode
func (doh *DualOutputHandler) handleOutput(content string) {
	switch doh.mode {
	case OutputModeCLI:
		// Direct CLI output
		fmt.Fprint(doh.cliOutput, content)

	case OutputModeInteractive:
		// Interactive buffer mode
		if doh.consoleBuffer != nil {
			doh.consoleBuffer.AddContent(content)
			if doh.autoRedraw {
				doh.Redraw()
			}
		} else {
			// Fallback to CLI mode if no buffer available
			fmt.Fprint(doh.cliOutput, content)
		}
	}
}

// SetAutoRedraw controls whether to auto-redraw in interactive mode
func (doh *DualOutputHandler) SetAutoRedraw(auto bool) {
	doh.autoRedraw = auto
}
