package ui

import "strings"

// Common ANSI/SGR sequences used across the TUI
const (
    SGRReset       = "\033[0m"
    SGRDefaultFgBg = "\033[39;49m"
    ClearLine      = "\033[2K"
)

// Bg wraps text with a background SGR sequence and resets at the end.
func Bg(text, bg string) string {
    return bg + text + SGRReset
}

// WithDim wraps text in a dim+white SGR and resets at the end.
func WithDim(text string) string {
    return "\033[2m\033[37m" + text + SGRReset
}

// BgPad creates a block of spaces with the given background and resets.
func BgPad(width int, bg string) string {
    if width <= 0 {
        return ""
    }
    return bg + strings.Repeat(" ", width) + SGRReset
}

