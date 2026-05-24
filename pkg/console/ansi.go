package console

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

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

// MoveCursorLeftSeq returns the escape sequence to move cursor left by n columns.
func MoveCursorLeftSeq(n int) string {
	return fmt.Sprintf("\033[%dD", n)
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

// StdoutIsTerminal returns true if os.Stdout is connected to a terminal.
func StdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StderrIsTerminal returns true if os.Stderr is connected to a terminal.
func StderrIsTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// BoldText wraps text with bold formatting using ANSI codes.
func BoldText(text string) string {
	return ColorBold + text + ColorReset
}

// FormatYesNoPrompt returns a [y/N] or [Y/n] prompt string with the default
// letter bolded. When yesDefault is true, the Y is bolded ([Y/n]).
// When yesDefault is false, the N is bolded ([y/N]).
//
// ANSI codes are only applied when the stderr is a TTY. When stderr is
// not a terminal (e.g., piped to a file), the plain text is returned
// without any escape codes.
func FormatYesNoPrompt(yesDefault bool) string {
	if !StderrIsTerminal() {
		if yesDefault {
			return "[Y/n]"
		}
		return "[y/N]"
	}
	if yesDefault {
		return "[" + ColorBold + "Y" + ColorReset + "/n]"
	}
	return "[y/" + ColorBold + "N" + ColorReset + "]"
}

// FormatYesNoPromptStdout returns a [y/N] or [Y/n] prompt string with the default
// letter bolded, checking stdout for terminal status (same logic as
// FormatYesNoPrompt but uses os.Stdout instead of os.Stderr for the TTY check).
func FormatYesNoPromptStdout(yesDefault bool) string {
	if !StdoutIsTerminal() {
		if yesDefault {
			return "[Y/n]"
		}
		return "[y/N]"
	}
	if yesDefault {
		return "[" + ColorBold + "Y" + ColorReset + "/n]"
	}
	return "[y/" + ColorBold + "N" + ColorReset + "]"
}
