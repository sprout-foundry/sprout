package console

import (
	"fmt"
	"strings"
)

// InteractiveOutputHandler handles interactive buffer mode output (Vim-like)
type InteractiveOutputHandler struct {
	buffer        *ConsoleBuffer
	terminal      TerminalManager
}

// NewInteractiveOutputHandler creates a new interactive output handler
func NewInteractiveOutputHandler(buffer *ConsoleBuffer, terminal TerminalManager) *InteractiveOutputHandler {
	return &InteractiveOutputHandler{
		buffer:   buffer,
		terminal: terminal,
	}
}

// Mode returns the output mode
func (h *InteractiveOutputHandler) Mode() OutputMode {
	return OutputModeInteractive
}

// Print writes formatted output
func (h *InteractiveOutputHandler) Print(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	h.addToBuffer(content)
}

// Println writes output with newline
func (h *InteractiveOutputHandler) Println(args ...interface{}) {
	content := fmt.Sprint(args...) + "\n"
	h.addToBuffer(content)
}

// Printf writes formatted output
func (h *InteractiveOutputHandler) Printf(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)
	h.addToBuffer(content)
}

// Write writes raw bytes
func (h *InteractiveOutputHandler) Write(p []byte) (int, error) {
	content := string(p)
	h.addToBuffer(content)
	return len(p), nil
}

// Flush ensures output is written to terminal
func (h *InteractiveOutputHandler) Flush() error {
	// In interactive mode, flushing just ensures buffer is up to date
	// Actual rendering is handled by the console component
	return nil
}

// Close cleans up resources
func (h *InteractiveOutputHandler) Close() error {
	return nil
}

// addToBuffer adds content to the console buffer and writes to terminal
func (h *InteractiveOutputHandler) addToBuffer(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	
	// Add to buffer for scrolling history
	h.buffer.AddLine(content)
	
	// Also write directly to terminal for immediate display
	// This ensures content appears at the correct cursor position
	if h.terminal != nil {
		// Use terminal's Write method for proper line handling
		h.terminal.Write([]byte(content))
	}
}