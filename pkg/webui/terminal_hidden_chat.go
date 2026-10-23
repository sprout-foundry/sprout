//go:build !js

package webui

// terminal_hidden_chat.go — the hidden-session-per-chat layer:
// GetOrCreateHiddenSessionForChat (one hidden session per chat, reused
// across agent execs), waitForShellReady, and sanitizeChatID. Split out of
// terminal_types.go.
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
// It subscribes to the session's output and waits for a quiet period after
// the last output chunk, indicating the shell prompt is ready.
// Returns nil if the shell becomes ready, or an error if the context expires.
func (tm *TerminalManager) waitForShellReady(ctx context.Context, session *TerminalSession) error {
	sub := session.subscribe()
	defer session.unsubscribe(sub)

	// Wait up to 15 seconds for shell readiness. Most shells source rc files
	// in under 1 second, but complex setups (pyenv, nvm, conda, starship,
	// tmux) can produce output in bursts. A round-trip probe (below) is
	// immune to burstiness, so the timeout only bounds a genuinely stuck
	// shell.
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	session.mutex.RLock()
	pty := session.Pty
	session.mutex.RUnlock()
	if pty == nil {
		return fmt.Errorf("session %s has no PTY", session.ID)
	}

	// Probe readiness by round-tripping a unique token: write a command that
	// prints it, and wait for the printed line. This replaces a quiet-period
	// heuristic, which could not distinguish a shell that finished starting
	// from one that was merely between rc-file bursts — declaring ready too
	// early let the first command interleave with pending output, and a
	// bursty shell could reset the quiet timer indefinitely. A round-trip is
	// a direct proof that the shell is consuming input.
	token := fmt.Sprintf("SPROUT-READY-%d", time.Now().UnixNano())
	if _, err := fmt.Fprintf(pty, "printf '%s\\n'\n", token); err != nil {
		return fmt.Errorf("writing readiness probe to session %s: %w", session.ID, err)
	}

	// Match the token as the whole of a line's payload, ignoring any prompt
	// prefix the shell prints before it: dash prints "$ <token>", zsh prints
	// "% <token>", bash "$ <token>". The echoed command line contains the
	// token too, but always as part of the printf invocation, so requiring
	// the token to end the line (modulo trailing prompt/whitespace) excludes
	// the echo.
	want := []byte(token)

	// Only the tail of the startup output can hold the probe's result, and a
	// chatty rc file could otherwise grow this buffer for the whole timeout.
	const retainedTail = 8 * 1024
	var seen []byte
	for {
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("shell did not answer the readiness probe within timeout for session %s", session.ID)

		case chunk, ok := <-sub.ch:
			if !ok {
				return fmt.Errorf("PTY channel closed before shell became ready for session %s", session.ID)
			}
			seen = append(seen, chunk...)
			if len(seen) > retainedTail {
				seen = seen[len(seen)-retainedTail:]
			}
			if probeAnswered(seen, want) {
				return nil
			}
		}
	}
}

// probeAnswered reports whether the readiness token appears as a line's own
// payload. The shell echoes the command that printed it, so the token also
// occurs inside the printf invocation; only a line whose content ends with the
// token (after any prompt prefix) is the command's output.
func probeAnswered(seen, token []byte) bool {
	for i := 0; i+len(token) <= len(seen); i++ {
		if !bytes.Equal(seen[i:i+len(token)], token) {
			continue
		}
		// The token must end the line. In the echoed command line it is
		// followed by the printf string's \n escape, so that occurrence is
		// rejected here; the printed line ends the token with a line break
		// (or the buffer ends mid-line while it is still arriving).
		rest := seen[i+len(token):]
		if len(rest) > 0 && rest[0] != '\r' && rest[0] != '\n' {
			continue
		}
		// It must also start its own line, after at most a prompt: the echo
		// has the printf string's opening quote immediately before the token.
		// Without this, an echo truncated right after the token would look
		// like the printed line and declare the shell ready mid-startup.
		if i > 0 && seen[i-1] == '\'' {
			continue
		}
		return true
	}
	return false
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
