//go:build windows
// +build windows

package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// TerminalSize represents the dimensions of the terminal
type TerminalSize struct {
	Width  int
	Height int
}

// GetTerminalSize returns the terminal size using multiple detection methods
func GetTerminalSize() (*TerminalSize, error) {
	// Method 1: Try Windows console API
	if size, err := getTerminalSizeWindows(); err == nil {
		return size, nil
	}

	// Method 2: Try PowerShell
	if size, err := getTerminalSizePowerShell(); err == nil {
		return size, nil
	}

	// Method 3: Try environment variables
	if size, err := getTerminalSizeEnv(); err == nil {
		return size, nil
	}

	// Default fallback
	return &TerminalSize{Width: 80, Height: 24}, nil
}

// getTerminalSizeWindows uses Windows Console API to get terminal size
func getTerminalSizeWindows() (*TerminalSize, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleScreenBufferInfo := kernel32.NewProc("GetConsoleScreenBufferInfo")

	type coord struct {
		X int16
		Y int16
	}

	type smallRect struct {
		Left   int16
		Top    int16
		Right  int16
		Bottom int16
	}

	type consoleScreenBufferInfo struct {
		Size              coord
		CursorPosition    coord
		Attributes        uint16
		Window            smallRect
		MaximumWindowSize coord
	}

	// Try stdout
	handle, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return nil, err
	}

	var info consoleScreenBufferInfo
	r, _, err := getConsoleScreenBufferInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return nil, err
	}

	width := int(info.Window.Right - info.Window.Left + 1)
	height := int(info.Window.Bottom - info.Window.Top + 1)

	if width > 0 && height > 0 {
		return &TerminalSize{
			Width:  width,
			Height: height,
		}, nil
	}

	return nil, fmt.Errorf("invalid terminal dimensions: %dx%d", width, height)
}

// getTerminalSizePowerShell uses PowerShell to get terminal size
func getTerminalSizePowerShell() (*TerminalSize, error) {
	cmd := exec.Command("powershell", "-Command", "$host.ui.rawui.WindowSize.Width; $host.ui.rawui.WindowSize.Height")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) >= 2 {
		width, err1 := strconv.Atoi(strings.TrimSpace(lines[0]))
		height, err2 := strconv.Atoi(strings.TrimSpace(lines[1]))
		if err1 == nil && err2 == nil && width > 0 && height > 0 {
			return &TerminalSize{Width: width, Height: height}, nil
		}
	}

	return nil, fmt.Errorf("failed to parse PowerShell output")
}

// IsInteractive checks if the current session is interactive
func IsInteractive() bool {
	// On Windows, check if we have a console attached
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")

	handle, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err != nil {
		return false
	}

	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// ClearScreen clears the terminal screen
func ClearScreen() error {
	cmd := exec.Command("cmd", "/c", "cls")
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

// MoveCursorTo moves the cursor to the specified position
func MoveCursorTo(x, y int) {
	// Windows console escape sequences (if supported)
	fmt.Printf("\x1b[%d;%dH", y+1, x+1)
}

// HideCursor hides the terminal cursor
func HideCursor() {
	fmt.Print("\x1b[?25l")
}

// ShowCursor shows the terminal cursor
func ShowCursor() {
	fmt.Print("\x1b[?25h")
}

// SetRawMode sets the terminal to raw mode (no-op on Windows for now)
func SetRawMode() error {
	// Windows doesn't have the same concept of raw mode as Unix
	// This would require more complex Windows Console API calls
	return nil
}

// RestoreTerminal restores the terminal to its original state
func RestoreTerminal() error {
	ShowCursor()
	return nil
}
