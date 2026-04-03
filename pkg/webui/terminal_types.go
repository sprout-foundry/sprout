package webui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// tmuxSessionPrefix is prepended to session IDs when creating tmux session names.
const tmuxSessionPrefix = "ledit_"

// ShellInfo describes an available shell on the system.
type ShellInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Default bool   `json:"default"`
}

// shellExists checks if a shell binary exists in PATH.
func shellExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// shellResolvePath returns the absolute path of a shell binary (or "" if not found).
func shellResolvePath(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
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

// knownUnixShells is the ordered list of shells to scan for on Unix systems.
var knownUnixShells = []string{
	"bash", "zsh", "fish", "sh", "dash", "ash", "ksh", "csh", "tcsh",
}

// AvailableShells returns a list of shells found on the system.
// On Unix, it scans for common shells plus the user's $SHELL.
// On Windows, it returns cmd.exe and PowerShell if found.
func (tm *TerminalManager) AvailableShells() []ShellInfo {
	if runtime.GOOS == "windows" {
		return tm.availableWindowsShells()
	}
	return tm.availableUnixShells()
}

// availableUnixShells scans PATH for common Unix shells.
func (tm *TerminalManager) availableUnixShells() []ShellInfo {
	userShell := os.Getenv("SHELL")
	// Normalize $SHELL to a basename so comparisons work regardless of
	// whether $SHELL is "/bin/bash" or simply "bash".
	defaultShell := ""
	if userShell != "" {
		defaultShell = filepath.Base(userShell)
	}
	if defaultShell == "" {
		for _, name := range knownUnixShells {
			if shellExists(name) {
				defaultShell = name
				break
			}
		}
	}

	seen := make(map[string]bool)
	var shells []ShellInfo

	for _, name := range knownUnixShells {
		if seen[name] {
			continue
		}
		path := shellResolvePath(name)
		if path == "" {
			continue
		}
		seen[name] = true
		shells = append(shells, ShellInfo{
			Name:    name,
			Path:    path,
			Default: name == defaultShell,
		})
	}

	// Include user's $SHELL if not already in the list.
	if userShell != "" {
		userShellBase := filepath.Base(userShell)
		if !seen[userShellBase] {
			path := shellResolvePath(userShell)
			if path != "" {
				shells = append(shells, ShellInfo{
					Name:    userShellBase,
					Path:    path,
					Default: true,
				})
			}
		}
	}

	return shells
}

// availableWindowsShells returns available shells on Windows.
func (tm *TerminalManager) availableWindowsShells() []ShellInfo {
	var shells []ShellInfo

	if path, err := exec.LookPath("cmd.exe"); err == nil {
		shells = append(shells, ShellInfo{Name: "cmd.exe", Path: path, Default: true})
	}
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		shells = append(shells, ShellInfo{Name: "powershell.exe", Path: path, Default: false})
	}

	return shells
}
