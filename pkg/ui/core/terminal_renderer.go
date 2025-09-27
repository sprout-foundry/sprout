package core

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/console"
)

// TerminalRenderer implements the Renderer interface for terminal output
type TerminalRenderer struct {
	terminal console.TerminalManager
	buffer   []renderOp
	width    int
	height   int
}

// renderOp represents a rendering operation
type renderOp struct {
	opType string
	x, y   int
	text   string
	width  int
	height int
}

// NewTerminalRenderer creates a new terminal renderer
func NewTerminalRenderer(terminal console.TerminalManager) *TerminalRenderer {
	width, height, _ := terminal.GetSize()
	return &TerminalRenderer{
		terminal: terminal,
		buffer:   make([]renderOp, 0),
		width:    width,
		height:   height,
	}
}

// Clear clears the render buffer
func (r *TerminalRenderer) Clear() error {
	r.buffer = r.buffer[:0]
	// Don't clear the entire screen - let the app handle that
	return nil
}

// DrawText draws text at a specific position
func (r *TerminalRenderer) DrawText(x, y int, text string) error {
	r.buffer = append(r.buffer, renderOp{
		opType: "text",
		x:      x,
		y:      y,
		text:   text,
	})
	return nil
}

// DrawBox draws a box
func (r *TerminalRenderer) DrawBox(x, y, width, height int) error {
	r.buffer = append(r.buffer, renderOp{
		opType: "box",
		x:      x,
		y:      y,
		width:  width,
		height: height,
	})
	return nil
}

// Flush commits all pending draws
func (r *TerminalRenderer) Flush() error {
	// Save cursor
	r.terminal.SaveCursor()
	defer r.terminal.RestoreCursor()

	// Process each operation
	for _, op := range r.buffer {
		switch op.opType {
		case "text":
			r.terminal.MoveCursor(op.x, op.y)
			r.terminal.WriteText(op.text)

		case "box":
			// Draw top border
			r.terminal.MoveCursor(op.x, op.y)
			r.terminal.WriteText("┌" + strings.Repeat("─", op.width-2) + "┐")

			// Draw sides with filled background
			for i := 1; i < op.height-1; i++ {
				r.terminal.MoveCursor(op.x, op.y+i)
				r.terminal.WriteText("│" + strings.Repeat(" ", op.width-2) + "│")
			}

			// Draw bottom border
			r.terminal.MoveCursor(op.x, op.y+op.height-1)
			r.terminal.WriteText("└" + strings.Repeat("─", op.width-2) + "┘")
		}
	}

	// Flush terminal output
	r.terminal.Flush()

	// Clear buffer after flush
	r.buffer = r.buffer[:0]

	return nil
}

// ClearRegion clears a specific region
func (r *TerminalRenderer) ClearRegion(x, y, width, height int) error {
	emptyLine := strings.Repeat(" ", width)
	for i := 0; i < height; i++ {
		r.DrawText(x, y+i, emptyLine)
	}
	return nil
}

// GetSize returns the terminal size
func (r *TerminalRenderer) GetSize() (width, height int) {
	return r.width, r.height
}

// UpdateSize updates the cached terminal size
func (r *TerminalRenderer) UpdateSize() error {
	width, height, err := r.terminal.GetSize()
	if err != nil {
		return err
	}
	r.width = width
	r.height = height
	return nil
}

// DrawTextWrapped draws text with word wrapping
func (r *TerminalRenderer) DrawTextWrapped(x, y int, text string, maxWidth int) error {
	lines := wrapText(text, maxWidth)
	for i, line := range lines {
		if err := r.DrawText(x, y+i, line); err != nil {
			return err
		}
	}
	return nil
}

// wrapText wraps text to fit within maxWidth
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 || len(text) <= maxWidth {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)

	var currentLine string
	for _, word := range words {
		if currentLine == "" {
			currentLine = word
		} else if len(currentLine)+1+len(word) <= maxWidth {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return lines
}

// DrawBorder draws a border with optional title
func (r *TerminalRenderer) DrawBorder(x, y, width, height int, title string) error {
	// Top border with title
	r.terminal.MoveCursor(x, y)
	topBorder := "┌"
	if title != "" && len(title)+4 < width {
		titlePart := fmt.Sprintf("─ %s ─", title)
		remaining := width - len(titlePart) - 2
		leftPad := remaining / 2
		rightPad := remaining - leftPad
		topBorder += strings.Repeat("─", leftPad) + titlePart + strings.Repeat("─", rightPad) + "┐"
	} else {
		topBorder += strings.Repeat("─", width-2) + "┐"
	}
	r.terminal.WriteText(topBorder)

	// Sides
	for i := 1; i < height-1; i++ {
		r.terminal.MoveCursor(x, y+i)
		r.terminal.WriteText("│")
		r.terminal.MoveCursor(x+width-1, y+i)
		r.terminal.WriteText("│")
	}

	// Bottom border
	r.terminal.MoveCursor(x, y+height-1)
	r.terminal.WriteText("└" + strings.Repeat("─", width-2) + "┘")

	return nil
}
