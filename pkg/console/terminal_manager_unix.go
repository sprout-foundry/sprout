//go:build !windows
// +build !windows

package console

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

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
	signalChan      chan os.Signal
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

	// Set up signal handling for resize events
	tm.signalChan = make(chan os.Signal, 1)
	signal.Notify(tm.signalChan, syscall.SIGWINCH)

	// Start resize monitoring goroutine
	go tm.monitorResize()

	return nil
}

// Cleanup restores terminal to original state
func (tm *terminalManager) Cleanup() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Stop resize monitoring
	if tm.stopChan != nil {
		close(tm.stopChan)
	}

	// Restore terminal mode
	if tm.rawMode && tm.oldState != nil {
		if err := term.Restore(int(os.Stdin.Fd()), tm.oldState); err != nil {
			return fmt.Errorf("failed to restore terminal: %w", err)
		}
		tm.rawMode = false
		tm.oldState = nil
	}

	// Show cursor
	tm.ShowCursor()

	return nil
}

// GetSize returns the current terminal size
func (tm *terminalManager) GetSize() (width, height int, err error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.width == 0 || tm.height == 0 {
		// Try to get size again
		if err := tm.updateSize(); err != nil {
			return 0, 0, err
		}
	}

	return tm.width, tm.height, nil
}

// OnResize registers a callback for terminal resize events
func (tm *terminalManager) OnResize(callback func(width, height int)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.resizeCallbacks = append(tm.resizeCallbacks, callback)
}

// SetRawMode enables or disables raw mode
func (tm *terminalManager) SetRawMode(enabled bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if enabled == tm.rawMode {
		return nil // Already in requested mode
	}

	fd := int(os.Stdin.Fd())

	if enabled {
		// Save current state and enter raw mode
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		tm.oldState = oldState
		tm.rawMode = true
	} else {
		// Restore previous state
		if tm.oldState != nil {
			if err := term.Restore(fd, tm.oldState); err != nil {
				return fmt.Errorf("failed to restore terminal: %w", err)
			}
			tm.oldState = nil
			tm.rawMode = false
		}
	}

	return nil
}

// IsRawMode returns true if terminal is in raw mode
func (tm *terminalManager) IsRawMode() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.rawMode
}

// MoveCursor moves cursor to specified position (1-based)
func (tm *terminalManager) MoveCursor(x, y int) error {
	_, err := fmt.Fprintf(tm.writer, "\033[%d;%dH", y, x)
	return err
}

// SaveCursor saves the current cursor position
func (tm *terminalManager) SaveCursor() error {
	_, err := fmt.Fprint(tm.writer, "\033[s")
	return err
}

// RestoreCursor restores the saved cursor position
func (tm *terminalManager) RestoreCursor() error {
	_, err := fmt.Fprint(tm.writer, "\033[u")
	return err
}

// HideCursor hides the cursor
func (tm *terminalManager) HideCursor() error {
	_, err := fmt.Fprint(tm.writer, "\033[?25l")
	return err
}

// ShowCursor shows the cursor
func (tm *terminalManager) ShowCursor() error {
	_, err := fmt.Fprint(tm.writer, "\033[?25h")
	return err
}

// ClearScreen clears the entire screen
func (tm *terminalManager) ClearScreen() error {
	_, err := fmt.Fprint(tm.writer, "\033[2J\033[H")
	return err
}

// ClearLine clears the current line
func (tm *terminalManager) ClearLine() error {
	_, err := fmt.Fprint(tm.writer, "\r\033[K")
	return err
}

// ClearToEndOfLine clears from cursor to end of line
func (tm *terminalManager) ClearToEndOfLine() error {
	_, err := fmt.Fprint(tm.writer, "\033[K")
	return err
}

// ClearToEndOfScreen clears from cursor to end of screen
func (tm *terminalManager) ClearToEndOfScreen() error {
	_, err := fmt.Fprint(tm.writer, "\033[J")
	return err
}

// Write writes data to the terminal
func (tm *terminalManager) Write(data []byte) (int, error) {
	return tm.writer.Write(data)
}

// WriteAt writes data at a specific position
func (tm *terminalManager) WriteAt(x, y int, data []byte) error {
	if err := tm.SaveCursor(); err != nil {
		return err
	}
	defer tm.RestoreCursor()

	if err := tm.MoveCursor(x, y); err != nil {
		return err
	}

	_, err := tm.Write(data)
	return err
}

// Flush flushes any buffered output
func (tm *terminalManager) Flush() error {
	if f, ok := tm.writer.(*os.File); ok {
		return f.Sync()
	}
	return nil
}

// SetScrollRegion sets the scrolling region (1-based, inclusive)
func (tm *terminalManager) SetScrollRegion(top, bottom int) error {
	_, err := fmt.Fprintf(tm.writer, "\033[%d;%dr", top, bottom)
	return err
}

// ResetScrollRegion resets the scrolling region to the entire screen
func (tm *terminalManager) ResetScrollRegion() error {
	_, err := fmt.Fprint(tm.writer, "\033[r")
	return err
}

// ScrollUp scrolls the current region up by n lines
func (tm *terminalManager) ScrollUp(lines int) error {
	for i := 0; i < lines; i++ {
		if _, err := fmt.Fprint(tm.writer, "\033[S"); err != nil {
			return err
		}
	}
	return nil
}

// ScrollDown scrolls the current region down by n lines
func (tm *terminalManager) ScrollDown(lines int) error {
	for i := 0; i < lines; i++ {
		if _, err := fmt.Fprint(tm.writer, "\033[T"); err != nil {
			return err
		}
	}
	return nil
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

// monitorResize monitors for terminal resize events
func (tm *terminalManager) monitorResize() {
	for {
		select {
		case <-tm.signalChan:
			// Terminal resized
			tm.mu.Lock()
			oldWidth, oldHeight := tm.width, tm.height
			if err := tm.updateSize(); err == nil {
				if tm.width != oldWidth || tm.height != oldHeight {
					// Notify callbacks
					callbacks := make([]func(int, int), len(tm.resizeCallbacks))
					copy(callbacks, tm.resizeCallbacks)
					tm.mu.Unlock()

					// Call callbacks outside of lock
					for _, callback := range callbacks {
						callback(tm.width, tm.height)
					}
				} else {
					tm.mu.Unlock()
				}
			} else {
				tm.mu.Unlock()
			}

		case <-tm.stopChan:
			// Stop monitoring
			return
		}
	}
}
