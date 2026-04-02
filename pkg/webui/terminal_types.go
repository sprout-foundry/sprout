package webui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// tmuxSessionPrefix is prepended to session IDs when creating tmux session names.
const tmuxSessionPrefix = "ledit_"

// shellExists checks if a shell binary exists in PATH.
func shellExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// TerminalSession represents a terminal session.
type TerminalSession struct {
	ID           string
	Command      *exec.Cmd
	Pty          *os.File  // PTY file handle
	Output       io.Reader // PTY handles both input and output
	Cancel       context.CancelFunc
	Active       bool
	mutex        sync.RWMutex
	LastUsed     time.Time
	History      []string
	HistoryIndex int
	OutputCh     chan []byte
	Size         *pty.Winsize  // Terminal size for resizing
	TmuxBacked   bool          // Whether this session uses tmux for persistence
	monitorDone  chan struct{} // Close to signal the monitor goroutine to stop
}

// TerminalManager manages terminal sessions.
type TerminalManager struct {
	sessions      map[string]*TerminalSession
	mutex         sync.RWMutex
	workspaceRoot string
	tmuxAvailable bool
	tmuxCheckOnce sync.Once
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(workspaceRoot string) *TerminalManager {
	tm := &TerminalManager{
		sessions:      make(map[string]*TerminalSession),
		workspaceRoot: workspaceRoot,
	}
	// Check tmux availability once (Unix only)
	tm.tmuxCheckOnce.Do(func() {
		if runtime.GOOS != "windows" {
			tm.tmuxAvailable = shellExists("tmux")
			if tm.tmuxAvailable {
				fmt.Printf("TerminalManager: tmux detected, terminal persistence enabled\n")
			} else {
				fmt.Printf("TerminalManager: tmux not found, terminal persistence disabled (install tmux for persistence)\n")
			}
		}
	})
	return tm
}

// TmuxSessionName returns the tmux session name for a given session ID.
func (tm *TerminalManager) TmuxSessionName(sessionID string) string {
	return tmuxSessionPrefix + sessionID
}

// IsTmuxAvailable returns whether tmux was found at startup.
func (tm *TerminalManager) IsTmuxAvailable() bool {
	return tm.tmuxAvailable
}

// HasSession checks if a session exists (for reattach).
func (tm *TerminalManager) HasSession(sessionID string) bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	_, exists := tm.sessions[sessionID]
	return exists
}

// GetSession retrieves a terminal session.
func (tm *TerminalManager) GetSession(sessionID string) (*TerminalSession, bool) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	session, exists := tm.sessions[sessionID]
	return session, exists
}

// ListSessions returns a list of active session IDs.
func (tm *TerminalManager) ListSessions() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	var sessions []string
	for sessionID := range tm.sessions {
		sessions = append(sessions, sessionID)
	}
	return sessions
}

// GetSessionCount returns the number of active sessions.
func (tm *TerminalManager) GetSessionCount() int {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	count := 0
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.Active {
			count++
		}
		session.mutex.RUnlock()
	}
	return count
}

// SessionCount returns the number of active sessions (alias for GetSessionCount).
func (tm *TerminalManager) SessionCount() int {
	return tm.GetSessionCount()
}
