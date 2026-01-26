package console

import "fmt"

// ANSI escape sequence helpers for consistent terminal control.

// Colorize wraps text with a color code and reset
func Colorize(text, color string) string {
	return color + text + ColorReset
}

// ColorizeBold wraps text with bold and a color code
func ColorizeBold(text, color string) string {
	return ColorBold + color + text + ColorReset
}

// MoveCursorSeq returns the escape sequence to move the cursor to (x,y)
// Note: ANSI uses row (y) first, then column (x).
func MoveCursorSeq(x, y int) string {
	return fmt.Sprintf("\033[%d;%dH", y, x)
}

// ClearLineSeq returns the escape sequence to clear the entire current line.
func ClearLineSeq() string { return "\033[2K" }

// ClearToEndOfLineSeq returns the escape sequence to clear from cursor to end of line.
func ClearToEndOfLineSeq() string { return "\033[K" }

// SetScrollRegionSeq returns the escape sequence to set the scrolling region (1-based, inclusive).
func SetScrollRegionSeq(top, bottom int) string {
	return fmt.Sprintf("\033[%d;%dr", top, bottom)
}

// ResetScrollRegionSeq resets the scrolling region to the full screen.
func ResetScrollRegionSeq() string { return "\033[r" }

// MoveCursorUpSeq returns the escape sequence to move cursor up by n lines.
func MoveCursorUpSeq(n int) string {
	return fmt.Sprintf("\033[%dA", n)
}

// MoveCursorDownSeq returns the escape sequence to move cursor down by n lines.
func MoveCursorDownSeq(n int) string {
	return fmt.Sprintf("\033[%dB", n)
}

// MoveCursorToColumnSeq returns the escape sequence to move cursor to column n (1-based).
func MoveCursorToColumnSeq(n int) string {
	return fmt.Sprintf("\033[%dG", n)
}

// ClearToStartOfLineSeq returns the escape sequence to clear from start of line to cursor.
func ClearToStartOfLineSeq() string { return "\033[1K" }

// HideCursorSeq returns the escape sequence to hide the cursor.
func HideCursorSeq() string { return "\033[?25l" }

// ShowCursorSeq returns the escape sequence to show the cursor.
func ShowCursorSeq() string { return "\033[?25h" }

// HomeCursorSeq returns the escape sequence to move cursor to home position (1,1).
func HomeCursorSeq() string { return "\033[H" }

// ClearScreenSeq returns the escape sequence to clear the entire screen.
func ClearScreenSeq() string { return "\033[2J" }

// ClearToEndOfScreenSeq returns the escape sequence to clear from cursor to end of screen.
func ClearToEndOfScreenSeq() string { return "\033[J" }
