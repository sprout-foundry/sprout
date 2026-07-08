//go:build !js

package webui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxCommandLength = 65536 // 64 KB — well below PTY and shell limits

// Cached regexes for performance in the hot path.
var (
	newlineRegex = regexp.MustCompile(`\r?\n`)
)

// closeSessionAfterGracePeriod runs tm.CloseSession in a goroutine after a
// 100ms grace window. Without a recover here, a panic inside CloseSession
// would crash the daemon and leave the PTY reader goroutine and shell process
// alive (the cleanup that CloseSession performs is what stops both).
func closeSessionAfterGracePeriod(tm *TerminalManager, sid, reason string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PTY session %s: panic in deferred close (%s): %v", sid, reason, r)
			}
		}()
		time.Sleep(100 * time.Millisecond)
		if err := tm.CloseSession(sid); err != nil {
			log.Printf("PTY session %s: failed to close after %s: %v", sid, reason, err)
		}
	}()
}

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

	// Derive a cancellable context so that derived resources are
	// released when this function returns, following Go context best
	// practices.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

	// Validate command length.
	if len(command) > maxCommandLength {
		return "", -1, fmt.Errorf("command too long: %d bytes (max %d)", len(command), maxCommandLength)
	}

	// Build the wrapped command with sentinel-based exit code detection.
	// We use a subshell pattern to ensure $? always reflects the command's exit status.
	// Use /bin/sh to ensure the sentinel command works on all shells.
	// $? is not supported by fish (which uses $status). Hidden sessions
	// may be running under the user's login shell via resolveShell.
	escapedCmd := strings.ReplaceAll(command, "'", "'\\''")
	wrappedCmd := fmt.Sprintf("/bin/sh -c '%s && echo \"__SPROUT_DONE__%s:$?\" || echo \"__SPROUT_DONE__%s:$?\"'\n", escapedCmd, marker, marker)

	// Pre-allocate the sentinel prefix for fast bytes.Index search.
	// markerStr is constant for the entire call so we allocate once.
	markerStr := []byte("__SPROUT_DONE__" + marker + ":")

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
	pty := session.Pty
	active := session.Active
	session.mutex.Unlock()

	if pty == nil || !active {
		return "", -1, fmt.Errorf("session became inactive before command could be sent")
	}
	_, err = pty.Write([]byte(wrappedCmd))
	if err != nil {
		return "", -1, fmt.Errorf("failed to write command to PTY: %w", err)
	}

	// Buffer to accumulate output from the PTY.
	var buf bytes.Buffer

	// Two independent timers gate the wait:
	//
	//   1. inactivityTimer — resets on every output chunk. If no output
	//      arrives for inactivityTimeout, the PTY is considered stuck and
	//      we Ctrl+C + close the session so a future command can start
	//      fresh. This distinguishes "command actively producing output"
	//      from "command (or PTY) hung", which the previous single hardcap
	//      conflated.
	//
	//   2. caller's ctx — the tool-level deadline (commonly 2 minutes for
	//      shell_command). DeadlineExceeded means "agent budget for this
	//      tool call is up, but the command may still be making progress".
	//      We promote the still-running command to a background session
	//      so the agent can poll its accumulated output via check_background.
	//
	// Previously a hardcoded 30 s sentinel timer ran independent of the
	// caller's deadline; commands streaming output for >30 s were killed
	// before the promotion branch ever became reachable.
	const inactivityTimeout = 30 * time.Second
	inactivityTimer := time.NewTimer(inactivityTimeout)
	defer inactivityTimer.Stop()
	resetInactivity := func() {
		if !inactivityTimer.Stop() {
			// Drain any pending tick so the next Reset starts clean.
			select {
			case <-inactivityTimer.C:
			default:
			}
		}
		inactivityTimer.Reset(inactivityTimeout)
	}

	// Wait for the sentinel line to appear in the output.
	for {
		select {
		case <-ctx.Done():
			callerErr := ctx.Err()
			if callerErr == context.DeadlineExceeded {
				// Tool executor's deadline expired but the command may still
				// be running.
				//
				// Try to promote to a background session. If the per-chat
				// cap is already at the limit, we can't add another — fall
				// through to the kill-and-close path with a clear error
				// the agent can act on (stop_background one of the existing
				// sessions, then retry).
				session.mutex.RLock()
				chatID := session.ChatID
				session.mutex.RUnlock()

				if tm.countBackgroundSessionsForChat(chatID) >= maxBackgroundSessionsPerChat {
					// At cap — kill the runaway command rather than silently
					// exceeding the limit. The error tells the agent how to
					// free a slot.
					session.mutex.RLock()
					if session.Pty != nil {
						_, _ = session.Pty.Write([]byte{3})
					}
					session.mutex.RUnlock()
					log.Printf("PTY session %s: tool deadline exceeded but background cap reached for chat %q, killing", session.ID, chatID)
					sid := session.ID
					closeSessionAfterGracePeriod(tm, sid, "cap hit")
					return stripANSI(buf.String()), -1, tm.errBackgroundCapReached(chatID)
				}

				// Mark the session IsBackground BEFORE returning so the
				// cleanup worker uses the 2-hour background timeout (not
				// the 30-min hidden one) — otherwise a long-running
				// promoted command would be silently killed by cleanup
				// within 30 minutes.
				session.mutex.Lock()
				session.IsBackground = true
				session.LastUsed = time.Now()
				session.mutex.Unlock()

				accumulatedOutput := stripANSI(buf.String())
				log.Printf("PTY session %s: tool deadline exceeded, promoting to background", session.ID)
				return accumulatedOutput, -1, fmt.Errorf("COMMAND_PROMOTED_TO_BACKGROUND:%s", session.ID)
			}
			// User interrupt — Ctrl+C and close the session for recreation.
			session.mutex.RLock()
			if session.Pty != nil {
				_, _ = session.Pty.Write([]byte{3})
			}
			session.mutex.RUnlock()
			log.Printf("PTY session %s: user cancelled, closing session for recreation", session.ID)
			sid := session.ID
			closeSessionAfterGracePeriod(tm, sid, "user cancel")
			return stripANSI(buf.String()), -1, callerErr

		case <-inactivityTimer.C:
			// No output for inactivityTimeout — assume the PTY or command
			// is stuck. Ctrl+C and close so the next command gets a fresh
			// session. (A command that's genuinely silent for >30 s but
			// still healthy is rare; the trade-off favors recovery over
			// patience.)
			session.mutex.RLock()
			if session.Pty != nil {
				_, _ = session.Pty.Write([]byte{3})
			}
			session.mutex.RUnlock()
			log.Printf("PTY session %s: no output for %s, closing as stuck", session.ID, inactivityTimeout)
			sid := session.ID
			closeSessionAfterGracePeriod(tm, sid, "stuck timeout")
			return stripANSI(buf.String()), -1, fmt.Errorf("PTY session %s stuck (no output for %s)", session.ID, inactivityTimeout)

		case chunk, ok := <-sub.ch:
			if !ok {
				// Channel closed — PTY session terminated unexpectedly.
				return stripANSI(buf.String()), -1, fmt.Errorf("PTY session terminated while waiting for command completion")
			}
			// Output is flowing — extend the inactivity grace.
			resetInactivity()
			buf.Write(chunk)

			// Only match when the full sentinel (with numeric exit code) appears.
			// The bytes-based detection will not match the PTY echo which contains the literal "$?".
			// We use bytes.Index to find the marker prefix, then verify the next char is a digit
			// (not '$' from the PTY echo which contains <marker>:$?).
			bufBytes := buf.Bytes()

			// Use bytes.Index for fast prefix search, then validate digit.
			idx := bytes.Index(bufBytes, markerStr)
			for idx >= 0 {
				afterPrefixStart := idx + len(markerStr)
				if afterPrefixStart < len(bufBytes) {
					nextChar := bufBytes[afterPrefixStart]
					if nextChar >= '0' && nextChar <= '9' {
						// Found actual sentinel output (not PTY echo). Parse exit code.
						// Note: We do NOT require the sentinel to be at line start because
						// shells (especially zsh) may emit OSC/title escape sequences right
						// before the sentinel without a newline separator. The 128-bit random
						// marker makes false positives astronomically unlikely regardless.
						var codeStr []byte
						for j := afterPrefixStart; j < len(bufBytes); j++ {
							b := bufBytes[j]
							if b >= '0' && b <= '9' {
								codeStr = append(codeStr, b)
							} else {
								break
							}
						}
						if len(codeStr) > 0 {
							afterDigitsStart := afterPrefixStart + len(codeStr)
							if afterDigitsStart < len(bufBytes) {
								afterDigits := bufBytes[afterDigitsStart:]
								lineEnd := bytes.IndexByte(afterDigits, '\n')
								if lineEnd >= 0 {
									code, err := strconv.Atoi(string(codeStr))
									if err != nil {
										return stripANSI(buf.String()), -1, fmt.Errorf("failed to parse exit code from sentinel: %w", err)
									}

									output := buf.String()[:idx]

									// Strip the command echo from the beginning of the output.
									// The PTY echoes the wrapped command we sent ("/bin/sh -c '...'").
									// We find the echo by looking for the "/bin/sh -c '" prefix and
									// stripping everything up to and including the first newline after it.
									echoPrefix := "/bin/sh -c '"
									echoIdx := strings.Index(output, echoPrefix)
									if echoIdx != -1 {
										restAfterEcho := output[echoIdx:]
										newlineIdx := newlineRegex.FindStringIndex(restAfterEcho)
										if newlineIdx != nil {
											output = output[echoIdx+newlineIdx[1]:]
										}
									}

									return stripANSI(output), code, nil
								}
							}
						}
					}
				}
				// Skip past this match and look for the next occurrence.
				remaining := bufBytes[idx+1:]
				nextIdx := bytes.Index(remaining, markerStr)
				if nextIdx >= 0 {
					idx = idx + 1 + nextIdx
				} else {
					idx = -1
				}
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
