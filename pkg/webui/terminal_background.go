package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors returned by GetBackgroundOutput, used for HTTP status code
// mapping in the agent sessions API handlers.
var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrNotBackgroundSession = errors.New("not a background session")
)

// ExecuteCommandInBackground creates a new hidden PTY session for a background command,
// writes the command to it, and returns immediately with the session ID.
// Unlike foreground hidden sessions (one per chat), each background command gets its own session.
// Background sessions get a 2-hour cleanup timeout (vs 30-min for regular hidden sessions).
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

	// Mark session as background for extended cleanup timeout
	session.mutex.Lock()
	session.IsBackground = true
	session.mutex.Unlock()

	// Write command to the PTY (no sentinel, no waiting - fire and forget)
	session.mutex.Lock()
	pty := session.Pty
	active := session.Active
	session.mutex.Unlock()

	if pty == nil || !active {
		return "", fmt.Errorf("session became inactive before command could be sent")
	}

	// Write command + newline to execute it
	_, err = pty.Write([]byte(command + "\n"))
	if err != nil {
		// Clean up the failed session
		_ = tm.CloseSession(sessionID)
		return "", fmt.Errorf("failed to write command to PTY: %w", err)
	}

	// Update LastUsed so the 2-hour cleanup timer starts from command execution
	session.mutex.Lock()
	session.LastUsed = time.Now()
	session.mutex.Unlock()

	return sessionID, nil
}

// GetBackgroundOutput returns the accumulated ring buffer output for a background session.
// The output is stripped of ANSI escape sequences for readability.
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
	session.mutex.RUnlock()

	output := stripANSI(string(snapshot))
	return output, nil
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

	// Send Ctrl+C to interrupt any running command (best-effort).
	session.mutex.RLock()
	if session.Pty != nil {
		_, _ = session.Pty.Write([]byte{3}) // Ctrl+C
	}
	session.mutex.RUnlock()

	// Brief pause to let the signal propagate before closing.
	time.Sleep(50 * time.Millisecond)

	// Close the session entirely.
	return tm.CloseSession(sessionID)
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
