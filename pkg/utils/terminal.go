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
	// Method 1: Try ioctl (most reliable on Unix systems)
	if size, err := getTerminalSizeIOCTL(); err == nil {
		return size, nil
	}

	// Method 2: Try tput commands
	if size, err := getTerminalSizeTput(); err == nil {
		return size, nil
	}

	// Method 3: Try environment variables
	if size, err := getTerminalSizeEnv(); err == nil {
		return size, nil
	}

	// Method 4: Try stty command
	if size, err := getTerminalSizeStty(); err == nil {
		return size, nil
	}

	// Default fallback
	return &TerminalSize{Width: 80, Height: 24}, nil
}

// getTerminalSizeIOCTL uses ioctl system call to get terminal size
func getTerminalSizeIOCTL() (*TerminalSize, error) {
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	// Try different file descriptors
	fds := []int{int(os.Stdout.Fd()), int(os.Stdin.Fd()), int(os.Stderr.Fd())}

	for _, fd := range fds {
		ws := &winsize{}
		retCode, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
			uintptr(fd),
			uintptr(syscall.TIOCGWINSZ),
			uintptr(unsafe.Pointer(ws)))

		if int(retCode) != -1 && ws.Row > 0 && ws.Col > 0 {
			return &TerminalSize{
				Width:  int(ws.Col),
				Height: int(ws.Row),
			}, nil
		}

		// Continue trying other file descriptors
		if errno != 0 && fd == fds[len(fds)-1] {
			return nil, errno
		}
	}

	return nil, fmt.Errorf("no valid terminal found")
}

// getTerminalSizeTput uses tput commands to get terminal size
func getTerminalSizeTput() (*TerminalSize, error) {
	widthCmd := exec.Command("tput", "cols")
	widthOut, err := widthCmd.Output()
	if err != nil {
		return nil, err
	}

	heightCmd := exec.Command("tput", "lines")
	heightOut, err := heightCmd.Output()
	if err != nil {
		return nil, err
	}

	width, err := strconv.Atoi(strings.TrimSpace(string(widthOut)))
	if err != nil {
		return nil, err
	}

	height, err := strconv.Atoi(strings.TrimSpace(string(heightOut)))
	if err != nil {
		return nil, err
	}

	if width <= 0 || height <= 0 {
		return nil, nil
	}

	return &TerminalSize{
		Width:  width,
		Height: height,
	}, nil
}

// getTerminalSizeEnv tries to get terminal size from environment variables
func getTerminalSizeEnv() (*TerminalSize, error) {
	width := 0
	height := 0

	// Try COLUMNS and LINES
	if val := os.Getenv("COLUMNS"); val != "" {
		if w, err := strconv.Atoi(val); err == nil && w > 0 {
			width = w
		}
	}

	if val := os.Getenv("LINES"); val != "" {
		if h, err := strconv.Atoi(val); err == nil && h > 0 {
			height = h
		}
	}

	// Also try TERM_WIDTH and TERM_HEIGHT
	if width == 0 {
		if val := os.Getenv("TERM_WIDTH"); val != "" {
			if w, err := strconv.Atoi(val); err == nil && w > 0 {
				width = w
			}
		}
	}

	if height == 0 {
		if val := os.Getenv("TERM_HEIGHT"); val != "" {
			if h, err := strconv.Atoi(val); err == nil && h > 0 {
				height = h
			}
		}
	}

	// LEDIT specific overrides
	if height == 0 {
		if val := os.Getenv("LEDIT_TERM_HEIGHT"); val != "" {
			if h, err := strconv.Atoi(val); err == nil && h > 0 {
				height = h
			}
		}
	}

	if width > 0 && height > 0 {
		return &TerminalSize{
			Width:  width,
			Height: height,
		}, nil
	}

	return nil, nil
}

// getTerminalSizeStty uses stty command as last resort
func getTerminalSizeStty() (*TerminalSize, error) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return nil, nil
	}

	height, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, err
	}

	width, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, err
	}

	if width <= 0 || height <= 0 {
		return nil, nil
	}

	return &TerminalSize{
		Width:  width,
		Height: height,
	}, nil
}

// ClearLine clears the current line and moves cursor to beginning
func ClearLine() {
	print("\r\033[K")
}

// MoveCursor moves the cursor to the specified position (1-based)
func MoveCursor(row, col int) {
	print("\033[", row, ";", col, "H")
}

// HideCursor hides the terminal cursor
func HideCursor() {
	print("\033[?25l")
}

// ShowCursor shows the terminal cursor
func ShowCursor() {
	print("\033[?25h")
}

// ClearScreen clears the entire screen
func ClearScreen() {
	print("\033[2J\033[H")
}

// SaveCursorPosition saves the current cursor position
func SaveCursorPosition() {
	print("\033[s")
}

// RestoreCursorPosition restores the saved cursor position
func RestoreCursorPosition() {
	print("\033[u")
}
