//go:build !js

package webui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	LastUsed  time.Time
	StartedAt time.Time // When the session was created (for duration display)
	Size     *pty.Winsize

	// Hidden session metadata — used for agent background PTY sessions.
	Hidden      bool   `json:"-"`
	IsBackground bool   `json:"-"` // true for background sessions (2-hour timeout vs 30-min for regular hidden)
	Owner       string `json:"-"` // "agent" or other entity that created this session
	ChatID      string `json:"-"` // chat session that owns this terminal
	Name        string `json:"-"` // human-readable name (e.g. command prefix for background tasks)
	AutoClose   bool   `json:"-"` // reserved for Phase B: close automatically when inactive

	// NoPTY indicates this session is running in fallback mode without a real
	// PTY (e.g. on Alpine Linux or minimal containers where /dev/pts is
	// unavailable). Commands are run via exec.Cmd with stdin/stdout pipes
	// instead. Terminal resize and raw terminal features are degraded.
	NoPTY bool `json:"-"`

	// History for shell command navigation.
	History      []string
	HistoryIndex int

	// ring holds the last ringCapacity bytes of PTY output for reconnect replay.
	ring *sessRing

	// subs is the list of active WebSocket subscribers.
	subsMu sync.Mutex
	subs   []*termSub

	// execMu serializes ExecuteCommandAndWait calls on the same session to prevent
	// interleaved sentinel markers and output from concurrent commands.
	execMu sync.Mutex
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
	cleanupOnce   sync.Once // ensures StartCleanupWorker only launches one goroutine
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(workspaceRoot string) *TerminalManager {
	return &TerminalManager{
		sessions:      make(map[string]*TerminalSession),
		workspaceRoot: workspaceRoot,
	}
}

// HasSession checks if a session exists (for reattach). Returns true for both
// visible and hidden sessions. Use HasVisibleSession() for user-facing checks.
func (tm *TerminalManager) HasSession(sessionID string) bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	_, exists := tm.sessions[sessionID]
	return exists
}

// HasVisibleSession checks if a non-hidden session exists (for user-facing checks).
// Holds tm.mutex while reading session.Hidden to maintain consistent lock ordering
// with ListSessions/ListHiddenSessions/GetSessionCount (tm.mutex → session.mutex).
func (tm *TerminalManager) HasVisibleSession(sessionID string) bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	session, exists := tm.sessions[sessionID]
	if !exists {
		return false
	}
	session.mutex.RLock()
	defer session.mutex.RUnlock()
	return !session.Hidden
}

// GetSession retrieves any terminal session, including hidden ones.
// Callers that need user-facing access should check session.Hidden before
// exposing session data, or use HasVisibleSession() first.
func (tm *TerminalManager) GetSession(sessionID string) (*TerminalSession, bool) {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	session, exists := tm.sessions[sessionID]
	return session, exists
}

// ListSessions returns a list of active session IDs, excluding hidden sessions.
func (tm *TerminalManager) ListSessions() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	var sessions []string
	for sessionID, session := range tm.sessions {
		session.mutex.RLock()
		hidden := session.Hidden
		session.mutex.RUnlock()
		if !hidden {
			sessions = append(sessions, sessionID)
		}
	}
	return sessions
}

// ListAllSessions returns ALL session IDs, including hidden (agent-owned) sessions.
// WARNING: Do NOT expose this to user-facing APIs. Use ListSessions() for user-visible
// session lists. This is intended for server-side operations like CloseAllSessions().
func (tm *TerminalManager) ListAllSessions() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	var sessions []string
	for sessionID := range tm.sessions {
		sessions = append(sessions, sessionID)
	}
	return sessions
}

// ListHiddenSessions returns only hidden session IDs.
func (tm *TerminalManager) ListHiddenSessions() []string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	var sessions []string
	for sessionID, session := range tm.sessions {
		session.mutex.RLock()
		hidden := session.Hidden
		session.mutex.RUnlock()
		if hidden {
			sessions = append(sessions, sessionID)
		}
	}
	return sessions
}

// GetSessionCount returns the number of all active sessions, including hidden ones.
// For user-facing counts, use GetVisibleSessionCount() instead.
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

// GetVisibleSessionCount returns the number of active sessions that are not hidden.
// Use this for user-facing stats. Use GetSessionCount() for internal/maintenance purposes.
func (tm *TerminalManager) GetVisibleSessionCount() int {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	count := 0
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.Active && !session.Hidden {
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

// GetOrCreateHiddenSessionForChat returns the ID of an existing hidden session for the given
// chat ID, or creates a new one. This enables one-hidden-session-per-chat reuse.
//
// The implementation uses a deterministic session ID ("agent-hidden-<chatID>") and handles
// the TOCTOU race by catching the "already exists" error from CreateHiddenSession and
// re-looking up the session that was created by the winning goroutine.
func (tm *TerminalManager) GetOrCreateHiddenSessionForChat(ctx context.Context, chatID string) (string, error) {
	// First check if we already have a hidden session for this chat (fast path)
	tm.mutex.RLock()
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.Hidden && session.ChatID == chatID && session.Active {
			id := session.ID
			session.mutex.RUnlock()
			tm.mutex.RUnlock()
			return id, nil
		}
		session.mutex.RUnlock()
	}
	tm.mutex.RUnlock()

	// No existing session — create one with deterministic ID "agent-hidden-<chatID>"
	// Sanitize chatID to ensure the resulting session ID is valid.
	sessionID := "agent-hidden-" + sanitizeChatID(chatID)
	session, err := tm.CreateHiddenSession(sessionID, "agent", chatID)
	if err != nil {
		// Handle TOCTOU race: another goroutine may have created this session
		// between our RUnlock and the CreateHiddenSession Lock.
		if errors.Is(err, ErrSessionExists) {
			// Re-lookup the session created by the other goroutine
			existing, exists := tm.GetSession(sessionID)
			if exists {
				existing.mutex.RLock()
				active := existing.Active
				existing.mutex.RUnlock()
				if active {
					return sessionID, nil
				}
				// Session exists but is inactive (e.g., closed after a timeout).
				// Clean it up so we can create a fresh one.
				_ = tm.CloseSession(sessionID)
			}
			// Retry creation after cleanup.
			session, err = tm.CreateHiddenSession(sessionID, "agent", chatID)
			if err != nil {
				return "", fmt.Errorf("failed to create hidden session for chat %s after cleanup: %w", chatID, err)
			}
		} else {
			return "", fmt.Errorf("failed to create hidden session for chat %s: %w", chatID, err)
		}
	}

	// Wait for the shell to finish initializing (source rc files, print banners)
	// before returning the session for command execution.
	if waitErr := tm.waitForShellReady(ctx, session); waitErr != nil {
		// Shell didn't become ready — close the session and return error.
		// The caller will fall back to os/exec.
		_ = tm.CloseSession(sessionID)
		return "", fmt.Errorf("hidden session created but shell not ready: %w", waitErr)
	}

	return sessionID, nil
}

// waitForShellReady waits for the shell in a session to finish initializing
// (sourcing rc files, printing banners, etc.) before commands can be sent.
// It subscribes to the session's output and waits for a quiet period of 500ms
// after the last output chunk, indicating the shell prompt is ready.
// Returns nil if the shell becomes ready, or an error if the context expires.
func (tm *TerminalManager) waitForShellReady(ctx context.Context, session *TerminalSession) error {
	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// Wait up to 10 seconds for shell readiness. Most shells source rc files
	// in under 1 second, but complex setups (pyenv, nvm, starship) can take longer.
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	quietPeriod := 500 * time.Millisecond
	quietTimer := time.NewTimer(quietPeriod)
	defer quietTimer.Stop()

	for {
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("shell did not become ready within timeout for session %s", session.ID)

		case _, ok := <-sub.ch:
			if !ok {
				return fmt.Errorf("PTY channel closed before shell became ready for session %s", session.ID)
			}
			// Reset quiet timer on each output chunk (shell still initializing).
			if !quietTimer.Stop() {
				select {
				case <-quietTimer.C:
				default:
				}
			}
			quietTimer.Reset(quietPeriod)

		case <-quietTimer.C:
			// Quiet period elapsed — shell is ready.
			return nil
		}
	}
}

// sanitizeChatID normalizes a chat ID for use in a session identifier.
// Characters outside [a-zA-Z0-9._-] are replaced with hyphens, and the
// result is truncated to preserve room for the "agent-hidden-" prefix
// within the 128-character session ID limit.
func sanitizeChatID(chatID string) string {
	const maxLen = 128 - len("agent-hidden-") // 115 chars
	var b strings.Builder
	for i, r := range chatID {
		if i >= maxLen {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
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
