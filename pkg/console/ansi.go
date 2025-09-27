package console

import "fmt"

// ANSI escape sequence helpers for consistent terminal control.

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

