//go:build !js

package webui

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// CloseSession terminates the shell process and removes the session from the manager.
// All active subscribers are notified via channel close before the session is deleted.
//
// The process Wait() runs OUTSIDE tm.mutex so a stuck or zombie shell (NFS hang,
// D-state process) cannot deadlock the entire TerminalManager. The session is
// removed from the map under tm.mutex first, so no new callers can observe it
// after the lock is released; the teardown then proceeds without holding the
// manager-level lock.
func (tm *TerminalManager) CloseSession(sessionID string) error {
	tm.mutex.Lock()
	session, exists := tm.sessions[sessionID]
	if !exists {
		tm.mutex.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	// Remove from the map immediately so no new caller can reach this session.
	// After we release tm.mutex, the session is effectively dead even though
	// we haven't finished waiting on the process yet.
	delete(tm.sessions, sessionID)
	tm.mutex.Unlock()

	// Ensure no ExecuteCommandAndWait call is in-flight. execMu is per-session,
	// so holding it here only blocks other callers of this exact session — it
	// does not block the manager.
	session.execMu.Lock()
	defer session.execMu.Unlock()

	// Signal all subscribers that the PTY is gone.
	session.closeAllSubs()

	// Cancel the shell process context and close the PTY file descriptor.
	// Closing the PTY unblocks the PTY reader goroutine (runPTYReader).
	session.mutex.Lock()
	session.Active = false
	if session.Cancel != nil {
		session.Cancel()
		session.Cancel = nil
	}
	if session.Pty != nil {
		session.Pty.Close()
		session.Pty = nil
	}
	cmd := session.Command
	session.mutex.Unlock()

	// Wait for the shell to exit OUTSIDE any manager/session lock. A hung
	// process here does not block CloseAllSessions, CreateSession, or the
	// cleanup worker — they only contend on tm.mutex, which we released above.
	if cmd != nil && cmd.Process != nil {
		if _, err := cmd.Process.Wait(); err != nil {
			webuiLogger.Error("terminal process wait failed", slog.String("session_id", sessionID), slog.Any("err", err))
		}
	}
	return nil
}

// DetachFromSession signals that the WebSocket has disconnected.
// The shell process keeps running; the subscriber goroutine (in websocket.go)
// handles unsubscription via its own defer, so this is a no-op but kept for
// API compatibility.
func (tm *TerminalManager) DetachFromSession(sessionID string) error {
	// No-op: shell persistence is handled by the subscriber pattern.
	// The output writer goroutine in handleTerminalWebSocket unsubscribes
	// via defer when the WebSocket goroutine exits.
	return nil
}

// CloseAllSessions closes all known terminal sessions and returns the first error encountered.
func (tm *TerminalManager) CloseAllSessions() error {
	sessionIDs := tm.ListAllSessions()
	var firstErr error
	for _, sessionID := range sessionIDs {
		if err := tm.CloseSession(sessionID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close session %s during CloseAllSessions: %w", sessionID, err)
		}
	}
	return firstErr
}

// ReattachSession returns the scrollback buffer of an existing session so the
// reconnecting WebSocket can replay it. The shell is already running; the caller
// subscribes to the session's live output stream immediately after this call.
func (tm *TerminalManager) ReattachSession(sessionID string) (string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return "", fmt.Errorf("session %s does not exist", sessionID)
	}

	session.mutex.Lock()
	if !session.Active {
		session.mutex.Unlock()
		return "", fmt.Errorf("session %s is no longer active", sessionID)
	}
	if session.Hidden {
		session.mutex.Unlock()
		// Intentionally does not distinguish between "does not exist" and "is hidden"
		// to prevent information leakage about agent-owned sessions.
		return "", fmt.Errorf("session %s is not accessible", sessionID)
	}
	session.LastUsed = time.Now()
	session.mutex.Unlock()

	scrollback := string(session.ring.snapshot())
	webuiLogger.Info("terminal session reattached", slog.String("session_id", sessionID), slog.Int("scrollback_bytes", len(scrollback)))
	return scrollback, nil
}

// CleanupInactiveSessions removes sessions that have been inactive for too long.
// Background sessions (IsBackground=true) use a separate timeout (default 2 hours)
// vs regular hidden sessions (30 minutes).
func (tm *TerminalManager) CleanupInactiveSessions(timeout time.Duration, backgroundTimeout ...time.Duration) {
	bgTimeout := 2 * time.Hour // default 2 hours for background sessions
	if len(backgroundTimeout) > 0 {
		bgTimeout = backgroundTimeout[0]
	}

	var toClose []string

	tm.mutex.RLock()
	now := time.Now()
	for sessionID, session := range tm.sessions {
		session.mutex.RLock()
		sessionTimeout := timeout
		if session.IsBackground {
			sessionTimeout = bgTimeout
		}
		inactive := now.Sub(session.LastUsed) > sessionTimeout
		session.mutex.RUnlock()

		if inactive {
			toClose = append(toClose, sessionID)
		}
	}
	tm.mutex.RUnlock()

	for _, sessionID := range toClose {
		webuiLogger.Info("cleaning up inactive terminal session", slog.String("session_id", sessionID))
		if err := tm.CloseSession(sessionID); err != nil {
			webuiLogger.Error("inactive terminal session cleanup failed", slog.String("session_id", sessionID), slog.Any("err", err))
		}
	}
}

// StartCleanupWorker starts a background worker to clean up inactive sessions.
// Background sessions get a separate timeout (default 2 hours) vs regular sessions (timeout).
// Safe to call multiple times — only one worker goroutine is started per TerminalManager.
func (tm *TerminalManager) StartCleanupWorker(ctx context.Context, interval time.Duration, timeout time.Duration, backgroundTimeout ...time.Duration) {
	tm.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					tm.CleanupInactiveSessions(timeout, backgroundTimeout...)
				}
			}
		}()
	})
}
