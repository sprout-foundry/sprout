//go:build !js

package webui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

// testSessionCap bounds live PTY sessions when running under a Go test
// binary. Historical webui tests leaked PTY login shells by the hundreds and
// froze the machine (three OOM incidents, 2026-09); this cap turns any such
// leak into a loud test failure at session #65 instead of a dead laptop.
// Production daemons are unaffected: the check only trips when the process
// is a `go test` binary (name ends in ".test" — also matched by the
// ".test.exe" Windows convention used elsewhere in this repo).
const testSessionCap = 64

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

// envVarsToStripFromUserShell lists environment variables sprout strips from
// the env it hands to user-interactive shells in the webui embedded terminal.
//
// NO_COLOR and FORCE_COLOR are filtered out because sprout's process-wide
// color policy (see cmd/agent_modes.go RunAgent, which auto-sets NO_COLOR=1
// when stdout is not a TTY to keep ANSI out of rotated daemon logs) is
// sprout's own writer concern. The webui embedded terminal has its own real
// PTY and its own xterm.js frontend, so the user's color preferences there
// must come from the user's shell rc files, not from sprout's log-rotation
// policy. Allowing these vars to leak produces spurious Node.js warnings
// ("NO_COLOR env is ignored due to FORCE_COLOR env being set") when JS tools
// with internal FORCE_COLOR=1 run inside the user's shell.
var envVarsToStripFromUserShell = []string{"NO_COLOR", "FORCE_COLOR"}

// buildTerminalEnv constructs the environment slice handed to the user's
// interactive shell in the webui embedded terminal. It starts from a sanitized
// copy of os.Environ() with sprout-internal color vars stripped (see
// envVarsToStripFromUserShell), then layers in the terminal-specific vars
// (TERM, COLORTERM, SHELL, the SPROUT marker vars, and COLUMNS/LINES).
//
// This is the single source of truth for the terminal env block, shared by
// createUnixSession and startPipeSession.
func buildTerminalEnv(shell string, size *pty.Winsize) []string {
	// envOverrides lists the names that buildTerminalEnv sets explicitly on
	// the appended entries below. Any pre-existing entries with the same
	// name from os.Environ() are stripped, otherwise the child would end up
	// with a duplicate (e.g. two SHELL entries) and behavior would depend on
	// whether exec(2) prefers the first or last. Stripping them forces a
	// single, well-defined final value.
	envOverrides := []string{"TERM", "COLORTERM", "SHELL", "SPROUT_WEB_TERMINAL", "SPROUT_WEB_TERMINAL", "COLUMNS", "LINES"}
	filtered := os.Environ()
	filtered = stripEnvVars(filtered, envVarsToStripFromUserShell)
	filtered = stripEnvVars(filtered, envOverrides)

	return append(filtered,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"SHELL="+shell,
		"SPROUT_WEB_TERMINAL=1", "SPROUT_WEB_TERMINAL=1",
		fmt.Sprintf("COLUMNS=%d", size.Cols),
		fmt.Sprintf("LINES=%d", size.Rows),
	)
}

// stripEnvVars returns a new []string with every entry whose name (the part
// before the first '=') matches one of the names in toStrip (case-sensitive,
// per the unix env(7) convention). Order and content of remaining entries
// are preserved.
func stripEnvVars(env, toStrip []string) []string {
	if len(env) == 0 || len(toStrip) == 0 {
		return env
	}
	strip := make(map[string]struct{}, len(toStrip))
	for _, name := range toStrip {
		strip[name] = struct{}{}
	}
	out := env[:0:0] // independent backing array so we don't alias the input slice
	for _, entry := range env {
		idx := strings.IndexByte(entry, '=')
		var name string
		if idx < 0 {
			name = entry // malformed entry; keep it but match against full string
		} else {
			name = entry[:idx]
		}
		if _, drop := strip[name]; drop {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// runningUnderGoTest reports whether this process is a Go test binary (or a
// test that opted in via SPROUT_TEST_SESSION_CAP). See testSessionCap.
func runningUnderGoTest() bool {
	if _, forced := os.LookupEnv("SPROUT_TEST_SESSION_CAP"); forced {
		return true
	}
	name := filepath.Base(os.Args[0])
	return strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe")
}

// checkTestSessionCap refuses new PTY sessions past the test-binary cap.
// Caller must hold tm.mutex. See testSessionCap for why this exists.
func (tm *TerminalManager) checkTestSessionCap() error {
	if !runningUnderGoTest() || len(tm.sessions) < testSessionCap {
		return nil
	}
	return fmt.Errorf("test session cap (%d) reached: a test is leaking PTY sessions — "+
		"close sessions with t.Cleanup (newTestTerminalManager) before creating more", testSessionCap)
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
	if err := tm.checkTestSessionCap(); err != nil {
		return nil, err
	}

	var override string
	if len(shellOverride) > 0 {
		override = shellOverride[0]
	}

	var session *TerminalSession
	var err error

	switch runtime.GOOS {
	case "windows":
		session, err = tm.createWindowsSession(sessionID, override)
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
	cmd.Env = buildTerminalEnv(shell, defaultSize)

	ptyFile, err := pty.StartWithSize(cmd, defaultSize)
	if err != nil {
		// Primary PTY creation failed — this can happen on Alpine Linux,
		// minimal containers, or systems without /dev/pts mounted.
		// Fall back to a basic exec.Cmd-based approach with stdin/stdout pipes.
		webuiLogger.Warn("PTY creation failed; falling back to pipe-based session", slog.String("session_id", sessionID), slog.Any("err", err))
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
			webuiLogger.Error("PTY reader panicked", slog.String("session_id", session.ID), slog.Any("panic", r))
			// Mark inactive and close subscribers so callers don't hang forever
			// waiting for output that will never arrive.
			session.mutex.Lock()
			session.Active = false
			session.closeBackgroundDoneLocked()
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
			// Background sessions: scan for the completion sentinel. Closing
			// bgDone wakes check_background waiters and the wakeup watcher.
			// Cheap no-op for sessions without a marker.
			if completed, code, isBg := session.checkBackgroundSentinel(chunk); completed {
				tm.notifySessionUpdate(map[string]interface{}{
					"session_id": session.ID,
					"chat_id":    session.ChatID,
					"event":      "completed",
					"exit_code":  code,
					"is_bg":      isBg,
				})
			}
			if session.hasSubscribers() {
				session.mutex.Lock()
				session.LastUsed = time.Now()
				session.mutex.Unlock()
			}
		}
		if err != nil {
			webuiLogger.Info("terminal session PTY closed", slog.String("session_id", session.ID), slog.Any("err", err))
			session.mutex.Lock()
			session.Active = false
			// Session died before the sentinel arrived — release any waiters
			// so check_background/wakeup watchers don't block forever.
			session.closeBackgroundDoneLocked()
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
	// SPROUT_TEST_SHELL overrides the whole resolution in test binaries: a
	// leaked PTY session under `go test` then costs one ~2MB /bin/sh instead
	// of a login zsh that sources the user's rc files (nvm etc., hundreds of
	// MB each) — the difference between a nuisance and a machine freeze.
	// (Wired by pkg/webui's TestMain; settable per-test via t.Setenv.)
	if testShell := strings.TrimSpace(os.Getenv("SPROUT_TEST_SHELL")); testShell != "" {
		return testShell, nil, nil
	}
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

// resolveShellArgs returns the extra arguments to pass when launching a shell
// in login/interactive mode so that rc files are sourced correctly.
func resolveShellArgs(shell string) []string {
	base := filepath.Base(shell)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".exe") {
		base = strings.TrimSuffix(base, ext)
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
	if err := tm.checkTestSessionCap(); err != nil {
		return nil, err
	}

	// Panic recovery: if option application panics, clean up the PTY goroutine
	// to prevent a session leak.
	defer func() {
		if r := recover(); r != nil {
			webuiLogger.Error("hidden terminal session creation panicked", slog.String("session_id", id), slog.Any("panic", r))
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
		session, err = tm.createWindowsSession(id, "")
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
