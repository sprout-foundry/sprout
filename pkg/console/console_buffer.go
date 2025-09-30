package console

import (
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// ConsoleBuffer represents a scrollable buffer with line wrapping
type ConsoleBuffer struct {
	mu            sync.RWMutex
	lines         []string      // Raw lines as added
	wrappedLines  []WrappedLine // Lines after wrapping for current width
	maxLines      int           // Maximum lines to keep (10000)
	terminalWidth int           // Current terminal width for wrapping
	scrollPos     int           // Current scroll position from bottom
	dirty         bool          // Whether wrapping needs to be recalculated
}

// WrappedLine represents a line that has been wrapped for display
type WrappedLine struct {
	Content    string // The wrapped line content
	OriginalID int    // Index of the original line this came from
	WrapIndex  int    // Which wrap of the original line this is (0 = first)
}

// NewConsoleBuffer creates a new console buffer
func NewConsoleBuffer(maxLines int) *ConsoleBuffer {
	return &ConsoleBuffer{
		lines:         make([]string, 0, maxLines),
		wrappedLines:  make([]WrappedLine, 0),
		maxLines:      maxLines,
		terminalWidth: 80, // Default width
	}
}

// AddLine adds a new line to the buffer
func (cb *ConsoleBuffer) AddLine(line string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Remove ANSI escape sequences for storage (but keep for display)
	// We store the raw line with colors for later display
	cb.lines = append(cb.lines, line)

	// Enforce max lines limit using circular buffer behavior
	if len(cb.lines) > cb.maxLines {
		// Remove oldest lines
		copy(cb.lines, cb.lines[len(cb.lines)-cb.maxLines:])
		cb.lines = cb.lines[:cb.maxLines]
	}

	cb.dirty = true
}

// AddContent adds content that may span multiple lines
func (cb *ConsoleBuffer) AddContent(content string) {
	// Split content into lines and add each
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Don't add empty line at the end if content ended with \n
		if i == len(lines)-1 && line == "" {
			continue
		}
		cb.AddLine(line)
	}
}

// SetTerminalWidth updates the terminal width and marks for rewrapping
func (cb *ConsoleBuffer) SetTerminalWidth(width int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if width != cb.terminalWidth {
		cb.terminalWidth = width
		cb.dirty = true
	}
}

// rewrapLines recalculates line wrapping for current terminal width
func (cb *ConsoleBuffer) rewrapLines() {
	if !cb.dirty {
		return
	}

	cb.wrappedLines = cb.wrappedLines[:0] // Clear but keep capacity

	for originalID, line := range cb.lines {
		wrappedParts := cb.wrapLine(line, cb.terminalWidth)
		for wrapIndex, part := range wrappedParts {
			cb.wrappedLines = append(cb.wrappedLines, WrappedLine{
				Content:    part,
				OriginalID: originalID,
				WrapIndex:  wrapIndex,
			})
		}
	}

	cb.dirty = false
}

// wrapLine wraps a single line to fit within the given width
// Takes into account ANSI escape sequences
func (cb *ConsoleBuffer) wrapLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}

	// If line fits without wrapping, return as-is
	visualLength := cb.visualLength(line)
	if visualLength <= width {
		return []string{line}
	}

	var wrapped []string
	remaining := line
	firstSegment := true

	for len(remaining) > 0 {
		// Find the best place to break the line
		breakPoint := cb.findWrapPoint(remaining, width)

		// List-aware guard: ensure the first segment of a list item keeps the marker
		// and at least a few characters of content on the same line.
		if firstSegment {
			if mEnd := listMarkerEnd(remaining); mEnd > 0 {
				// Keep at least 3 chars after the marker with it
				minAfter := 3
				minPos := mEnd + minAfter
				if breakPoint <= minPos && len(remaining) > minPos {
					// Try to break at the next space after the minimum position
					if next := nextSpaceAfter(remaining, minPos); next > 0 && next < len(remaining) {
						breakPoint = next
					} else {
						breakPoint = minPos
					}
				}
			}
		}
		if breakPoint <= 0 {
			// Can't wrap nicely, force break
			breakPoint = cb.forceWrapPoint(remaining, width)
		}

		if breakPoint >= len(remaining) {
			// Last piece
			wrapped = append(wrapped, remaining)
			break
		}

		// Extract the piece
		piece := remaining[:breakPoint]
		wrapped = append(wrapped, piece)
		remaining = remaining[breakPoint:]

		// Skip at most one leading space on continuation lines (preserve indentation)
		if strings.HasPrefix(remaining, " ") {
			remaining = strings.TrimPrefix(remaining, " ")
		}

		firstSegment = false
	}

	return wrapped
}

// findWrapPoint finds a good place to wrap (at word boundary)
func (cb *ConsoleBuffer) findWrapPoint(line string, maxWidth int) int {
	if maxWidth <= 0 {
		return 0
	}

	visualPos := 0
	runePos := 0
	lastSpace := -1
	inEscape := false

	for runePos < len(line) {
		r, size := utf8.DecodeRuneInString(line[runePos:])

		// Handle ANSI escape sequences
		if r == '\033' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
			runePos += size
			continue
		} else if inEscape {
			runePos += size
			continue
		}

		// Count visual width
		if !inEscape {
			if visualPos >= maxWidth {
				break
			}

			if r == ' ' || r == '\t' {
				lastSpace = runePos
			}

			visualPos++
		}

		runePos += size
	}

	// Prefer not to break immediately after list markers like "• ", "- ", "* ", or "1. "
	markerEnd := listMarkerEnd(line)

	// If we found a space within reasonable distance, use it (with list marker safeguard)
	if lastSpace > 0 && lastSpace > runePos-20 {
		if markerEnd > 0 && lastSpace <= markerEnd {
			if next := nextSpaceAfter(line, markerEnd); next > 0 && next < runePos {
				return next
			}
		}
		return lastSpace
	}

	return runePos
}

// forceWrapPoint finds a character-level wrap point when word wrapping fails
func (cb *ConsoleBuffer) forceWrapPoint(line string, maxWidth int) int {
	visualPos := 0
	runePos := 0
	inEscape := false

	for runePos < len(line) {
		r, size := utf8.DecodeRuneInString(line[runePos:])

		// Handle ANSI escape sequences
		if r == '\033' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
			runePos += size
			continue
		} else if inEscape {
			runePos += size
			continue
		}

		if !inEscape {
			if visualPos >= maxWidth {
				break
			}
			visualPos++
		}

		runePos += size
	}

	if runePos == 0 {
		// Ensure we make progress
		_, size := utf8.DecodeRuneInString(line)
		return size
	}

	return runePos
}

// listMarkerEnd returns the byte index after a list marker if present at start (e.g., "• ", "- ", "* ", "1. ", "1) ")
func listMarkerEnd(s string) int {
	// Bullet markers
	if strings.HasPrefix(s, "• ") || strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") {
		return 2
	}
	// Numbered: digits then '.' or ')' then space
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && s[i+1] == ' ' {
		return i + 2
	}
	return 0
}

// nextSpaceAfter finds the next space byte index after pos
func nextSpaceAfter(s string, pos int) int {
	if pos < 0 {
		pos = 0
	}
	for i := pos; i < len(s); i++ {
		if s[i] == ' ' {
			return i
		}
	}
	return -1
}

// visualLength calculates the visual length of a string (ignoring ANSI escapes)
func (cb *ConsoleBuffer) visualLength(s string) int {
	length := 0
	inEscape := false

	for _, r := range s {
		if r == '\033' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
		} else if !inEscape {
			length++
		}
	}

	return length
}

// GetVisibleLines returns the lines that should be visible in the given viewport
func (cb *ConsoleBuffer) GetVisibleLines(viewportHeight int) []string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cb.rewrapLines()

	totalWrapped := len(cb.wrappedLines)
	if totalWrapped == 0 {
		return []string{}
	}

	// Calculate the start position based on scroll
	startLine := totalWrapped - viewportHeight - cb.scrollPos
	if startLine < 0 {
		startLine = 0
	}

	endLine := startLine + viewportHeight
	if endLine > totalWrapped {
		endLine = totalWrapped
	}

	// Extract visible lines
	visible := make([]string, 0, endLine-startLine)
	for i := startLine; i < endLine; i++ {
		visible = append(visible, cb.wrappedLines[i].Content)
	}

	return visible
}

// ScrollUp scrolls the buffer up by the given number of lines
func (cb *ConsoleBuffer) ScrollUp(lines int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.scrollPos += lines
	// Limit scroll to available content
	maxScroll := len(cb.wrappedLines) - 1
	if cb.scrollPos > maxScroll {
		cb.scrollPos = maxScroll
	}
}

// ScrollDown scrolls the buffer down by the given number of lines
func (cb *ConsoleBuffer) ScrollDown(lines int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.scrollPos -= lines
	if cb.scrollPos < 0 {
		cb.scrollPos = 0
	}
}

// ScrollToBottom scrolls to the bottom of the buffer
func (cb *ConsoleBuffer) ScrollToBottom() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.scrollPos = 0
}

// Clear clears the buffer
func (cb *ConsoleBuffer) Clear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lines = cb.lines[:0]
	cb.wrappedLines = cb.wrappedLines[:0]
	cb.scrollPos = 0
	cb.dirty = false
}

// GetStats returns buffer statistics
func (cb *ConsoleBuffer) GetStats() BufferStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cb.rewrapLines()

	return BufferStats{
		TotalLines:     len(cb.lines),
		WrappedLines:   len(cb.wrappedLines),
		ScrollPosition: cb.scrollPos,
		TerminalWidth:  cb.terminalWidth,
	}
}

// BufferStats contains buffer statistics
type BufferStats struct {
	TotalLines     int
	WrappedLines   int
	ScrollPosition int
	TerminalWidth  int
}

// RedrawBuffer redraws the entire buffer content to the terminal
func (cb *ConsoleBuffer) RedrawBuffer(terminal TerminalManager, viewportHeight int) error {
	lines := cb.GetVisibleLines(viewportHeight)

	// Clear the content area
	for i := 0; i < viewportHeight; i++ {
		terminal.MoveCursor(1, i+1)
		terminal.ClearLine()
	}

	// Draw the visible lines
	for i, line := range lines {
		if i >= viewportHeight {
			break
		}
		terminal.MoveCursor(1, i+1)
		terminal.Write([]byte(line))
	}

	return nil
}

// Debug method for troubleshooting
func (cb *ConsoleBuffer) Debug() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cb.rewrapLines()

	return fmt.Sprintf("Buffer: %d lines, %d wrapped, scroll: %d, width: %d, dirty: %v",
		len(cb.lines), len(cb.wrappedLines), cb.scrollPos, cb.terminalWidth, cb.dirty)
}
