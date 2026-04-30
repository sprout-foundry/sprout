package webui

import (
	"context"
	"fmt"
	"log"
	"time"
)

// CloseSession terminates the shell process and removes the session from the manager.
// All active subscribers are notified via channel close before the session is deleted.
func (tm *TerminalManager) CloseSession(sessionID string) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	session, exists := tm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Signal all subscribers that the PTY is gone.
	session.closeAllSubs()

	// Cancel the shell process context.
	session.mutex.Lock()
	session.Active = false
	if session.Cancel != nil {
		session.Cancel()
		session.Cancel = nil
	}
	// Close the PTY file to unblock the PTY reader goroutine.
	if session.Pty != nil {
		session.Pty.Close()
		session.Pty = nil
	}
	// Wait for the shell to exit.
	if session.Command != nil && session.Command.Process != nil {
		if _, err := session.Command.Process.Wait(); err != nil {
			log.Printf("Terminal %s: process wait: %v", sessionID, err)
		}
		session.Command = nil
	}
	session.mutex.Unlock()

	delete(tm.sessions, sessionID)
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
		return "", fmt.Errorf("session %s does not exist", sessionID)
	}
	session.LastUsed = time.Now()
	session.mutex.Unlock()

	scrollback := string(session.ring.snapshot())
	log.Printf("TerminalManager: reattached to session %s (scrollback: %d bytes)", sessionID, len(scrollback))
	return scrollback, nil
}

// CleanupInactiveSessions removes sessions that have been inactive for too long.
func (tm *TerminalManager) CleanupInactiveSessions(timeout time.Duration) {
	var toClose []string

	tm.mutex.RLock()
	now := time.Now()
	for sessionID, session := range tm.sessions {
		session.mutex.RLock()
		inactive := now.Sub(session.LastUsed) > timeout
		session.mutex.RUnlock()
		if inactive {
			toClose = append(toClose, sessionID)
		}
	}
	tm.mutex.RUnlock()

	for _, sessionID := range toClose {
		log.Printf("Cleaning up inactive terminal session: %s", sessionID)
		if err := tm.CloseSession(sessionID); err != nil {
			log.Printf("CleanupInactiveSessions: failed to close %s: %v", sessionID, err)
		}
	}
}

// StartCleanupWorker starts a background worker to clean up inactive sessions.
func (tm *TerminalManager) StartCleanupWorker(ctx context.Context, interval time.Duration, timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tm.CleanupInactiveSessions(timeout)
			}
		}
	}()
}
