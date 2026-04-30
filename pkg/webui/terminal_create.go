package webui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/creack/pty"
)

// CreateSession creates a new terminal session with PTY support.
// The shell process runs for the lifetime of the session and persists across
// WebSocket disconnections. On reconnect, the ring buffer replays recent output.
// shellOverride, if non-empty, specifies the preferred shell binary (must be in PATH).
func (tm *TerminalManager) CreateSession(sessionID string, shellOverride ...string) (*TerminalSession, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if _, exists := tm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}

	switch runtime.GOOS {
	case "windows":
		return tm.createWindowsSession(sessionID)
	default:
		return tm.createUnixSession(sessionID, override)
	}
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
		cancel()
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	session := &TerminalSession{
		ID:       sessionID,
		Command:  cmd,
		Pty:      ptyFile,
		Cancel:   cancel,
		Active:   true,
		LastUsed: time.Now(),
		Size:     defaultSize,
		ring:     newSessRing(),
	}

	tm.sessions[sessionID] = session
	go tm.runPTYReader(session)

	return session, nil
}

// runPTYReader reads output from the PTY, writing it to the session's ring buffer
// and broadcasting to all active subscribers. The goroutine runs for the entire
// lifetime of the shell process — it only exits when the PTY closes.
func (tm *TerminalManager) runPTYReader(session *TerminalSession) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PTY reader panic for session %s: %v\n", session.ID, r)
		}
	}()

	buf := make([]byte, 32768)
	for {
		n, err := session.Pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			session.mutex.Lock()
			session.LastUsed = time.Now()
			session.mutex.Unlock()
			session.broadcast(chunk)
		}
		if err != nil {
			fmt.Printf("Terminal session %s PTY closed: %v\n", session.ID, err)
			session.mutex.Lock()
			session.Active = false
			session.mutex.Unlock()
			session.closeAllSubs()
			return
		}
	}
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
		ID:       sessionID,
		Command:  cmd,
		Cancel:   cancel,
		Active:   true,
		LastUsed: time.Now(),
		ring:     newSessRing(),
	}

	// Store stdin in the Pty field for WriteRawInput compatibility.
	if ptyFile, ok := stdin.(*os.File); ok {
		session.Pty = ptyFile
	}

	tm.sessions[sessionID] = session

	// Start a reader goroutine for the stdout pipe.
	go func() {
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
		s.Name = name
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
func (tm *TerminalManager) CreateHiddenSession(id, owner, chatID string, opts ...SessionOption) (*TerminalSession, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("hidden session ID is required")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, fmt.Errorf("hidden session owner is required")
	}
	if strings.TrimSpace(chatID) == "" {
		return nil, fmt.Errorf("hidden session chatID is required")
	}

	// Create the underlying PTY session.
	session, err := tm.CreateSession(id)
	if err != nil {
		return nil, err
	}

	// Acquire tm.mutex to prevent concurrent ListSessions from seeing
	// the session in the map before its hidden flag is set.  Lock order:
	// tm.mutex → session.mutex (consistent with the rest of the codebase).
	tm.mutex.Lock()
	session.mutex.Lock()
	session.Hidden = true
	session.Owner = owner
	session.ChatID = chatID
	session.AutoClose = true // default for hidden sessions

	for _, opt := range opts {
		opt(session)
	}
	session.mutex.Unlock()
	tm.mutex.Unlock()

	return session, nil
}
