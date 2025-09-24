//go:build windows
// +build windows

package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
	"golang.org/x/term"
)

// terminalManager implements TerminalManager interface
type terminalManager struct {
	mu              sync.RWMutex
	width           int
	height          int
	oldState        *term.State
	rawMode         bool
	resizeCallbacks []func(width, height int)
	stopChan        chan struct{}
	writer          io.Writer
}

// NewTerminalManager creates a new terminal manager
func NewTerminalManager() TerminalManager {
	return &terminalManager{
		writer:          os.Stdout,
		resizeCallbacks: make([]func(width, height int), 0),
		stopChan:        make(chan struct{}),
	}
}

// Init initializes the terminal manager
func (tm *terminalManager) Init() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Get initial size
	if err := tm.updateSize(); err != nil {
		return fmt.Errorf("failed to get terminal size: %w", err)
	}

	// Start resize monitoring goroutine
	// On Windows, we poll for size changes instead of using signals
	go tm.monitorResize()

	return nil
}

// monitorResize monitors terminal resize events
func (tm *terminalManager) monitorResize() {
	ticker := time.NewTicker(500 * time.Millisecond) // Poll every 500ms
	defer ticker.Stop()

	lastWidth, lastHeight, _ := tm.GetSize()

	for {
		select {
		case <-ticker.C:
			// Check if size has changed
			size, err := utils.GetTerminalSize()
			if err == nil && (size.Width != lastWidth || size.Height != lastHeight) {
				tm.mu.Lock()
				tm.width = size.Width
				tm.height = size.Height
				callbacks := make([]func(width, height int), len(tm.resizeCallbacks))
				copy(callbacks, tm.resizeCallbacks)
				tm.mu.Unlock()

				// Notify callbacks
				for _, cb := range callbacks {
					cb(size.Width, size.Height)
				}

				lastWidth = size.Width
				lastHeight = size.Height
			}
		case <-tm.stopChan:
			return
		}
	}
}

// updateSize updates the cached terminal size
func (tm *terminalManager) updateSize() error {
	size, err := utils.GetTerminalSize()
	if err != nil {
		return err
	}

	tm.width = size.Width
	tm.height = size.Height
	return nil
}

// GetSize returns the current terminal width and height
func (tm *terminalManager) GetSize() (width, height int, err error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.width, tm.height, nil
}

// SetRawMode enables or disables raw terminal mode
func (tm *terminalManager) SetRawMode(enabled bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if enabled {
		if tm.rawMode {
			return nil // Already in raw mode
		}

		fd := int(os.Stdin.Fd())
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}

		tm.oldState = oldState
		tm.rawMode = true
		return nil
	} else {
		// Disable raw mode
		if !tm.rawMode || tm.oldState == nil {
			return nil // Not in raw mode
		}

		fd := int(os.Stdin.Fd())
		if err := term.Restore(fd, tm.oldState); err != nil {
			return fmt.Errorf("failed to restore terminal mode: %w", err)
		}

		tm.rawMode = false
		tm.oldState = nil
		return nil
	}
}

// RestoreMode restores the terminal to its previous mode
func (tm *terminalManager) RestoreMode() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.rawMode || tm.oldState == nil {
		return nil // Not in raw mode
	}

	fd := int(os.Stdin.Fd())
	if err := term.Restore(fd, tm.oldState); err != nil {
		return fmt.Errorf("failed to restore terminal mode: %w", err)
	}

	tm.rawMode = false
	tm.oldState = nil
	return nil
}

// Clear clears the terminal screen
func (tm *terminalManager) Clear() error {
	utils.ClearScreen()
	return nil
}

// MoveCursor moves the cursor to the specified position
func (tm *terminalManager) MoveCursor(x, y int) error {
	fmt.Fprintf(tm.writer, "\x1b[%d;%dH", y+1, x+1)
	return nil
}

// HideCursor hides the terminal cursor
func (tm *terminalManager) HideCursor() error {
	fmt.Fprint(tm.writer, "\x1b[?25l")
	return nil
}

// ShowCursor shows the terminal cursor
func (tm *terminalManager) ShowCursor() error {
	fmt.Fprint(tm.writer, "\x1b[?25h")
	return nil
}

// Write writes data to the terminal
func (tm *terminalManager) Write(p []byte) (n int, err error) {
	return tm.writer.Write(p)
}

// WriteText writes text with automatic raw mode line ending handling
func (tm *terminalManager) WriteText(text string) (int, error) {
	// In raw mode, convert \n to \r\n for proper line breaks
	if tm.IsRawMode() {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return tm.writer.Write([]byte(text))
}

// OnResize registers a callback for terminal resize events
func (tm *terminalManager) OnResize(callback func(width, height int)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.resizeCallbacks = append(tm.resizeCallbacks, callback)
}

// Cleanup cleans up terminal resources
func (tm *terminalManager) Cleanup() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Stop resize monitoring
	close(tm.stopChan)

	// Restore terminal mode if needed
	if tm.rawMode && tm.oldState != nil {
		fd := int(os.Stdin.Fd())
		if err := term.Restore(fd, tm.oldState); err != nil {
			return err
		}
	}

	// Show cursor
	fmt.Fprint(tm.writer, "\x1b[?25h")
	return nil
}

// SetWriter sets the output writer
func (tm *terminalManager) SetWriter(w io.Writer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.writer = w
}

// IsRawMode returns whether the terminal is in raw mode
func (tm *terminalManager) IsRawMode() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.rawMode
}

// SaveCursor saves the current cursor position
func (tm *terminalManager) SaveCursor() error {
	fmt.Fprint(tm.writer, "\x1b[s")
	return nil
}

// RestoreCursor restores the saved cursor position
func (tm *terminalManager) RestoreCursor() error {
	fmt.Fprint(tm.writer, "\x1b[u")
	return nil
}

// ClearScreen clears the entire screen
func (tm *terminalManager) ClearScreen() error {
	return utils.ClearScreen()
}

// ClearLine clears the current line
func (tm *terminalManager) ClearLine() error {
	fmt.Fprint(tm.writer, "\r\x1b[K")
	return nil
}

// ClearToEndOfLine clears from cursor to end of line
func (tm *terminalManager) ClearToEndOfLine() error {
	fmt.Fprint(tm.writer, "\x1b[K")
	return nil
}

// ClearToEndOfScreen clears from cursor to end of screen
func (tm *terminalManager) ClearToEndOfScreen() error {
	fmt.Fprint(tm.writer, "\x1b[J")
	return nil
}

// SetScrollRegion sets the scroll region
func (tm *terminalManager) SetScrollRegion(top, bottom int) error {
	fmt.Fprintf(tm.writer, "\x1b[%d;%dr", top+1, bottom+1)
	return nil
}

// ResetScrollRegion resets the scroll region
func (tm *terminalManager) ResetScrollRegion() error {
	fmt.Fprint(tm.writer, "\x1b[r")
	return nil
}

// ScrollUp scrolls the terminal up by the specified number of lines
func (tm *terminalManager) ScrollUp(lines int) error {
	for i := 0; i < lines; i++ {
		fmt.Fprint(tm.writer, "\x1b[S")
	}
	return nil
}

// ScrollDown scrolls the terminal down by the specified number of lines
func (tm *terminalManager) ScrollDown(lines int) error {
	for i := 0; i < lines; i++ {
		fmt.Fprint(tm.writer, "\x1b[T")
	}
	return nil
}

// WriteAt writes data at the specified position
func (tm *terminalManager) WriteAt(x, y int, data []byte) error {
	if err := tm.MoveCursor(x, y); err != nil {
		return err
	}
	_, err := tm.Write(data)
	return err
}

// Flush flushes any buffered output
func (tm *terminalManager) Flush() error {
	// For now, we don't buffer output
	return nil
}
