package console

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func (ir *InputReader) processPendingResize(resizeCh <-chan os.Signal, parser *EscapeParser) bool {
	if resizeCh == nil {
		return false
	}

	handled := false
	for {
		select {
		case <-resizeCh:
			parser.Reset()
			if ir.handleResize() {
				handled = true
			}
		default:
			return handled
		}
	}
}

func (ir *InputReader) handleResize() bool {
	oldWidth := ir.terminalWidth
	ir.updateTerminalWidth()
	return ir.applyTerminalWidthChange(oldWidth, ir.terminalWidth)
}

func (ir *InputReader) applyTerminalWidthChange(oldWidth, newWidth int) bool {
	if newWidth <= 0 {
		newWidth = 80
	}
	if oldWidth == newWidth {
		ir.terminalWidth = newWidth
		return false
	}

	ir.terminalWidth = newWidth
	ir.lastLineLength = 0
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = false

	// After a terminal resize, the previous wrapped geometry is invalid.
	// Redraw on a fresh line rather than trying to clear using stale counts.
	fmt.Printf("\r%s\n", ClearLineSeq())
	ir.Refresh()
	return true
}

// updateTerminalWidth gets the current terminal width
func (ir *InputReader) updateTerminalWidth() {
	if width, _, err := term.GetSize(ir.termFd); err == nil {
		ir.terminalWidth = width
	} else {
		ir.terminalWidth = 80 // Fallback to standard width
	}
}
