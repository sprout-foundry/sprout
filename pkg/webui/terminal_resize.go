//go:build !js

package webui

import (
	"fmt"

	"github.com/creack/pty"
)

// ResizeTerminal resizes the PTY for the given session.
func (tm *TerminalManager) ResizeTerminal(sessionID string, rows, cols uint16) error {
	// Reject zero dimensions — these corrupt process.stdout.columns in child
	// processes (e.g. Node.js tools using process.stdout.columns directly).
	// The frontend guards against this too, but we also guard server-side.
	if cols == 0 || rows == 0 {
		return fmt.Errorf("resize rejected: zero dimensions (%dx%d) would corrupt PTY", cols, rows)
	}

	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}
	if session.Hidden {
		return fmt.Errorf("session %s is not accessible", sessionID)
	}
	if session.Pty == nil {
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	// Fallback (NoPTY) sessions use pipes which don't support TIOCSWINSZ.
	// Record the new size for GetTerminalSize and environment variable updates
	// but skip the ioctl.
	if session.NoPTY {
		session.Size = &pty.Winsize{Rows: rows, Cols: cols}
		return nil
	}

	newSize := &pty.Winsize{Rows: rows, Cols: cols}
	if err := pty.Setsize(session.Pty, newSize); err != nil {
		return fmt.Errorf("failed to resize PTY: %w", err)
	}
	session.Size = newSize
	return nil
}

// GetTerminalSize returns the current terminal size for the session.
func (tm *TerminalManager) GetTerminalSize(sessionID string) (*pty.Winsize, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.RLock()
	defer session.mutex.RUnlock()

	if session.Hidden {
		return nil, fmt.Errorf("session %s is not accessible", sessionID)
	}

	if session.Size == nil {
		return nil, fmt.Errorf("terminal size not set for session %s", sessionID)
	}

	// Return a copy to prevent external modification
	size := &pty.Winsize{
		Rows: session.Size.Rows,
		Cols: session.Size.Cols,
	}

	return size, nil
}
