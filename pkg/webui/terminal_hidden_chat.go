//go:build !js

package webui

// terminal_hidden_chat.go — the hidden-session-per-chat layer:
// GetOrCreateHiddenSessionForChat (one hidden session per chat, reused
// across agent execs), waitForShellReady, and sanitizeChatID. Split out of
// terminal_types.go.
import (
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
	// tmux) can produce output in bursts separated by >500ms gaps. The larger
	// timeout accommodates these without false-positive "ready" declarations.
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// The quiet period must be long enough to span the typical gap between
	// rc-file output bursts (pyenv, nvm, conda each print asynchronously).
	// 500ms was too short — a bursty shell would be declared ready mid-init,
	// and the first command would interleave with pending output. 1s is still
	// short enough that the total readiness wait stays under 2s for fast shells.
	quietPeriod := 1 * time.Second
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
