//go:build !js

package webui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
)

var validSessionID = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

// ErrSessionExists is returned by CreateSession and CreateHiddenSession when a
// session with the requested ID already exists. Callers can use errors.Is to
// detect this condition for idempotent get-or-create patterns.
var ErrSessionExists = errors.New("session already exists")

func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("session ID too long (max 128 characters)")
	}
	if !validSessionID.MatchString(id) {
		return fmt.Errorf("session ID contains invalid characters (allowed: alphanumeric, hyphens, underscores, dots)")
	}
	return nil
}

// CreateSession creates a new terminal session with PTY support.
// The shell process runs for the lifetime of the session and persists across
// WebSocket disconnections. On reconnect, the ring buffer replays recent output.
// shellOverride, if non-empty, specifies the preferred shell binary (must be in PATH).
func (tm *TerminalManager) CreateSession(sessionID string, shellOverride ...string) (*TerminalSession, error) {
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrSessionExists, sessionID)
	}

	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}

	var session *TerminalSession
	var err error

	switch runtime.GOOS {
	case "windows":
		session, err = tm.createWindowsSession(sessionID)
	default:
		session, err = tm.createUnixSession(sessionID, override)
	}

	if err != nil {
		return nil, err
	}

	tm.sessions[sessionID] = session
	return session, nil
}

// createUnixSession spawns a raw PTY terminal session. A background goroutine
// reads PTY output into the ring buffer and broadcasts to any WebSocket subscribers.
// The shell process keeps running even when no subscriber is attached.
func (tm *TerminalManager) createUnixSession(sessionID, shellOverride string) (*TerminalSession, error) {
	shell, shellArgs, err := tm.resolveShell(shellOverride)
	if err != nil {
		return nil, fmt.Errorf("resolve shell: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	defaultSize := &pty.Winsize{Rows: 24, Cols: 80}

	// COLUMNS and LINES are set to the default PTY size so tools that read them
	// at startup (e.g. Node.js packages) get a valid value. Shells update
	// $COLUMNS dynamically in response to SIGWINCH when the frontend resizes.
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"SHELL="+shell,
		"SPROUT_WEB_TERMINAL=1", "LEDIT_WEB_TERMINAL=1",
		fmt.Sprintf("COLUMNS=%d", defaultSize.Cols),
		fmt.Sprintf("LINES=%d", defaultSize.Rows),
	)

	ptyFile, err := pty.StartWithSize(cmd, defaultSize)
	if err != nil {
		// Primary PTY creation failed — this can happen on Alpine Linux,
		// minimal containers, or systems without /dev/pts mounted.
		// Fall back to a basic exec.Cmd-based approach with stdin/stdout pipes.
		log.Printf("PTY creation failed for session %s (%v), falling back to pipe-based session", sessionID, err)
		cancel() // cancel the original context; createFallbackUnixSession creates its own.
		return tm.createFallbackUnixSession(sessionID, shellOverride)
	}

	session := &TerminalSession{
		ID:        sessionID,
		Command:   cmd,
		Pty:       ptyFile,
		Cancel:    cancel,
		Active:    true,
		LastUsed:  time.Now(),
		StartedAt: time.Now(),
		Size:      defaultSize,
		ring:      newSessRing(),
	}

	go tm.runPTYReader(session)

	return session, nil
}

// runPTYReader reads output from the PTY, writing it to the session's ring buffer
// and broadcasting to all active subscribers. The goroutine runs for the entire
// lifetime of the shell process — it only exits when the PTY closes.
func (tm *TerminalManager) runPTYReader(session *TerminalSession) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PTY reader panic for session %s: %v", session.ID, r)
			// Mark inactive and close subscribers so callers don't hang forever
			// waiting for output that will never arrive.
			session.mutex.Lock()
			session.Active = false
			session.mutex.Unlock()
			session.closeAllSubs()
		}
	}()

	buf := make([]byte, 32768)
	for {
		session.mutex.RLock()
		pty := session.Pty
		session.mutex.RUnlock()

		if pty == nil {
			return
		}

		n, err := pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			// Only refresh LastUsed when at least one subscriber is actively
			// watching. A disconnected session that still produces output
			// (shell prompt clocks, async notifications, background jobs) must
			// not keep itself alive forever — otherwise the cleanup worker
			// never evicts it and the PTY process leaks until daemon restart.
			// The ring buffer still captures the output for scrollback replay
			// regardless of subscriber count.
			session.broadcast(chunk)
			if session.hasSubscribers() {
				session.mutex.Lock()
				session.LastUsed = time.Now()
				session.mutex.Unlock()
			}
		}
		if err != nil {
			log.Printf("Terminal session %s PTY closed: %v", session.ID, err)
			session.mutex.Lock()
			session.Active = false
			session.mutex.Unlock()
			session.closeAllSubs()
			return
		}
	}
}

// createFallbackUnixSession creates a terminal session without a real PTY,
// using exec.Cmd with stdin/stdout pipes instead. This is the fallback path
// when pty.StartWithSize fails (e.g. on Alpine Linux, minimal containers, or
// systems without /dev/pts mounted).
//
// The approach mirrors createWindowsSession: the stdin pipe is stored in
// session.Pty (so WriteRawInput and ExecuteCommandInHidden continue to work),
// and a goroutine reads from stdout to broadcast output to subscribers.
//
// Limitations of the fallback mode (session.NoPTY == true):
//   - Terminal resize is a no-op (pipes don't support TIOCSWINSZ).
//   - Full interactive terminal features (line editing, cursor control) are
//     degraded because the shell is not connected to a real TTY.
//   - Signal delivery (Ctrl+C via \x03) may not work reliably.
func (tm *TerminalManager) createFallbackUnixSession(sessionID, shellOverride string) (*TerminalSession, error) {
	shell, shellArgs, err := tm.resolveShell(shellOverride)
	if err != nil {
		return nil, fmt.Errorf("resolve shell: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	defaultSize := &pty.Winsize{Rows: 24, Cols: 80}

	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"SHELL="+shell,
		"SPROUT_WEB_TERMINAL=1", "LEDIT_WEB_TERMINAL=1",
		fmt.Sprintf("COLUMNS=%d", defaultSize.Cols),
		fmt.Sprintf("LINES=%d", defaultSize.Rows),
	)

	// Create stdin pipe — stored in session.Pty for write compatibility.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("fallback session stdin pipe: %w", err)
	}

	// Combine stdout and stderr into a single reader.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("fallback session stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("fallback session start: %w", err)
	}

	// Wrap the stdin pipe as an *os.File so session.Pty.Write continues to work.
	// stdin is an *os.File on Unix (os.Pipe), so this assertion should succeed.
	ptyFile, ok := stdin.(*os.File)
	if !ok {
		// Close pipes and process if type assertion fails.
		stdin.Close()
		stdout.Close()
		cmd.Process.Kill()
		cancel()
		return nil, fmt.Errorf("fallback session: stdin pipe is not *os.File (unexpected type %T)", stdin)
	}

	session := &TerminalSession{
		ID:        sessionID,
		Command:   cmd,
		Pty:       ptyFile,
		Cancel:    cancel,
		Active:    true,
		LastUsed:  time.Now(),
		StartedAt: time.Now(),
		Size:      defaultSize,
		ring:      newSessRing(),
		NoPTY:     true,
	}

	// Reader goroutine — reads from stdout pipe (not from session.Pty which is stdin).
	go func() {
		buf := make([]byte, 32768)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				session.mutex.Lock()
				session.LastUsed = time.Now()
				session.mutex.Unlock()
				session.broadcast(chunk)
			}
			if readErr != nil {
				log.Printf("Fallback session %s stdout closed: %v", session.ID, readErr)
				session.mutex.Lock()
				session.Active = false
				session.mutex.Unlock()
				session.closeAllSubs()
				return
			}
		}
	}()

	return session, nil
}

// resolveShell determines which shell to use on Unix systems.
// shellOverride, if non-empty, is used directly (after verifying it exists).
func (tm *TerminalManager) resolveShell(shellOverride string) (shell string, shellArgs []string, err error) {
	override := strings.TrimSpace(shellOverride)
	if override != "" {
		if !shellExists(override) {
			return "", nil, fmt.Errorf("requested shell %q not found in PATH", override)
		}
		return override, resolveShellArgs(override), nil
	}

	// Prefer the user's login shell, then fall back to common choices.
	candidates := []string{os.Getenv("SHELL")}
	for _, s := range []string{"bash", "zsh", "sh", "fish"} {
		candidates = append(candidates, s)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if shellExists(candidate) {
			return candidate, resolveShellArgs(candidate), nil
		}
	}
	return "", nil, fmt.Errorf("no suitable shell found; tried %v", candidates)
}

// createWindowsSession creates a fallback session for Windows (non-PTY).
func (tm *TerminalManager) createWindowsSession(sessionID string) (*TerminalSession, error) {
	// Windows implementation - simplified fallback without PTY.
	// Full PTY on Windows requires conpty which is more complex.
	cmd := exec.Command("cmd")
	ctx, cancel := context.WithCancel(context.Background())
	cmd = exec.CommandContext(ctx, cmd.Path)
	if strings.TrimSpace(tm.workspaceRoot) != "" {
		cmd.Dir = tm.workspaceRoot
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	session := &TerminalSession{
		ID:        sessionID,
		Command:   cmd,
		Cancel:    cancel,
		Active:    true,
		LastUsed:  time.Now(),
		StartedAt: time.Now(),
		Size:      &pty.Winsize{Rows: 24, Cols: 80},
		ring:      newSessRing(),
	}

	// Store stdin in the Pty field for WriteRawInput compatibility.
	if ptyFile, ok := stdin.(*os.File); ok {
		session.Pty = ptyFile
	}

	// Start a reader goroutine for the stdout pipe.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Windows terminal reader panic for session %s: %v", sessionID, r)
				session.mutex.Lock()
				session.Active = false
				session.mutex.Unlock()
				session.closeAllSubs()
			}
		}()
		buf := make([]byte, 32768)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				session.mutex.Lock()
				session.LastUsed = time.Now()
				session.mutex.Unlock()
				session.broadcast(chunk)
			}
			if err != nil {
				session.mutex.Lock()
				session.Active = false
				session.mutex.Unlock()
				session.closeAllSubs()
				return
			}
		}
	}()

	return session, nil
}

// resolveShellArgs returns the extra arguments to pass when launching a shell
// in login/interactive mode so that rc files are sourced correctly.
func resolveShellArgs(shell string) []string {
	base := shell
	if idx := strings.LastIndex(shell, "/"); idx >= 0 {
		base = shell[idx+1:]
	}
	switch base {
	case "bash", "zsh":
		return []string{"--login"}
	default:
		return nil
	}
}

// SessionOption is a functional option for configuring a terminal session.
type SessionOption func(*TerminalSession)

// WithName sets a human-readable name for the session.
func WithName(name string) SessionOption {
	return func(s *TerminalSession) {
		s.Name = strings.TrimSpace(name)
	}
}

// WithAutoClose sets whether the session should be auto-closed when inactive.
func WithAutoClose(autoClose bool) SessionOption {
	return func(s *TerminalSession) {
		s.AutoClose = autoClose
	}
}

// CreateHiddenSession creates a hidden PTY session for agent use.
// Hidden sessions are excluded from the default ListSessions() output
// but still participate in inactive-session cleanup.
//
// NOTE: Session creation runs while holding tm.mutex to prevent the PTY
// reader goroutine (launched by createUnixSession/createWindowsSession)
// from being visible to ListSessions() before the Hidden flag is set.
func (tm *TerminalManager) CreateHiddenSession(id, owner, chatID string, opts ...SessionOption) (session *TerminalSession, err error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	chatID = strings.TrimSpace(chatID)
	if owner == "" {
		return nil, fmt.Errorf("hidden session owner is required")
	}
	if chatID == "" {
		return nil, fmt.Errorf("hidden session chatID is required")
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	// NOTE: Session creation (including blocking PTY startup) happens while
	// holding tm.mutex. This serializes session creation but ensures the PTY
	// reader goroutine never sees a session with Hidden=false in the map.
	// For Phase A, this trade-off is acceptable. If creation latency becomes
	// a concern, consider two-phase creation with a "creating" sentinel state.

	// Check for duplicate session ID while holding the lock.
	if _, exists := tm.sessions[id]; exists {
		return nil, fmt.Errorf("%w: %s", ErrSessionExists, id)
	}

	// Panic recovery: if option application panics, clean up the PTY goroutine
	// to prevent a session leak.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CreateHiddenSession panic for %s: %v", id, r)
			if session != nil {
				session.mutex.Lock()
				if session.Pty != nil {
					session.Pty.Close()
					session.Pty = nil
				}
				if session.Cancel != nil {
					session.Cancel()
					session.Cancel = nil
				}
				session.mutex.Unlock()
			}
			session = nil
			err = fmt.Errorf("CreateHiddenSession panic: %v", r)
		}
	}()

	// Create the underlying PTY session (without inserting into map).
	switch runtime.GOOS {
	case "windows":
		session, err = tm.createWindowsSession(id)
	default:
		session, err = tm.createUnixSession(id, "")
	}

	if err != nil {
		return nil, err
	}

	// Set hidden metadata before inserting into map.
	session.mutex.Lock()
	// Unlock in defer so the panic recover below can safely re-acquire the
	// lock to clean up Pty and Cancel fields.
	func() {
		defer session.mutex.Unlock()
		session.Hidden = true
		session.Owner = owner
		session.ChatID = chatID
		// AutoClose is reserved for SP-008 Phase B — not yet consumed by the
		// cleanup worker. When consumed, hidden sessions should auto-expire
		// after N minutes of inactivity.
		session.AutoClose = true // default for hidden sessions

		for _, opt := range opts {
			opt(session)
		}
	}()

	// Now insert into map with hidden flag already set.
	tm.sessions[id] = session

	return session, nil
}
