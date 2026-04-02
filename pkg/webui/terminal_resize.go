package webui

import (
	"fmt"
	"os/exec"

	"github.com/creack/pty"
)

// ResizeTerminal resizes the terminal for the given session.
// For tmux-backed sessions, the tmux pane is also resized.
func (tm *TerminalManager) ResizeTerminal(sessionID string, rows, cols uint16) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}

	if session.Pty == nil {
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	// Create new window size
	newSize := &pty.Winsize{
		Rows: rows,
		Cols: cols,
	}

	// Resize the PTY
	if err := pty.Setsize(session.Pty, newSize); err != nil {
		return fmt.Errorf("failed to resize PTY: %w", err)
	}

	// For tmux-backed sessions, also resize the tmux pane and update
	// COLUMNS/LINES environment variables so that child processes (e.g.
	// Node.js reading process.env.COLUMNS) pick up the new dimensions.
	if session.TmuxBacked {
		tmuxName := tm.TmuxSessionName(sessionID)
		// Note: we release the lock on session before calling tmux,
		// but we can't here because of the defer. So run it in a goroutine.
		go func() {
			resizeCmd := exec.Command("tmux", "resize-pane",
				"-t", tmuxName,
				"-x", fmt.Sprintf("%d", cols),
				"-y", fmt.Sprintf("%d", rows),
			)
			if err := resizeCmd.Run(); err != nil {
				// Non-fatal: the PTY resize already happened, tmux adjust may
				// not be critical in all cases
				fmt.Printf("TerminalManager: failed to resize tmux pane %s: %v\n", tmuxName, err)
			}

			// Propagate updated dimensions into the tmux session environment
			// so that new child processes inherit the correct COLUMNS/LINES.
			for _, pair := range []struct{ k, v string }{
				{"COLUMNS", fmt.Sprintf("%d", cols)},
				{"LINES", fmt.Sprintf("%d", rows)},
			} {
				cmd := exec.Command("tmux", "set-environment", "-t", tmuxName, pair.k, pair.v)
				if err := cmd.Run(); err != nil {
					fmt.Printf("TerminalManager: failed to set %s in tmux session %s: %v\n", pair.k, tmuxName, err)
				}
			}
		}()
	}

	// Update the stored size
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
