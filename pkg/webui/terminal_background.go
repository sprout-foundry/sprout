//go:build !js

package webui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Sentinel errors returned by GetBackgroundOutput, used for HTTP status code
// mapping in the agent sessions API handlers.
var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrNotBackgroundSession = errors.New("not a background session")
)

// Re-export the agent_tools background exit sentinels locally; the
// TerminalAccess contract and its sentinel values live in one place.
const (
	BgExitNone    = agenttools.BgExitNone
	BgExitStopped = agenttools.BgExitStopped
)

// sentinelPrefix is the marker prefix background commands echo on
// completion. Same convention as ExecuteCommandAndWait's foreground
// sentinel ("__SPROUT_DONE__<marker>:$?").
const sentinelPrefix = "__SPROUT_DONE__"

// bgTailKeep is how many trailing bytes of previously-scanned output are
// carried into the next scan. Must exceed the longest possible sentinel
// (16-byte marker = 32 hex chars) plus prefix and exit digits, so a marker
// split across PTY read chunks is still matched.
const bgTailKeep = 96

// ExecuteCommandInBackground creates a new hidden PTY session for a background command,
// writes the command to it, and returns immediately with the session ID.
// Unlike foreground hidden sessions (one per chat), each background command gets its own session.
// Background sessions get a 2-hour cleanup timeout (vs 30-min for regular hidden sessions).
//
// The command is wrapped with a completion sentinel — the same
// __SPROUT_DONE__<marker>:$? convention ExecuteCommandAndWait uses. The
// session's PTY reader watches for the sentinel and closes the session's
// bgDone channel (recording the exit code) when the command finishes. The
// underlying shell keeps running, so IsSessionActive stays true — bgDone is
// the "command completed" signal, watched by check_background (blocking
// waits) and the wakeup watcher (completion notifications).
func (tm *TerminalManager) ExecuteCommandInBackground(ctx context.Context, chatID, command string) (string, error) {
	if chatID == "" {
		return "", fmt.Errorf("chatID is required for background sessions")
	}
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	// Validate command length (same limit as ExecuteCommandAndWait)
	if len(command) > maxCommandLength {
		return "", fmt.Errorf("command too long: %d bytes (max %d)", len(command), maxCommandLength)
	}

	// Enforce the per-chat background-session cap so a runaway agent can't
	// pile sessions up indefinitely. See maxBackgroundSessionsPerChat for
	// the rationale.
	if tm.countBackgroundSessionsForChat(chatID) >= maxBackgroundSessionsPerChat {
		return "", tm.errBackgroundCapReached(chatID)
	}

	// Generate a session ID: "bg-" + sanitized command prefix (first word) + "-" + random hex (8 chars)
	prefix := extractCommandPrefix(command)
	if prefix == "" {
		prefix = "cmd"
	}
	sanitizedPrefix := sanitizeSessionIDPart(prefix)
	randomHex, err := generateRandomHex(4) // 4 bytes = 8 hex chars
	if err != nil {
		return "", fmt.Errorf("failed to generate random hex: %w", err)
	}
	sessionID := fmt.Sprintf("bg-%s-%s", sanitizedPrefix, randomHex)

	// Extract name from first 30 chars of command (for display purposes)
	name := command
	if len(name) > 30 {
		name = name[:30] + "..."
	}

	// Create hidden session with IsBackground=true
	session, err := tm.CreateHiddenSession(sessionID, "agent", chatID, WithName(name))
	if err != nil {
		return "", fmt.Errorf("failed to create hidden session: %w", err)
	}

	// Wait for the login shell to finish initializing (rc files, banners)
	// before writing the command — same gate GetOrCreateHiddenSessionForChat
	// applies on the foreground path. Without it the command write races
	// shell startup and can be swallowed by rc-file noise (the flaky
	// "sentinel never arrives" failure mode).
	if waitErr := tm.waitForShellReady(ctx, session); waitErr != nil {
		_ = tm.CloseSession(sessionID)
		return "", fmt.Errorf("background session created but shell not ready: %w", waitErr)
	}

	// Generate the completion marker and install the sentinel watch BEFORE
	// writing the command, so the reader cannot miss the sentinel even if
	// the command completes before this function returns.
	marker, err := generateMarker()
	if err != nil {
		_ = tm.CloseSession(sessionID)
		return "", fmt.Errorf("failed to generate sentinel marker: %w", err)
	}

	session.mutex.Lock()
	session.IsBackground = true
	session.bgMarker = marker
	session.bgDone = make(chan struct{})
	session.bgExitCode = BgExitNone
	session.mutex.Unlock()

	session.mutex.Lock()
	pty := session.Pty
	active := session.Active
	session.mutex.Unlock()

	if pty == nil || !active {
		return "", fmt.Errorf("session became inactive before command could be sent")
	}

	// Wrap with the completion sentinel: /bin/sh -c '... && echo M:$? || echo M:$?'.
	// /bin/sh guarantees $? semantics regardless of the session's login shell
	// (fish uses $status). Both branches echo, so the sentinel fires
	// regardless of the command's exit code.
	wrappedCmd := wrapBackgroundCommand(command, marker)
	if _, err := pty.Write([]byte(wrappedCmd)); err != nil {
		// Clean up the failed session
		_ = tm.CloseSession(sessionID)
		return "", fmt.Errorf("failed to write command to PTY: %w", err)
	}

	// Update LastUsed so the 2-hour cleanup timer starts from command execution
	session.mutex.Lock()
	session.LastUsed = time.Now()
	session.mutex.Unlock()

	// Notify listeners (BackgroundTasks badge, attachable-session lists).
	tm.notifySessionUpdate(map[string]interface{}{
		"session_id": sessionID,
		"chat_id":    chatID,
		"event":      "started",
		"name":       name,
	})

	return sessionID, nil
}

// wrapBackgroundCommand wraps a command with the completion sentinel, the
// same convention ExecuteCommandAndWait uses. Both the success and failure
// branches echo the marker with the command's exit status, so the sentinel
// fires regardless of exit code.
func wrapBackgroundCommand(command, marker string) string {
	escapedCmd := strings.ReplaceAll(command, "'", "'\\''")
	return fmt.Sprintf(
		"/bin/sh -c '%s && echo \"%s$?\" || echo \"%s$?\"'\n",
		escapedCmd, sentinelPrefix+marker+":", sentinelPrefix+marker+":",
	)
}

// checkBackgroundSentinel examines newly-arrived output for the session's
// completion sentinel. Called from the PTY reader with session.mutex held.
// Carries a small tail of previously-scanned bytes so a sentinel straddling
// a chunk boundary is still detected. Closes bgDone exactly once when the
// sentinel is found, recording the parsed exit code.
//
// Returns (justCompleted, exitCode, true) exactly once — on the transition
// to completed — so the caller can fire the lifecycle hook AFTER releasing
// session.mutex (the hook takes tm.mutex; holding both would invert the
// manager→session lock order and can deadlock).
func (s *TerminalSession) checkBackgroundSentinel(chunk []byte) (justCompleted bool, exitCode int, wasBackground bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.bgMarker == "" || s.bgDone == nil {
		return false, 0, false
	}
	select {
	case <-s.bgDone:
		return false, 0, false // already completed
	default:
	}

	markerStr := []byte(sentinelPrefix + s.bgMarker + ":")

	window := make([]byte, 0, len(s.bgTail)+len(chunk))
	window = append(window, s.bgTail...)
	window = append(window, chunk...)

	idx := bytes.Index(window, markerStr)
	for idx >= 0 {
		after := idx + len(markerStr)
		if after < len(window) {
			next := window[after]
			if next >= '0' && next <= '9' {
				// Real sentinel echo (not the PTY's echo of the wrapped
				// command line, which ends with ":$?"). Parse exit digits.
				var codeStr []byte
				for j := after; j < len(window); j++ {
					if window[j] >= '0' && window[j] <= '9' {
						codeStr = append(codeStr, window[j])
					} else {
						break
					}
				}
				if len(codeStr) > 0 && len(codeStr) <= 3 {
					if code, err := strconv.Atoi(string(codeStr)); err == nil {
						// A BgExitStopped recorded by StopBackgroundSession
						// wins over a stray post-SIGINT sentinel echo: a
						// deliberate stop is the more truthful outcome.
						if s.bgExitCode != BgExitStopped {
							s.bgExitCode = code
						}
						close(s.bgDone)
						s.bgTail = nil
						return true, code, true
					}
				}
			}
		}
		// Not the exit echo — try the next occurrence (e.g. the command's
		// own echo of the wrapped line, which ends with ":$?").
		remaining := window[idx+1:]
		nextIdx := bytes.Index(remaining, markerStr)
		if nextIdx >= 0 {
			idx = idx + 1 + nextIdx
		} else {
			idx = -1
		}
	}

	// Keep only the trailing bytes that could still complete a marker.
	if keep := len(window) - bgTailKeep; keep > 0 {
		s.bgTail = append(s.bgTail[:0:0], window[keep:]...)
	} else {
		s.bgTail = append(s.bgTail[:0:0], window...)
	}
	return false, 0, false
}

// closeBackgroundDoneLocked closes bgDone if not already closed, marking the
// command as finished with code BgExitNone. Used when a session dies before
// producing a sentinel. Caller holds session.mutex. A BgExitStopped recorded
// by StopBackgroundSession is preserved — a deliberate stop is not "ended
// without a report".
func (s *TerminalSession) closeBackgroundDoneLocked() {
	if s.bgDone == nil {
		return
	}
	select {
	case <-s.bgDone:
	default:
		if s.bgExitCode != BgExitStopped {
			s.bgExitCode = BgExitNone
		}
		close(s.bgDone)
	}
}

// BackgroundDoneChan implements TerminalAccess.BackgroundDoneChan.
func (tm *TerminalManager) BackgroundDoneChan(sessionID string) (<-chan struct{}, bool) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return nil, false
	}
	session.mutex.RLock()
	defer session.mutex.RUnlock()
	if !session.IsBackground {
		return nil, false
	}
	if session.bgDone == nil {
		return nil, false
	}
	return session.bgDone, true
}

// BackgroundExitCode implements TerminalAccess.BackgroundExitCode.
// Falls back to the post-close tombstone when the session has already been
// removed from the map (stop/reap) — watchers read the code after bgDone
// closes, which can race session removal.
func (tm *TerminalManager) BackgroundExitCode(sessionID string) int {
	session, exists := tm.GetSession(sessionID)
	if exists {
		session.mutex.RLock()
		isBg := session.IsBackground
		code := session.bgExitCode
		session.mutex.RUnlock()
		if isBg {
			return code
		}
		return BgExitNone
	}
	tm.mutex.RLock()
	code, tombstoned := tm.bgExitTombstones[sessionID]
	tm.mutex.RUnlock()
	if tombstoned {
		return code
	}
	return BgExitNone
}

// GetBackgroundOutput returns the accumulated ring buffer output for a background session.
// The output is stripped of ANSI escape sequences and of the completion
// sentinel line (the wrapped command's echo and the __SPROUT_DONE__ marker
// are transport, not command output).
func (tm *TerminalManager) GetBackgroundOutput(sessionID string) (string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return "", fmt.Errorf("session %s not found: %w", sessionID, ErrSessionNotFound)
	}

	// Verify it's a background session
	session.mutex.RLock()
	isBackground := session.IsBackground
	session.mutex.RUnlock()

	if !isBackground {
		return "", fmt.Errorf("session %s is not a background session: %w", sessionID, ErrNotBackgroundSession)
	}

	// Get the ring buffer snapshot and strip ANSI
	session.mutex.RLock()
	snapshot := session.ring.snapshot()
	marker := session.bgMarker
	session.mutex.RUnlock()

	output := stripANSI(string(snapshot))
	if marker != "" {
		output = stripSentinelLines(output, marker)
	}
	return output, nil
}

// stripSentinelLines removes the completion-sentinel result line(s) from
// background output. Only exact sentinel matches are removed — the wrapped
// command's PTY echo is left in place since it can share a line with real
// output under arbitrary PTY chunking.
func stripSentinelLines(output, marker string) string {
	markerLine := sentinelPrefix + marker + ":"
	var b strings.Builder
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, markerLine) {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// StopBackgroundSession terminates a background session by sending Ctrl+C to the PTY
// and then closing the session. Returns an error if the session is not found or is
// not a background session.
func (tm *TerminalManager) StopBackgroundSession(sessionID string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found: %w", sessionID, ErrSessionNotFound)
	}

	// Verify it's a background session
	session.mutex.RLock()
	isBackground := session.IsBackground
	session.mutex.RUnlock()

	if !isBackground {
		return fmt.Errorf("session %s is not a background session: %w", sessionID, ErrNotBackgroundSession)
	}

	// Record that this was a deliberate stop (not a natural completion)
	// before anything closes the PTY, so watchers report the right outcome.
	// A command that already completed keeps its real exit code.
	session.mutex.Lock()
	if session.bgDone != nil {
		select {
		case <-session.bgDone:
			// Already completed on its own — keep the real exit code.
		default:
			session.bgExitCode = BgExitStopped
		}
	}
	session.mutex.Unlock()

	// Send Ctrl+C to interrupt any running command (best-effort).
	session.mutex.RLock()
	if session.Pty != nil {
		_, _ = session.Pty.Write([]byte{3}) // Ctrl+C
	}
	session.mutex.RUnlock()

	// Brief pause to let the signal propagate before closing.
	time.Sleep(50 * time.Millisecond)

	// Close the session entirely. CloseSession closes bgDone (still open
	// unless the command already completed), releasing any
	// check_background waiters and the wakeup watcher.
	err := tm.CloseSession(sessionID)

	// Notify listeners (BackgroundTasks badge, attachable-session lists).
	if err == nil {
		tm.notifySessionUpdate(map[string]interface{}{
			"session_id": sessionID,
			"event":      "stopped",
		})
	}
	return err
}

// IsSessionActive checks whether a session is still active.
func (tm *TerminalManager) IsSessionActive(sessionID string) bool {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return false
	}
	session.mutex.RLock()
	active := session.Active
	session.mutex.RUnlock()
	return active
}

// HasRunningBackgroundSessions reports whether any background session still
// has a command in flight (sentinel not yet observed, session alive). The
// idle-context evictor consults this to avoid killing background commands
// the tool results promised would live for up to 2 hours — and whose wakeup
// watchers belong to agents that would be released by the same eviction.
func (tm *TerminalManager) HasRunningBackgroundSessions() bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	for _, session := range tm.sessions {
		session.mutex.RLock()
		running := session.IsBackground && session.Active && session.bgDone != nil
		var done <-chan struct{}
		if session.bgDone != nil {
			done = session.bgDone
		}
		session.mutex.RUnlock()
		if !running {
			continue
		}
		select {
		case <-done:
			continue // command finished; session is just awaiting cleanup
		default:
			return true
		}
	}
	return false
}

// extractCommandPrefix extracts the first word from a command (up to the first space or special character).
// Used for generating readable session IDs for background commands.
func extractCommandPrefix(command string) string {
	// Trim leading whitespace
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	// Find the first word (up to space or shell metacharacter)
	for i, r := range command {
		// Stop at whitespace, &, |, ;, >, <, (, ), \, or quote marks
		if r == ' ' || r == '\t' || r == '\n' || r == '&' || r == '|' || r == ';' ||
			r == '>' || r == '<' || r == '(' || r == ')' || r == '\\' ||
			r == '"' || r == '\'' || r == '`' {
			return command[:i]
		}
	}

	// Entire command is a single word
	return command
}

// sanitizeSessionIDPart sanitizes a string for use in a session ID component.
// Replaces invalid characters with hyphens and limits length.
func sanitizeSessionIDPart(part string) string {
	const maxLen = 32 // limit to 32 chars for the prefix part
	var b strings.Builder
	for i, r := range part {
		if i >= maxLen {
			break
		}
		// Only allow alphanumeric, hyphens, underscores, and dots
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := b.String()
	if result == "" {
		return "unknown"
	}
	return result
}

// generateRandomHex generates a random hex string of the specified byte length.
// For example, n=4 returns 8 hex characters.
func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
