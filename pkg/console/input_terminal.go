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
	ir.lastVisualRows = 0
	ir.lastWrapPending = false

	// After a resize the terminal re-soft-wraps the on-screen rows to the new
	// width, so our captured geometry is stale and the block's top row can't be
	// located reliably without a cursor-position query. Clear from the cursor to
	// the end of the screen (dropping any stale wrapped rows below) and redraw
	// the prompt+line fresh in place. This avoids the extra blank line the prior
	// "\r CLEAR \n" inserted on every resize; rows above the cursor are left to
	// the terminal's own reflow.
	LockOutput()
	fmt.Print("\r\033[J")
	UnlockOutput()
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
