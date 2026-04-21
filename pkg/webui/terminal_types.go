package webui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// ringCapacity is the number of bytes retained in the per-session scrollback ring.
// 256 KB provides enough replay history for typical reconnect scenarios.
const ringCapacity = 256 * 1024

// sessRing is a thread-safe circular byte buffer that retains the last ringCapacity
// bytes of terminal output. Older bytes are silently dropped when the buffer is full.
type sessRing struct {
	mu   sync.Mutex
	data []byte
	head int // index of oldest byte
	n    int // number of bytes currently stored
}

func newSessRing() *sessRing {
	return &sessRing{data: make([]byte, ringCapacity)}
}

// write appends p to the ring, dropping the oldest bytes when the capacity is exceeded.
func (r *sessRing) write(p []byte) {
	if len(p) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cap := len(r.data)
	for _, b := range p {
		if r.n == cap {
			// Buffer full: drop oldest byte by advancing head.
			r.head = (r.head + 1) % cap
		} else {
			r.n++
		}
		r.data[(r.head+r.n-1)%cap] = b
	}
}

// snapshot returns a copy of all buffered bytes in order (oldest first).
func (r *sessRing) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return nil
	}
	out := make([]byte, r.n)
	cap := len(r.data)
	for i := 0; i < r.n; i++ {
		out[i] = r.data[(r.head+i)%cap]
	}
	return out
}

// termSub is a per-WebSocket subscriber for a terminal session's output stream.
// The channel is closed when the PTY process exits or when the subscriber's
// buffer overflows (so the WebSocket goroutine can reconnect and replay the ring).
type termSub struct {
	ch chan []byte
}

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

// TerminalSession represents a persistent terminal session backed by a raw PTY.
// The shell process keeps running even when no WebSocket is connected; output is
// buffered in the ring for replay on reconnect.
type TerminalSession struct {
	ID       string
	Command  *exec.Cmd
	Pty      *os.File
	Cancel   context.CancelFunc
	Active   bool
	mutex    sync.RWMutex
	LastUsed time.Time
	Size     *pty.Winsize

	// History for shell command navigation.
	History      []string
	HistoryIndex int

	// ring holds the last ringCapacity bytes of PTY output for reconnect replay.
	ring *sessRing

	// subs is the list of active WebSocket subscribers.
	subsMu sync.Mutex
	subs   []*termSub
}

// subscribe adds a new WebSocket subscriber and returns its termSub.
func (s *TerminalSession) subscribe() *termSub {
	sub := &termSub{ch: make(chan []byte, 10000)}
	s.subsMu.Lock()
	s.subs = append(s.subs, sub)
	s.subsMu.Unlock()
	return sub
}

// unsubscribe removes a subscriber. Safe to call even if the sub was already removed.
func (s *TerminalSession) unsubscribe(sub *termSub) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for i, existing := range s.subs {
		if existing == sub {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

// broadcast writes p to the ring buffer and forwards it to all active subscribers.
// If a subscriber's channel is full it is evicted (its channel is closed) so the
// WebSocket goroutine can reconnect and replay the ring.
func (s *TerminalSession) broadcast(p []byte) {
	s.ring.write(p)

	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	live := s.subs[:0]
	for _, sub := range s.subs {
		select {
		case sub.ch <- p:
			live = append(live, sub)
		default:
			// Subscriber channel full — evict and close so the goroutine stops.
			close(sub.ch)
		}
	}
	s.subs = live
}

// closeAllSubs closes every active subscriber channel (signals PTY exit) and
// clears the subscriber list.
func (s *TerminalSession) closeAllSubs() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, sub := range s.subs {
		close(sub.ch)
	}
	s.subs = nil
}

// TerminalManager manages terminal sessions.
type TerminalManager struct {
	sessions      map[string]*TerminalSession
	mutex         sync.RWMutex
	workspaceRoot string
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(workspaceRoot string) *TerminalManager {
	return &TerminalManager{
		sessions:      make(map[string]*TerminalSession),
		workspaceRoot: workspaceRoot,
	}
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
