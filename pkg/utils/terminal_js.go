//go:build js
// +build js

package utils

// TerminalSize represents the dimensions of the terminal.
type TerminalSize struct {
	Width  int
	Height int
}

// GetTerminalSize returns a sensible default for the WASM/browser environment.
// The hosting page can override this by injecting a TerminalSize via the
// configuration layer if it needs different dimensions for layout purposes.
func GetTerminalSize() (*TerminalSize, error) {
	return &TerminalSize{Width: 80, Height: 24}, nil
}

// The cursor / clear-screen helpers below all emit ANSI escapes. They work
// fine in xterm.js-style browser terminals, so the implementations match
// the unix versions byte-for-byte. Kept as separate definitions rather than
// !windows-shared so the build matrix stays explicit.

func ClearLine()              { print("\r\033[K") }
func MoveCursor(row, col int) { print("\033[", row, ";", col, "H") }
func HideCursor()             { print("\033[?25l") }
func ShowCursor()             { print("\033[?25h") }
func ClearScreen()            { print("\033[2J\033[H") }
func SaveCursorPosition()     { print("\033[s") }
func RestoreCursorPosition()  { print("\033[u") }
