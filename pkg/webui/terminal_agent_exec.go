package webui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Cached regexes for performance in the hot path.
var (
	newlineRegex = regexp.MustCompile(`\r?\n`)
)

// ExecuteCommandAndWait executes a command synchronously on a hidden PTY session,
// waiting for command completion and returning the output and exit code.
// This function is designed for agent use on hidden sessions only.
//
// The command is wrapped via /bin/sh -c with a sentinel marker to detect completion:
//
//	/bin/sh -c '<command> && echo "__SPROUT_DONE__<marker>:$?" || echo "__SPROUT_DONE__<marker>:$?"'
//
// Using /bin/sh ensures $? works regardless of the session's login shell
// (e.g., fish uses $status instead of $?).
//
// Parameters:
//   - ctx: context for cancellation and timeout control
//   - session: the terminal session to execute the command on (must be hidden)
//   - command: the command string to execute
//
// Returns:
//   - output: the command output with ANSI escape sequences stripped
//   - exitCode: the command's exit code (or -1 if timeout/cancelled)
//   - err: any error that occurred during execution
func (tm *TerminalManager) ExecuteCommandAndWait(ctx context.Context, session *TerminalSession, command string) (output string, exitCode int, err error) {
	// Serialize command execution on this session to prevent interleaved output.
	session.execMu.Lock()
	defer session.execMu.Unlock()

	// Validate session state.
	session.mutex.RLock()
	if !session.Active {
		session.mutex.RUnlock()
		return "", -1, fmt.Errorf("session %s is not active", session.ID)
	}
	if session.Pty == nil {
		session.mutex.RUnlock()
		return "", -1, fmt.Errorf("no PTY available for session %s", session.ID)
	}
	if !session.Hidden {
		session.mutex.RUnlock()
		return "", -1, fmt.Errorf("ExecuteCommandAndWait is only for hidden sessions")
	}
	session.mutex.RUnlock()

	// Generate a unique sentinel marker.
	marker, err := generateMarker()
	if err != nil {
		return "", -1, fmt.Errorf("failed to generate sentinel marker: %w", err)
	}

	// Reject commands with embedded newlines.
	if strings.Contains(command, "\n") {
		return "", -1, fmt.Errorf("commands with embedded newlines are not supported; use separate commands")
	}

	// Validate that the command is not empty or whitespace-only.
	if strings.TrimSpace(command) == "" {
		return "", -1, fmt.Errorf("command is empty")
	}

	// Build the wrapped command with sentinel-based exit code detection.
	// We use a subshell pattern to ensure $? always reflects the command's exit status.
	// Use /bin/sh to ensure the sentinel command works on all shells.
	// $? is not supported by fish (which uses $status). Hidden sessions
	// may be running under the user's login shell via resolveShell.
	escapedCmd := strings.ReplaceAll(command, "'", "'\\''")
	wrappedCmd := fmt.Sprintf("/bin/sh -c '%s && echo \"__SPROUT_DONE__%s:$?\" || echo \"__SPROUT_DONE__%s:$?\"'\n", escapedCmd, marker, marker)

	// Build a sentinel regex that matches the ACTUAL output line, not the PTY echo.
	// The echo contains the literal "$?" while the output has a real exit code (digits).
	// The regex captures the exit code in group 1.
	sentinelRe := regexp.MustCompile(fmt.Sprintf(`__SPROUT_DONE__%s:(\d+)\s*\r?\n`, regexp.QuoteMeta(marker)))

	// Subscribe to the session's output stream.
	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// Check if context is already cancelled before sending the command.
	select {
	case <-ctx.Done():
		return "", -1, ctx.Err()
	default:
	}

	// Write the command to the PTY.
	session.mutex.Lock()
	if session.Pty == nil || !session.Active {
		session.mutex.Unlock()
		return "", -1, fmt.Errorf("session became inactive before command could be sent")
	}
	_, err = session.Pty.Write([]byte(wrappedCmd))
	if err != nil {
		session.mutex.Unlock()
		return "", -1, fmt.Errorf("failed to write command to PTY: %w", err)
	}
	session.mutex.Unlock()

	// Buffer to accumulate output from the PTY.
	var buf bytes.Buffer

	// Apply a 30s default timeout only when the caller hasn't set one.
	// This allows callers to pass longer deadlines for slow commands.
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	} else {
		cancel = func() {}
	}
	defer cancel()

	// Wait for the sentinel line to appear in the output.
	for {
		select {
		case <-ctx.Done():
			// Context cancelled or timeout.
			// Try to interrupt the running command so the session can be reused.
			session.mutex.RLock()
			if session.Pty != nil {
				// Best-effort Ctrl+C to interrupt any running command so the
				// session can be reused. Error is ignored — the PTY may already
				// be closed or the command may have already exited.
				_, _ = session.Pty.Write([]byte{3})
			}
			session.mutex.RUnlock()
			return stripANSI(buf.String()), -1, ctx.Err()

		case chunk, ok := <-sub.ch:
			if !ok {
				// Channel closed — PTY session terminated unexpectedly.
				return stripANSI(buf.String()), -1, fmt.Errorf("PTY session terminated while waiting for command completion")
			}
			buf.Write(chunk)

			// Only match when the full sentinel (with numeric exit code) appears.
			// The regex will not match the PTY echo which contains the literal "$?".
			matches := sentinelRe.FindSubmatch(buf.Bytes())
			if matches != nil {
				// Parse exit code from regex capture group 1.
				code, err := strconv.Atoi(string(matches[1]))
				if err != nil {
					return stripANSI(buf.String()), -1, fmt.Errorf("failed to parse exit code from sentinel: %w", err)
				}

				// Find the sentinel line position and strip from there.
				loc := sentinelRe.FindIndex(buf.Bytes())
				output := buf.String()[:loc[0]]

				// Strip the command echo from the beginning of the output.
				// The PTY echoes the wrapped command we sent ("/bin/sh -c '...'").
				// We find the echo by looking for the "/bin/sh -c '" prefix and
				// stripping everything up to and including the first newline after it.
				echoPrefix := "/bin/sh -c '"
				echoIdx := strings.Index(output, echoPrefix)
				if echoIdx != -1 {
					// Find the first newline after the echo prefix start.
					restAfterEcho := output[echoIdx:]
					newlineIdx := newlineRegex.FindStringIndex(restAfterEcho)
					if newlineIdx != nil {
						// Strip everything from the echo start through the newline
						output = output[echoIdx+newlineIdx[1]:]
					}
				}

				// Return the stripped output and exit code.
				return stripANSI(output), code, nil
			}
		}
	}
}

// generateMarker creates a unique hex string using crypto/rand.
func generateMarker() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// stripANSI removes ANSI escape sequences from the output string.
// This handles common escape sequences including:
// - CSI sequences: \x1b[<params><letter> (colors, cursor, modes)
// - CSI private sequences: \x1b[?<params><letter> (bracketed paste, cursor show/hide)
// - OSC sequences: \x1b]...\x07 (window title, etc.)
// - DCS/PM/APC sequences: \x1b[P^_...\x07 (device control strings, etc.)
// - Two-character ESC sequences: \x1b[A-Z, etc.
// - C0 control characters (except \t, \n, \r which are preserved)
var ansiEscapeRegex = regexp.MustCompile(
	`\x1b\[\??[0-9;]*[a-zA-Z]` + // CSI sequences (including private ? prefix)
		`|\x1b\][^\x07]*\x07` + // OSC sequences (terminated by BEL)
		`|\x1b[P^_][^\x07]*\x07` + // DCS/PM/APC sequences (terminated by BEL)
		`|\x1b[P^_]` + // Hanging DCS/PM/APC (no terminator)
		`|\x1b[^[\]P^_]` + // Two-character ESC sequences (e.g., ESC c, ESC 7)
		`|[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`, // Other C0 controls and DEL (except \t=\x09, \n=\x0a, \r=\x0d)
)

func stripANSI(s string) string {
	return ansiEscapeRegex.ReplaceAllString(s, "")
}

// ExecuteCommandInHidden is a convenience wrapper that looks up a hidden session by ID
// and executes a command synchronously, returning the output and exit code.
//
// This is the primary entry point for agent code that needs to run a command
// in a hidden PTY session and wait for completion.
func (tm *TerminalManager) ExecuteCommandInHidden(ctx context.Context, sessionID, command string) (string, int, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return "", -1, fmt.Errorf("session %s not found", sessionID)
	}

	return tm.ExecuteCommandAndWait(ctx, session, command)
}
